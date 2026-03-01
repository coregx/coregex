package meta

// Tests for anchored literal detection and matching,
// including Unicode encoding and char class tables.

import (
	"regexp"
	"regexp/syntax"
	"testing"
)

func TestEncodeRuneToBytes_Unicode(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		// 2-byte UTF-8: Cyrillic (U+0400-U+04FF)
		{"cyrillic_match", `^/.*фото\.jpg$`, "/path/to/фото.jpg", true},
		{"cyrillic_nomatch", `^/.*фото\.jpg$`, "/path/to/photo.jpg", false},
		// 3-byte UTF-8: CJK (U+4E00-U+9FFF)
		{"cjk_match", `^.*文件\.txt$`, "some文件.txt", true},
		{"cjk_nomatch", `^.*文件\.txt$`, "somefile.txt", false},
		// Mixed ASCII and Unicode
		{"mixed_match", `^.*данные$`, "test данные", true},
		{"mixed_nomatch", `^.*данные$`, "test data", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			got := engine.IsMatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.input, got, tt.want)
			}

			re := regexp.MustCompile(tt.pattern)
			stdGot := re.MatchString(tt.input)
			if got != stdGot {
				t.Errorf("stdlib mismatch: ours=%v, stdlib=%v", got, stdGot)
			}
		})
	}
}

// TestWave4_EncodeRuneToBytes_Direct directly tests encodeRuneToBytes for
// all UTF-8 encoding lengths (1-byte, 2-byte, 3-byte, 4-byte).

func TestEncodeRuneToBytes_Direct(t *testing.T) {
	tests := []struct {
		name     string
		r        rune
		wantLen  int
		wantByte byte // first byte
	}{
		// 1-byte: ASCII
		{"ascii_a", 'a', 1, 'a'},
		{"ascii_zero", 0, 1, 0},
		{"ascii_max", 0x7F, 1, 0x7F},
		// 2-byte: U+0080-U+07FF
		{"two_byte_min", 0x80, 2, 0xC2},
		{"cyrillic", 0x0444, 2, 0xD1},
		{"two_byte_max", 0x07FF, 2, 0xDF},
		// 3-byte: U+0800-U+FFFF
		{"cjk", 0x6587, 3, 0xE6},
		{"three_byte_max", 0xFFFF, 3, 0xEF},
		// 4-byte: U+10000-U+10FFFF
		{"emoji", 0x1F600, 4, 0xF0},
		{"four_byte_max", 0x10FFFF, 4, 0xF4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, 4)
			n := encodeRuneToBytes(tt.r, buf)
			if n != tt.wantLen {
				t.Errorf("encodeRuneToBytes(%U) length = %d, want %d", tt.r, n, tt.wantLen)
			}
			if buf[0] != tt.wantByte {
				t.Errorf("encodeRuneToBytes(%U) first byte = 0x%02X, want 0x%02X", tt.r, buf[0], tt.wantByte)
			}

			// Verify round-trip
			decoded := decodeUTF8(buf[:n])
			if decoded != tt.r {
				t.Errorf("round-trip: encoded %U, decoded %U", tt.r, decoded)
			}
		})
	}
}

// decodeUTF8 is a test helper that decodes a UTF-8 byte sequence to a rune.

