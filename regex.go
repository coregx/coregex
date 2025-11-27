// Package coregex provides a high-performance regex engine for Go.
//
// coregex achieves 5-50x speedup over Go's stdlib regexp through:
//   - Multi-engine architecture (NFA, Lazy DFA, prefilters)
//   - SIMD-accelerated primitives (memchr, memmem, teddy)
//   - Literal extraction and prefiltering
//   - Automatic strategy selection
//
// The public API is compatible with stdlib regexp where possible, making it
// easy to migrate existing code.
//
// Basic usage:
//
//	// Compile a pattern
//	re, err := coregex.Compile(`\d+`)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Find first match
//	match := re.Find([]byte("hello 123 world"))
//	fmt.Println(string(match)) // "123"
//
//	// Check if matches
//	if re.Match([]byte("hello 123")) {
//	    fmt.Println("matched!")
//	}
//
// Advanced usage:
//
//	// Custom configuration
//	config := coregex.DefaultConfig()
//	config.MaxDFAStates = 50000
//	re, err := coregex.CompileWithConfig("(a|b|c)*", config)
//
// Performance characteristics:
//   - Patterns with literals: 5-50x faster (prefilter optimization)
//   - Simple patterns: comparable to stdlib
//   - Complex patterns: 2-10x faster (DFA avoids backtracking)
//   - Worst case: guaranteed O(m*n) (ReDoS safe)
//
// Limitations (v1.0):
//   - No capture groups (coming in v1.1)
//   - No replace functions (coming in v1.1)
//   - No multiline/case-insensitive flags (coming in v1.1)
package coregex

import (
	"github.com/coregx/coregex/meta"
)

// Regex represents a compiled regular expression.
//
// A Regex is safe to use concurrently from multiple goroutines, except for
// methods that modify internal state (like ResetStats).
//
// Example:
//
//	re := coregex.MustCompile(`hello`)
//	if re.Match([]byte("hello world")) {
//	    println("matched!")
//	}
type Regex struct {
	engine  *meta.Engine
	pattern string
}

// Compile compiles a regular expression pattern.
//
// Syntax is Perl-compatible (same as Go's stdlib regexp).
// Returns an error if the pattern is invalid.
//
// Example:
//
//	re, err := coregex.Compile(`\d{3}-\d{4}`)
//	if err != nil {
//	    log.Fatal(err)
//	}
func Compile(pattern string) (*Regex, error) {
	engine, err := meta.Compile(pattern)
	if err != nil {
		return nil, err
	}

	return &Regex{
		engine:  engine,
		pattern: pattern,
	}, nil
}

// MustCompile compiles a regular expression pattern and panics if it fails.
//
// This is useful for patterns known to be valid at compile time.
//
// Example:
//
//	var emailRegex = coregex.MustCompile(`[a-z]+@[a-z]+\.[a-z]+`)
func MustCompile(pattern string) *Regex {
	re, err := Compile(pattern)
	if err != nil {
		panic("coregex: Compile(" + pattern + "): " + err.Error())
	}
	return re
}

// CompileWithConfig compiles a pattern with custom configuration.
//
// This allows fine-tuning of performance characteristics.
//
// Example:
//
//	config := coregex.DefaultConfig()
//	config.MaxDFAStates = 100000 // Larger cache
//	re, err := coregex.CompileWithConfig("(a|b|c)*", config)
func CompileWithConfig(pattern string, config meta.Config) (*Regex, error) {
	engine, err := meta.CompileWithConfig(pattern, config)
	if err != nil {
		return nil, err
	}

	return &Regex{
		engine:  engine,
		pattern: pattern,
	}, nil
}

// DefaultConfig returns the default configuration for compilation.
//
// Users can customize this and pass to CompileWithConfig.
//
// Example:
//
//	config := coregex.DefaultConfig()
//	config.EnableDFA = false // Use NFA only
//	re, _ := coregex.CompileWithConfig("pattern", config)
func DefaultConfig() meta.Config {
	return meta.DefaultConfig()
}

// Match reports whether the byte slice b contains any match of the pattern.
//
// Example:
//
//	re := coregex.MustCompile(`\d+`)
//	if re.Match([]byte("hello 123")) {
//	    println("contains digits")
//	}
func (r *Regex) Match(b []byte) bool {
	return r.engine.IsMatch(b)
}

// MatchString reports whether the string s contains any match of the pattern.
//
// Example:
//
//	re := coregex.MustCompile(`hello`)
//	if re.MatchString("hello world") {
//	    println("matched!")
//	}
func (r *Regex) MatchString(s string) bool {
	return r.Match([]byte(s))
}

// Find returns a slice holding the text of the leftmost match in b.
// Returns nil if no match is found.
//
// Example:
//
//	re := coregex.MustCompile(`\d+`)
//	match := re.Find([]byte("age: 42"))
//	println(string(match)) // "42"
func (r *Regex) Find(b []byte) []byte {
	match := r.engine.Find(b)
	if match == nil {
		return nil
	}
	return match.Bytes()
}

// FindString returns a string holding the text of the leftmost match in s.
// Returns empty string if no match is found.
//
// Example:
//
//	re := coregex.MustCompile(`\d+`)
//	match := re.FindString("age: 42")
//	println(match) // "42"
func (r *Regex) FindString(s string) string {
	match := r.Find([]byte(s))
	if match == nil {
		return ""
	}
	return string(match)
}

// FindIndex returns a two-element slice of integers defining the location of
// the leftmost match in b. The match is at b[loc[0]:loc[1]].
// Returns nil if no match is found.
//
// Example:
//
//	re := coregex.MustCompile(`\d+`)
//	loc := re.FindIndex([]byte("age: 42"))
//	println(loc[0], loc[1]) // 5, 7
func (r *Regex) FindIndex(b []byte) []int {
	match := r.engine.Find(b)
	if match == nil {
		return nil
	}
	return []int{match.Start(), match.End()}
}

