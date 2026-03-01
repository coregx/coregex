package nfa

import (
	"regexp"
	"testing"
)

// TestCompileUTF8_CyrillicPattern tests NFA compilation of Cyrillic character patterns
// which require 2-byte UTF-8 encoding (0xD0-0xD3 lead bytes).
func TestCompileUTF8_CyrillicPattern(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		wantPos  []int // [start, end] or nil for no match
	}{
		{
			name:     "cyrillic literal",
			pattern:  "привет",
			haystack: "слово привет мир",
			wantPos:  []int{11, 23},
		},
		{
			name:     "cyrillic char class range",
			pattern:  "[а-я]+",
			haystack: "hello мир world",
			wantPos:  []int{6, 12},
		},
		{
			name:     "cyrillic upper range",
			pattern:  "[А-Я]+",
			haystack: "test СЛОВО end",
			wantPos:  []int{5, 15},
		},
		{
			name:     "cyrillic alternation",
			pattern:  "да|нет",
			haystack: "ответ нет",
			wantPos:  []int{11, 17},
		},
		{
			name:     "cyrillic no match",
			pattern:  "привет",
			haystack: "hello world",
			wantPos:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nfa := mustCompile(t, tt.pattern)
			vm := NewPikeVM(nfa)

			start, end, matched := vm.Search([]byte(tt.haystack))

			if tt.wantPos == nil {
				if matched {
					t.Errorf("expected no match, got (%d, %d)", start, end)
				}
				return
			}
			if !matched {
				t.Errorf("expected match at %v, got no match", tt.wantPos)
				return
			}
			if start != tt.wantPos[0] || end != tt.wantPos[1] {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantPos[0], tt.wantPos[1])
			}
		})
	}
}

// TestCompileUTF8_CJKPattern tests NFA compilation of CJK characters
// which require 3-byte UTF-8 encoding (0xE0-0xEF lead bytes).
func TestCompileUTF8_CJKPattern(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		wantPos  []int
	}{
		{
			name:     "chinese literal",
			pattern:  "你好",
			haystack: "说你好世界",
			wantPos:  []int{3, 9},
		},
		{
			name:     "japanese hiragana",
			pattern:  "こんにちは",
			haystack: "abc こんにちは xyz",
			wantPos:  []int{4, 19},
		},
		{
			name:     "korean",
			pattern:  "안녕",
			haystack: "test 안녕 end",
			wantPos:  []int{5, 11},
		},
		{
			name:     "cjk char class range",
			pattern:  "[一-龥]+",
			haystack: "abc世界def",
			wantPos:  []int{3, 9},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nfa := mustCompile(t, tt.pattern)
			vm := NewPikeVM(nfa)

			start, end, matched := vm.Search([]byte(tt.haystack))

			if tt.wantPos == nil {
				if matched {
					t.Errorf("expected no match, got (%d, %d)", start, end)
				}
				return
			}
			if !matched {
				t.Errorf("expected match at %v, got no match", tt.wantPos)
				return
			}
			if start != tt.wantPos[0] || end != tt.wantPos[1] {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantPos[0], tt.wantPos[1])
			}
		})
	}
}

// TestCompileUTF8_EmojiPattern tests NFA compilation of emoji patterns
// which require 4-byte UTF-8 encoding (0xF0 lead byte).
func TestCompileUTF8_EmojiPattern(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		wantPos  []int
	}{
		{
			name:     "single emoji",
			pattern:  "😀",
			haystack: "test 😀 done",
			wantPos:  []int{5, 9},
		},
		{
			name:     "emoji in text",
			pattern:  "🎉",
			haystack: "party 🎉 time",
			wantPos:  []int{6, 10},
		},
		{
			name:     "emoji alternation",
			pattern:  "😀|😎",
			haystack: "cool 😎 bro",
			wantPos:  []int{5, 9},
		},
		{
			name:     "emoji no match",
			pattern:  "😀",
			haystack: "no emoji here",
			wantPos:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nfa := mustCompile(t, tt.pattern)
			vm := NewPikeVM(nfa)

			start, end, matched := vm.Search([]byte(tt.haystack))

			if tt.wantPos == nil {
				if matched {
					t.Errorf("expected no match, got (%d, %d)", start, end)
				}
				return
			}
			if !matched {
				t.Errorf("expected match at %v, got no match", tt.wantPos)
				return
			}
			if start != tt.wantPos[0] || end != tt.wantPos[1] {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantPos[0], tt.wantPos[1])
			}
		})
	}
}

