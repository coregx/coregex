package meta

// Tests for multiline reverse suffix strategy
// including prefix verification.

import (
	"regexp"
	"regexp/syntax"
	"testing"
)

func TestMultilineReverseSuffix_FindAll(t *testing.T) {
	pattern := `(?m)^/.*\.php`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	requireStrategy(t, engine, UseMultilineReverseSuffix)

	re := regexp.MustCompile(pattern)
	haystack := "/index.php\n/admin/dashboard.php\n/static/style.css\n/api/handler.php"

	count := engine.Count([]byte(haystack), -1)
	stdCount := len(re.FindAllString(haystack, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d", count, stdCount)
	}

	// FindAt at non-zero
	m := engine.FindAt([]byte(haystack), 11)
	if m != nil {
		t.Logf("FindAt(11): %q", m.String())
	}
}

// -----------------------------------------------------------------------------
// 12. Find() coverage for reverse strategies (75% -> higher)
// -----------------------------------------------------------------------------

func TestMultilineReverseSuffix_VerifyPrefix(t *testing.T) {
	// Test various multiline patterns to exercise verifyPrefix branches
	patterns := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{"basic_match", `(?m)^/.*\.php`, "/index.php", true},
		{"with_newline", `(?m)^/.*\.php`, "text\n/admin.php", true},
		{"no_match", `(?m)^/.*\.php`, "admin.php", false}, // no leading /
		{"empty_line", `(?m)^/.*\.php`, "\n/test.php", true},
		{"multiple_lines", `(?m)^/.*\.php`, "/a.txt\n/b.php\n/c.css", true},
	}

	for _, tt := range patterns {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}
			requireStrategy(t, engine, UseMultilineReverseSuffix)

			re := regexp.MustCompile(tt.pattern)
			got := engine.IsMatch([]byte(tt.input))
			stdGot := re.MatchString(tt.input)
			if got != stdGot {
				t.Errorf("IsMatch(%q) = %v, stdlib = %v", tt.input, got, stdGot)
			}
		})
	}
}

// =============================================================================
// Wave 4C: Additional coverage push tests (76.6% -> 80%+)
// Targeting the remaining low-coverage functions with the highest statement counts.
// =============================================================================

// -----------------------------------------------------------------------------
// 28. findIndicesBoundedBacktrackerAtWithState (27.5%) — V12 windowed fallback
//     and anchoredFirstBytes early rejection.
//     This function is called internally by FindAll/Count for BT patterns.
// -----------------------------------------------------------------------------

func TestMultilineReverseSuffix_VerifyPrefix_Detailed(t *testing.T) {
	pattern := `(?m)^/api/.*\.json`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	requireStrategy(t, engine, UseMultilineReverseSuffix)

	re := regexp.MustCompile(pattern)

	tests := []struct {
		name  string
		input string
	}{
		{"match_single", "/api/users.json"},
		{"match_multi_line", "header\n/api/data.json\nfooter"},
		{"no_prefix", "/web/page.json"},
		{"no_suffix", "/api/users.xml"},
		{"empty", ""},
		{"prefix_at_end", "text\n/api/x.json"},
		{"many_lines", "/api/a.json\n/api/b.json\n/api/c.json"},
		{"prefix_too_short", "/ap"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Find
			match := engine.Find([]byte(tt.input))
			stdMatch := re.FindString(tt.input)
			if (match != nil) != (stdMatch != "") {
				t.Errorf("Find(%q): got=%v, stdlib=%v", tt.input, match != nil, stdMatch != "")
			}

			// IsMatch
			got := engine.IsMatch([]byte(tt.input))
			stdGot := re.MatchString(tt.input)
			if got != stdGot {
				t.Errorf("IsMatch(%q) = %v, stdlib = %v", tt.input, got, stdGot)
			}

			// FindIndices
			s, e, found := engine.FindIndices([]byte(tt.input))
			stdLoc := re.FindStringIndex(tt.input)
			if found != (stdLoc != nil) {
				t.Errorf("FindIndices(%q): found=%v, stdlib=%v", tt.input, found, stdLoc != nil)
			}
			if found && stdLoc != nil {
				if s != stdLoc[0] || e != stdLoc[1] {
					t.Errorf("FindIndices(%q): got [%d,%d], stdlib [%d,%d]", tt.input, s, e, stdLoc[0], stdLoc[1])
				}
			}
		})
	}
}

