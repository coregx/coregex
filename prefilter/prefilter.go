// Package prefilter provides fast candidate filtering for regex search using
// extracted literal sequences.
//
// A prefilter is used to quickly reject positions in the haystack that cannot
// possibly match the full regex pattern. This provides dramatic speedup (10-100x)
// for patterns with literals, as we can use SIMD-accelerated search primitives
// instead of running the full automaton.
//
// The package automatically selects the optimal prefilter strategy based on
// extracted literals:
//   - Single byte → MemchrPrefilter (SIMD byte search)
//   - Single substring → MemmemPrefilter (SIMD substring search)
//   - 2-8 literals (len ≥ 3) → TeddyPrefilter (SIMD multi-literal)
//   - Many literals → AhoCorasickPrefilter (automaton, future)
//
// Example usage:
//
//	// Extract literals from regex pattern
//	re, _ := syntax.Parse("(hello|world)", syntax.Perl)
//	extractor := literal.New(literal.DefaultConfig())
//	prefixes := extractor.ExtractPrefixes(re)
//
//	// Build prefilter
//	builder := prefilter.NewBuilder(prefixes, nil)
//	pf := builder.Build()
//
//	// Use prefilter to find candidates
//	haystack := []byte("foo hello bar world baz")
//	pos := pf.Find(haystack, 0)
//	// pos == 4 (position of "hello")
package prefilter

import (
	"github.com/coregx/coregex/literal"
	"github.com/coregx/coregex/simd"
)

// Prefilter is used to quickly find candidate match positions before running
// the full regex engine.
//
// The prefilter scans the haystack for literals extracted from the regex pattern.
// When a literal is found, that position is returned as a candidate. The regex
// engine then verifies if a full match exists at that position.
//
// Key methods:
//   - Find: returns the next candidate position
//   - IsComplete: indicates if prefilter match is sufficient (no verification needed)
//   - HeapBytes: returns memory usage for profiling
type Prefilter interface {
	// Find returns the index of the first candidate match starting at or after
	// 'start', or -1 if no candidate is found.
	//
	// A candidate match means a position where one of the prefilter literals
	// was found. This does NOT guarantee a full regex match - the caller must
	// verify the match using the full regex engine (unless IsComplete() is true).
	//
	// Parameters:
	//   haystack - the byte buffer to search
	//   start - the starting position (must be >= 0 and <= len(haystack))
	//
	// Returns:
	//   index >= start if a candidate is found
	//   -1 if no candidate exists at or after start
	//
	// Example:
	//
	//	pf := /* some prefilter */
	//	pos := pf.Find(haystack, 0)
	//	for pos != -1 {
	//	    // Verify match at pos using full regex engine
	//	    if fullRegexMatches(haystack, pos) {
	//	        return pos
	//	    }
	//	    // Continue searching
	//	    pos = pf.Find(haystack, pos+1)
	//	}
	Find(haystack []byte, start int) int

	// IsComplete returns true if a prefilter match guarantees a full regex match.
	//
	// When true, the regex engine can skip verification and directly return the
	// prefilter result. This is the case when:
	//   - The regex is an exact literal (e.g., /hello/)
	//   - The literal sequence is complete and non-overlapping
	//
	// Most prefilters return false, meaning verification is required.
	//
	// Example:
	//
	//	if pf.IsComplete() {
	//	    // Direct return, no verification needed
	//	    return pf.Find(haystack, start)
	//	} else {
	//	    // Must verify with full regex engine
	//	    pos := pf.Find(haystack, start)
	//	    if pos != -1 && fullRegexMatches(haystack, pos) {
	//	        return pos
	//	    }
	//	}
	IsComplete() bool

	// LiteralLen returns the length of the matched literal when IsComplete() is true.
	//
	// This allows the regex engine to calculate exact match bounds without
	// running the full automata: end = start + LiteralLen().
	//
	// Returns:
	//   > 0 if IsComplete() is true (the length of the complete literal)
	//   0 if IsComplete() is false or if the prefilter matches variable lengths
	//
	// Example:
	//
	//	if pf.IsComplete() {
	//	    pos := pf.Find(haystack, start)
	//	    if pos != -1 {
	//	        matchEnd := pos + pf.LiteralLen()
	//	        return haystack[pos:matchEnd]
	//	    }
	//	}
	LiteralLen() int

	// HeapBytes returns the number of bytes of heap memory used by this prefilter.
	//
	// This is useful for profiling and memory budgeting in the regex engine.
	// Simple prefilters (Memchr, Memmem) typically return 0 as they don't
	// allocate heap memory. Complex prefilters (Teddy, AhoCorasick) may use
	// significant memory for lookup tables.
	//
	// Returns:
	//   0 if no heap allocation
	//   positive value indicating heap bytes used
	HeapBytes() int
}

