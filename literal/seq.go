// Package literal provides types and operations for representing and manipulating
// literal byte sequences extracted from regex patterns.
//
// The primary use case is for prefilter optimization in regex engines: by extracting
// literal strings from patterns (e.g., "hello" from /hello.*world/), we can quickly
// filter out non-matching text before running the full regex automaton.
//
// Key concepts:
//   - A Literal is a concrete byte sequence that may appear in matches
//   - A Seq is a set of alternative literals (e.g., from alternations like /foo|bar/)
//   - Operations like Minimize, LCP, LCS help optimize prefilter strategies
package literal

import (
	"bytes"
	"sort"
)

// Literal represents a literal byte sequence extracted from a regex pattern.
// The Complete flag indicates whether this literal represents a complete match
// (true) or just a prefix/substring of potential matches (false).
//
// Example:
//   - Pattern /hello/ → Literal{[]byte("hello"), true}
//   - Pattern /hello.*world/ → Literal{[]byte("hello"), false} (prefix only)
//   - Pattern /.*world/ → Literal{[]byte("world"), false} (suffix, but here treated as complete=false)
type Literal struct {
	// Bytes contains the actual literal byte sequence.
	Bytes []byte

	// Complete indicates whether this literal represents the entire match.
	// If true, matching this literal is sufficient (no regex engine needed).
	// If false, this literal is just a necessary prefix/substring.
	Complete bool
}

// NewLiteral creates a new Literal from the given byte sequence and completeness flag.
//
// Example:
//
//	lit := literal.NewLiteral([]byte("hello"), true)
//	fmt.Printf("%s (complete=%v)\n", lit.Bytes, lit.Complete)
//	// Output: hello (complete=true)
func NewLiteral(b []byte, complete bool) Literal {
	return Literal{
		Bytes:    b,
		Complete: complete,
	}
}

// Len returns the length of the literal in bytes.
//
// Example:
//
//	lit := literal.NewLiteral([]byte("hello"), true)
//	fmt.Println(lit.Len()) // Output: 5
func (l Literal) Len() int {
	return len(l.Bytes)
}

// String returns a string representation of the literal for debugging purposes.
// Format: "literal{bytes, complete=true/false}"
//
// Example:
//
//	lit := literal.NewLiteral([]byte("test"), true)
//	fmt.Println(lit.String()) // Output: literal{test, complete=true}
func (l Literal) String() string {
	complete := "false"
	if l.Complete {
		complete = "true"
	}
	return "literal{" + string(l.Bytes) + ", complete=" + complete + "}"
}

// Seq represents a sequence of alternative literals that can match.
// This is the foundation for prefilter optimization: we extract multiple
// possible literals from a regex (e.g., from alternations /foo|bar|baz/)
// and use them for fast candidate filtering.
//
// Operations:
//   - Minimize: Remove redundant literals (e.g., "foo" makes "foobar" redundant for prefix matching)
//   - LongestCommonPrefix: Find shared prefix (e.g., "he" from ["hello", "help", "hero"])
//   - LongestCommonSuffix: Find shared suffix (e.g., "at" from ["cat", "bat", "rat"])
//
// Example:
//
//	seq := literal.NewSeq(
//	    literal.NewLiteral([]byte("foo"), true),
//	    literal.NewLiteral([]byte("bar"), true),
//	)
//	fmt.Printf("Sequence has %d literals\n", seq.Len()) // Output: Sequence has 2 literals
type Seq struct {
	literals []Literal
}

// NewSeq creates a new sequence from the given literals.
//
// Example:
//
//	seq := literal.NewSeq(
//	    literal.NewLiteral([]byte("hello"), true),
//	    literal.NewLiteral([]byte("world"), true),
//	)
//	fmt.Println(seq.Len()) // Output: 2
//
// Example with empty sequence:
//
//	seq := literal.NewSeq()
//	fmt.Println(seq.IsEmpty()) // Output: true
func NewSeq(lits ...Literal) *Seq {
	return &Seq{
		literals: lits,
	}
}

// Len returns the number of literals in the sequence.
//
// Example:
//
//	seq := literal.NewSeq(
//	    literal.NewLiteral([]byte("foo"), true),
//	    literal.NewLiteral([]byte("bar"), true),
//	)
//	fmt.Println(seq.Len()) // Output: 2
func (s *Seq) Len() int {
	if s == nil {
		return 0
	}
	return len(s.literals)
}

