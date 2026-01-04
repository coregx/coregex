package meta

import (
	"math/rand"
	"regexp"
	"testing"
)

// IP regex pattern - the target for Issue #50 optimization
const ipPattern = `(?:(?:25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])\.){3}(?:25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])`

// makeIPHaystack creates a haystack with IPs at specified density
// density: fraction of haystack that contains IP addresses
func makeIPHaystack(size int, density float64) []byte {
	result := make([]byte, size)

	// Fill with random lowercase letters (non-digit text)
	for i := range result {
		result[i] = 'a' + byte(rand.Intn(26))
	}

	// Sample IPs to embed
	ips := []string{
		"192.168.1.1",
		"10.0.0.1",
		"172.16.0.1",
		"255.255.255.255",
		"0.0.0.0",
		"1.2.3.4",
	}

	// Calculate number of IPs to embed based on density
	avgIPLen := 12.0 // Average IP length
	numIPs := int(float64(size) * density / avgIPLen)
	if numIPs == 0 && density > 0 {
		numIPs = 1
	}

	// Embed IPs at random positions
	for i := 0; i < numIPs; i++ {
		ip := ips[rand.Intn(len(ips))]
		pos := rand.Intn(size - len(ip))
		copy(result[pos:], ip)
	}

	return result
}

// BenchmarkIPRegex_Find compares Find performance for IP pattern
func BenchmarkIPRegex_Find(b *testing.B) {
	benchmarks := []struct {
		name    string
		size    int
		density float64
	}{
		{"1KB_sparse", 1024, 0.001},
		{"64KB_sparse", 64 * 1024, 0.001},
		{"1MB_sparse", 1024 * 1024, 0.001},
		{"6MB_sparse", 6 * 1024 * 1024, 0.001},
		{"64KB_dense", 64 * 1024, 0.05},
		{"1MB_dense", 1024 * 1024, 0.05},
		{"64KB_no_ips", 64 * 1024, 0},
		{"1MB_no_ips", 1024 * 1024, 0},
	}

	for _, bm := range benchmarks {
		haystack := makeIPHaystack(bm.size, bm.density)

		// Stdlib benchmark
		b.Run("stdlib_"+bm.name, func(b *testing.B) {
			re := regexp.MustCompile(ipPattern)
			b.SetBytes(int64(bm.size))
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_ = re.Find(haystack)
			}
		})

		// Coregex benchmark
		b.Run("coregex_"+bm.name, func(b *testing.B) {
			engine, err := Compile(ipPattern)
			if err != nil {
				b.Fatal(err)
			}

			b.SetBytes(int64(bm.size))
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_ = engine.Find(haystack)
			}
		})
	}
}

// BenchmarkIPRegex_Count compares Count performance for IP pattern
func BenchmarkIPRegex_Count(b *testing.B) {
	benchmarks := []struct {
		name    string
		size    int
		density float64
	}{
		{"64KB_sparse", 64 * 1024, 0.01},
		{"1MB_sparse", 1024 * 1024, 0.01},
		{"64KB_dense", 64 * 1024, 0.05},
	}

	for _, bm := range benchmarks {
		haystack := makeIPHaystack(bm.size, bm.density)

		// Stdlib benchmark (using FindAllIndex as stdlib has no Count)
		b.Run("stdlib_"+bm.name, func(b *testing.B) {
			re := regexp.MustCompile(ipPattern)
			b.SetBytes(int64(bm.size))
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_ = len(re.FindAllIndex(haystack, -1))
			}
		})

		// Coregex benchmark
		b.Run("coregex_"+bm.name, func(b *testing.B) {
			engine, err := Compile(ipPattern)
			if err != nil {
				b.Fatal(err)
			}

			b.SetBytes(int64(bm.size))
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_ = engine.Count(haystack, -1)
			}
		})
	}
}

// BenchmarkIPRegex_IsMatch compares IsMatch performance (no-match scenario)
func BenchmarkIPRegex_IsMatch(b *testing.B) {
	benchmarks := []struct {
		name string
		size int
	}{
		{"1KB", 1024},
		{"64KB", 64 * 1024},
		{"1MB", 1024 * 1024},
		{"6MB", 6 * 1024 * 1024},
	}

	for _, bm := range benchmarks {
		// No IPs - tests prefilter skip efficiency
		haystack := makeIPHaystack(bm.size, 0)

		// Stdlib benchmark
		b.Run("stdlib_noip_"+bm.name, func(b *testing.B) {
			re := regexp.MustCompile(ipPattern)
			b.SetBytes(int64(bm.size))
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_ = re.Match(haystack)
			}
		})

		// Coregex benchmark
		b.Run("coregex_noip_"+bm.name, func(b *testing.B) {
			engine, err := Compile(ipPattern)
			if err != nil {
				b.Fatal(err)
			}

			b.SetBytes(int64(bm.size))
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_ = engine.IsMatch(haystack)
			}
		})
	}
}