// MatchFinder is an optional interface for prefilters that can return
// the matched range directly, avoiding the need for NFA verification.
//
// This is particularly useful for multi-pattern prefilters like Teddy
// where the matched pattern length varies.
type MatchFinder interface {
	// FindMatch returns the start and end positions of the first match.
	// Returns (start, end) if found, (-1, -1) if not found.
	// The matched bytes are haystack[start:end].
	FindMatch(haystack []byte, start int) (start2, end int)
}

// Builder constructs the optimal prefilter from extracted literals.
//
// The builder analyzes the literal sequences (prefixes and suffixes) and
// selects the most efficient prefilter strategy. The selection is based on:
//   - Number of literals
//   - Length of literals
//   - Completeness flag
//
// Selection strategy (in order of preference):
//  1. Single byte literal → MemchrPrefilter (fastest)
//  2. Single substring literal → MemmemPrefilter (very fast)
//  3. 2-8 literals, len≥3 → TeddyPrefilter (SIMD multi-pattern)
//  4. Many literals → AhoCorasickPrefilter (automaton, future)
//  5. No suitable literals → nil (no prefilter)
//
// Example:
//
//	// Build from extracted prefixes
//	builder := prefilter.NewBuilder(prefixes, nil)
//	pf := builder.Build()
//	if pf != nil {
//	    pos := pf.Find(haystack, 0)
//	}
type Builder struct {
	prefixes *literal.Seq
	suffixes *literal.Seq
}

// NewBuilder creates a new prefilter builder from extracted literal sequences.
//
// Parameters:
//
//	prefixes - literals that must appear at the start of matches (from ExtractPrefixes)
//	suffixes - literals that must appear at the end of matches (from ExtractSuffixes)
//
// The builder prefers prefixes over suffixes because forward search is more
// natural and cache-friendly. Suffixes are only used if prefixes are empty.
//
// Either or both can be nil, indicating no literals of that type were extracted.
//
// Example:
//
//	extractor := literal.New(literal.DefaultConfig())
//	prefixes := extractor.ExtractPrefixes(re)
//	suffixes := extractor.ExtractSuffixes(re)
//	builder := prefilter.NewBuilder(prefixes, suffixes)
func NewBuilder(prefixes, suffixes *literal.Seq) *Builder {
	return &Builder{
		prefixes: prefixes,
		suffixes: suffixes,
	}
}

// Build constructs the best prefilter for the given literals.
//
// Returns nil if no effective prefilter can be built (e.g., no literals,
// or literals are too complex for available strategies).
//
// The selection logic:
//  1. Prefer prefixes over suffixes (forward search)
//  2. Single byte → Memchr (10-15x speedup)
//  3. Single substring → Memmem (5-10x speedup)
//  4. Multiple literals (2-8, len≥3) → Teddy (20-50x speedup)
//  5. Many literals → (future) AhoCorasick
//  6. Otherwise → nil
//
// Example:
//
//	builder := prefilter.NewBuilder(prefixes, nil)
//	pf := builder.Build()
//	if pf == nil {
//	    // No prefilter available, use full regex engine
//	    return fullRegexSearch(haystack, pattern)
//	}
//	// Use prefilter for fast candidate finding
//	pos := pf.Find(haystack, 0)
func (b *Builder) Build() Prefilter {
	return selectPrefilter(b.prefixes, b.suffixes)
}

// selectPrefilter chooses the best prefilter strategy based on literal sequences.
//
// Selection algorithm:
//  1. Choose sequence: prefer prefixes, fallback to suffixes
//  2. If no literals → return nil
//  3. If 1 literal:
//     - len==1 → MemchrPrefilter (single byte search)
//     - len>1 → MemmemPrefilter (substring search)
//  4. If 2-8 literals and minLen≥3 → TeddyPrefilter (SIMD multi-pattern)
//  5. If many/short literals → nil (Aho-Corasick handled via meta.UseAhoCorasick strategy)
//
// Returns nil if no effective prefilter can be constructed.
func selectPrefilter(prefixes, suffixes *literal.Seq) Prefilter {
	// Prefer prefixes over suffixes (forward search is natural)
	seq := prefixes
	if seq.IsEmpty() {
		seq = suffixes
	}
	if seq.IsEmpty() {
		return nil // No literals → no prefilter
	}

	// Single literal optimizations
	if seq.Len() == 1 {
		lit := seq.Get(0)

		// Single byte → Memchr (fastest possible prefilter)
		if len(lit.Bytes) == 1 {
			return newMemchrPrefilter(lit.Bytes[0], lit.Complete)
		}

		// Single substring → Memmem (very fast)
		return newMemmemPrefilter(lit.Bytes, lit.Complete)
	}

	// Multiple literals: use Teddy if suitable
	if seq.Len() >= 2 && seq.Len() <= 8 && minLen(seq) >= 3 {
		// Teddy is effective for 2-8 literals of length >= 3
		// Provides 20-50x speedup using SSSE3 SIMD instructions
		return newTeddy(seq)
	}

	// Many literals or short literals - return nil
	// Aho-Corasick is handled at strategy level (meta.UseAhoCorasick)
	// for >8 literal alternations, not as a prefilter
	return nil
}

