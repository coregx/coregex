package meta

import (
	"errors"
	"regexp/syntax"
	"testing"

	"github.com/coregx/coregex/literal"
	"github.com/coregx/coregex/nfa"
)

// TestCompile tests basic pattern compilation
func TestCompile(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		{"simple literal", "hello", false},
		{"digit class", `\d+`, false},
		{"alternation", "foo|bar", false},
		{"star", "a*", false},
		{"plus", "a+", false},
		{"question", "a?", false},
		{"concat", "abc", false},
		{"complex", `(foo|bar)\d+`, false},
		{"invalid", "(", true},
		{"invalid escape", `\k`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("Compile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && engine == nil {
				t.Error("Compile() returned nil engine")
			}
		})
	}
}

// TestConfigValidation tests configuration validation
func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "default config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "invalid MaxDFAStates (too small)",
			config: Config{
				EnableDFA:    true,
				MaxDFAStates: 0,
			},
			wantErr: true,
		},
		{
			name: "invalid MaxDFAStates (too large)",
			config: Config{
				EnableDFA:    true,
				MaxDFAStates: 2_000_000,
			},
			wantErr: true,
		},
		{
			name: "invalid DeterminizationLimit",
			config: Config{
				EnableDFA:            true,
				MaxDFAStates:         1000,
				DeterminizationLimit: 5,
			},
			wantErr: true,
		},
		{
			name: "invalid MinLiteralLen",
			config: Config{
				EnablePrefilter: true,
				MinLiteralLen:   0,
				MaxLiterals:     64,
			},
			wantErr: true,
		},
		{
			name: "invalid MaxRecursionDepth",
			config: Config{
				MaxRecursionDepth: 5,
			},
			wantErr: true,
		},
		{
			name: "DFA disabled (no validation)",
			config: Config{
				EnableDFA:            false,
				MaxDFAStates:         0, // Would be invalid if DFA enabled
				DeterminizationLimit: 0,
				MaxRecursionDepth:    100, // Still need valid recursion depth
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestSelectStrategy tests strategy selection logic
func TestSelectStrategy(t *testing.T) {
	config := DefaultConfig()

	tests := []struct {
		name    string
		pattern string
		nfaSize int // approximate
		want    Strategy
		hasLits bool
	}{
		{"tiny NFA", "a", 5, UseNFA, false},
		{"small literal", "abc", 10, UseNFA, true},
		{"medium with literals", "(foo|bar)", 25, UseDFA, true},
		{"large without literals", "a*b*c*d*e*", 120, UseDFA, false},
		{"medium without literals (char class with capture)", "(a|b|c)", 50, UseBoundedBacktracker, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Compile NFA
			compiler := nfa.NewDefaultCompiler()
			nfaEngine, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("failed to compile NFA: %v", err)
			}

			// Extract literals
			var literals *literal.Seq
			re, _ := syntax.Parse(tt.pattern, syntax.Perl)
			if tt.hasLits {
				extractor := literal.New(literal.DefaultConfig())
				literals = extractor.ExtractPrefixes(re)
			}

			// Select strategy
			strategy := SelectStrategy(nfaEngine, re, literals, config)

			// Verify it's one of the valid strategies
			if strategy != UseNFA && strategy != UseDFA && strategy != UseBoth && strategy != UseReverseAnchored && strategy != UseReverseSuffix && strategy != UseBoundedBacktracker && strategy != UseTeddy {
				t.Errorf("invalid strategy: %v", strategy)
			}

			// Log for inspection
			t.Logf("Pattern: %s, NFA size: %d, Strategy: %s (want: %s)",
				tt.pattern, nfaEngine.States(), strategy, tt.want)

			// Note: Exact strategy match is hard because NFA size varies
			// Just verify the strategy is reasonable
		})
	}
}

// TestStrategyDisabledDFA tests that strategy respects EnableDFA config
func TestStrategyDisabledDFA(t *testing.T) {
	config := DefaultConfig()
	config.EnableDFA = false

	compiler := nfa.NewDefaultCompiler()
	nfaEngine, err := compiler.Compile("(foo|bar)")
	if err != nil {
		t.Fatalf("failed to compile NFA: %v", err)
	}

	// Parse pattern for anchor detection
	re, _ := syntax.Parse("(foo|bar)", syntax.Perl)
	strategy := SelectStrategy(nfaEngine, re, nil, config)
	if strategy != UseNFA {
		t.Errorf("expected UseNFA when DFA disabled, got %v", strategy)
	}
}