// Get returns the literal at the specified index.
// Panics if index is out of bounds.
//
// Example:
//
//	seq := literal.NewSeq(
//	    literal.NewLiteral([]byte("first"), true),
//	    literal.NewLiteral([]byte("second"), true),
//	)
//	fmt.Println(string(seq.Get(0).Bytes)) // Output: first
//	fmt.Println(string(seq.Get(1).Bytes)) // Output: second
func (s *Seq) Get(i int) Literal {
	return s.literals[i]
}

// IsEmpty returns true if the sequence has no literals.
//
// Example:
//
//	empty := literal.NewSeq()
//	fmt.Println(empty.IsEmpty()) // Output: true
//
//	nonempty := literal.NewSeq(literal.NewLiteral([]byte("x"), true))
//	fmt.Println(nonempty.IsEmpty()) // Output: false
func (s *Seq) IsEmpty() bool {
	return s == nil || len(s.literals) == 0
}

// IsFinite returns true if the sequence represents a finite language.
// A sequence is finite if it has at least one literal.
//
// In regex theory, a finite language is one with a bounded number of strings.
// For our purposes, any non-empty literal set represents a finite language.
//
// Example:
//
//	seq := literal.NewSeq(literal.NewLiteral([]byte("hello"), true))
//	fmt.Println(seq.IsFinite()) // Output: true
//
//	empty := literal.NewSeq()
//	fmt.Println(empty.IsFinite()) // Output: false
func (s *Seq) IsFinite() bool {
	return !s.IsEmpty()
}

// Clone returns a deep copy of the sequence.
// All literals and their byte slices are duplicated.
//
// Example:
//
//	original := literal.NewSeq(literal.NewLiteral([]byte("test"), true))
//	clone := original.Clone()
//	clone.Get(0).Bytes[0] = 'X' // Modifying clone doesn't affect original
//	fmt.Println(string(original.Get(0).Bytes)) // Output: test
func (s *Seq) Clone() *Seq {
	if s == nil {
		return nil
	}

	cloned := make([]Literal, len(s.literals))
	for i, lit := range s.literals {
		// Deep copy the byte slice
		bytesCopy := make([]byte, len(lit.Bytes))
		copy(bytesCopy, lit.Bytes)
		cloned[i] = Literal{
			Bytes:    bytesCopy,
			Complete: lit.Complete,
		}
	}

	return &Seq{literals: cloned}
}

// Minimize removes redundant literals from the sequence.
//
// For prefix matching, a literal L is redundant if there exists a shorter literal S
// that is a prefix of L. For example, in ["foo", "foobar"], "foo" makes "foobar"
// redundant because any string containing "foobar" also contains "foo".
//
// Algorithm:
//  1. Sort literals by length (shortest first)
//  2. For each literal L:
//     - Check if any shorter literal S is a prefix of L
//     - If yes, L is redundant (skip it)
//     - If no, keep L
//
// Time complexity: O(n² * m) where n = number of literals, m = average literal length
//
// Example:
//
//	seq := literal.NewSeq(
//	    literal.NewLiteral([]byte("foo"), true),
//	    literal.NewLiteral([]byte("foobar"), true),
//	)
//	seq.Minimize()
//	fmt.Println(seq.Len()) // Output: 1 (only "foo" remains)
//
// Example with no redundancy:
//
//	seq := literal.NewSeq(
//	    literal.NewLiteral([]byte("hello"), true),
//	    literal.NewLiteral([]byte("world"), true),
//	)
//	seq.Minimize()
//	fmt.Println(seq.Len()) // Output: 2 (both remain)
func (s *Seq) Minimize() {
	if s.IsEmpty() {
		return
	}

	// Sort by length (shortest first) for efficient redundancy detection
	sort.Slice(s.literals, func(i, j int) bool {
		return len(s.literals[i].Bytes) < len(s.literals[j].Bytes)
	})

	// Keep track of non-redundant literals
	kept := make([]Literal, 0, len(s.literals))

	for i := 0; i < len(s.literals); i++ {
		current := s.literals[i]
		isRedundant := false

		// Check if any shorter (already kept) literal is a prefix of current
		for j := 0; j < len(kept); j++ {
			if isPrefix(kept[j].Bytes, current.Bytes) {
				// current is redundant (covered by shorter prefix)
				isRedundant = true
				break
			}
		}

		if !isRedundant {
			kept = append(kept, current)
		}
	}

	s.literals = kept
}

