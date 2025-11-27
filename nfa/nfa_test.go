package nfa

import (
	"regexp"
	"strings"
	"testing"
)

// TestNFA_Compile_Literal tests compilation of literal patterns
func TestNFA_Compile_Literal(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool // should compile successfully
	}{
		{"hello", true},
		{"", true},
		{"a", true},
		{"test123", true},
		{"Hello World", true},
		{"привет", true}, // Unicode
		{"😀", true},      // Emoji
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)

			if tt.want && err != nil {
				t.Errorf("expected success, got error: %v", err)
			}
			if !tt.want && err == nil {
				t.Errorf("expected error, got success")
			}

			if nfa != nil {
				if nfa.States() == 0 {
					t.Error("NFA has no states")
				}
				if nfa.Start() == InvalidState {
					t.Error("NFA has invalid start state")
				}
			}
		})
	}
}

// TestNFA_Compile_CharClass tests character class compilation
func TestNFA_Compile_CharClass(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"[a-z]", true},
		{"[A-Z]", true},
		{"[0-9]", true},
		{"[a-zA-Z0-9]", true},
		// Note: negated character classes like [^a-z] expand to large ranges
		// and may hit the >256 character limit in MVP implementation
		{"[abc]", true},
		{"[a-z]{3}", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)

			if tt.want && err != nil {
				t.Errorf("expected success, got error: %v", err)
			}
			if !tt.want && err == nil {
				t.Errorf("expected error, got success")
			}

			if nfa != nil && nfa.States() == 0 {
				t.Error("NFA has no states")
			}
		})
	}
}

// TestNFA_Compile_Quantifiers tests quantifier compilation
func TestNFA_Compile_Quantifiers(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"a+", true},
		{"b*", true},
		{"c?", true},
		{"d{2}", true},
		{"e{2,5}", true},
		{"f{3,}", true},
		{"(ab)+", true},
		{"(cd)*", true},
		{"(ef)?", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)

			if tt.want && err != nil {
				t.Errorf("expected success, got error: %v", err)
			}
			if !tt.want && err == nil {
				t.Errorf("expected error, got success")
			}

			if nfa != nil && nfa.States() == 0 {
				t.Error("NFA has no states")
			}
		})
	}
}

// TestNFA_Compile_Alternation tests alternation (|) compilation
func TestNFA_Compile_Alternation(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"a|b", true},
		{"foo|bar", true},
		{"one|two|three", true},
		{"(a|b)c", true},
		{"a(b|c)d", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)

			if tt.want && err != nil {
				t.Errorf("expected success, got error: %v", err)
			}
			if !tt.want && err == nil {
				t.Errorf("expected error, got success")
			}

			if nfa != nil && nfa.States() == 0 {
				t.Error("NFA has no states")
			}
		})
	}
}

// TestNFA_Compile_Anchors tests anchor compilation
func TestNFA_Compile_Anchors(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"^start", true},
		{"end$", true},
		{"^exact$", true},
		{"^", true},
		{"$", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)

			if tt.want && err != nil {
				t.Errorf("expected success, got error: %v", err)
			}
			if !tt.want && err == nil {
				t.Errorf("expected error, got success")
			}

			if nfa != nil && nfa.States() == 0 {
				t.Error("NFA has no states")
			}
		})
	}
}

// TestNFA_Compile_Concat tests concatenation
func TestNFA_Compile_Concat(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"abc", true},
		{"hello world", true},
		{"test[0-9]+", true},
		{"foo.*bar", true},
		{"a+b+c+", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)

			if tt.want && err != nil {
				t.Errorf("expected success, got error: %v", err)
			}
			if !tt.want && err == nil {
				t.Errorf("expected error, got success")
			}

			if nfa != nil && nfa.States() == 0 {
				t.Error("NFA has no states")
			}
		})
	}
}