func TestMultilineReverseSuffix_FindAll_Extended(t *testing.T) {
	pattern := `(?m)^/.*\.php`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	requireStrategy(t, engine, UseMultilineReverseSuffix)

	re := regexp.MustCompile(pattern)

	// Multiple matches across lines
	input := "/index.php\n/admin.php\nstatic/style.css\n/api.php"
	count := engine.Count([]byte(input), -1)
	stdCount := len(re.FindAllString(input, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d", count, stdCount)
	}

	// FindAll
	indices := engine.FindAllIndicesStreaming([]byte(input), 0, nil)
	stdAll := re.FindAllStringIndex(input, -1)
	if len(indices) != len(stdAll) {
		t.Errorf("FindAll: got %d, stdlib %d", len(indices), len(stdAll))
	}
}

// -----------------------------------------------------------------------------
// 34. isSafeForReverseInner (61.5%) — exercise OpCapture and CharClass branches.
// -----------------------------------------------------------------------------

func TestIsSafeForMultilineReverseSuffix_Patterns(t *testing.T) {
	// Exercise isSafeForMultilineReverseSuffix with various multiline patterns
	patterns := []struct {
		name    string
		pattern string
	}{
		{"basic", `(?m)^/.*\.php`},
		{"with_charclass", `(?m)^[A-Z].*\.log`},
		{"with_plus", `(?m)^/.+\.txt`},
		{"with_capture", `(?m)^(GET|POST) .*`},
	}

	for _, tt := range patterns {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s: strategy=%s", tt.name, engine.Strategy())
			// Exercise the strategy selection path
			re := regexp.MustCompile(tt.pattern)
			input := "/test.php"
			got := engine.IsMatch([]byte(input))
			std := re.MatchString(input)
			if got != std {
				t.Errorf("IsMatch(%q) = %v, stdlib = %v", input, got, std)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 62. isCharClassPlus (60%) — exercise through anchored literal detection.
// -----------------------------------------------------------------------------

func TestIsSafeForMultilineReverseSuffix_Branches(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    bool
	}{
		// Multiline with wildcard + suffix - safe
		{"multiline_suffix", `(?m)^.*\.php`, true},
		// Non-multiline - not safe
		{"non_multiline", `.*\.php`, false},
		// Multiline without wildcard - not safe
		{"multiline_no_wildcard", `(?m)^hello`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatal(err)
			}
			got := isSafeForMultilineReverseSuffix(re)
			if got != tt.want {
				t.Errorf("isSafeForMultilineReverseSuffix(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}

	// Direct AST tests for OpCapture wrapping
	t.Run("capture_wrapping_safe", func(t *testing.T) {
		re, err := syntax.Parse(`(?m)^.*\.php`, syntax.Perl)
		if err != nil {
			t.Fatal(err)
		}
		// Wrap in capture
		wrapped := &syntax.Regexp{
			Op:  syntax.OpCapture,
			Sub: []*syntax.Regexp{re},
		}
		got := isSafeForMultilineReverseSuffix(wrapped)
		if !got {
			t.Error("expected true for capture-wrapped safe multiline pattern")
		}
	})

	// OpCapture with no sub
	t.Run("capture_no_sub", func(t *testing.T) {
		re := &syntax.Regexp{
			Op:  syntax.OpCapture,
			Sub: []*syntax.Regexp{},
		}
		got := isSafeForMultilineReverseSuffix(re)
		if got {
			t.Error("expected false for capture with no sub")
		}
	})

	// Non-concat, non-capture
	t.Run("literal", func(t *testing.T) {
		re := &syntax.Regexp{Op: syntax.OpLiteral, Rune: []rune{'a'}}
		got := isSafeForMultilineReverseSuffix(re)
		if got {
			t.Error("expected false for literal")
		}
	})
}

// --- Test 77: isDigitRunSkipSafe OpRepeat and OpCapture branches ---
// Covers: strategy.go isDigitRunSkipSafe lines 530-560
// Targets: OpRepeat with Max==-1, OpCapture wrapping, nil check

func TestFindIndicesMultilineReverseSuffixAt_Through_FindAll(t *testing.T) {
	pattern := `(?m)^.*\.php`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseMultilineReverseSuffix {
		t.Skipf("Strategy is %s, not UseMultilineReverseSuffix", engine.Strategy())
	}

	input := "/var/www/index.php\n/var/www/admin.php\n/var/www/README.md"
	results := engine.FindAllIndicesStreaming([]byte(input), 0, nil)
	re := regexp.MustCompile(pattern)
	stdAll := re.FindAllStringIndex(input, -1)
	if len(results) != len(stdAll) {
		t.Errorf("count: coregex=%d, stdlib=%d", len(results), len(stdAll))
	}
}

// --- Test 104: ReverseAnchored FindIndicesAt (through findIndicesReverseAnchoredAt) ---
// Covers: find_indices.go various reverse dispatch functions at position 0
