package nfa

import (
	"regexp/syntax"
	"testing"
)

func TestExtractFirstBytes(t *testing.T) {
	tests := []struct {
		pattern     string
		wantBytes   string // Expected first bytes as string
		wantUseful  bool
		wantNil     bool
	}{
		// Literals
		{"hello", "h", true, false},
		{"ABC", "A", true, false},

		// Alternation
		{`\d+|UUID|hex32`, "0123456789Uh", true, false},
		{`foo|bar|baz`, "bf", true, false},
		{`[a-z]+|[0-9]+`, "0123456789abcdefghijklmnopqrstuvwxyz", true, false},

		// Character classes
		{`[a-z]+`, "abcdefghijklmnopqrstuvwxyz", true, false},
		{`[0-9]+`, "0123456789", true, false},
		{`[A-Za-z]`, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz", true, false},

		// Anchored patterns (anchor is skipped)
		{`^hello`, "h", true, false},
		{`^(\d+|UUID)`, "0123456789U", true, false},

		// Patterns with optional parts (not useful - can match empty)
		{`a*`, "", false, true},  // Can match empty
		{`a?b`, "", false, true}, // Can match starting with b but a? is optional

		// Capture groups
		{`(foo)`, "f", true, false},
		{`(foo|bar)`, "bf", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}

			fb := ExtractFirstBytes(re)

			if tt.wantNil {
				if fb != nil && fb.IsComplete() {
					t.Errorf("Expected nil or incomplete, got complete with %d bytes", fb.Count())
				}
				return
			}

			if fb == nil {
				t.Fatalf("Expected non-nil FirstByteSet")
			}

			if fb.IsUseful() != tt.wantUseful {
				t.Errorf("IsUseful() = %v, want %v", fb.IsUseful(), tt.wantUseful)
			}

			// Check expected bytes
			for _, b := range tt.wantBytes {
				if !fb.Contains(byte(b)) {
					t.Errorf("Expected byte %q to be in set", b)
				}
			}

			// Check count
			if fb.Count() != len(tt.wantBytes) {
				t.Errorf("Count() = %d, want %d", fb.Count(), len(tt.wantBytes))
			}
		})
	}
}

func TestExtractFirstBytes_Rejection(t *testing.T) {
	// Test that first-byte prefilter correctly rejects non-matching inputs
	re, _ := syntax.Parse(`\d+|UUID|hex32`, syntax.Perl)
	fb := ExtractFirstBytes(re)

	if fb == nil {
		t.Fatal("Expected non-nil FirstByteSet")
	}

	// Valid first bytes
	valid := []byte{'0', '1', '9', 'U', 'h'}
	for _, b := range valid {
		if !fb.Contains(b) {
			t.Errorf("Expected %q to be valid first byte", b)
		}
	}

	// Invalid first bytes (should be rejected)
	invalid := []byte{'a', 'z', 'A', 'Z', ' ', '\n', '.', '-'}
	for _, b := range invalid {
		if fb.Contains(b) {
			t.Errorf("Expected %q to NOT be valid first byte", b)
		}
	}
}

func BenchmarkExtractFirstBytes(b *testing.B) {
	re, _ := syntax.Parse(`^(\d+|UUID-[a-f0-9]+|hex32[a-f0-9]+)`, syntax.Perl)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ExtractFirstBytes(re)
	}
}