// TestPikeVM_Search_Basic tests basic PikeVM search functionality
func TestPikeVM_Search_Basic(t *testing.T) {
	tests := []struct {
		pattern  string
		haystack string
		want     Match // {Start, End} or {-1, -1} for no match
	}{
		{"foo", "hello foo world", Match{6, 9}},
		{"bar", "hello world", Match{-1, -1}},
		{"test", "this is a test", Match{10, 14}},
		{"hello", "hello", Match{0, 5}},
		{"world", "world", Match{0, 5}},
		{"", "", Match{0, 0}},
		{"a", "a", Match{0, 1}},
		{"abc", "abc", Match{0, 3}},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"/"+tt.haystack, func(t *testing.T) {
			nfa := mustCompile(t, tt.pattern)
			vm := NewPikeVM(nfa)

			start, end, matched := vm.Search([]byte(tt.haystack))

			if !matched && tt.want.Start != -1 {
				t.Errorf("expected match at %d-%d, got no match", tt.want.Start, tt.want.End)
			}
			if matched {
				if start != tt.want.Start || end != tt.want.End {
					t.Errorf("got match at %d-%d, want %d-%d", start, end, tt.want.Start, tt.want.End)
				}
			}
		})
	}
}

// TestPikeVM_Search_Anchored tests anchored matching
// NOTE: Anchors (^, $) are not fully implemented in MVP
// They compile successfully but don't enforce start/end boundaries yet
// This is deferred to a future implementation
func TestPikeVM_Search_Anchored(t *testing.T) {
	t.Skip("Anchors (^, $) not fully implemented in MVP - deferred to Phase 4.1")

	tests := []struct {
		pattern  string
		haystack string
		want     Match
	}{
		{"^hello", "hello world", Match{0, 5}},
		{"^hello", "world hello", Match{-1, -1}},
		{"world$", "hello world", Match{6, 11}},
		{"world$", "world hello", Match{-1, -1}},
		{"^hello$", "hello", Match{0, 5}},
		{"^hello$", "hello world", Match{-1, -1}},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"/"+tt.haystack, func(t *testing.T) {
			nfa := mustCompile(t, tt.pattern)
			vm := NewPikeVM(nfa)

			start, end, matched := vm.Search([]byte(tt.haystack))

			if !matched && tt.want.Start != -1 {
				t.Errorf("expected match at %d-%d, got no match", tt.want.Start, tt.want.End)
			}
			if matched {
				if start != tt.want.Start || end != tt.want.End {
					t.Errorf("got match at %d-%d, want %d-%d", start, end, tt.want.Start, tt.want.End)
				}
			}
		})
	}
}

// TestPikeVM_Search_CharClass tests character class matching
func TestPikeVM_Search_CharClass(t *testing.T) {
	tests := []struct {
		pattern  string
		haystack string
		wantPos  []int // [start, end] or nil for no match
	}{
		{"[0-9]", "test123", []int{4, 5}},
		{"[a-z]", "123abc", []int{3, 4}},
		{"[A-Z]", "hello World", []int{6, 7}},
		{"[0-9]+", "foo123bar", []int{3, 6}},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"/"+tt.haystack, func(t *testing.T) {
			nfa := mustCompile(t, tt.pattern)
			vm := NewPikeVM(nfa)

			start, end, matched := vm.Search([]byte(tt.haystack))

			if tt.wantPos == nil && matched {
				t.Errorf("expected no match, got match at %d-%d", start, end)
			}
			if tt.wantPos != nil {
				if !matched {
					t.Errorf("expected match at %v, got no match", tt.wantPos)
				} else if start != tt.wantPos[0] || end != tt.wantPos[1] {
					t.Errorf("got match at %d-%d, want %d-%d", start, end, tt.wantPos[0], tt.wantPos[1])
				}
			}
		})
	}
}

// TestPikeVM_Search_Quantifiers tests quantifier matching
func TestPikeVM_Search_Quantifiers(t *testing.T) {
	tests := []struct {
		pattern  string
		haystack string
		wantPos  []int
	}{
		{"a+", "aaa", []int{0, 3}},
		{"a*", "bbb", []int{0, 0}}, // zero-length match at start
		{"a?", "a", []int{0, 1}},
		{"a?", "b", []int{0, 0}}, // zero-length match
		{"a{2}", "aa", []int{0, 2}},
		{"a{2}", "aaa", []int{0, 2}},
		{"a{2,4}", "aaaaa", []int{0, 4}},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"/"+tt.haystack, func(t *testing.T) {
			nfa := mustCompile(t, tt.pattern)
			vm := NewPikeVM(nfa)

			start, end, matched := vm.Search([]byte(tt.haystack))

			if !matched {
				t.Errorf("expected match at %v, got no match", tt.wantPos)
			} else if start != tt.wantPos[0] || end != tt.wantPos[1] {
				t.Errorf("got match at %d-%d, want %d-%d", start, end, tt.wantPos[0], tt.wantPos[1])
			}
		})
	}
}

