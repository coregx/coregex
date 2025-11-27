package lazy

import (
	"testing"
)

func TestStartKindString(t *testing.T) {
	tests := []struct {
		kind     StartKind
		expected string
	}{
		{StartNonWord, "NonWord"},
		{StartWord, "Word"},
		{StartText, "Text"},
		{StartLineLF, "LineLF"},
		{StartLineCR, "LineCR"},
		{StartKind(255), "Unknown"},
	}

	for _, tc := range tests {
		got := tc.kind.String()
		if got != tc.expected {
			t.Errorf("StartKind(%d).String() = %q, want %q", tc.kind, got, tc.expected)
		}
	}
}

func TestStartTableByteMap(t *testing.T) {
	st := NewStartTable()

	// Test word bytes [a-zA-Z0-9_]
	wordBytes := []byte{'a', 'z', 'A', 'Z', '0', '9', '_'}
	for _, b := range wordBytes {
		kind := st.GetKind(b)
		if kind != StartWord {
			t.Errorf("GetKind(%q) = %v, want StartWord", b, kind)
		}
	}

	// Test line feed
	if kind := st.GetKind('\n'); kind != StartLineLF {
		t.Errorf("GetKind('\\n') = %v, want StartLineLF", kind)
	}

	// Test carriage return
	if kind := st.GetKind('\r'); kind != StartLineCR {
		t.Errorf("GetKind('\\r') = %v, want StartLineCR", kind)
	}

	// Test non-word bytes
	nonWordBytes := []byte{' ', '.', '-', '!', '@', '#', '$', '%', '^', '&', '*', '(', ')'}
	for _, b := range nonWordBytes {
		kind := st.GetKind(b)
		if kind != StartNonWord {
			t.Errorf("GetKind(%q) = %v, want StartNonWord", b, kind)
		}
	}
}

func TestStartTableGetKindForPosition(t *testing.T) {
	st := NewStartTable()

	tests := []struct {
		name     string
		haystack []byte
		pos      int
		expected StartKind
	}{
		{"start of input", []byte("hello"), 0, StartText},
		{"after word char", []byte("hello"), 1, StartWord},
		{"after non-word char", []byte("h llo"), 2, StartNonWord},
		{"after newline", []byte("h\nllo"), 2, StartLineLF},
		{"after CR", []byte("h\rllo"), 2, StartLineCR},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind := st.GetKindForPosition(tc.haystack, tc.pos)
			if kind != tc.expected {
				t.Errorf("GetKindForPosition(%q, %d) = %v, want %v",
					tc.haystack, tc.pos, kind, tc.expected)
			}
		})
	}
}

func TestStartTableCaching(t *testing.T) {
	st := NewStartTable()

	// Initially all states should be InvalidState
	for anchored := 0; anchored < 2; anchored++ {
		for kind := StartKind(0); kind < startKindCount; kind++ {
			stateID := st.Get(kind, anchored == 1)
			if stateID != InvalidState {
				t.Errorf("Initial Get(%v, %v) = %d, want InvalidState",
					kind, anchored == 1, stateID)
			}
			if st.IsInitialized(kind, anchored == 1) {
				t.Errorf("Initial IsInitialized(%v, %v) = true, want false",
					kind, anchored == 1)
			}
		}
	}

	// Set some states
	st.Set(StartText, false, StateID(1))
	st.Set(StartWord, false, StateID(2))
	st.Set(StartText, true, StateID(3))

	// Verify they were set correctly
	if id := st.Get(StartText, false); id != StateID(1) {
		t.Errorf("Get(StartText, false) = %d, want 1", id)
	}
	if id := st.Get(StartWord, false); id != StateID(2) {
		t.Errorf("Get(StartWord, false) = %d, want 2", id)
	}
	if id := st.Get(StartText, true); id != StateID(3) {
		t.Errorf("Get(StartText, true) = %d, want 3", id)
	}

	// Verify initialized flags
	if !st.IsInitialized(StartText, false) {
		t.Error("IsInitialized(StartText, false) = false, want true")
	}
	if !st.IsInitialized(StartWord, false) {
		t.Error("IsInitialized(StartWord, false) = false, want true")
	}
	if !st.IsInitialized(StartText, true) {
		t.Error("IsInitialized(StartText, true) = false, want true")
	}

	// Verify uninitialized states are still InvalidState
	if id := st.Get(StartLineLF, false); id != InvalidState {
		t.Errorf("Get(StartLineLF, false) = %d, want InvalidState", id)
	}
}

func TestIsWordByte(t *testing.T) {
	// Test lowercase letters
	for b := byte('a'); b <= 'z'; b++ {
		if !isWordByte(b) {
			t.Errorf("isWordByte(%q) = false, want true", b)
		}
	}

	// Test uppercase letters
	for b := byte('A'); b <= 'Z'; b++ {
		if !isWordByte(b) {
			t.Errorf("isWordByte(%q) = false, want true", b)
		}
	}

	// Test digits
	for b := byte('0'); b <= '9'; b++ {
		if !isWordByte(b) {
			t.Errorf("isWordByte(%q) = false, want true", b)
		}
	}

	// Test underscore
	if !isWordByte('_') {
		t.Error("isWordByte('_') = false, want true")
	}

	// Test non-word bytes
	nonWord := []byte{' ', '.', '-', '!', '@', '#', '\n', '\r', '\t', 0}
	for _, b := range nonWord {
		if isWordByte(b) {
			t.Errorf("isWordByte(%q) = true, want false", b)
		}
	}
}

func TestAllStartConfigs(t *testing.T) {
	configs := AllStartConfigs()

	// Should have startKindCount * 2 configurations
	expected := int(startKindCount) * 2
	if len(configs) != expected {
		t.Errorf("AllStartConfigs() returned %d configs, want %d", len(configs), expected)
	}

	// Verify all combinations are present
	seen := make(map[StartConfig]bool)
	for _, cfg := range configs {
		seen[cfg] = true
	}

	for anchored := 0; anchored < 2; anchored++ {
		for kind := StartKind(0); kind < startKindCount; kind++ {
			cfg := StartConfig{Kind: kind, Anchored: anchored == 1}
			if !seen[cfg] {
				t.Errorf("Missing config: %+v", cfg)
			}
		}
	}
}

func TestDefaultStartConfig(t *testing.T) {
	cfg := DefaultStartConfig()
	if cfg.Kind != StartText {
		t.Errorf("DefaultStartConfig().Kind = %v, want StartText", cfg.Kind)
	}
	if cfg.Anchored {
		t.Error("DefaultStartConfig().Anchored = true, want false")
	}
}

func TestStartStateContextAwareness(t *testing.T) {
	// Test that DFA uses context-aware start states
	// This validates the getStartStateForUnanchored implementation

	// Pattern with word boundary: \bfoo
	// This pattern should behave differently based on look-behind context
	// (Note: word boundaries aren't fully implemented yet, so this test
	// validates the infrastructure is in place)

	dfa, err := CompilePattern("foo")
	if err != nil {
		t.Fatalf("Failed to compile pattern: %v", err)
	}

	tests := []struct {
		name      string
		haystack  string
		wantMatch bool
	}{
		{"match at start", "foo bar", true},
		{"match after space", "bar foo", true},
		{"match after newline", "bar\nfoo", true},
		{"match after word", "barfoo", true}, // no word boundary yet
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := dfa.IsMatch([]byte(tc.haystack))
			if got != tc.wantMatch {
				t.Errorf("IsMatch(%q) = %v, want %v", tc.haystack, got, tc.wantMatch)
			}
		})
	}
}
