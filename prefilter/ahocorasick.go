package prefilter

import (
	"github.com/coregx/ahocorasick"
	"github.com/coregx/coregex/literal"
)

// AhoCorasickPrefilter wraps an Aho-Corasick DFA automaton as a Prefilter.
//
// Used for patterns with many literals (>32) where Teddy's fingerprint
// collision rate becomes too high. AC provides zero false positives via
// deterministic state machine traversal.
//
// Performance characteristics:
//   - O(n) scan with flat transition table (DFA backend)
//   - Zero false positives (exact multi-pattern match)
//   - No SIMD, but no Go/ASM round-trip overhead either
//   - 100-130x faster than FatTeddy for 48+ short (3-byte) patterns
type AhoCorasickPrefilter struct {
	ac       *ahocorasick.Automaton
	complete bool
	minLen   int
}

// newACPrefilter builds an Aho-Corasick prefilter from a literal sequence.
// Returns nil if construction fails.
func newACPrefilter(seq *literal.Seq) Prefilter {
	patterns := make([][]byte, seq.Len())
	minLen := int(^uint(0) >> 1) // MaxInt
	for i := 0; i < seq.Len(); i++ {
		patterns[i] = seq.Get(i).Bytes
		if len(patterns[i]) < minLen {
			minLen = len(patterns[i])
		}
	}

	ac, err := ahocorasick.NewBuilder().
		AddPatterns(patterns).
		SetPrefilter(false). // Disable start-byte skip — degrades to O(n²) when start bytes are common
		Build()
	if err != nil {
		return nil
	}

	return &AhoCorasickPrefilter{
		ac:       ac,
		complete: seq.AllComplete(),
		minLen:   minLen,
	}
}

// Find returns the position of the first matching literal at or after start.
func (p *AhoCorasickPrefilter) Find(haystack []byte, start int) int {
	if start < 0 || start >= len(haystack) {
		return -1
	}
	m := p.ac.Find(haystack, start)
	if m == nil {
		return -1
	}
	return m.Start
}

// IsComplete returns true if prefilter match guarantees a full regex match.
func (p *AhoCorasickPrefilter) IsComplete() bool {
	return p.complete
}

// LiteralLen returns 0 (AC matches variable-length patterns).
func (p *AhoCorasickPrefilter) LiteralLen() int {
	return 0
}

// HeapBytes returns approximate heap memory used by the AC automaton.
func (p *AhoCorasickPrefilter) HeapBytes() int {
	// Approximate: states * 256 bytes for DFA transition table
	return p.ac.StateCount() * 256
}

// IsFast returns true. AC DFA scan is O(n) with no false positives,
// making it faster than running the regex engine for multi-literal patterns.
func (p *AhoCorasickPrefilter) IsFast() bool {
	return p.minLen >= 2
}