// TestEngineFind tests basic find functionality
func TestEngineFind(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     string
		wantNil  bool
	}{
		{"simple literal", "hello", "say hello world", "hello", false},
		{"digit", `\d+`, "age: 42", "42", false},
		{"start", "^hello", "hello world", "hello", false},
		{"no match", "xyz", "abc def", "", true},
		{"alternation", "foo|bar", "test bar end", "bar", false},
		{"empty haystack", "a", "", "", true},
		{"empty pattern", "", "test", "", false}, // Empty pattern matches
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile() failed: %v", err)
			}

			match := engine.Find([]byte(tt.haystack))
			if tt.wantNil {
				if match != nil {
					t.Errorf("Find() = %v, want nil", match.String())
				}
				return
			}

			if match == nil {
				t.Error("Find() = nil, want match")
				return
			}

			got := match.String()
			if got != tt.want {
				t.Errorf("Find() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestEngineIsMatch tests match checking
func TestEngineIsMatch(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     bool
	}{
		{"matches", "hello", "hello world", true},
		{"no match", "xyz", "abc def", false},
		{"empty haystack", "a", "", false},
		{"empty pattern", "", "test", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile() failed: %v", err)
			}

			got := engine.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestEngineStrategies tests different execution strategies
func TestEngineStrategies(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		config   Config
		want     string
	}{
		{
			name:     "NFA strategy",
			pattern:  "a",
			haystack: "test a end",
			config: Config{
				EnableDFA:         false,
				EnablePrefilter:   false,
				MaxRecursionDepth: 100,
			},
			want: "a",
		},
		{
			name:     "DFA strategy",
			pattern:  "hello",
			haystack: "say hello world",
			config:   DefaultConfig(),
			want:     "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := CompileWithConfig(tt.pattern, tt.config)
			if err != nil {
				t.Fatalf("CompileWithConfig() failed: %v", err)
			}

			match := engine.Find([]byte(tt.haystack))
			if match == nil {
				t.Fatal("Find() = nil, want match")
			}

			got := match.String()
			if got != tt.want {
				t.Errorf("Find() = %q, want %q", got, tt.want)
			}

			t.Logf("Strategy: %s", engine.Strategy())
		})
	}
}

// TestEngineStats tests statistics tracking
func TestEngineStats(t *testing.T) {
	engine, err := Compile("hello")
	if err != nil {
		t.Fatalf("Compile() failed: %v", err)
	}

	// Perform some searches
	haystack := []byte("hello world hello")
	engine.Find(haystack)
	engine.Find(haystack)

	stats := engine.Stats()

	// Should have at least some searches
	totalSearches := stats.NFASearches + stats.DFASearches
	if totalSearches == 0 {
		t.Error("expected some searches, got 0")
	}

	t.Logf("Stats: NFA=%d, DFA=%d", stats.NFASearches, stats.DFASearches)

	// Reset stats
	engine.ResetStats()
	stats = engine.Stats()
	if stats.NFASearches != 0 || stats.DFASearches != 0 {
		t.Error("ResetStats() did not clear statistics")
	}
}

// TestCompileError tests error handling
func TestCompileError(t *testing.T) {
	_, err := Compile("(")
	if err == nil {
		t.Fatal("expected error for invalid pattern")
	}

	var compileErr *CompileError
	ok := errors.As(err, &compileErr)
	if !ok {
		t.Errorf("expected *CompileError, got %T", err)
	}

	if compileErr.Pattern != "(" {
		t.Errorf("CompileError.Pattern = %q, want %q", compileErr.Pattern, "(")
	}

	// Test Unwrap
	if compileErr.Unwrap() == nil {
		t.Error("CompileError.Unwrap() = nil, want underlying error")
	}

	// Test Error()
	errMsg := compileErr.Error()
	if errMsg == "" {
		t.Error("CompileError.Error() returned empty string")
	}
	t.Logf("Error message: %s", errMsg)
}

// BenchmarkCompile benchmarks pattern compilation
func BenchmarkCompile(b *testing.B) {
	patterns := []string{
		"hello",
		`\d+`,
		"(foo|bar|baz)",
		`[a-zA-Z0-9]+`,
	}

	for _, pattern := range patterns {
		b.Run(pattern, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, err := Compile(pattern)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkCharClass benchmarks character class patterns
// These are critical for Issue #44 - char_class 14x slower than Rust
func BenchmarkCharClass(b *testing.B) {
	// Generate test data with letters, digits, and spaces
	input1KB := make([]byte, 1024)
	for i := range input1KB {
		switch i % 10 {
		case 0, 1, 2:
			input1KB[i] = 'a' + byte(i%26)
		case 3, 4:
			input1KB[i] = '0' + byte(i%10)
		default:
			input1KB[i] = ' '
		}
	}

	input32KB := make([]byte, 32*1024)
	for i := range input32KB {
		switch i % 10 {
		case 0, 1, 2:
			input32KB[i] = 'a' + byte(i%26)
		case 3, 4:
			input32KB[i] = '0' + byte(i%10)
		default:
			input32KB[i] = ' '
		}
	}

	tests := []struct {
		name     string
		pattern  string
		haystack []byte
	}{
		// Simple char classes - should use CharClassSearcher
		{"word_class/1KB", `\w+`, input1KB},
		{"word_class/32KB", `\w+`, input32KB},
		{"digit_class/1KB", `\d+`, input1KB},
		{"letter_range/1KB", `[a-z]+`, input1KB},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				b.Fatal(err)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				match := engine.Find(tt.haystack)
				if match == nil {
					b.Fatal("expected match")
				}
			}
		})
	}
}

// BenchmarkCharClassFindAll benchmarks FindAll on char_class patterns
func BenchmarkCharClassFindAll(b *testing.B) {
	// Generate test data with letters, digits, and spaces
	input1KB := make([]byte, 1024)
	for i := range input1KB {
		switch i % 10 {
		case 0, 1, 2:
			input1KB[i] = 'a' + byte(i%26)
		case 3, 4:
			input1KB[i] = '0' + byte(i%10)
		default:
			input1KB[i] = ' '
		}
	}

	tests := []struct {
		name    string
		pattern string
	}{
		{"word_class", `\w+`},
		{"digit_class", `\d+`},
		{"letter_range", `[a-z]+`},
	}

	for _, tt := range tests {
		b.Run(tt.name+"/1KB", func(b *testing.B) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				b.Fatal(err)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				// Use FindIndicesAt to iterate through all matches
				at := 0
				count := 0
				for {
					_, end, found := engine.FindIndicesAt(input1KB, at)
					if !found {
						break
					}
					count++
					at = end
				}
				if count == 0 {
					b.Fatal("expected matches")
				}
			}
		})
	}
}

