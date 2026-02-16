package meta

import (
	"bytes"
	"math/rand"
	"regexp"
	"testing"
)

// dnaPatterns defines the 9 patterns from The Computer Language Benchmarks Game (regexdna).
// These patterns search for specific nucleotide sequences in DNA data,
// using alternations and character classes to match both forward and
// reverse complement strands.
var dnaPatterns = []struct {
	name    string
	pattern string
}{
	{"dna_1", `agggtaaa|tttaccct`},
	{"dna_2", `[cgt]gggtaaa|tttaccc[acg]`},
	{"dna_3", `a[act]ggtaaa|tttacc[agt]t`},
	{"dna_4", `ag[act]gtaaa|tttac[agt]ct`},
	{"dna_5", `agg[act]taaa|ttta[agt]cct`},
	{"dna_6", `aggg[acg]aaa|ttt[cgt]ccct`},
	{"dna_7", `agggt[cgt]aa|tt[acg]accct`},
	{"dna_8", `agggta[cgt]a|t[acg]taccct`},
	{"dna_9", `agggtaa[cgt]|[acg]ttaccct`},
}

// generateDNA generates a deterministic DNA sequence of given size
// using IUB homo sapiens nucleotide frequencies.
// Seed is fixed at 42 for reproducibility across test runs.
//
// Frequencies (from regexdna benchmark):
//
//	a=0.3029549427, c=0.1979883005, g=0.1975473066, t=0.3015094502
func generateDNA(size int) []byte {
	const (
		freqA  = 0.3029549427
		freqAC = freqA + 0.1979883005
		freqACG = freqAC + 0.1975473066
	)

	rng := rand.New(rand.NewSource(42))
	data := make([]byte, size)
	for i := range data {
		r := rng.Float64()
		switch {
		case r < freqA:
			data[i] = 'a'
		case r < freqAC:
			data[i] = 'c'
		case r < freqACG:
			data[i] = 'g'
		default:
			data[i] = 't'
		}
	}
	return data
}

// TestDNAPatternCorrectness verifies that coregex matches Go stdlib regexp
// for all 9 DNA patterns across multiple input sizes.
// This is the primary correctness regression test for DNA/regexdna workloads.
func TestDNAPatternCorrectness(t *testing.T) {
	sizes := []struct {
		name string
		size int
	}{
		{"1KB", 1024},
		{"64KB", 64 * 1024},
		{"1MB", 1024 * 1024},
	}

	for _, sz := range sizes {
		data := generateDNA(sz.size)

		for _, p := range dnaPatterns {
			t.Run(sz.name+"/"+p.name, func(t *testing.T) {
				// Compile with coregex
				engine, err := Compile(p.pattern)
				if err != nil {
					t.Fatalf("Compile(%q) failed: %v", p.pattern, err)
				}

				// Compile with stdlib
				reStd := regexp.MustCompile(p.pattern)

				// Get all matches from both engines
				cgxIndices := engine.FindAllIndicesStreaming(data, -1, nil)
				stdIndices := reStd.FindAllIndex(data, -1)

				// Compare match count
				if len(cgxIndices) != len(stdIndices) {
					t.Errorf("match count mismatch: coregex=%d, stdlib=%d",
						len(cgxIndices), len(stdIndices))
					// Log first few mismatches for debugging
					maxLog := 5
					if maxLog > len(cgxIndices) {
						maxLog = len(cgxIndices)
					}
					for i := 0; i < maxLog; i++ {
						t.Logf("  coregex[%d]: [%d,%d] = %q",
							i, cgxIndices[i][0], cgxIndices[i][1],
							data[cgxIndices[i][0]:cgxIndices[i][1]])
					}
					maxLog = 5
					if maxLog > len(stdIndices) {
						maxLog = len(stdIndices)
					}
					for i := 0; i < maxLog; i++ {
						t.Logf("  stdlib[%d]: [%d,%d] = %q",
							i, stdIndices[i][0], stdIndices[i][1],
							data[stdIndices[i][0]:stdIndices[i][1]])
					}
					return
				}

				// Compare actual match positions
				for i := range stdIndices {
					stdStart, stdEnd := stdIndices[i][0], stdIndices[i][1]
					cgxStart, cgxEnd := cgxIndices[i][0], cgxIndices[i][1]

					if stdStart != cgxStart || stdEnd != cgxEnd {
						t.Errorf("match %d position mismatch: coregex=[%d,%d], stdlib=[%d,%d]",
							i, cgxStart, cgxEnd, stdStart, stdEnd)
						t.Errorf("  coregex match: %q", data[cgxStart:cgxEnd])
						t.Errorf("  stdlib match:  %q", data[stdStart:stdEnd])
						break // Stop at first mismatch to avoid flooding output
					}
				}

				t.Logf("pattern=%s matches=%d strategy=%s",
					p.pattern, len(cgxIndices), engine.Strategy())
			})
		}
	}
}

// TestDNAPatternFindAll verifies that FindAll results match stdlib exactly
// on 64KB DNA input. This tests both match positions and match content,
// ensuring byte-for-byte equivalence with Go's standard regexp.
func TestDNAPatternFindAll(t *testing.T) {
	data := generateDNA(64 * 1024)

	for _, p := range dnaPatterns {
		t.Run(p.name, func(t *testing.T) {
			engine, err := Compile(p.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) failed: %v", p.pattern, err)
			}

			reStd := regexp.MustCompile(p.pattern)

			// Get all matches
			cgxIndices := engine.FindAllIndicesStreaming(data, -1, nil)
			stdMatches := reStd.FindAll(data, -1)

			if len(cgxIndices) != len(stdMatches) {
				t.Fatalf("match count mismatch: coregex=%d, stdlib=%d",
					len(cgxIndices), len(stdMatches))
			}

			// Compare match content byte-for-byte
			for i, stdMatch := range stdMatches {
				cgxMatch := data[cgxIndices[i][0]:cgxIndices[i][1]]
				if !bytes.Equal(cgxMatch, stdMatch) {
					t.Errorf("match %d content mismatch: coregex=%q, stdlib=%q",
						i, cgxMatch, stdMatch)
				}
			}
		})
	}
}

// TestDNAPatternStrategies logs the strategy selected for each DNA pattern.
// This is informational (not assertive) because strategies may change
// as the engine is optimized. Use this to track strategy evolution.
func TestDNAPatternStrategies(t *testing.T) {
	for _, p := range dnaPatterns {
		t.Run(p.name, func(t *testing.T) {
			engine, err := Compile(p.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) failed: %v", p.pattern, err)
			}

			strategy := engine.Strategy()
			nfaStates := engine.nfa.States()

			t.Logf("pattern=%-35s strategy=%-20s nfa_states=%d",
				p.pattern, strategy, nfaStates)
		})
	}
}