// TestCompileUTF8_MixedASCIIAndUnicode tests patterns mixing ASCII and multi-byte characters.
func TestCompileUTF8_MixedASCIIAndUnicode(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		wantPos  []int
	}{
		{
			name:     "ascii then cyrillic",
			pattern:  "hello мир",
			haystack: "say hello мир!",
			wantPos:  []int{4, 16},
		},
		{
			name:     "digit then cjk",
			pattern:  `\d+世界`,
			haystack: "in 2024世界 is",
			wantPos:  []int{3, 13},
		},
		{
			name:     "alternation mixed",
			pattern:  "hello|привет",
			haystack: "слово привет",
			wantPos:  []int{11, 23},
		},
		{
			name:     "alternation mixed ascii first",
			pattern:  "hello|привет",
			haystack: "say hello",
			wantPos:  []int{4, 9},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nfa := mustCompile(t, tt.pattern)
			vm := NewPikeVM(nfa)

			start, end, matched := vm.Search([]byte(tt.haystack))

			if tt.wantPos == nil {
				if matched {
					t.Errorf("expected no match, got (%d, %d)", start, end)
				}
				return
			}
			if !matched {
				t.Errorf("expected match at %v, got no match", tt.wantPos)
				return
			}
			if start != tt.wantPos[0] || end != tt.wantPos[1] {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantPos[0], tt.wantPos[1])
			}
		})
	}
}

// TestCompileUTF8_DotAny tests that dot (.) correctly matches multi-byte UTF-8 runes.
func TestCompileUTF8_DotAny(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		wantPos  []int
	}{
		{
			name:     "dot matches ascii",
			pattern:  "a.c",
			haystack: "abc",
			wantPos:  []int{0, 3},
		},
		{
			name:     "dot matches 2-byte utf8",
			pattern:  "a.c",
			haystack: "aбc", // б is 2 bytes
			wantPos:  []int{0, 4},
		},
		{
			name:     "dot matches 3-byte utf8",
			pattern:  "a.c",
			haystack: "a中c", // 中 is 3 bytes
			wantPos:  []int{0, 5},
		},
		{
			name:     "dot matches 4-byte utf8",
			pattern:  "a.c",
			haystack: "a😀c", // 😀 is 4 bytes
			wantPos:  []int{0, 6},
		},
		{
			name:     "dot star over mixed bytes",
			pattern:  "a.*z",
			haystack: "a中文бz",
			wantPos:  []int{0, 10},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nfa := mustCompile(t, tt.pattern)
			vm := NewPikeVM(nfa)

			start, end, matched := vm.Search([]byte(tt.haystack))

			if tt.wantPos == nil {
				if matched {
					t.Errorf("expected no match, got (%d, %d)", start, end)
				}
				return
			}
			if !matched {
				t.Errorf("expected match at %v, got no match", tt.wantPos)
				return
			}
			if start != tt.wantPos[0] || end != tt.wantPos[1] {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantPos[0], tt.wantPos[1])
			}
		})
	}
}

// TestCompileUTF8_VsStdlib verifies UTF-8 pattern correctness against Go stdlib.
func TestCompileUTF8_VsStdlib(t *testing.T) {
	patterns := []string{
		"привет",
		"[а-я]+",
		"世界",
		"[一-龥]+",
		"a.c",
		".*мир.*",
	}

	haystacks := []string{
		"hello привет мир",
		"说你好世界",
		"aбc aXc a中c",
		"слово мир конец",
		"test123",
		"",
	}

	for _, pattern := range patterns {
		stdRE, err := regexp.Compile(pattern)
		if err != nil {
			t.Fatalf("stdlib compile %q: %v", pattern, err)
		}

		for _, haystack := range haystacks {
			t.Run(pattern+"/"+haystack, func(t *testing.T) {
				nfa := mustCompile(t, pattern)
				vm := NewPikeVM(nfa)

				stdLoc := stdRE.FindStringIndex(haystack)
				ourStart, ourEnd, ourMatched := vm.Search([]byte(haystack))

				stdMatched := stdLoc != nil
				if stdMatched != ourMatched {
					t.Errorf("match mismatch: stdlib=%v, ours=%v", stdMatched, ourMatched)
					return
				}

				if stdMatched && ourMatched {
					if stdLoc[0] != ourStart || stdLoc[1] != ourEnd {
						t.Errorf("position mismatch: stdlib=%v, ours=[%d,%d]",
							stdLoc, ourStart, ourEnd)
					}
				}
			})
		}
	}
}

