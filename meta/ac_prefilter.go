package meta

import (
	"github.com/coregx/ahocorasick"
	"github.com/coregx/coregex/prefilter"
)

// acPrefilter wraps an Aho-Corasick automaton as a Prefilter.
// Used when the pattern produces >32 case-fold prefix literals (beyond
// SlimTeddy capacity) and FatTeddy has known bugs with certain positions.
// Aho-Corasick provides correct O(n) multi-pattern matching without SIMD
// edge cases, at ~1.6 GB/s throughput.
//
// Reference: Issue #137 (case-insensitive literal extraction)
type acPrefilter struct {
	auto *ahocorasick.Automaton
}

// Find returns the position of the first matching prefix literal at or after start.
func (p *acPrefilter) Find(haystack []byte, start int) int {
	if start < 0 || start >= len(haystack) {
		return -1
	}
	m := p.auto.Find(haystack, start)
	if m == nil {
		return -1
	}
	return m.Start
}

// IsComplete returns false because prefix literals require regex verification.
func (p *acPrefilter) IsComplete() bool {
	return false
}

// LiteralLen returns 0 because the prefilter is not complete.
func (p *acPrefilter) LiteralLen() int {
	return 0
}

// HeapBytes returns the heap memory used by the Aho-Corasick automaton.
func (p *acPrefilter) HeapBytes() int {
	return 0 // Aho-Corasick doesn't expose heap size
}

// IsFast returns true because Aho-Corasick with dense transitions is fast enough
// to serve as a prefilter (~1.6 GB/s throughput).
func (p *acPrefilter) IsFast() bool {
	return true
}

// buildACPrefilter creates an Aho-Corasick prefilter from the given prefilter
// when it's a FatTeddy (>32 patterns). Returns the original prefilter if it's
// not FatTeddy or if AC construction fails.
func buildACPrefilter(pf prefilter.Prefilter) prefilter.Prefilter {
	fatTeddy, ok := pf.(*prefilter.FatTeddy)
	if !ok {
		return pf
	}

	// Extract patterns from FatTeddy and build Aho-Corasick
	patterns := fatTeddy.Patterns()
	if len(patterns) == 0 {
		return pf
	}

	builder := ahocorasick.NewBuilder()
	for _, pattern := range patterns {
		builder.AddPattern(pattern)
	}
	auto, err := builder.Build()
	if err != nil {
		return pf // Fallback to FatTeddy on error
	}

	return &acPrefilter{auto: auto}
}