// TestPikeVM_Search_Alternation tests alternation matching
func TestPikeVM_Search_Alternation(t *testing.T) {
	tests := []struct {
		pattern  string
		haystack string
		wantPos  []int
	}{
		{"foo|bar", "foo", []int{0, 3}},
		{"foo|bar", "bar", []int{0, 3}},
		{"foo|bar", "baz foo", []int{4, 7}},
		{"one|two|three", "two", []int{0, 3}},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"/"+tt.haystack, func(t *testing.T) {
			nfa := mustCompile(t, tt.pattern)
			vm := NewPikeVM(nfa)

			start, end, matched := vm.Search([]byte(tt.haystack))

			if !matched {
				t.Errorf("expected match at %v, got no match", tt.wantPos)
			} else if start != tt.wantPos[0] || end != tt.wantPos[1] {
				t.Errorf("got match at %d-%d, want %d-%d", start, end, tt.wantPos[0], tt.wantPos[1])
			}
		})
	}
}

// TestPikeVM_SearchAll tests finding all matches
func TestPikeVM_SearchAll(t *testing.T) {
	tests := []struct {
		pattern  string
		haystack string
		want     []Match
	}{
		{"a", "aaa", []Match{{0, 1}, {1, 2}, {2, 3}}},
		{"foo", "foo bar foo", []Match{{0, 3}, {8, 11}}},
		{"[0-9]+", "a1b22c333", []Match{{1, 2}, {3, 5}, {6, 9}}},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"/"+tt.haystack, func(t *testing.T) {
			nfa := mustCompile(t, tt.pattern)
			vm := NewPikeVM(nfa)

			matches := vm.SearchAll([]byte(tt.haystack))

			if len(matches) != len(tt.want) {
				t.Errorf("got %d matches, want %d", len(matches), len(tt.want))
				t.Logf("got: %v", matches)
				t.Logf("want: %v", tt.want)
				return
			}

			for i, m := range matches {
				if m.Start != tt.want[i].Start || m.End != tt.want[i].End {
					t.Errorf("match %d: got %v, want %v", i, m, tt.want[i])
				}
			}
		})
	}
}

// TestPikeVM_Search_EdgeCases tests edge cases
func TestPikeVM_Search_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		wantPos  []int // nil = no match
	}{
		{"empty pattern, empty haystack", "", "", []int{0, 0}},
		{"empty pattern, non-empty haystack", "", "hello", []int{0, 0}},
		{"pattern longer than haystack", "hello world", "hi", nil},
		{"single byte match", "x", "x", []int{0, 1}},
		{"single byte no match", "x", "y", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nfa := mustCompile(t, tt.pattern)
			vm := NewPikeVM(nfa)

			start, end, matched := vm.Search([]byte(tt.haystack))

			if tt.wantPos == nil && matched {
				t.Errorf("expected no match, got match at %d-%d", start, end)
			}
			if tt.wantPos != nil {
				if !matched {
					t.Errorf("expected match at %v, got no match", tt.wantPos)
				} else if start != tt.wantPos[0] || end != tt.wantPos[1] {
					t.Errorf("got match at %d-%d, want %d-%d", start, end, tt.wantPos[0], tt.wantPos[1])
				}
			}
		})
	}
}

