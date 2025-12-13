package coregex_test

import (
	"regexp"
	"testing"

	"github.com/coregx/coregex"
)

// GoAWK benchmark patterns from Ben Hoyt's testing
// https://github.com/golang/go/issues/26623#issuecomment-3649763824
//
// These represent real-world small-string workloads where stdlib
// may outperform coregex due to lower overhead.

const (
	// 44-byte input string used in match benchmarks
	goawkMatchInput = "The quick brown fox jumps over the lazy dog"

	// 150-byte input string used in split benchmarks
	goawkSplitInput = "a fox ab fax abc fix a fox ab fax abc fix a fox ab fax abc fix a fox ab fax abc fix a fox ab fax abc fix a fox ab fax abc fix a fox ab fax abc fix"

	// 137-byte input string used in sub/gsub benchmarks
	goawkSubInput = "The quick brown fox jumps over the lazy dog. The quick brown fox jumps over the lazy dog. The quick brown fox jumps over the lazy dog."
)

// =============================================================================
// Correctness Tests
// =============================================================================

func TestGoAWK_MatchPattern(t *testing.T) {
	// Pattern: j[a-z]+p should match "jump" in the input
	pattern := `j[a-z]+p`
	input := goawkMatchInput

	reCoregex := coregex.MustCompile(pattern)
	reStdlib := regexp.MustCompile(pattern)

	// Test FindString
	gotCoregex := reCoregex.FindString(input)
	gotStdlib := reStdlib.FindString(input)

	if gotCoregex != gotStdlib {
		t.Errorf("FindString mismatch: coregex=%q, stdlib=%q", gotCoregex, gotStdlib)
	}

	// Test FindStringIndex
	idxCoregex := reCoregex.FindStringIndex(input)
	idxStdlib := reStdlib.FindStringIndex(input)

	if len(idxCoregex) != len(idxStdlib) {
		t.Errorf("FindStringIndex length mismatch: coregex=%v, stdlib=%v", idxCoregex, idxStdlib)
	} else if len(idxCoregex) == 2 {
		if idxCoregex[0] != idxStdlib[0] || idxCoregex[1] != idxStdlib[1] {
			t.Errorf("FindStringIndex mismatch: coregex=%v, stdlib=%v", idxCoregex, idxStdlib)
		}
	}

	// Test MatchString
	matchCoregex := reCoregex.MatchString(input)
	matchStdlib := reStdlib.MatchString(input)

	if matchCoregex != matchStdlib {
		t.Errorf("MatchString mismatch: coregex=%v, stdlib=%v", matchCoregex, matchStdlib)
	}
}

func TestGoAWK_SplitPattern(t *testing.T) {
	// Pattern: f[a-z]x matches "fox", "fax", "fix"
	pattern := `f[a-z]x`
	input := goawkSplitInput

	reCoregex := coregex.MustCompile(pattern)
	reStdlib := regexp.MustCompile(pattern)

	// Test Split
	splitCoregex := reCoregex.Split(input, -1)
	splitStdlib := reStdlib.Split(input, -1)

	if len(splitCoregex) != len(splitStdlib) {
		t.Errorf("Split count mismatch: coregex=%d, stdlib=%d", len(splitCoregex), len(splitStdlib))
	}

	for i := range splitCoregex {
		if i < len(splitStdlib) && splitCoregex[i] != splitStdlib[i] {
			t.Errorf("Split[%d] mismatch: coregex=%q, stdlib=%q", i, splitCoregex[i], splitStdlib[i])
		}
	}

	// Test FindAllString
	allCoregex := reCoregex.FindAllString(input, -1)
	allStdlib := reStdlib.FindAllString(input, -1)

	if len(allCoregex) != len(allStdlib) {
		t.Errorf("FindAllString count mismatch: coregex=%d, stdlib=%d", len(allCoregex), len(allStdlib))
	}
}

func TestGoAWK_SubPattern(t *testing.T) {
	// Pattern: f[a-z]x for replacement
	pattern := `f[a-z]x`
	input := goawkSubInput
	replacement := "foxes"

	reCoregex := coregex.MustCompile(pattern)
	reStdlib := regexp.MustCompile(pattern)

	// Test ReplaceAllString (gsub equivalent)
	replCoregex := reCoregex.ReplaceAllString(input, replacement)
	replStdlib := reStdlib.ReplaceAllString(input, replacement)

	if replCoregex != replStdlib {
		t.Errorf("ReplaceAllString mismatch:\ncoregex: %q\nstdlib:  %q", replCoregex, replStdlib)
	}

	// Test ReplaceAllLiteralString
	replLitCoregex := reCoregex.ReplaceAllLiteralString(input, replacement)
	replLitStdlib := reStdlib.ReplaceAllLiteralString(input, replacement)

	if replLitCoregex != replLitStdlib {
		t.Errorf("ReplaceAllLiteralString mismatch:\ncoregex: %q\nstdlib:  %q", replLitCoregex, replLitStdlib)
	}
}

// =============================================================================
// Benchmarks - Small String (GoAWK workload)
// =============================================================================