// LongestCommonPrefix returns the longest common prefix of all literals in the sequence.
// If the sequence is empty or has no common prefix, returns an empty slice.
//
// Algorithm:
//  1. If sequence is empty, return empty slice
//  2. Take first literal as candidate prefix
//  3. For each other literal:
//     - Find common prefix with current candidate
//     - Update candidate to this shorter prefix
//  4. Return final prefix
//
// Time complexity: O(n * m) where n = number of literals, m = length of result prefix
//
// Example:
//
//	seq := literal.NewSeq(
//	    literal.NewLiteral([]byte("hello"), true),
//	    literal.NewLiteral([]byte("help"), true),
//	    literal.NewLiteral([]byte("hero"), true),
//	)
//	prefix := seq.LongestCommonPrefix()
//	fmt.Println(string(prefix)) // Output: he
//
// Example with no common prefix:
//
//	seq := literal.NewSeq(
//	    literal.NewLiteral([]byte("abc"), true),
//	    literal.NewLiteral([]byte("def"), true),
//	)
//	prefix := seq.LongestCommonPrefix()
//	fmt.Println(len(prefix)) // Output: 0
func (s *Seq) LongestCommonPrefix() []byte {
	if s.IsEmpty() {
		return []byte{}
	}

	// Start with first literal as candidate
	prefix := s.literals[0].Bytes

	// Iteratively find common prefix with each subsequent literal
	for i := 1; i < len(s.literals); i++ {
		prefix = commonPrefix(prefix, s.literals[i].Bytes)
		// Early exit if no common prefix
		if len(prefix) == 0 {
			return []byte{}
		}
	}

	// Return a copy to avoid aliasing issues
	result := make([]byte, len(prefix))
	copy(result, prefix)
	return result
}

// LongestCommonSuffix returns the longest common suffix of all literals in the sequence.
// If the sequence is empty or has no common suffix, returns an empty slice.
//
// Algorithm:
//  1. Reverse all literals
//  2. Find longest common prefix of reversed literals
//  3. Reverse the result
//
// Time complexity: O(n * m) where n = number of literals, m = length of result suffix
//
// Example:
//
//	seq := literal.NewSeq(
//	    literal.NewLiteral([]byte("cat"), true),
//	    literal.NewLiteral([]byte("bat"), true),
//	    literal.NewLiteral([]byte("rat"), true),
//	)
//	suffix := seq.LongestCommonSuffix()
//	fmt.Println(string(suffix)) // Output: at
//
// Example with no common suffix:
//
//	seq := literal.NewSeq(
//	    literal.NewLiteral([]byte("abc"), true),
//	    literal.NewLiteral([]byte("def"), true),
//	)
//	suffix := seq.LongestCommonSuffix()
//	fmt.Println(len(suffix)) // Output: 0
func (s *Seq) LongestCommonSuffix() []byte {
	if s.IsEmpty() {
		return []byte{}
	}

	// Start with last bytes of first literal as candidate suffix
	suffix := s.literals[0].Bytes

	// Iteratively find common suffix with each subsequent literal
	for i := 1; i < len(s.literals); i++ {
		suffix = commonSuffix(suffix, s.literals[i].Bytes)
		// Early exit if no common suffix
		if len(suffix) == 0 {
			return []byte{}
		}
	}

	// Return a copy to avoid aliasing issues
	result := make([]byte, len(suffix))
	copy(result, suffix)
	return result
}

// Helper functions

// isPrefix returns true if prefix is a prefix of s.
func isPrefix(prefix, s []byte) bool {
	if len(prefix) > len(s) {
		return false
	}
	return bytes.Equal(prefix, s[:len(prefix)])
}

// commonPrefix returns the longest common prefix of a and b.
func commonPrefix(a, b []byte) []byte {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}

	for i := 0; i < minLen; i++ {
		if a[i] != b[i] {
			return a[:i]
		}
	}

	return a[:minLen]
}

// commonSuffix returns the longest common suffix of a and b.
func commonSuffix(a, b []byte) []byte {
	aLen := len(a)
	bLen := len(b)
	minLen := aLen
	if bLen < minLen {
		minLen = bLen
	}

	for i := 0; i < minLen; i++ {
		if a[aLen-1-i] != b[bLen-1-i] {
			if i == 0 {
				return []byte{}
			}
			return a[aLen-i:]
		}
	}

	return a[aLen-minLen:]
}
