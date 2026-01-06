package meta

import (
	"regexp"
	"testing"
)

// BenchmarkAhoCorasickVsStdlib compares Aho-Corasick strategy against stdlib
// for patterns with many literals (>32).
func BenchmarkAhoCorasickVsStdlib(b *testing.B) {
	// 35 patterns - above Teddy's 32 pattern limit, triggers Aho-Corasick
	pattern := `alpha|bravo|charlie|delta|echo|foxtrot|golf|hotel|india|juliet|` +
		`kilo|lima|mike|november|oscar|papa|quebec|romeo|sierra|tango|` +
		`uniform|victor|whiskey|xray|yankee|zulu|one|two|three|four|` +
		`five|six|seven|eight|nine`

	// Create haystack with matches scattered throughout
	haystack := []byte(`The quick brown fox jumped over the lazy dog.
	Alpha team is on standby. Bravo team approaching target.
	Charlie reports all clear. Delta is in position.
	Echo confirms. Foxtrot is ready. Golf reports negative.
	Hotel is standing by. India and Juliet are on route.
	Kilo and Lima confirm. Mike is monitoring radio.
	November Oscar Papa Quebec Romeo Sierra Tango all check in.
	The mission is proceeding according to plan.
	All units report: alpha bravo charlie delta echo foxtrot golf.`)

	b.Run("coregex_IsMatch", func(b *testing.B) {
		re, err := Compile(pattern)
		if err != nil {
			b.Fatal(err)
		}
		b.SetBytes(int64(len(haystack)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.IsMatch(haystack)
		}
	})

	b.Run("stdlib_MatchString", func(b *testing.B) {
		re := regexp.MustCompile(pattern)
		b.SetBytes(int64(len(haystack)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.Match(haystack)
		}
	})

	b.Run("coregex_Find", func(b *testing.B) {
		re, err := Compile(pattern)
		if err != nil {
			b.Fatal(err)
		}
		b.SetBytes(int64(len(haystack)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.Find(haystack)
		}
	})

	b.Run("stdlib_Find", func(b *testing.B) {
		re := regexp.MustCompile(pattern)
		b.SetBytes(int64(len(haystack)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.Find(haystack)
		}
	})

	b.Run("coregex_Count", func(b *testing.B) {
		re, err := Compile(pattern)
		if err != nil {
			b.Fatal(err)
		}
		b.SetBytes(int64(len(haystack)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.Count(haystack, -1)
		}
	})

	b.Run("stdlib_FindAllIndex", func(b *testing.B) {
		re := regexp.MustCompile(pattern)
		b.SetBytes(int64(len(haystack)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.FindAllIndex(haystack, -1)
		}
	})
}

// BenchmarkAhoCorasickLargeInput tests performance on large haystack.
func BenchmarkAhoCorasickLargeInput(b *testing.B) {
	// 15 patterns
	pattern := `error|warning|critical|fatal|debug|info|trace|notice|alert|emergency|` +
		`panic|exception|failure|timeout|refused`

	// Create 64KB haystack with sparse matches
	base := "This is a line of log output without any matching keywords. Just normal text. "
	largeHaystack := make([]byte, 0, 64*1024)
	for len(largeHaystack) < 60*1024 {
		largeHaystack = append(largeHaystack, base...)
	}
	// Add some matches near the end
	largeHaystack = append(largeHaystack, "error occurred. warning issued. critical alert."...)

	b.Run("coregex_IsMatch_64KB", func(b *testing.B) {
		re, err := Compile(pattern)
		if err != nil {
			b.Fatal(err)
		}
		b.SetBytes(int64(len(largeHaystack)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.IsMatch(largeHaystack)
		}
	})

	b.Run("stdlib_Match_64KB", func(b *testing.B) {
		re := regexp.MustCompile(pattern)
		b.SetBytes(int64(len(largeHaystack)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.Match(largeHaystack)
		}
	})

	b.Run("coregex_Find_64KB", func(b *testing.B) {
		re, err := Compile(pattern)
		if err != nil {
			b.Fatal(err)
		}
		b.SetBytes(int64(len(largeHaystack)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.Find(largeHaystack)
		}
	})

	b.Run("stdlib_Find_64KB", func(b *testing.B) {
		re := regexp.MustCompile(pattern)
		b.SetBytes(int64(len(largeHaystack)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.Find(largeHaystack)
		}
	})
}

// BenchmarkAhoCorasickManyPatterns tests with increasing pattern counts.
func BenchmarkAhoCorasickManyPatterns(b *testing.B) {
	// Test with 10, 50, 100 patterns
	patterns := []struct {
		name    string
		pattern string
	}{
		{"10_patterns", `p01|p02|p03|p04|p05|p06|p07|p08|p09|p10`},
		{"25_patterns", `p01|p02|p03|p04|p05|p06|p07|p08|p09|p10|` +
			`p11|p12|p13|p14|p15|p16|p17|p18|p19|p20|` +
			`p21|p22|p23|p24|p25`},
		{"50_patterns", `p01|p02|p03|p04|p05|p06|p07|p08|p09|p10|` +
			`p11|p12|p13|p14|p15|p16|p17|p18|p19|p20|` +
			`p21|p22|p23|p24|p25|p26|p27|p28|p29|p30|` +
			`p31|p32|p33|p34|p35|p36|p37|p38|p39|p40|` +
			`p41|p42|p43|p44|p45|p46|p47|p48|p49|p50`},
	}

	haystack := []byte("prefix p25 middle p42 suffix p01 end")

	for _, tc := range patterns {
		b.Run("coregex_"+tc.name, func(b *testing.B) {
			re, err := Compile(tc.pattern)
			if err != nil {
				b.Fatal(err)
			}
			b.SetBytes(int64(len(haystack)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = re.Find(haystack)
			}
		})

		b.Run("stdlib_"+tc.name, func(b *testing.B) {
			re := regexp.MustCompile(tc.pattern)
			b.SetBytes(int64(len(haystack)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = re.Find(haystack)
			}
		})
	}
}
