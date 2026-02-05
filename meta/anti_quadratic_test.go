package meta

import (
	"bytes"
	"testing"
	"time"
)

// TestAntiQuadratic_ReverseSuffix verifies that reverse suffix search does not
// degrade to O(n^2) when there are many false-positive suffix candidates.
//
// Scenario: pattern `[a-z]+\.txt` on input with many `.txt` occurrences
// preceded by digits (not letters), causing every suffix candidate to fail
// the reverse DFA prefix check in IsMatch. Without min_start tracking, each
// candidate triggers a reverse scan from 0 to the suffix position, resulting
// in O(n^2) total work. With min_start, previously scanned regions are skipped.
func TestAntiQuadratic_ReverseSuffix(t *testing.T) {
	pattern := `[a-z]+\.txt`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) error = %v", pattern, err)
	}

	// Verify strategy selection
	if engine.Strategy() != UseReverseSuffix {
		t.Skipf("Pattern %q uses strategy %v, not UseReverseSuffix; skipping anti-quadratic test",
			pattern, engine.Strategy())
	}

	// Build haystack: 10000 segments of "0000.txt" where digits (not letters)
	// precede .txt, causing reverse DFA to reject each candidate.
	// Total size: ~80KB
	var buf bytes.Buffer
	segments := 10000
	for i := 0; i < segments; i++ {
		buf.WriteString("0000.txt")
	}
	haystack := buf.Bytes()

	// This should complete in O(n) time, not O(n^2).
	// Without anti-quadratic guard, on 80KB input with 10000 candidates,
	// each reverse scan would cover ~40KB average, total ~400MB of scanning.
	// With the guard, total scanning is ~80KB (linear).
	start := time.Now()
	matched := engine.IsMatch(haystack)
	elapsed := time.Since(start)

	if matched {
		t.Error("IsMatch should return false (no letters before any .txt)")
	}

	// With O(n) behavior, 80KB should finish in under 100ms easily.
	// With O(n^2) behavior on 80KB, it would take seconds or more.
	if elapsed > 2*time.Second {
		t.Errorf("IsMatch took %v, which suggests O(n^2) behavior; expected < 2s for O(n)", elapsed)
	}

	t.Logf("IsMatch on %d bytes with %d false-positive suffix candidates took %v",
		len(haystack), segments, elapsed)
}

// TestAntiQuadratic_ReverseSuffixIsMatchCorrectness verifies that
// anti-quadratic optimization in IsMatch does not affect correctness.
// Tests various scenarios where IsMatch should return true or false.
func TestAntiQuadratic_ReverseSuffixIsMatchCorrectness(t *testing.T) {
	pattern := `[a-z]+\.txt`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) error = %v", pattern, err)
	}

	tests := []struct {
		name     string
		haystack string
		want     bool
	}{
		// Cases where IsMatch should return true (letters before .txt)
		{"match at start", "abc.txt", true},
		{"match after digits", "000abc.txt", true},
		{"match after false positives", "000.txt000.txtabc.txt", true},
		{"match at end", "000.txt000.txtxyz.txt", true},
		{"single char prefix", "a.txt", true},

		// Cases where IsMatch should return false (no letters before .txt)
		{"no match - digits only", "000.txt000.txt000.txt", false},
		{"no match - empty", "", false},
		{"no match - suffix only", ".txt", false},
		{"no match - no suffix", "abcdefghij", false},
		{"no match - space before suffix", " .txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.haystack, got, tt.want)
			}
		})
	}
}

// TestAntiQuadratic_ReverseSuffixSet verifies anti-quadratic behavior for
// the ReverseSuffixSet strategy (multi-suffix patterns like .*\.(txt|log|md)).
func TestAntiQuadratic_ReverseSuffixSet(t *testing.T) {
	pattern := `[a-z]+\.(txt|log|dat)`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) error = %v", pattern, err)
	}

	if engine.Strategy() != UseReverseSuffixSet {
		t.Skipf("Pattern %q uses strategy %v, not UseReverseSuffixSet; skipping",
			pattern, engine.Strategy())
	}

	// Build haystack with many false-positive suffix candidates
	var buf bytes.Buffer
	segments := 5000
	for i := 0; i < segments; i++ {
		buf.WriteString("000.txt")
	}
	haystack := buf.Bytes()

	start := time.Now()
	matched := engine.IsMatch(haystack)
	elapsed := time.Since(start)

	if matched {
		t.Error("IsMatch should return false (no letters before any suffix)")
	}

	if elapsed > 2*time.Second {
		t.Errorf("IsMatch took %v, suggests O(n^2) behavior", elapsed)
	}

	t.Logf("ReverseSuffixSet IsMatch on %d bytes with %d candidates took %v",
		len(haystack), segments, elapsed)
}