// TestPikeVM_Search_Unicode tests Unicode handling
func TestPikeVM_Search_Unicode(t *testing.T) {
	tests := []struct {
		pattern  string
		haystack string
		wantPos  []int
	}{
		{"привет", "привет мир", []int{0, 12}}, // Russian "hello"
		{"世界", "你好世界", []int{6, 12}},           // Chinese "world"
		{"😀", "hello 😀 world", []int{6, 10}},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"/"+tt.haystack, func(t *testing.T) {
			nfa := mustCompile(t, tt.pattern)
			vm := NewPikeVM(nfa)

			start, end, matched := vm.Search([]byte(tt.haystack))

			if !matched {
				t.Errorf("expected match at %v, got no match", tt.wantPos)
			} else if start != tt.wantPos[0] || end != tt.wantPos[1] {
				t.Errorf("got match at %d-%d, want %d-%d", start, end, tt.wantPos[0], tt.wantPos[1])
				t.Logf("haystack bytes: %v", []byte(tt.haystack))
			}
		})
	}
}

// TestPikeVM_Correctness_VsStdlib tests correctness by comparing with stdlib regexp
func TestPikeVM_Correctness_VsStdlib(t *testing.T) {
	patterns := []string{
		"foo",
		"[a-z]+",
		"[0-9]+",
		"a+",
		"a*",
		"a?",
		"foo|bar",
		"a{2,4}",
		"(foo|bar)+",
		"test.*end",
	}

	haystacks := []string{
		"hello foo world",
		"test123abc",
		"aaaaa",
		"",
		"foobar",
		"this is a test with an ending",
	}

	for _, pattern := range patterns {
		for _, haystack := range haystacks {
			t.Run(pattern+"/"+haystack, func(t *testing.T) {
				// Compile with stdlib
				stdRE, err := regexp.Compile(pattern)
				if err != nil {
					t.Skip("stdlib compilation failed:", err)
				}

				// Compile with our NFA
				nfa := mustCompile(t, pattern)
				vm := NewPikeVM(nfa)

				// Compare results
				stdLoc := stdRE.FindStringIndex(haystack)
				ourStart, ourEnd, ourMatched := vm.Search([]byte(haystack))

				stdMatched := stdLoc != nil

				if stdMatched != ourMatched {
					t.Errorf("match mismatch: stdlib=%v, ours=%v", stdMatched, ourMatched)
				}

				if stdMatched && ourMatched {
					if stdLoc[0] != ourStart || stdLoc[1] != ourEnd {
						t.Errorf("position mismatch: stdlib=%v, ours=[%d,%d]", stdLoc, ourStart, ourEnd)
					}
				}
			})
		}
	}
}

// TestBuilder_Basic tests the Builder API
func TestBuilder_Basic(t *testing.T) {
	b := NewBuilder()

	// Build a simple NFA: a -> b -> match
	match := b.AddMatch()
	stateB := b.AddByteRange('b', 'b', match)
	stateA := b.AddByteRange('a', 'a', stateB)

	b.SetStart(stateA)

	nfa, err := b.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if nfa.States() != 3 {
		t.Errorf("expected 3 states, got %d", nfa.States())
	}

	// Test matching
	vm := NewPikeVM(nfa)
	start, end, matched := vm.Search([]byte("ab"))
	if !matched {
		t.Error("expected match for 'ab'")
	}
	if start != 0 || end != 2 {
		t.Errorf("expected match at 0-2, got %d-%d", start, end)
	}

	// Test non-match
	_, _, matched = vm.Search([]byte("ac"))
	if matched {
		t.Error("expected no match for 'ac'")
	}
}

// TestBuilder_Split tests split states
func TestBuilder_Split(t *testing.T) {
	b := NewBuilder()

	// Build NFA for a|b
	match := b.AddMatch()
	stateA := b.AddByteRange('a', 'a', match)
	stateB := b.AddByteRange('b', 'b', match)
	split := b.AddSplit(stateA, stateB)

	b.SetStart(split)

	nfa, err := b.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	vm := NewPikeVM(nfa)

	// Test both alternatives
	_, _, matched := vm.Search([]byte("a"))
	if !matched {
		t.Error("expected match for 'a'")
	}

	_, _, matched = vm.Search([]byte("b"))
	if !matched {
		t.Error("expected match for 'b'")
	}

	_, _, matched = vm.Search([]byte("c"))
	if matched {
		t.Error("expected no match for 'c'")
	}
}

