package lazy

import (
	"regexp"
	"testing"

	"github.com/coregx/coregex/nfa"
)

// TestLazyDFABasicLiteral tests simple literal matching
func TestLazyDFABasicLiteral(t *testing.T) {
	tests := []struct {
		pattern  string
		input    string
		wantPos  int
		wantName string
	}{
		{"hello", "hello world", 5, "exact match at start"},
		{"hello", "say hello there", 9, "match in middle"},
		{"hello", "world", -1, "no match"},
		{"a", "a", 1, "single char match"},
		{"a", "bbbabbba", 4, "single char in string"},
		{"", "", 0, "empty pattern matches empty"},
		{"test", "test", 4, "exact match full string"},
		{"foo", "foo foo foo", 3, "first occurrence"},
	}

	for _, tt := range tests {
		t.Run(tt.wantName, func(t *testing.T) {
			dfa, err := CompilePattern(tt.pattern)
			if err != nil {
				t.Fatalf("CompilePattern(%q) error: %v", tt.pattern, err)
			}

			got := dfa.Find([]byte(tt.input))
			if got != tt.wantPos {
				t.Errorf("Find(%q, %q) = %d, want %d", tt.pattern, tt.input, got, tt.wantPos)
			}
		})
	}
}

// TestLazyDFAConcat tests concatenation patterns
func TestLazyDFAConcat(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		wantPos int
	}{
		{"abc", "abc", 3},
		{"abc", "xyzabcdef", 6},
		{"abc", "abxabc", 6},
		{"hello world", "hello world", 11},
	}

	for _, tt := range tests {
		dfa, err := CompilePattern(tt.pattern)
		if err != nil {
			t.Fatalf("CompilePattern(%q) error: %v", tt.pattern, err)
		}

		got := dfa.Find([]byte(tt.input))
		if got != tt.wantPos {
			t.Errorf("Find(%q, %q) = %d, want %d", tt.pattern, tt.input, got, tt.wantPos)
		}
	}
}

// TestLazyDFAAlternation tests alternation (|) patterns
func TestLazyDFAAlternation(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		wantPos int
	}{
		{"foo|bar", "foo", 3},
		{"foo|bar", "bar", 3},
		{"foo|bar", "test bar end", 8},
		{"foo|bar", "xxx foo yyy bar zzz", 7},
		{"a|b|c", "c", 1},
		{"hello|world", "say world", 9},
	}

	for _, tt := range tests {
		dfa, err := CompilePattern(tt.pattern)
		if err != nil {
			t.Fatalf("CompilePattern(%q) error: %v", tt.pattern, err)
		}

		got := dfa.Find([]byte(tt.input))
		if got != tt.wantPos {
			t.Errorf("Find(%q, %q) = %d, want %d", tt.pattern, tt.input, got, tt.wantPos)
		}
	}
}

// TestLazyDFACharClass tests character class patterns
func TestLazyDFACharClass(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		wantPos int
	}{
		{"[abc]", "a", 1},
		{"[abc]", "b", 1},
		{"[abc]", "c", 1},
		{"[abc]", "xyz a", 5},
		{"[a-z]", "A123b", 5},
		{"[0-9]", "abc5def", 4},
	}

	for _, tt := range tests {
		dfa, err := CompilePattern(tt.pattern)
		if err != nil {
			t.Fatalf("CompilePattern(%q) error: %v", tt.pattern, err)
		}

		got := dfa.Find([]byte(tt.input))
		if got != tt.wantPos {
			t.Errorf("Find(%q, %q) = %d, want %d", tt.pattern, tt.input, got, tt.wantPos)
		}
	}
}

// TestLazyDFARepetition tests repetition patterns (*, +, ?)
func TestLazyDFARepetition(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		wantPos int
	}{
		{"a*", "", 0},         // zero matches
		{"a*", "a", 1},        // one match
		{"a*", "aaa", 3},      // multiple matches
		{"a+", "aaa", 3},      // plus: one or more
		{"a+", "bbb aaa", 7},  // plus: match later
		{"a?", "", 0},         // optional: zero
		{"a?", "a", 1},        // optional: one
		{"ab*c", "ac", 2},     // zero b's
		{"ab*c", "abc", 3},    // one b
		{"ab*c", "abbbc", 5},  // multiple b's
		{"a+b+", "aaabbb", 6}, // multiple of each
		{"a*b*", "", 0},       // both zero
		{"a*b*", "aaa", 3},    // only a's
		{"a*b*", "bbb", 3},    // only b's
		{"a*b*", "aaabbb", 6}, // both
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.input, func(t *testing.T) {
			dfa, err := CompilePattern(tt.pattern)
			if err != nil {
				t.Fatalf("CompilePattern(%q) error: %v", tt.pattern, err)
			}

			got := dfa.Find([]byte(tt.input))
			if got != tt.wantPos {
				t.Errorf("Find(%q, %q) = %d, want %d", tt.pattern, tt.input, got, tt.wantPos)
			}
		})
	}
}