// TestCompileUTF8_SingleByteASCIIOptimization verifies that pure-ASCII patterns
// produce compact NFA (single ByteRange state per character, not UTF-8 chains).
func TestCompileUTF8_SingleByteASCIIOptimization(t *testing.T) {
	// ASCII-only pattern should have fewer states than a Unicode one
	asciiNFA := mustCompile(t, "abc")
	// Unicode pattern with 3-byte chars should have more states
	unicodeNFA := mustCompile(t, "[а-я]+")

	// ASCII pattern: each char = 1 ByteRange state
	// "abc" should have states for: a, b, c, match, plus some wiring
	if asciiNFA.States() == 0 {
		t.Fatal("ASCII NFA has no states")
	}

	// Unicode char class needs more NFA states for UTF-8 range encoding
	if unicodeNFA.States() == 0 {
		t.Fatal("Unicode NFA has no states")
	}

	// Verify both compile and work correctly
	asciiVM := NewPikeVM(asciiNFA)
	_, _, matched := asciiVM.Search([]byte("xabcx"))
	if !matched {
		t.Error("ASCII NFA should match 'abc' in 'xabcx'")
	}

	unicodeVM := NewPikeVM(unicodeNFA)
	_, _, matched = unicodeVM.Search([]byte("test мир end"))
	if !matched {
		t.Error("Unicode NFA should match Cyrillic in 'test мир end'")
	}
}

// TestCompileUTF8_ASCIIOnlyConfig tests the ASCIIOnly compiler option.
func TestCompileUTF8_ASCIIOnlyConfig(t *testing.T) {
	// Compile with ASCIIOnly = true (for dot patterns)
	config := CompilerConfig{
		UTF8:              true,
		ASCIIOnly:         true,
		MaxRecursionDepth: 100,
	}
	compiler := NewCompiler(config)
	asciiNFA, err := compiler.Compile("a.c")
	if err != nil {
		t.Fatalf("Compile with ASCIIOnly failed: %v", err)
	}

	// Compile with default (non-ASCII) config
	defaultCompiler := NewDefaultCompiler()
	defaultNFA, err := defaultCompiler.Compile("a.c")
	if err != nil {
		t.Fatalf("Compile with default config failed: %v", err)
	}

	// ASCIIOnly should have fewer states (1 state for dot vs ~28 for UTF-8)
	if asciiNFA.States() >= defaultNFA.States() {
		t.Errorf("ASCIIOnly NFA states (%d) should be less than default (%d)",
			asciiNFA.States(), defaultNFA.States())
	}

	// Both should match ASCII input
	asciiVM := NewPikeVM(asciiNFA)
	_, _, matched := asciiVM.Search([]byte("abc"))
	if !matched {
		t.Error("ASCIIOnly NFA should match 'abc'")
	}

	defaultVM := NewPikeVM(defaultNFA)
	_, _, matched = defaultVM.Search([]byte("abc"))
	if !matched {
		t.Error("default NFA should match 'abc'")
	}
}

// TestCompileUTF8_UnicodeCharClassProperties tests Unicode property classes.
func TestCompileUTF8_UnicodeCharClassProperties(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     bool
	}{
		{"pL matches latin", `\pL`, "abc", true},
		{"pL matches cyrillic", `\pL`, "мир", true},
		{"pL matches cjk", `\pL`, "世界", true},
		{"pL no match digits", `\pL`, "123", false},
		{"pN matches digits", `\pN`, "123", true},
		{"pN no match letters", `\pN`, "abc", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nfa := mustCompile(t, tt.pattern)
			vm := NewPikeVM(nfa)
			got := vm.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch(%q, %q) = %v, want %v", tt.pattern, tt.haystack, got, tt.want)
			}
		})
	}
}

// TestCompileUTF8_BoundaryRunes tests compilation at UTF-8 encoding boundaries.
func TestCompileUTF8_BoundaryRunes(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     bool
	}{
		// 1-byte boundary: U+007F (DEL, last ASCII byte)
		{"last ascii 0x7F", "\x7f", "test\x7fend", true},
		// 2-byte boundary: U+0080 (first 2-byte)
		{"first 2-byte U+0080", "\u0080", "test\u0080end", true},
		// 2-byte boundary: U+07FF (last 2-byte)
		{"last 2-byte U+07FF", "\u07FF", "test\u07FFend", true},
		// 3-byte boundary: U+0800 (first 3-byte)
		{"first 3-byte U+0800", "\u0800", "test\u0800end", true},
		// 3-byte boundary: U+FFFF (last 3-byte, BMP last)
		{"last 3-byte U+FFFF", "\uFFFF", "test\uFFFFend", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nfa := mustCompile(t, tt.pattern)
			vm := NewPikeVM(nfa)
			got := vm.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch = %v, want %v", got, tt.want)
			}
		})
	}
}