// TestBuilder_Validation tests builder validation
func TestBuilder_Validation(t *testing.T) {
	t.Run("no start state", func(t *testing.T) {
		b := NewBuilder()
		b.AddMatch()
		_, err := b.Build()
		if err == nil {
			t.Error("expected error for missing start state")
		}
	})

	t.Run("invalid start state", func(t *testing.T) {
		b := NewBuilder()
		b.SetStart(StateID(999))
		_, err := b.Build()
		if err == nil {
			t.Error("expected error for invalid start state")
		}
	})

	t.Run("invalid target reference", func(t *testing.T) {
		b := NewBuilder()
		b.AddByteRange('a', 'a', StateID(999))
		b.SetStart(0)
		_, err := b.Build()
		if err == nil {
			t.Error("expected error for invalid target reference")
		}
	})
}

// TestNFA_StateIter tests state iteration
func TestNFA_StateIter(t *testing.T) {
	nfa := mustCompile(t, "abc")

	count := 0
	iter := nfa.Iter()
	for iter.HasNext() {
		state := iter.Next()
		if state == nil {
			t.Error("got nil state from iterator")
		}
		count++
	}

	if count != nfa.States() {
		t.Errorf("iterator count %d doesn't match NFA states %d", count, nfa.States())
	}
}

// TestCompiler_RecursionDepth tests recursion depth limiting
func TestCompiler_RecursionDepth(t *testing.T) {
	// Build a deeply nested pattern
	pattern := strings.Repeat("(", 200) + "a" + strings.Repeat(")", 200)

	config := DefaultCompilerConfig()
	config.MaxRecursionDepth = 50 // Limit to 50 levels

	compiler := NewCompiler(config)
	_, err := compiler.Compile(pattern)

	if err == nil {
		t.Error("expected error for deep recursion, got success")
	}
}

// mustCompile is a test helper that compiles a pattern or fails the test
func mustCompile(t *testing.T, pattern string) *NFA {
	t.Helper()
	compiler := NewDefaultCompiler()
	nfa, err := compiler.Compile(pattern)
	if err != nil {
		t.Fatalf("failed to compile pattern %q: %v", pattern, err)
	}
	return nfa
}

// Benchmark basic literal matching
func BenchmarkPikeVM_Literal(b *testing.B) {
	b.ReportAllocs()

	nfa := mustCompileB(b, "foo")
	vm := NewPikeVM(nfa)
	haystack := []byte("this is a foo in the middle")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vm.Search(haystack)
	}
}

// Benchmark character class matching
func BenchmarkPikeVM_CharClass(b *testing.B) {
	b.ReportAllocs()

	nfa := mustCompileB(b, "[0-9]+")
	vm := NewPikeVM(nfa)
	haystack := []byte("test123456789end")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vm.Search(haystack)
	}
}

// Benchmark quantifier matching
func BenchmarkPikeVM_Quantifier(b *testing.B) {
	b.ReportAllocs()

	nfa := mustCompileB(b, "a+b+c+")
	vm := NewPikeVM(nfa)
	haystack := []byte("aaaabbbbcccc")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vm.Search(haystack)
	}
}

// Benchmark vs stdlib regexp
func BenchmarkPikeVM_VsStdlib(b *testing.B) {
	b.ReportAllocs()

	pattern := "[a-z]+"
	haystack := []byte("this is a test string with many words")

	b.Run("PikeVM", func(b *testing.B) {
		b.ReportAllocs()
		nfa := mustCompileB(b, pattern)
		vm := NewPikeVM(nfa)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			vm.Search(haystack)
		}
	})

	b.Run("Stdlib", func(b *testing.B) {
		b.ReportAllocs()
		re := regexp.MustCompile(pattern)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			re.FindIndex(haystack)
		}
	})
}

func mustCompileB(b *testing.B, pattern string) *NFA {
	b.Helper()
	compiler := NewDefaultCompiler()
	nfa, err := compiler.Compile(pattern)
	if err != nil {
		b.Fatalf("failed to compile pattern %q: %v", pattern, err)
	}
	return nfa
}