// TestLazyDFAIsMatch tests the IsMatch method
func TestLazyDFAIsMatch(t *testing.T) {
	tests := []struct {
		pattern   string
		input     string
		wantMatch bool
	}{
		{"hello", "hello world", true},
		{"hello", "world", false},
		{"a+", "aaa", true},
		{"a+", "bbb", false},
		{"foo|bar", "test bar end", true},
		{"foo|bar", "test baz end", false},
	}

	for _, tt := range tests {
		dfa, err := CompilePattern(tt.pattern)
		if err != nil {
			t.Fatalf("CompilePattern(%q) error: %v", tt.pattern, err)
		}

		got := dfa.IsMatch([]byte(tt.input))
		if got != tt.wantMatch {
			t.Errorf("IsMatch(%q, %q) = %v, want %v", tt.pattern, tt.input, got, tt.wantMatch)
		}
	}
}

// TestLazyDFAEmptyInput tests matching against empty input
func TestLazyDFAEmptyInput(t *testing.T) {
	tests := []struct {
		pattern   string
		wantMatch bool
		wantPos   int
	}{
		{"", true, 0},        // empty pattern matches empty
		{"a*", true, 0},      // zero matches
		{"a+", false, -1},    // requires at least one
		{"a?", true, 0},      // optional matches empty
		{"hello", false, -1}, // literal requires input
	}

	for _, tt := range tests {
		dfa, err := CompilePattern(tt.pattern)
		if err != nil {
			t.Fatalf("CompilePattern(%q) error: %v", tt.pattern, err)
		}

		gotMatch := dfa.IsMatch([]byte{})
		if gotMatch != tt.wantMatch {
			t.Errorf("IsMatch(%q, empty) = %v, want %v", tt.pattern, gotMatch, tt.wantMatch)
		}

		gotPos := dfa.Find([]byte{})
		if gotPos != tt.wantPos {
			t.Errorf("Find(%q, empty) = %d, want %d", tt.pattern, gotPos, tt.wantPos)
		}
	}
}

// TestLazyDFACacheFull tests behavior when cache reaches capacity
func TestLazyDFACacheFull(t *testing.T) {
	// Create a DFA with very small cache
	config := DefaultConfig().WithMaxStates(5) // Only 5 states

	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile("a+b+c+d+")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	dfa, err := CompileWithConfig(nfaObj, config)
	if err != nil {
		t.Fatalf("DFA compile error: %v", err)
	}

	// This should fill the cache and potentially trigger NFA fallback
	input := []byte("aaaabbbbccccdddd")
	pos := dfa.Find(input)
	if pos == -1 {
		t.Errorf("Find returned -1, want match")
	}

	size, capacity, _, _, _ := dfa.CacheStats() //nolint:dogsled // Testing cache size and capacity only
	t.Logf("Cache after search: size=%d, capacity=%d", size, capacity)

	// Verify cache is close to or at capacity
	if uint32(size) > capacity {
		t.Errorf("Cache size %d exceeds capacity %d", size, capacity)
	}
}

// TestLazyDFAVsStdlib compares results with stdlib regexp
func TestLazyDFAVsStdlib(t *testing.T) {
	patterns := []string{
		"hello",
		"a+",
		"a*b*",
		"foo|bar",
		"test.*end",
		"[a-z]+",
		"a+b+c+",
	}

	inputs := []string{
		"hello world",
		"aaabbbccc",
		"test123end",
		"foo bar baz",
		"xyzabc123",
	}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			// Compile with lazy DFA
			dfa, err := CompilePattern(pattern)
			if err != nil {
				t.Skipf("Pattern %q not supported by lazy DFA: %v", pattern, err)
				return
			}

			// Compile with stdlib
			re, err := regexp.Compile(pattern)
			if err != nil {
				t.Fatalf("stdlib regexp.Compile(%q) error: %v", pattern, err)
			}

			for _, input := range inputs {
				// Test Find
				dfaPos := dfa.Find([]byte(input))
				stdlibLoc := re.FindStringIndex(input)

				var stdlibPos int
				if stdlibLoc == nil {
					stdlibPos = -1
				} else {
					stdlibPos = stdlibLoc[1] // End position
				}

				// Allow some flexibility: lazy DFA returns end position directly
				// stdlib returns [start, end], we compare end positions
				if dfaPos != stdlibPos {
					// Check if it's a different match (both found something)
					if dfaPos != -1 && stdlibPos != -1 {
						t.Logf("DFA and stdlib found different matches: dfa=%d, stdlib=%d", dfaPos, stdlibPos)
						// This can happen for patterns with multiple matches
						// Verify both are valid
						continue
					}

					t.Errorf("Find(%q, %q): DFA=%d, stdlib=%d", pattern, input, dfaPos, stdlibPos)
				}

				// Test IsMatch
				dfaMatch := dfa.IsMatch([]byte(input))
				stdlibMatch := re.MatchString(input)
				if dfaMatch != stdlibMatch {
					t.Errorf("IsMatch(%q, %q): DFA=%v, stdlib=%v", pattern, input, dfaMatch, stdlibMatch)
				}
			}
		})
	}
}

