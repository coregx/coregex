package nfa

import (
	"testing"
)

func TestUtf8SuffixCache_Basic(t *testing.T) {
	cache := newUtf8SuffixCache()

	// First lookup should miss
	key := utf8SuffixKey{from: 1, start: 0x80, end: 0xBF}
	_, found := cache.get(key)
	if found {
		t.Error("expected cache miss on first lookup")
	}

	// Set a value
	cache.set(key, 42)

	// Should hit now
	val, found := cache.get(key)
	if !found {
		t.Error("expected cache hit after set")
	}
	if val != 42 {
		t.Errorf("expected value 42, got %d", val)
	}
}

func TestUtf8SuffixCache_Clear(t *testing.T) {
	cache := newUtf8SuffixCache()

	key := utf8SuffixKey{from: 1, start: 0x80, end: 0xBF}
	cache.set(key, 42)

	// Verify it's there
	_, found := cache.get(key)
	if !found {
		t.Error("expected cache hit before clear")
	}

	// Clear
	cache.clear()

	// Should miss after clear
	_, found = cache.get(key)
	if found {
		t.Error("expected cache miss after clear")
	}
}

func TestUtf8SuffixCache_GetOrCreate(t *testing.T) {
	cache := newUtf8SuffixCache()
	builder := NewBuilder()

	// Create end state
	endState := builder.AddMatch()

	// First getOrCreate should create a new state
	state1 := cache.getOrCreate(builder, endState, 0x80, 0xBF)

	// Second getOrCreate with same params should return cached state
	state2 := cache.getOrCreate(builder, endState, 0x80, 0xBF)

	if state1 != state2 {
		t.Errorf("expected same state ID, got %d and %d", state1, state2)
	}

	// Different params should create a new state
	state3 := cache.getOrCreate(builder, endState, 0xC2, 0xDF)
	if state3 == state1 {
		t.Error("expected different state for different byte range")
	}

	// Different target should create a new state
	state4 := cache.getOrCreate(builder, state1, 0x80, 0xBF)
	if state4 == state1 {
		t.Error("expected different state for different target")
	}
}

func TestCompileUTF8Any_StateCount(t *testing.T) {
	// Verify that suffix sharing reduces state count
	tests := []struct {
		pattern       string
		maxStates     int // Upper bound after optimization
		description   string
	}{
		{".", 30, "dot should have suffix sharing"},
		{".*", 32, "dot-star should have suffix sharing"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("compile error: %v", err)
			}

			states := nfa.States()
			if states > tt.maxStates {
				t.Errorf("%s: got %d states, want <= %d states (%s)",
					tt.pattern, states, tt.maxStates, tt.description)
			}
			t.Logf("%s: %d states (max %d)", tt.pattern, states, tt.maxStates)
		})
	}
}

func TestCompileUTF8Any_Correctness(t *testing.T) {
	// Verify that the optimized dot still matches correctly
	compiler := NewDefaultCompiler()
	nfa, err := compiler.Compile(".")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	backtracker := NewBoundedBacktracker(nfa)

	tests := []struct {
		input string
		match bool
	}{
		// ASCII
		{"a", true},
		{"z", true},
		{"0", true},
		{" ", true},
		{"\t", true},
		{"\n", false}, // dot doesn't match newline by default

		// UTF-8 2-byte
		{"ä", true},  // U+00E4
		{"é", true},  // U+00E9
		{"ñ", true},  // U+00F1
		{"ß", true},  // U+00DF

		// UTF-8 3-byte
		{"中", true},  // U+4E2D Chinese
		{"日", true},  // U+65E5 Japanese
		{"€", true},  // U+20AC Euro sign

		// UTF-8 4-byte
		{"𝕳", true},  // U+1D573 Mathematical H
		{"🎉", true}, // U+1F389 Party popper
		{"😀", true}, // U+1F600 Emoji

		// Empty
		{"", false},

		// Multiple chars (dot should match only first)
		{"ab", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := backtracker.IsMatch([]byte(tt.input))
			if got != tt.match {
				t.Errorf("Match(%q) = %v, want %v", tt.input, got, tt.match)
			}
		})
	}
}