// minLen returns the minimum literal length in the sequence.
// Returns max int if sequence is empty.
func minLen(seq *literal.Seq) int {
	if seq.IsEmpty() {
		return int(^uint(0) >> 1) // Max int
	}

	minLength := int(^uint(0) >> 1) // Max int
	for i := 0; i < seq.Len(); i++ {
		if l := len(seq.Get(i).Bytes); l < minLength {
			minLength = l
		}
	}
	return minLength
}

// memchrPrefilter wraps simd.Memchr as a Prefilter.
//
// This is the fastest prefilter for patterns with a single byte literal.
// Uses SIMD (AVX2/SSE4.2) to scan 32 bytes per iteration on x86-64.
//
// Performance: 10-15x faster than full regex on large inputs.
//
// Example patterns:
//
//	/a.*/         → search for 'a'
//	/foo|bar/     → extract 'f' or 'b', search for first
//	/[abc]/       → if expanded to single byte
type memchrPrefilter struct {
	needle   byte
	complete bool
}

// newMemchrPrefilter creates a new Memchr-based prefilter.
//
// Parameters:
//
//	needle - the byte to search for
//	complete - true if finding this byte is sufficient for a match
func newMemchrPrefilter(needle byte, complete bool) Prefilter {
	return &memchrPrefilter{
		needle:   needle,
		complete: complete,
	}
}

// Find implements Prefilter.Find using simd.Memchr.
func (p *memchrPrefilter) Find(haystack []byte, start int) int {
	// Bounds check
	if start < 0 || start >= len(haystack) {
		return -1
	}

	// Search for needle starting at 'start'
	idx := simd.Memchr(haystack[start:], p.needle)
	if idx == -1 {
		return -1
	}

	// Return absolute position in haystack
	return start + idx
}

// IsComplete implements Prefilter.IsComplete.
func (p *memchrPrefilter) IsComplete() bool {
	return p.complete
}

// LiteralLen implements Prefilter.LiteralLen.
func (p *memchrPrefilter) LiteralLen() int {
	if p.complete {
		return 1 // single byte literal
	}
	return 0
}

// HeapBytes implements Prefilter.HeapBytes.
// Returns 0 as no heap allocation is needed.
func (p *memchrPrefilter) HeapBytes() int {
	return 0 // No heap allocation
}

// memmemPrefilter wraps simd.Memmem as a Prefilter.
//
// This prefilter searches for a single substring literal using SIMD-accelerated
// substring search. Uses rare byte heuristic + memchr for fast candidate finding.
//
// Performance: 5-10x faster than full regex on large inputs.
//
// Example patterns:
//
//	/hello/       → search for "hello"
//	/foo|foobar/  → after minimization → search for "foo"
//	/prefix.*/    → search for "prefix"
type memmemPrefilter struct {
	needle   []byte
	complete bool
}

// newMemmemPrefilter creates a new Memmem-based prefilter.
//
// Parameters:
//
//	needle - the byte sequence to search for (must be len > 1)
//	complete - true if finding this substring is sufficient for a match
//
// The needle slice is copied to prevent aliasing issues.
func newMemmemPrefilter(needle []byte, complete bool) Prefilter {
	// Copy needle to prevent aliasing
	needleCopy := make([]byte, len(needle))
	copy(needleCopy, needle)

	return &memmemPrefilter{
		needle:   needleCopy,
		complete: complete,
	}
}

// Find implements Prefilter.Find using simd.Memmem.
func (p *memmemPrefilter) Find(haystack []byte, start int) int {
	// Bounds check
	if start < 0 || start >= len(haystack) {
		return -1
	}

	// Search for needle starting at 'start'
	idx := simd.Memmem(haystack[start:], p.needle)
	if idx == -1 {
		return -1
	}

	// Return absolute position in haystack
	return start + idx
}

// IsComplete implements Prefilter.IsComplete.
func (p *memmemPrefilter) IsComplete() bool {
	return p.complete
}

// LiteralLen implements Prefilter.LiteralLen.
func (p *memmemPrefilter) LiteralLen() int {
	if p.complete {
		return len(p.needle)
	}
	return 0
}

// HeapBytes implements Prefilter.HeapBytes.
// Returns the size of the needle buffer (stored on heap).
func (p *memmemPrefilter) HeapBytes() int {
	return len(p.needle)
}