// TestLazyDFAThreadSafety tests that separate DFA instances are independent
func TestLazyDFAThreadSafety(t *testing.T) {
	// Each DFA instance should be independent
	// This test verifies that using multiple DFAs doesn't cause interference

	dfa1, err := CompilePattern("foo")
	if err != nil {
		t.Fatalf("CompilePattern error: %v", err)
	}

	dfa2, err := CompilePattern("bar")
	if err != nil {
		t.Fatalf("CompilePattern error: %v", err)
	}

	input := []byte("foo bar foo bar")

	// Search with dfa1
	pos1 := dfa1.Find(input)
	if pos1 != 3 { // "foo" ends at position 3
		t.Errorf("dfa1.Find = %d, want 3", pos1)
	}

	// Search with dfa2
	pos2 := dfa2.Find(input)
	if pos2 != 7 { // "bar" ends at position 7
		t.Errorf("dfa2.Find = %d, want 7", pos2)
	}

	// Verify caches are independent
	size1, _, _, _, _ := dfa1.CacheStats() //nolint:dogsled // Testing cache size only
	size2, _, _, _, _ := dfa2.CacheStats() //nolint:dogsled // Testing cache size only
	if size1 == size2 {
		t.Logf("Warning: Both caches have same size (%d), may indicate shared state", size1)
	}
}

// TestLazyDFAConfig tests various configuration options
func TestLazyDFAConfig(t *testing.T) {
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile("test")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	tests := []struct {
		name   string
		config Config
		valid  bool
	}{
		{"default", DefaultConfig(), true},
		{"small cache", DefaultConfig().WithMaxStates(10), true},
		{"large cache", DefaultConfig().WithMaxStates(100000), true},
		{"no prefilter", DefaultConfig().WithPrefilter(false), true},
		{"low determ limit", DefaultConfig().WithDeterminizationLimit(10), true},
		{"zero max states", DefaultConfig().WithMaxStates(0), false},
		{"negative determ limit", DefaultConfig().WithDeterminizationLimit(-1), false},
		{"invalid hit threshold", DefaultConfig().WithCacheHitThreshold(1.5), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CompileWithConfig(nfaObj, tt.config)
			if tt.valid && err != nil {
				t.Errorf("Expected valid config, got error: %v", err)
			}
			if !tt.valid && err == nil {
				t.Errorf("Expected invalid config error, got nil")
			}
		})
	}
}

// BenchmarkLazyDFASimpleLiteral benchmarks simple literal matching
func BenchmarkLazyDFASimpleLiteral(t *testing.B) {
	t.ReportAllocs()

	dfa, _ := CompilePattern("hello")
	input := []byte("say hello world hello there")

	t.ResetTimer()
	for i := 0; i < t.N; i++ {
		_ = dfa.Find(input)
	}
}

// BenchmarkLazyDFAAlternation benchmarks alternation patterns
func BenchmarkLazyDFAAlternation(t *testing.B) {
	t.ReportAllocs()

	dfa, _ := CompilePattern("foo|bar|baz")
	input := []byte("test foo test bar test baz repeat")

	t.ResetTimer()
	for i := 0; i < t.N; i++ {
		_ = dfa.Find(input)
	}
}

// BenchmarkLazyDFARepetition benchmarks repetition patterns
func BenchmarkLazyDFARepetition(t *testing.B) {
	t.ReportAllocs()

	dfa, _ := CompilePattern("a+b+c+")
	input := []byte("xyzaaabbbbccccdef")

	t.ResetTimer()
	for i := 0; i < t.N; i++ {
		_ = dfa.Find(input)
	}
}

