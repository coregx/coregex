package coregex

import (
	"regexp"
	"testing"
)

// Benchmarks for anchored alternation patterns (ID validation, UUID, hex).
// Common enterprise patterns: ^(\d+|UUID|hex32)$

var anchoredAltPattern = `^(\d+|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|[0-9a-fA-F]{32})$`

var anchoredAltInputs = []string{
	"12345",                                // matches \d+
	"550e8400-e29b-41d4-a716-446655440000", // matches UUID
	"550e8400e29b41d4a716446655440000",     // matches hex32
	"not-a-match",                          // no match
	"12345-extra",                          // no match
	"abc",                                  // no match
}

func BenchmarkAnchoredAlt_UUID_GoStdlib(b *testing.B) {
	re := regexp.MustCompile(anchoredAltPattern)
	input := []byte("550e8400-e29b-41d4-a716-446655440000")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		re.Match(input)
	}
}

func BenchmarkAnchoredAlt_UUID_Coregex(b *testing.B) {
	re := MustCompile(anchoredAltPattern)
	input := []byte("550e8400-e29b-41d4-a716-446655440000")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		re.Match(input)
	}
}

func BenchmarkAnchoredAlt_NoMatch_GoStdlib(b *testing.B) {
	re := regexp.MustCompile(anchoredAltPattern)
	input := []byte("not-a-match-at-all")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		re.Match(input)
	}
}

func BenchmarkAnchoredAlt_NoMatch_Coregex(b *testing.B) {
	re := MustCompile(anchoredAltPattern)
	input := []byte("not-a-match-at-all")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		re.Match(input)
	}
}

func BenchmarkAnchoredAlt_Digits_GoStdlib(b *testing.B) {
	re := regexp.MustCompile(anchoredAltPattern)
	input := []byte("12345")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		re.Match(input)
	}
}

func BenchmarkAnchoredAlt_Digits_Coregex(b *testing.B) {
	re := MustCompile(anchoredAltPattern)
	input := []byte("12345")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		re.Match(input)
	}
}

// TestAnchoredAltCorrectness verifies coregex matches the same as stdlib
func TestAnchoredAltCorrectness(t *testing.T) {
	re := regexp.MustCompile(anchoredAltPattern)
	cre := MustCompile(anchoredAltPattern)

	for _, input := range anchoredAltInputs {
		stdMatch := re.MatchString(input)
		coreMatch := cre.MatchString(input)
		if stdMatch != coreMatch {
			t.Errorf("input %q: stdlib=%v, coregex=%v", input, stdMatch, coreMatch)
		}
	}
}
