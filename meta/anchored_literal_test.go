package meta

import (
	"regexp"
	"regexp/syntax"
	"testing"
)

// TestDetectAnchoredLiteral tests the pattern detection for UseAnchoredLiteral strategy.
func TestDetectAnchoredLiteral(t *testing.T) {
	tests := []struct {
		pattern        string
		wantDetected   bool
		wantPrefix     string
		wantSuffix     string
		wantCharClass  bool
		wantWildcardMin int
		wantMinLength  int
	}{
		// Should be detected (eligible patterns)
		{
			pattern:        `^/.*[\w-]+\.php$`,
			wantDetected:   true,
			wantPrefix:     "/",
			wantSuffix:     ".php",
			wantCharClass:  true,
			wantWildcardMin: 0,
			wantMinLength:  6, // "/" + 0 + 1 + ".php"
		},
		{
			pattern:        `^/.*\.txt$`,
			wantDetected:   true,
			wantPrefix:     "/",
			wantSuffix:     ".txt",
			wantCharClass:  false,
			wantWildcardMin: 0,
			wantMinLength:  5, // "/" + 0 + 0 + ".txt"
		},
		{
			pattern:        `^.*\.log$`,
			wantDetected:   true,
			wantPrefix:     "",
			wantSuffix:     ".log",
			wantCharClass:  false,
			wantWildcardMin: 0,
			wantMinLength:  4, // "" + 0 + 0 + ".log"
		},
		{
			pattern:        `^prefix.*[a-z]+suffix$`,
			wantDetected:   true,
			wantPrefix:     "prefix",
			wantSuffix:     "suffix",
			wantCharClass:  true,
			wantWildcardMin: 0,
			wantMinLength:  13, // "prefix" + 0 + 1 + "suffix"
		},
		{
			pattern:        `^/.+\.php$`, // .+ instead of .*
			wantDetected:   true,
			wantPrefix:     "/",
			wantSuffix:     ".php",
			wantCharClass:  false,
			wantWildcardMin: 1,
			wantMinLength:  6, // "/" + 1 + 0 + ".php"
		},
		{
			pattern:        `^api/v1/.*\.json$`,
			wantDetected:   true,
			wantPrefix:     "api/v1/",
			wantSuffix:     ".json",
			wantCharClass:  false,
			wantWildcardMin: 0,
			wantMinLength:  12, // "api/v1/" + 0 + 0 + ".json"
		},

		// Should NOT be detected (ineligible patterns)
		{
			pattern:      `^/.*\.php`, // No end anchor
			wantDetected: false,
		},
		{
			pattern:      `/.*\.php$`, // No start anchor
			wantDetected: false,
		},
		{
			pattern:      `^/.*$`, // No suffix literal
			wantDetected: false,
		},
		{
			pattern:      `^/[\w]+\.php$`, // No .* wildcard
			wantDetected: false,
		},
		{
			pattern:      `foo|bar`, // Alternation, not Concat
			wantDetected: false,
		},
		{
			pattern:      `^.*$`, // No suffix literal
			wantDetected: false,
		},
		{
			pattern:      `^\.php$`, // No wildcard
			wantDetected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.pattern, func(t *testing.T) {
			re, err := syntax.Parse(tc.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			info := DetectAnchoredLiteral(re)

			if tc.wantDetected {
				if info == nil {
					t.Fatal("Expected pattern to be detected, got nil")
				}

				if string(info.Prefix) != tc.wantPrefix {
					t.Errorf("Prefix = %q, want %q", info.Prefix, tc.wantPrefix)
				}
				if string(info.Suffix) != tc.wantSuffix {
					t.Errorf("Suffix = %q, want %q", info.Suffix, tc.wantSuffix)
				}
				if (info.CharClassTable != nil) != tc.wantCharClass {
					t.Errorf("HasCharClass = %v, want %v", info.CharClassTable != nil, tc.wantCharClass)
				}
				if info.WildcardMin != tc.wantWildcardMin {
					t.Errorf("WildcardMin = %d, want %d", info.WildcardMin, tc.wantWildcardMin)
				}
				if info.MinLength != tc.wantMinLength {
					t.Errorf("MinLength = %d, want %d", info.MinLength, tc.wantMinLength)
				}
			} else {
				if info != nil {
					t.Errorf("Expected pattern to NOT be detected, got info: %+v", info)
				}
			}
		})
	}
}

// TestMatchAnchoredLiteral tests the fast matching algorithm.
func TestMatchAnchoredLiteral(t *testing.T) {
	// Test pattern: ^/.*[\w-]+\.php$
	phpInfo := &AnchoredLiteralInfo{
		Prefix:         []byte("/"),
		Suffix:         []byte(".php"),
		CharClassTable: buildWordHyphenTable(),
		CharClassMin:   1,
		WildcardMin:    0,
		MinLength:      6,
	}

	phpTests := []struct {
		input string
		want  bool
	}{
		// Should match
		{"/test.php", true},
		{"/a.php", true},
		{"/path/to/file.php", true},
		{"/path/to/admin/file.php", true},
		{"/x.php", true},
		{"/file-name.php", true},
		{"/file_name.php", true},
		{"/123.php", true},

		// Should NOT match
		{"", false},                     // Too short
		{".php", false},                 // No prefix
		{"/.php", false},                // No charclass before suffix
		{"/test.txt", false},            // Wrong suffix
		{"test.php", false},             // No prefix
		{"/", false},                    // Too short
		{"/test", false},                // No suffix
		{"///.php", false},              // No charclass (slashes don't match [\w-])
	}

	for _, tc := range phpTests {
		t.Run("php_"+tc.input, func(t *testing.T) {
			got := MatchAnchoredLiteral([]byte(tc.input), phpInfo)
			if got != tc.want {
				t.Errorf("MatchAnchoredLiteral(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}

	// Test pattern: ^.*\.log$ (no prefix, no charclass)
	logInfo := &AnchoredLiteralInfo{
		Prefix:         nil,
		Suffix:         []byte(".log"),
		CharClassTable: nil,
		CharClassMin:   0,
		WildcardMin:    0,
		MinLength:      4,
	}

	logTests := []struct {
		input string
		want  bool
	}{
		// Should match
		{".log", true},
		{"a.log", true},
		{"test.log", true},
		{"/var/log/app.log", true},

		// Should NOT match
		{"", false},
		{".lo", false},
		{"log", false},
		{".LOG", false}, // Case sensitive
	}

	for _, tc := range logTests {
		t.Run("log_"+tc.input, func(t *testing.T) {
			got := MatchAnchoredLiteral([]byte(tc.input), logInfo)
			if got != tc.want {
				t.Errorf("MatchAnchoredLiteral(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}

	// Test pattern: ^/.+\.php$ (wildcard .+ requires at least 1 char)
	phpPlusInfo := &AnchoredLiteralInfo{
		Prefix:         []byte("/"),
		Suffix:         []byte(".php"),
		CharClassTable: nil,
		CharClassMin:   0,
		WildcardMin:    1,
		MinLength:      6, // "/" + 1 + 0 + ".php"
	}

	phpPlusTests := []struct {
		input string
		want  bool
	}{
		{"/a.php", true},
		{"/test.php", true},
		{"/.php", false}, // .+ requires at least 1 char
	}

	for _, tc := range phpPlusTests {
		t.Run("phpPlus_"+tc.input, func(t *testing.T) {
			got := MatchAnchoredLiteral([]byte(tc.input), phpPlusInfo)
			if got != tc.want {
				t.Errorf("MatchAnchoredLiteral(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// buildWordHyphenTable builds lookup table for [\w-] = [A-Za-z0-9_-]
func buildWordHyphenTable() *[256]bool {
	var table [256]bool
	for c := 'A'; c <= 'Z'; c++ {
		table[c] = true
	}
	for c := 'a'; c <= 'z'; c++ {
		table[c] = true
	}
	for c := '0'; c <= '9'; c++ {
		table[c] = true
	}
	table['_'] = true
	table['-'] = true
	return &table
}

// TestBuildCharClassTable tests the charclass table builder.
func TestBuildCharClassTable(t *testing.T) {
	// Test [\w-] pattern
	pattern := `[\w-]+`
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Extract the CharClass from Plus
	if re.Op != syntax.OpPlus || len(re.Sub) == 0 {
		t.Fatalf("Expected Plus, got %v", re.Op)
	}
	charClassRe := re.Sub[0]

	table := buildCharClassTable(charClassRe)
	if table == nil {
		t.Fatal("Expected non-nil table")
	}

	// Verify expected chars are in the table
	expected := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-"
	for _, c := range expected {
		if !table[c] {
			t.Errorf("Expected %c to be in table", c)
		}
	}

	// Verify unexpected chars are NOT in the table
	unexpected := " !@#$%^&*()+=[]{}|\\;:'\",.<>?/`~"
	for _, c := range unexpected {
		if table[c] {
			t.Errorf("Expected %c to NOT be in table", c)
		}
	}
}

// TestDetectAndMatchIntegration tests detection followed by matching.
func TestDetectAndMatchIntegration(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    bool
	}{
		// Pattern: ^/.*[\w-]+\.php$
		{`^/.*[\w-]+\.php$`, "/test.php", true},
		{`^/.*[\w-]+\.php$`, "/path/to/file.php", true},
		{`^/.*[\w-]+\.php$`, "/test.txt", false},
		{`^/.*[\w-]+\.php$`, "test.php", false},
		{`^/.*[\w-]+\.php$`, "/.php", false},

		// Pattern: ^.*\.log$
		{`^.*\.log$`, "app.log", true},
		{`^.*\.log$`, "/var/log/app.log", true},
		{`^.*\.log$`, "app.txt", false},

		// Pattern: ^prefix.*suffix$
		{`^prefix.*suffix$`, "prefixsuffix", true},
		{`^prefix.*suffix$`, "prefix-anything-suffix", true},
		{`^prefix.*suffix$`, "prefixsuffx", false},
	}

	for _, tc := range tests {
		t.Run(tc.pattern+"_"+tc.input, func(t *testing.T) {
			re, err := syntax.Parse(tc.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			info := DetectAnchoredLiteral(re)
			if info == nil {
				t.Fatalf("Pattern should be detected: %s", tc.pattern)
			}

			got := MatchAnchoredLiteral([]byte(tc.input), info)
			if got != tc.want {
				t.Errorf("Match(%q, %q) = %v, want %v", tc.pattern, tc.input, got, tc.want)
			}
		})
	}
}

// TestAnchoredLiteralMetaEngineIntegration tests the full meta-engine flow.
// This verifies that Compile → Strategy Selection → IsMatch/Find all work correctly.
func TestAnchoredLiteralMetaEngineIntegration(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    bool
	}{
		// Pattern: ^/.*[\w-]+\.php$
		{`^/.*[\w-]+\.php$`, "/test.php", true},
		{`^/.*[\w-]+\.php$`, "/path/to/file.php", true},
		{`^/.*[\w-]+\.php$`, "/test.txt", false},
		{`^/.*[\w-]+\.php$`, "test.php", false},
		{`^/.*[\w-]+\.php$`, "/.php", false},

		// Pattern: ^.*\.log$
		{`^.*\.log$`, "app.log", true},
		{`^.*\.log$`, "/var/log/app.log", true},
		{`^.*\.log$`, "app.txt", false},

		// Pattern: ^prefix.*suffix$
		{`^prefix.*suffix$`, "prefixsuffix", true},
		{`^prefix.*suffix$`, "prefix-anything-suffix", true},
		{`^prefix.*suffix$`, "prefixsuffx", false},

		// Pattern: ^api/v1/.*\.json$
		{`^api/v1/.*\.json$`, "api/v1/users.json", true},
		{`^api/v1/.*\.json$`, "api/v1/data/config.json", true},
		{`^api/v1/.*\.json$`, "api/v2/users.json", false},
	}

	for _, tc := range tests {
		t.Run(tc.pattern+"_"+tc.input, func(t *testing.T) {
			// Compile pattern
			engine, err := Compile(tc.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) failed: %v", tc.pattern, err)
			}

			// Verify strategy is UseAnchoredLiteral
			if engine.strategy != UseAnchoredLiteral {
				t.Errorf("Expected strategy UseAnchoredLiteral, got %s", engine.strategy)
			}

			// Test IsMatch
			gotIsMatch := engine.IsMatch([]byte(tc.input))
			if gotIsMatch != tc.want {
				t.Errorf("IsMatch(%q) = %v, want %v", tc.input, gotIsMatch, tc.want)
			}

			// Test Find
			match := engine.Find([]byte(tc.input))
			gotFind := match != nil
			if gotFind != tc.want {
				t.Errorf("Find(%q) != nil is %v, want %v", tc.input, gotFind, tc.want)
			}

			// If match expected, verify it spans the entire input
			if tc.want && match != nil {
				if match.Start() != 0 || match.End() != len(tc.input) {
					t.Errorf("Match span = [%d, %d), want [0, %d)", match.Start(), match.End(), len(tc.input))
				}
			}
		})
	}
}

// BenchmarkMatchAnchoredLiteral benchmarks the fast matching algorithm.
func BenchmarkMatchAnchoredLiteral(b *testing.B) {
	pattern := `^/.*[\w-]+\.php$`
	re, _ := syntax.Parse(pattern, syntax.Perl)
	info := DetectAnchoredLiteral(re)

	inputs := []struct {
		name  string
		input []byte
		match bool
	}{
		{"short_match", []byte("/a.php"), true},
		{"medium_match", []byte("/path/to/file.php"), true},
		{"long_match", []byte("/path/to/admin/file.php"), true},
		{"no_match_suffix", []byte("/path/to/file.txt"), false},
		{"no_match_prefix", []byte("path/to/file.php"), false},
	}

	for _, inp := range inputs {
		b.Run(inp.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = MatchAnchoredLiteral(inp.input, info)
			}
		})
	}
}

// BenchmarkAnchoredLiteralVsStdlib compares UseAnchoredLiteral strategy against stdlib.
// This is the benchmark for Issue #79: pattern ^/.*[\w-]+\.php$ was 5.3x slower than stdlib.
// With UseAnchoredLiteral, we achieve 25-130x speedup OVER stdlib.
func BenchmarkAnchoredLiteralVsStdlib(b *testing.B) {
	pattern := `^/.*[\w-]+\.php$`

	// Compile coregex engine
	coregexEngine, err := Compile(pattern)
	if err != nil {
		b.Fatalf("Compile failed: %v", err)
	}

	// Compile stdlib regexp
	stdlibRe, err := regexp.Compile(pattern)
	if err != nil {
		b.Fatalf("stdlib Compile failed: %v", err)
	}

	// Verify strategy is UseAnchoredLiteral
	if coregexEngine.strategy != UseAnchoredLiteral {
		b.Errorf("Expected UseAnchoredLiteral, got %s", coregexEngine.strategy)
	}

	inputs := []struct {
		name  string
		input []byte
	}{
		{"short", []byte("/a.php")},
		{"medium", []byte("/path/to/file.php")},
		{"long", []byte("/path/to/admin/config/file.php")},
		{"no_match", []byte("/path/to/file.txt")},
	}

	for _, inp := range inputs {
		b.Run("coregex_"+inp.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = coregexEngine.IsMatch(inp.input)
			}
		})

		b.Run("stdlib_"+inp.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = stdlibRe.Match(inp.input)
			}
		})
	}
}