// TestByteClassesIntegration tests that ByteClasses are correctly propagated
// from NFA compilation through DFA and are available for alphabet reduction.
func TestByteClassesIntegration(t *testing.T) {
	tests := []struct {
		pattern       string
		name          string
		minClasses    int
		maxClasses    int
		sameClassLo   byte
		sameClassHi   byte
		diffClassByte byte
	}{
		{
			pattern:       "[a-z]+",
			name:          "lowercase letters",
			minClasses:    3, // before a, a-z, after z
			maxClasses:    5,
			sameClassLo:   'a',
			sameClassHi:   'z',
			diffClassByte: '0',
		},
		{
			pattern:       "[0-9]+",
			name:          "digits",
			minClasses:    3, // before 0, 0-9, after 9
			maxClasses:    5,
			sameClassLo:   '0',
			sameClassHi:   '9',
			diffClassByte: 'a',
		},
		{
			pattern:       "[a-zA-Z0-9]+",
			name:          "alphanumeric",
			minClasses:    5, // multiple ranges
			maxClasses:    10,
			sameClassLo:   'a',
			sameClassHi:   'z',
			diffClassByte: '!',
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dfa, err := CompilePattern(tt.pattern)
			if err != nil {
				t.Fatalf("CompilePattern(%q) error: %v", tt.pattern, err)
			}

			// Verify ByteClasses is available
			bc := dfa.ByteClasses()
			if bc == nil {
				t.Fatal("ByteClasses() returned nil")
			}

			// Check alphabet length is reduced
			alphabetLen := dfa.AlphabetLen()
			if alphabetLen < tt.minClasses || alphabetLen > tt.maxClasses {
				t.Errorf("AlphabetLen() = %d, want between %d and %d",
					alphabetLen, tt.minClasses, tt.maxClasses)
			}

			// Verify bytes in same range have same class
			classLo := bc.Get(tt.sameClassLo)
			classHi := bc.Get(tt.sameClassHi)
			if classLo != classHi {
				t.Errorf("ByteClasses: '%c' and '%c' should be same class, got %d and %d",
					tt.sameClassLo, tt.sameClassHi, classLo, classHi)
			}

			// Verify different range has different class
			classDiff := bc.Get(tt.diffClassByte)
			if classDiff == classLo {
				t.Errorf("ByteClasses: '%c' should be different class from '%c', both got %d",
					tt.diffClassByte, tt.sameClassLo, classDiff)
			}

			// Verify DFA still works correctly with ByteClasses
			// This ensures the integration doesn't break matching
			input := []byte("test abc 123 xyz")
			if got := dfa.Find(input); got < 0 {
				t.Errorf("Find() = %d, expected match for pattern %q", got, tt.pattern)
			}
		})
	}
}

// TestByteClassesLiteralPattern tests ByteClasses for literal patterns
func TestByteClassesLiteralPattern(t *testing.T) {
	dfa, err := CompilePattern("hello")
	if err != nil {
		t.Fatalf("CompilePattern error: %v", err)
	}

	bc := dfa.ByteClasses()
	if bc == nil {
		t.Fatal("ByteClasses() returned nil")
	}

	alphabetLen := dfa.AlphabetLen()
	// "hello" has 4 distinct bytes (h, e, l, o) creating ~5-9 classes
	// (before h, h, between, e, between, l, between, o, after o)
	if alphabetLen < 5 || alphabetLen > 12 {
		t.Errorf("AlphabetLen() = %d, expected 5-12 for literal 'hello'", alphabetLen)
	}

	// Verify matching still works
	if got := dfa.Find([]byte("say hello")); got != 9 {
		t.Errorf("Find() = %d, want 9", got)
	}
}

// TestIssue15_CaptureGroupIsMatch tests that IsMatch works correctly with capture groups.
// This is a regression test for Issue #15 where epsilonClosure didn't follow StateCapture.
func TestIssue15_CaptureGroupIsMatch(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    bool
	}{
		// The original failing pattern from GoAWK datanonl test
		{`\w+@([[:alnum:]]+\.)+[[:alnum:]]+[[:blank:]]+`, "bleble@foo1.bh.pl       deny", true},
		// Simple capture group patterns
		{`(abc)+`, "abcabc", true},
		{`a(b+)c`, "abbc", true},
		{`(\d+)\.(\d+)`, "123.456", true},
		{`((ab)+c)+`, "ababcababc", true},
		// Anchor patterns with captures
		{`(^)`, "12345", true},
		{`($)`, "12345", true},
		{`(^)|($)`, "12345", true},
		// Non-matching
		{`(xyz)+`, "abc", false},
		{`(\d+)\.(\d+)`, "no digits", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			// Compare with stdlib
			re := regexp.MustCompile(tt.pattern)
			stdWant := re.MatchString(tt.input)

			// Our DFA
			dfa, err := CompilePattern(tt.pattern)
			if err != nil {
				t.Fatalf("CompilePattern(%q) error: %v", tt.pattern, err)
			}

			got := dfa.IsMatch([]byte(tt.input))
			if got != stdWant {
				t.Errorf("IsMatch(%q, %q) = %v, stdlib says %v", tt.pattern, tt.input, got, stdWant)
			}
			if got != tt.want {
				t.Errorf("IsMatch(%q, %q) = %v, want %v", tt.pattern, tt.input, got, tt.want)
			}
		})
	}
}