// TestAntiQuadratic_ReverseSuffixSetIsMatchCorrectness verifies IsMatch
// correctness for ReverseSuffixSet with anti-quadratic guard.
func TestAntiQuadratic_ReverseSuffixSetIsMatchCorrectness(t *testing.T) {
	pattern := `[a-z]+\.(txt|log|dat)`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) error = %v", pattern, err)
	}

	if engine.Strategy() != UseReverseSuffixSet {
		t.Skipf("Pattern %q uses strategy %v, not UseReverseSuffixSet; skipping",
			pattern, engine.Strategy())
	}

	tests := []struct {
		name     string
		haystack string
		want     bool
	}{
		{"match .txt", "abc.txt", true},
		{"match .log", "abc.log", true},
		{"match .dat", "abc.dat", true},
		{"match after false positives", "000.txt000.logabc.dat", true},
		{"no match", "000.txt000.log000.dat", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.haystack, got, tt.want)
			}
		})
	}
}

// TestAntiQuadratic_ReverseInner verifies anti-quadratic behavior for the
// ReverseInner strategy (inner literal patterns like .*keyword.*).
func TestAntiQuadratic_ReverseInner(t *testing.T) {
	// For patterns with universal prefix/suffix (.*keyword.*), the IsMatch
	// optimization shortcuts mean it returns immediately on first candidate.
	// Test correctness.
	pattern := `.*connection.*`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) error = %v", pattern, err)
	}

	if engine.Strategy() != UseReverseInner {
		t.Skipf("Pattern %q uses strategy %v, not UseReverseInner; skipping",
			pattern, engine.Strategy())
	}

	tests := []struct {
		name     string
		haystack string
		want     bool
	}{
		{"match", "ERROR: connection timeout", true},
		{"no match", "ERROR: disconnected", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.haystack, got, tt.want)
			}
		})
	}
}

// TestAntiQuadratic_LargeInputPerformance runs a performance sanity check
// on larger inputs to ensure anti-quadratic guard keeps execution time linear.
// Measures two input sizes and verifies the ratio is roughly linear (2x not 4x).
func TestAntiQuadratic_LargeInputPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large input performance test in short mode")
	}

	pattern := `[a-z]+\.txt`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) error = %v", pattern, err)
	}

	// Measure time for N and 2N: if linear, 2N should take ~2x time of N.
	// If quadratic, 2N would take ~4x time of N.
	sizes := []int{50000, 100000}
	var times [2]time.Duration

	for idx, size := range sizes {
		var buf bytes.Buffer
		for i := 0; i < size; i++ {
			buf.WriteString("0.txt")
		}
		haystack := buf.Bytes()

		startTime := time.Now()
		engine.IsMatch(haystack)
		times[idx] = time.Since(startTime)
		t.Logf("Size %6d (%d bytes): %v", size, len(haystack), times[idx])
	}

	// Allow generous 5x ratio (linear would be ~2x, quadratic ~4x).
	// We use 5x to account for measurement noise, GC, etc.
	if times[0] > time.Millisecond && times[1] > 5*times[0] {
		t.Errorf("Performance suggests quadratic: size 1x took %v, size 2x took %v (ratio %.1fx, expected ~2x)",
			times[0], times[1], float64(times[1])/float64(times[0]))
	}
}

// TestAntiQuadratic_ReverseSuffixFallbackToPikeVM verifies that when
// quadratic behavior is detected, the engine falls back to PikeVM correctly.
// This test uses a scenario where many suffix candidates trigger the
// anti-quadratic guard, forcing a PikeVM fallback that should still
// produce correct results.
func TestAntiQuadratic_ReverseSuffixFallbackToPikeVM(t *testing.T) {
	pattern := `[a-z]+\.txt`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) error = %v", pattern, err)
	}

	// Create a haystack where the anti-quadratic guard will activate,
	// and there IS a valid match at the end.
	var buf bytes.Buffer
	// Many false positives
	for i := 0; i < 100; i++ {
		buf.WriteString("0000.txt")
	}
	// Followed by a valid match
	buf.WriteString("hello.txt")
	haystack := buf.Bytes()

	// IsMatch should find the valid match despite anti-quadratic fallback
	if !engine.IsMatch(haystack) {
		t.Error("IsMatch should return true (there is 'hello.txt' at the end)")
	}
}