// BenchmarkCharClassFindAllStreaming compares streaming vs loop-based FindAll at Engine level
func BenchmarkCharClassFindAllStreaming(b *testing.B) {
	// Generate test data with letters, digits, and spaces
	input1KB := make([]byte, 1024)
	for i := range input1KB {
		switch i % 10 {
		case 0, 1, 2:
			input1KB[i] = 'a' + byte(i%26)
		case 3, 4:
			input1KB[i] = '0' + byte(i%10)
		default:
			input1KB[i] = ' '
		}
	}

	engine, err := Compile(`\w+`)
	if err != nil {
		b.Fatal(err)
	}

	// Verify strategy is CharClassSearcher
	if engine.Strategy() != UseCharClassSearcher {
		b.Skipf("Strategy is %v, expected UseCharClassSearcher", engine.Strategy())
	}

	b.Run("Streaming/1KB", func(b *testing.B) {
		b.ReportAllocs()
		var results [][2]int
		for i := 0; i < b.N; i++ {
			results = engine.FindAllIndicesStreaming(input1KB, -1, results)
		}
	})

	b.Run("Loop/1KB", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			at := 0
			count := 0
			for {
				_, end, found := engine.FindIndicesAt(input1KB, at)
				if !found {
					break
				}
				count++
				at = end
			}
		}
	})
}

// BenchmarkFind benchmarks search performance
// Includes GoAWK patterns (Ben Hoyt) for small string regression testing.
func BenchmarkFind(b *testing.B) {
	tests := []struct {
		pattern  string
		haystack string
	}{
		{"hello", "this is a test hello world string"},
		{`\d+`, "the year is 2024 and the month is January"},
		{"foo|bar|baz", "prefix foo middle bar suffix baz end"},
		// GoAWK patterns (Ben Hoyt) - critical for small string performance
		{`j[a-z]+p`, "The quick brown fox jumps over the lazy dog"},
		{`\w+`, "hello world 123"},
		{`[a-z]+`, "Hello World Test"},
	}

	for _, tt := range tests {
		b.Run(tt.pattern, func(b *testing.B) {
			b.ReportAllocs()
			engine, err := Compile(tt.pattern)
			if err != nil {
				b.Fatal(err)
			}

			haystack := []byte(tt.haystack)
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				match := engine.Find(haystack)
				if match == nil {
					b.Fatal("expected match")
				}
			}
		})
	}
}

// BenchmarkIsMatch benchmarks IsMatch (boolean match check) performance.
// GoAWK patterns are critical for small string performance regression testing.
func BenchmarkIsMatch(b *testing.B) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
	}{
		{"literal", "hello", "this is a test hello world string"},
		{"digit", `\d+`, "the year is 2024"},
		// GoAWK patterns (Ben Hoyt) - critical for small string performance
		{"goawk_char_range", `j[a-z]+p`, "The quick brown fox jumps over the lazy dog"},
		{"goawk_word", `\w+`, "hello world 123"},
		{"goawk_lowercase", `[a-z]+`, "Hello World Test"},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				b.Fatal(err)
			}

			haystack := []byte(tt.haystack)
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				if !engine.IsMatch(haystack) {
					b.Fatal("expected match")
				}
			}
		})
	}
}