// FindStringIndex returns a two-element slice of integers defining the location
// of the leftmost match in s. The match is at s[loc[0]:loc[1]].
// Returns nil if no match is found.
//
// Example:
//
//	re := coregex.MustCompile(`\d+`)
//	loc := re.FindStringIndex("age: 42")
//	println(loc[0], loc[1]) // 5, 7
func (r *Regex) FindStringIndex(s string) []int {
	return r.FindIndex([]byte(s))
}

// FindAll returns a slice of all successive matches of the pattern in b.
// If n > 0, it returns at most n matches. If n <= 0, it returns all matches.
//
// Example:
//
//	re := coregex.MustCompile(`\d+`)
//	matches := re.FindAll([]byte("1 2 3"), -1)
//	// matches = [[]byte("1"), []byte("2"), []byte("3")]
func (r *Regex) FindAll(b []byte, n int) [][]byte {
	if n == 0 {
		return nil
	}

	var matches [][]byte
	pos := 0
	for {
		// Search from current position
		match := r.engine.Find(b[pos:])
		if match == nil {
			break
		}

		// Adjust match positions to absolute offsets
		absStart := pos + match.Start()
		absEnd := pos + match.End()
		matches = append(matches, b[absStart:absEnd])

		// Move position past this match
		if absEnd > pos {
			pos = absEnd
		} else {
			// Empty match: advance by 1 to avoid infinite loop
			pos++
		}

		if pos > len(b) {
			break
		}

		// Check limit
		if n > 0 && len(matches) >= n {
			break
		}
	}

	return matches
}

// FindAllString returns a slice of all successive matches of the pattern in s.
// If n > 0, it returns at most n matches. If n <= 0, it returns all matches.
//
// Example:
//
//	re := coregex.MustCompile(`\d+`)
//	matches := re.FindAllString("1 2 3", -1)
//	// matches = ["1", "2", "3"]
func (r *Regex) FindAllString(s string, n int) []string {
	matches := r.FindAll([]byte(s), n)
	if matches == nil {
		return nil
	}

	result := make([]string, len(matches))
	for i, m := range matches {
		result[i] = string(m)
	}
	return result
}

// String returns the source text used to compile the regular expression.
//
// Example:
//
//	re := coregex.MustCompile(`\d+`)
//	println(re.String()) // `\d+`
func (r *Regex) String() string {
	return r.pattern
}

// NumSubexp returns the number of parenthesized subexpressions (capture groups).
// Group 0 is the entire match, so the returned value equals the number of
// explicit capture groups plus 1.
//
// Example:
//
//	re := coregex.MustCompile(`(\w+)@(\w+)\.(\w+)`)
//	println(re.NumSubexp()) // 4 (entire match + 3 groups)
func (r *Regex) NumSubexp() int {
	return r.engine.NumCaptures()
}

// FindSubmatch returns a slice holding the text of the leftmost match
// and the matches of all capture groups.
//
// A return value of nil indicates no match.
// Result[0] is the entire match, result[i] is the ith capture group.
// Unmatched groups will be nil.
//
// Example:
//
//	re := coregex.MustCompile(`(\w+)@(\w+)\.(\w+)`)
//	match := re.FindSubmatch([]byte("user@example.com"))
//	// match[0] = "user@example.com"
//	// match[1] = "user"
//	// match[2] = "example"
//	// match[3] = "com"
func (r *Regex) FindSubmatch(b []byte) [][]byte {
	match := r.engine.FindSubmatch(b)
	if match == nil {
		return nil
	}
	return match.AllGroups()
}

// FindStringSubmatch returns a slice of strings holding the text of the leftmost
// match and the matches of all capture groups.
//
// Example:
//
//	re := coregex.MustCompile(`(\w+)@(\w+)\.(\w+)`)
//	match := re.FindStringSubmatch("user@example.com")
//	// match[0] = "user@example.com"
//	// match[1] = "user"
func (r *Regex) FindStringSubmatch(s string) []string {
	match := r.engine.FindSubmatch([]byte(s))
	if match == nil {
		return nil
	}
	return match.AllGroupStrings()
}

// FindSubmatchIndex returns a slice holding the index pairs for the leftmost
// match and the matches of all capture groups.
//
// A return value of nil indicates no match.
// Result[2*i:2*i+2] is the indices for the ith group.
// Unmatched groups have -1 indices.
//
// Example:
//
//	re := coregex.MustCompile(`(\w+)@(\w+)\.(\w+)`)
//	idx := re.FindSubmatchIndex([]byte("user@example.com"))
//	// idx[0:2] = indices for entire match
//	// idx[2:4] = indices for first capture group
func (r *Regex) FindSubmatchIndex(b []byte) []int {
	match := r.engine.FindSubmatch(b)
	if match == nil {
		return nil
	}

	numGroups := match.NumCaptures()
	result := make([]int, numGroups*2)
	for i := 0; i < numGroups; i++ {
		idx := match.GroupIndex(i)
		if len(idx) >= 2 {
			result[i*2] = idx[0]
			result[i*2+1] = idx[1]
		} else {
			result[i*2] = -1
			result[i*2+1] = -1
		}
	}
	return result
}

// FindStringSubmatchIndex returns the index pairs for the leftmost match
// and capture groups. Same as FindSubmatchIndex but for strings.
func (r *Regex) FindStringSubmatchIndex(s string) []int {
	return r.FindSubmatchIndex([]byte(s))
}
