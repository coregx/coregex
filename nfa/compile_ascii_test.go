package nfa

import (
	"regexp/syntax"
	"testing"
)

// TestASCIICompilation tests that ASCIIOnly mode produces fewer states
func TestASCIICompilation(t *testing.T) {
	tests := []struct {
		pattern     string
		asciiStates int // expected state count in ASCII mode
		utf8States  int // expected state count in UTF-8 mode
	}{
		// Single dot - ASCII should have ~2 states (ByteRange + Epsilon + Match)
		// UTF-8 should have ~30 states (all UTF-8 branches)
		{".", 3, 30},

		// Pattern from Issue #79: ^/.*[\w-]+\.php
		// The .* creates many states in UTF-8 mode
		{"^/.*[a-z]+\\.php", 20, 50},

		// Multiple dots
		{"...", 7, 90},
	}

	for _, tc := range tests {
		t.Run(tc.pattern, func(t *testing.T) {
			// Parse pattern
			re, err := syntax.Parse(tc.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("Failed to parse pattern: %v", err)
			}

			// Compile in ASCII mode
			asciiCompiler := NewCompiler(CompilerConfig{
				UTF8:      true,
				ASCIIOnly: true,
			})
			asciiNFA, err := asciiCompiler.CompileRegexp(re)
			if err != nil {
				t.Fatalf("ASCII compile failed: %v", err)
			}

			// Compile in UTF-8 mode
			utf8Compiler := NewCompiler(CompilerConfig{
				UTF8:      true,
				ASCIIOnly: false,
			})
			utf8NFA, err := utf8Compiler.CompileRegexp(re)
			if err != nil {
				t.Fatalf("UTF-8 compile failed: %v", err)
			}

			asciiCount := asciiNFA.States()
			utf8Count := utf8NFA.States()

			t.Logf("Pattern %q: ASCII=%d states, UTF-8=%d states (%.1fx reduction)",
				tc.pattern, asciiCount, utf8Count, float64(utf8Count)/float64(asciiCount))

			// Verify ASCII mode has significantly fewer states
			if asciiCount >= utf8Count {
				t.Errorf("ASCII mode should have fewer states than UTF-8 mode: ASCII=%d, UTF-8=%d",
					asciiCount, utf8Count)
			}

			// Verify ASCII mode has at most expected states (with some tolerance)
			if asciiCount > tc.asciiStates*2 {
				t.Errorf("ASCII mode has too many states: got %d, expected <= %d",
					asciiCount, tc.asciiStates*2)
			}
		})
	}
}

// TestASCIINFACorrectness verifies that ASCII NFA matches correctly on ASCII input
func TestASCIINFACorrectness(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		match   bool
	}{
		{".", "a", true},
		{".", "z", true},
		{".", "0", true},
		{".", " ", true},
		{".", "\n", false}, // . doesn't match newline by default
		{".", "", false},

		{"(?s).", "\n", true}, // With DOTALL flag, . matches newline

		{".*", "", true},
		{".*", "hello", true},
		{".*", "hello world", true},

		{".+", "", false},
		{".+", "a", true},
		{".+", "hello", true},

		{"^.*$", "", true},
		{"^.*$", "hello", true},
		{"^.*$", "hello\nworld", false}, // multiline not enabled

		{"^/.*\\.php$", "/test.php", true},
		{"^/.*\\.php$", "/path/to/file.php", true},
		{"^/.*\\.php$", "/test.txt", false},
	}

	for _, tc := range tests {
		t.Run(tc.pattern+"_"+tc.input, func(t *testing.T) {
			// Parse with appropriate flags
			flags := syntax.Perl
			re, err := syntax.Parse(tc.pattern, flags)
			if err != nil {
				t.Fatalf("Failed to parse pattern: %v", err)
			}

			// Compile in ASCII mode
			compiler := NewCompiler(CompilerConfig{
				UTF8:      true,
				ASCIIOnly: true,
				Anchored:  false,
			})
			nfa, err := compiler.CompileRegexp(re)
			if err != nil {
				t.Fatalf("Compile failed: %v", err)
			}

			// Create BoundedBacktracker and test match
			bt := NewBoundedBacktracker(nfa)
			result := bt.IsMatch([]byte(tc.input))

			if result != tc.match {
				t.Errorf("Pattern %q on input %q: got %v, want %v",
					tc.pattern, tc.input, result, tc.match)
			}
		})
	}
}

// TestASCIIStateCount verifies exact state count for single dot
func TestASCIIStateCount(t *testing.T) {
	// Parse single dot
	re, err := syntax.Parse(".", syntax.Perl)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	// ASCII mode
	asciiCompiler := NewCompiler(CompilerConfig{UTF8: true, ASCIIOnly: true})
	asciiNFA, _ := asciiCompiler.CompileRegexp(re)

	// UTF-8 mode
	utf8Compiler := NewCompiler(CompilerConfig{UTF8: true, ASCIIOnly: false})
	utf8NFA, _ := utf8Compiler.CompileRegexp(re)

	t.Logf("Single '.': ASCII=%d states, UTF-8=%d states",
		asciiNFA.States(), utf8NFA.States())

	// ASCII should have very few states
	if asciiNFA.States() > 10 {
		t.Errorf("ASCII '.' should have <= 10 states, got %d", asciiNFA.States())
	}

	// UTF-8 should have many states for UTF-8 handling
	if utf8NFA.States() < 20 {
		t.Errorf("UTF-8 '.' should have >= 20 states, got %d", utf8NFA.States())
	}
}

// BenchmarkASCIIvsUTF8Compile benchmarks compilation time
func BenchmarkASCIIvsUTF8Compile(b *testing.B) {
	pattern := "^/.*[a-z]+\\.php$"
	re, _ := syntax.Parse(pattern, syntax.Perl)

	b.Run("ASCII", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			compiler := NewCompiler(CompilerConfig{UTF8: true, ASCIIOnly: true})
			_, _ = compiler.CompileRegexp(re)
		}
	})

	b.Run("UTF8", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			compiler := NewCompiler(CompilerConfig{UTF8: true, ASCIIOnly: false})
			_, _ = compiler.CompileRegexp(re)
		}
	})
}

// BenchmarkASCIIvsUTF8Match benchmarks matching performance
func BenchmarkASCIIvsUTF8Match(b *testing.B) {
	pattern := "^/.*[a-z]+\\.php$"
	input := []byte("/path/to/admin/file.php")

	// Parse pattern
	re, _ := syntax.Parse(pattern, syntax.Perl)

	// Compile both NFAs
	asciiCompiler := NewCompiler(CompilerConfig{UTF8: true, ASCIIOnly: true})
	asciiNFA, _ := asciiCompiler.CompileRegexp(re)

	utf8Compiler := NewCompiler(CompilerConfig{UTF8: true, ASCIIOnly: false})
	utf8NFA, _ := utf8Compiler.CompileRegexp(re)

	b.Run("ASCII_NFA", func(b *testing.B) {
		bt := NewBoundedBacktracker(asciiNFA)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = bt.IsMatch(input)
		}
	})

	b.Run("UTF8_NFA", func(b *testing.B) {
		bt := NewBoundedBacktracker(utf8NFA)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = bt.IsMatch(input)
		}
	})
}