// BenchmarkGoAWK_Match - j[a-z]+p pattern matching
// This is the core benchmark that Ben Hoyt reported as slower
func BenchmarkGoAWK_Match(b *testing.B) {
	pattern := `j[a-z]+p`
	input := goawkMatchInput

	b.Run("coregex_MatchString", func(b *testing.B) {
		re := coregex.MustCompile(pattern)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.MatchString(input)
		}
	})

	b.Run("stdlib_MatchString", func(b *testing.B) {
		re := regexp.MustCompile(pattern)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.MatchString(input)
		}
	})

	b.Run("coregex_FindStringIndex", func(b *testing.B) {
		re := coregex.MustCompile(pattern)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.FindStringIndex(input)
		}
	})

	b.Run("stdlib_FindStringIndex", func(b *testing.B) {
		re := regexp.MustCompile(pattern)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.FindStringIndex(input)
		}
	})
}

// BenchmarkGoAWK_Split - f[a-z]x pattern for split
func BenchmarkGoAWK_Split(b *testing.B) {
	pattern := `f[a-z]x`
	input := goawkSplitInput

	b.Run("coregex_Split", func(b *testing.B) {
		re := coregex.MustCompile(pattern)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.Split(input, -1)
		}
	})

	b.Run("stdlib_Split", func(b *testing.B) {
		re := regexp.MustCompile(pattern)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.Split(input, -1)
		}
	})

	b.Run("coregex_FindAllStringIndex", func(b *testing.B) {
		re := coregex.MustCompile(pattern)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.FindAllStringIndex(input, -1)
		}
	})

	b.Run("stdlib_FindAllStringIndex", func(b *testing.B) {
		re := regexp.MustCompile(pattern)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.FindAllStringIndex(input, -1)
		}
	})
}

// BenchmarkGoAWK_Sub - f[a-z]x pattern for sub/gsub
func BenchmarkGoAWK_Sub(b *testing.B) {
	pattern := `f[a-z]x`
	input := goawkSubInput
	replacement := "foxes"

	b.Run("coregex_ReplaceAllString", func(b *testing.B) {
		re := coregex.MustCompile(pattern)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.ReplaceAllString(input, replacement)
		}
	})

	b.Run("stdlib_ReplaceAllString", func(b *testing.B) {
		re := regexp.MustCompile(pattern)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.ReplaceAllString(input, replacement)
		}
	})

	b.Run("coregex_ReplaceAllLiteralString", func(b *testing.B) {
		re := coregex.MustCompile(pattern)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.ReplaceAllLiteralString(input, replacement)
		}
	})

	b.Run("stdlib_ReplaceAllLiteralString", func(b *testing.B) {
		re := regexp.MustCompile(pattern)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.ReplaceAllLiteralString(input, replacement)
		}
	})
}

// BenchmarkGoAWK_RegexMatch - Pure match test (~ operator in AWK)
func BenchmarkGoAWK_RegexMatch(b *testing.B) {
	pattern := `j[a-z]+p`
	input := []byte(goawkMatchInput)

	b.Run("coregex_Match", func(b *testing.B) {
		re := coregex.MustCompile(pattern)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.Match(input)
		}
	})

	b.Run("stdlib_Match", func(b *testing.B) {
		re := regexp.MustCompile(pattern)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.Match(input)
		}
	})
}

// =============================================================================
// Additional edge case benchmarks
// =============================================================================

// BenchmarkGoAWK_NoMatch - Pattern that doesn't match
func BenchmarkGoAWK_NoMatch(b *testing.B) {
	pattern := `xyz123`
	input := goawkMatchInput

	b.Run("coregex_MatchString", func(b *testing.B) {
		re := coregex.MustCompile(pattern)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.MatchString(input)
		}
	})

	b.Run("stdlib_MatchString", func(b *testing.B) {
		re := regexp.MustCompile(pattern)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.MatchString(input)
		}
	})
}

// BenchmarkGoAWK_SimpleChar - Simple character class
func BenchmarkGoAWK_SimpleChar(b *testing.B) {
	pattern := `[a-z]+`
	input := goawkMatchInput

	b.Run("coregex_FindAllString", func(b *testing.B) {
		re := coregex.MustCompile(pattern)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.FindAllString(input, -1)
		}
	})

	b.Run("stdlib_FindAllString", func(b *testing.B) {
		re := regexp.MustCompile(pattern)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.FindAllString(input, -1)
		}
	})
}

// =============================================================================
// Longest() Mode Benchmarks - Critical for GoAWK
// =============================================================================

// BenchmarkGoAWK_Longest tests performance with Longest() mode enabled
// GoAWK calls re.Longest() on every compiled regex
func BenchmarkGoAWK_Longest(b *testing.B) {
	pattern := `j[a-z]+p`
	input := goawkMatchInput

	b.Run("coregex_default", func(b *testing.B) {
		re := coregex.MustCompile(pattern)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.FindStringIndex(input)
		}
	})

	b.Run("coregex_Longest", func(b *testing.B) {
		re := coregex.MustCompile(pattern)
		re.Longest()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.FindStringIndex(input)
		}
	})

	b.Run("stdlib_default", func(b *testing.B) {
		re := regexp.MustCompile(pattern)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.FindStringIndex(input)
		}
	})

	b.Run("stdlib_Longest", func(b *testing.B) {
		re := regexp.MustCompile(pattern)
		re.Longest()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.FindStringIndex(input)
		}
	})
}
