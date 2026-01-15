package meta

import (
	"bytes"
	"testing"
)

// TestASCIIOptimization tests the V11-002 ASCII runtime detection optimization.
// This optimization compiles two NFAs for patterns with '.':
// - UTF-8 NFA: handles all valid UTF-8 codepoints (~28 states per '.')
// - ASCII NFA: optimized for ASCII-only input (1-2 states per '.')
func TestASCIIOptimization(t *testing.T) {
	// Pattern from Issue #79
	pattern := `^/.*[\w-]+\.php$`

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	// Verify ASCII NFA was compiled
	if engine.asciiNFA == nil {
		t.Error("Expected asciiNFA to be compiled for pattern with '.'")
	}
	if engine.asciiBoundedBacktracker == nil {
		t.Error("Expected asciiBoundedBacktracker to be created")
	}

	tests := []struct {
		name    string
		input   string
		isASCII bool
		match   bool
	}{
		// ASCII inputs - should use ASCII NFA
		{"ascii_match", "/path/to/admin/file.php", true, true},
		{"ascii_match_simple", "/test.php", true, true},
		{"ascii_match_deep", "/a/b/c/d/e/file.php", true, true},
		{"ascii_no_match_wrong_ext", "/path/to/file.txt", true, false},
		{"ascii_no_match_no_path", "file.php", true, false},

		// Non-ASCII inputs - should use UTF-8 NFA
		// Note: [\w-] only matches ASCII word chars, so Cyrillic doesn't match
		// These test that UTF-8 NFA is used but the pattern itself doesn't match Cyrillic
		{"utf8_no_match_cyrillic", "/path/to/файл.php", false, false}, // Cyrillic doesn't match [\w-]+
		{"utf8_match_mixed", "/path/to/file-файл.php", false, false},  // Mixed, but [\w-]+ only matches ASCII
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := []byte(tc.input)

			// Test IsMatch
			result := engine.IsMatch(input)
			if result != tc.match {
				t.Errorf("IsMatch(%q) = %v, want %v", tc.input, result, tc.match)
			}

			// Test Find
			match := engine.Find(input)
			if tc.match {
				if match == nil {
					t.Errorf("Find(%q) = nil, want match", tc.input)
				}
			} else {
				if match != nil {
					t.Errorf("Find(%q) = %v, want nil", tc.input, match)
				}
			}
		})
	}
}

// TestASCIIOptimizationDisabled tests that ASCII optimization can be disabled.
func TestASCIIOptimizationDisabled(t *testing.T) {
	pattern := `^/.*[\w-]+\.php$`

	config := DefaultConfig()
	config.EnableASCIIOptimization = false

	engine, err := CompileWithConfig(pattern, config)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	// Verify ASCII NFA was NOT compiled
	if engine.asciiNFA != nil {
		t.Error("Expected asciiNFA to be nil when optimization is disabled")
	}
	if engine.asciiBoundedBacktracker != nil {
		t.Error("Expected asciiBoundedBacktracker to be nil when optimization is disabled")
	}

	// Pattern should still work
	if !engine.IsMatch([]byte("/test.php")) {
		t.Error("Expected match for /test.php")
	}
}

// TestASCIIOptimizationNoDot tests that patterns without '.' don't compile ASCII NFA.
func TestASCIIOptimizationNoDot(t *testing.T) {
	pattern := `^/path/to/file\.php$` // No '.' - literal only

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	// Verify ASCII NFA was NOT compiled (no '.' in pattern)
	if engine.asciiNFA != nil {
		t.Error("Expected asciiNFA to be nil for pattern without '.'")
	}
}

// TestASCIIOptimizationCorrectness verifies ASCII and UTF-8 NFAs produce same results.
func TestASCIIOptimizationCorrectness(t *testing.T) {
	patterns := []string{
		`^/.*\.php$`,
		`^.*test.*$`,
		`.+`,
		`.*`,
		`^.$`,
		`^.{1,10}$`,
	}

	inputs := []string{
		"",
		"a",
		"abc",
		"/test.php",
		"/path/to/file.php",
		"hello world",
		"test123test",
	}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			// Compile with ASCII optimization enabled
			configWith := DefaultConfig()
			configWith.EnableASCIIOptimization = true
			engineWith, err := CompileWithConfig(pattern, configWith)
			if err != nil {
				t.Fatalf("Compile failed: %v", err)
			}

			// Compile with ASCII optimization disabled
			configWithout := DefaultConfig()
			configWithout.EnableASCIIOptimization = false
			engineWithout, err := CompileWithConfig(pattern, configWithout)
			if err != nil {
				t.Fatalf("Compile failed: %v", err)
			}

			for _, input := range inputs {
				inputBytes := []byte(input)

				// Both should produce the same result
				resultWith := engineWith.IsMatch(inputBytes)
				resultWithout := engineWithout.IsMatch(inputBytes)

				if resultWith != resultWithout {
					t.Errorf("Pattern %q on input %q: with_ascii=%v, without_ascii=%v",
						pattern, input, resultWith, resultWithout)
				}

				// Also check Find
				matchWith := engineWith.Find(inputBytes)
				matchWithout := engineWithout.Find(inputBytes)

				if (matchWith == nil) != (matchWithout == nil) {
					t.Errorf("Pattern %q on input %q: Find mismatch - with_ascii=%v, without_ascii=%v",
						pattern, input, matchWith, matchWithout)
				}

				if matchWith != nil && matchWithout != nil {
					if !bytes.Equal(matchWith.Bytes(), matchWithout.Bytes()) {
						t.Errorf("Pattern %q on input %q: match bytes differ - %q vs %q",
							pattern, input, matchWith.Bytes(), matchWithout.Bytes())
					}
				}
			}
		})
	}
}

// BenchmarkASCIIOptimization benchmarks the ASCII optimization impact.
func BenchmarkASCIIOptimization(b *testing.B) {
	pattern := `^/.*[\w-]+\.php$`
	input := []byte("/path/to/admin/file.php")

	b.Run("WithASCII", func(b *testing.B) {
		config := DefaultConfig()
		config.EnableASCIIOptimization = true
		engine, _ := CompileWithConfig(pattern, config)
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			_ = engine.IsMatch(input)
		}
	})

	b.Run("WithoutASCII", func(b *testing.B) {
		config := DefaultConfig()
		config.EnableASCIIOptimization = false
		engine, _ := CompileWithConfig(pattern, config)
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			_ = engine.IsMatch(input)
		}
	})
}

// BenchmarkASCIIOptimization_Issue79 benchmarks the specific Issue #79 pattern.
func BenchmarkASCIIOptimization_Issue79(b *testing.B) {
	pattern := `^/.*[\w-]+\.php`
	inputs := []struct {
		name  string
		input []byte
	}{
		{"short", []byte("/t.php")},
		{"medium", []byte("/path/to/admin/file.php")},
		{"long", []byte("/a/very/long/path/to/deeply/nested/admin/file.php")},
	}

	for _, inp := range inputs {
		b.Run(inp.name+"_WithASCII", func(b *testing.B) {
			config := DefaultConfig()
			config.EnableASCIIOptimization = true
			engine, _ := CompileWithConfig(pattern, config)
			b.SetBytes(int64(len(inp.input)))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_ = engine.IsMatch(inp.input)
			}
		})

		b.Run(inp.name+"_WithoutASCII", func(b *testing.B) {
			config := DefaultConfig()
			config.EnableASCIIOptimization = false
			engine, _ := CompileWithConfig(pattern, config)
			b.SetBytes(int64(len(inp.input)))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_ = engine.IsMatch(inp.input)
			}
		})
	}
}