func TestAnchoredLiteral_CharClassBridge(t *testing.T) {
	// Patterns like ^prefix[charclass]+suffix$ exercise isCharClassPlus
	patterns := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{"word_bridge", `^/[\w-]+\.php$`, "/hello-world.php", true},
		{"word_bridge_no_match", `^/[\w-]+\.php$`, "/hello world.php", false},
		{"digit_bridge", `^v[\d]+\.[\d]+$`, "v1.0", true},
		{"digit_bridge_no_match", `^v[\d]+\.[\d]+$`, "version1.0", false},
		{"alpha_bridge", `^[a-zA-Z]+\d+$`, "abc123", true},
	}

	for _, tt := range patterns {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			re := regexp.MustCompile(tt.pattern)
			got := engine.IsMatch([]byte(tt.input))
			std := re.MatchString(tt.input)
			if got != std {
				t.Errorf("IsMatch(%q) = %v, stdlib = %v (strategy=%s)",
					tt.input, got, std, engine.Strategy())
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 63. Exercise findall.go more paths — FindAllIndicesStreaming with callback.
// -----------------------------------------------------------------------------

func TestBuildCharClassTable_Branches(t *testing.T) {
	// Non-charclass
	t.Run("non_charclass", func(t *testing.T) {
		re := &syntax.Regexp{Op: syntax.OpLiteral, Rune: []rune{'a'}}
		got := buildCharClassTable(re)
		if got != nil {
			t.Error("expected nil for non-charclass")
		}
	})

	// CharClass with range above 255
	t.Run("above_255", func(t *testing.T) {
		re := &syntax.Regexp{
			Op:   syntax.OpCharClass,
			Rune: []rune{0x100, 0x200}, // Range entirely above 255
		}
		got := buildCharClassTable(re)
		if got == nil {
			t.Fatal("expected non-nil table")
		}
		// All entries should be false since range is above 255
		for i := 0; i < 256; i++ {
			if got[i] {
				t.Errorf("table[%d] should be false for range above 255", i)
				break
			}
		}
	})

	// CharClass with range crossing 255 boundary
	t.Run("cross_255", func(t *testing.T) {
		re := &syntax.Regexp{
			Op:   syntax.OpCharClass,
			Rune: []rune{250, 260}, // Range crosses 255
		}
		got := buildCharClassTable(re)
		if got == nil {
			t.Fatal("expected non-nil table")
		}
		if !got[250] || !got[255] {
			t.Error("expected table[250] and table[255] to be true")
		}
	})
}

// --- Test 81: DetectAnchoredLiteral edge cases ---
// Covers: anchored_literal.go DetectAnchoredLiteral lines 66-163
// Targets: <3 subs, multiple wildcards, optional before wildcard, charclass bridge

func TestDetectAnchoredLiteral_Branches(t *testing.T) {
	// Pattern with charclass bridge: ^.*[a-z]+world$
	t.Run("charclass_bridge", func(t *testing.T) {
		re, err := syntax.Parse(`^.*[a-z]+world$`, syntax.Perl)
		if err != nil {
			t.Fatal(err)
		}
		info := DetectAnchoredLiteral(re)
		if info != nil {
			t.Logf("Detected: prefix=%q suffix=%q minLen=%d", info.Prefix, info.Suffix, info.MinLength)
		}
	})

	// Non-concat pattern
	t.Run("non_concat", func(t *testing.T) {
		re := &syntax.Regexp{Op: syntax.OpLiteral, Rune: []rune{'a'}}
		info := DetectAnchoredLiteral(re)
		if info != nil {
			t.Error("expected nil for non-concat")
		}
	})

	// Too few subs
	t.Run("too_few_subs", func(t *testing.T) {
		re := &syntax.Regexp{
			Op: syntax.OpConcat,
			Sub: []*syntax.Regexp{
				{Op: syntax.OpBeginText},
				{Op: syntax.OpEndText},
			},
		}
		info := DetectAnchoredLiteral(re)
		if info != nil {
			t.Error("expected nil for too few subs")
		}
	})

	// Pattern with prefix: ^prefix.*suffix$
	t.Run("with_prefix", func(t *testing.T) {
		re, err := syntax.Parse(`^prefix.*suffix$`, syntax.Perl)
		if err != nil {
			t.Fatal(err)
		}
		info := DetectAnchoredLiteral(re)
		if info != nil {
			t.Logf("Detected: prefix=%q suffix=%q minLen=%d", info.Prefix, info.Suffix, info.MinLength)
			if string(info.Prefix) != "prefix" {
				t.Errorf("prefix = %q, want 'prefix'", info.Prefix)
			}
			if string(info.Suffix) != "suffix" {
				t.Errorf("suffix = %q, want 'suffix'", info.Suffix)
			}
		}
	})
}

// --- Test 82: FindAllIndicesStreaming start-anchored pattern (capacity=1 path) ---
// Covers: findall.go findAllIndicesLoop lines 118-120 (isStartAnchored initCap=1)

func TestDetectAnchoredLiteral_MultipleWildcards(t *testing.T) {
	re, err := syntax.Parse(`^.*foo.*bar$`, syntax.Perl)
	if err != nil {
		t.Fatal(err)
	}
	info := DetectAnchoredLiteral(re)
	if info != nil {
		t.Logf("info: prefix=%q suffix=%q", info.Prefix, info.Suffix)
	} else {
		t.Log("nil (expected: multiple wildcards rejected)")
	}
}

// --- Test 112: DetectAnchoredLiteral with non-literal between start and wildcard ---
// Covers: anchored_literal.go lines 126-133 (optional before wildcard), 144-147 (non-charclass after wildcard)

func TestDetectAnchoredLiteral_NonLiteralBeforeWildcard(t *testing.T) {
	// Optional literal before wildcard: ^a?.*suffix$ -- the Quest sub has a literal
	re, err := syntax.Parse(`^a?.*suffix$`, syntax.Perl)
	if err != nil {
		t.Fatal(err)
	}
	info := DetectAnchoredLiteral(re)
	t.Logf("DetectAnchoredLiteral(^a?.*suffix$) = %v", info)

	// Non-charclass between wildcard and suffix: ^.*\d+suffix$
	re2, err := syntax.Parse(`^.*\d+suffix$`, syntax.Perl)
	if err != nil {
		t.Fatal(err)
	}
	info2 := DetectAnchoredLiteral(re2)
	t.Logf("DetectAnchoredLiteral(^.*\\d+suffix$) = %v", info2)
}

// --- Test 113: MatchAnchoredLiteral prefix length check ---
// Covers: anchored_literal.go line 305-307 (prefix check: len(input) < len(prefix))

func TestMatchAnchoredLiteral_PrefixTooShort(t *testing.T) {
	// Create an AnchoredLiteralInfo with a long prefix
	info := &AnchoredLiteralInfo{
		Prefix:    []byte("longprefix"),
		Suffix:    []byte("end"),
		MinLength: 13,
	}

	// Input shorter than prefix
	got := MatchAnchoredLiteral([]byte("lp"), info)
	if got {
		t.Error("expected false: input shorter than prefix")
	}

	// Input shorter than MinLength
	got2 := MatchAnchoredLiteral([]byte("short"), info)
	if got2 {
		t.Error("expected false: input shorter than MinLength")
	}
}

// --- Test 114: strategy.go analyzeLiterals empty literal check ---
// Covers: strategy.go analyzeLiterals line 1175 (empty literal bytes check)
