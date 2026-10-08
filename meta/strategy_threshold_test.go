package meta

import (
	"strings"
	"testing"
	"time"
)

// TestBotsPatternUsesDFA verifies that the LangArena bots pattern routes
// to UseDFA. FIX-3 (SimpleFold orbits) grew NFA from 196→202 states,
// crossing the old threshold at 200. Threshold raised to 250.
//
// Pattern copied from regex-bench/go-coregex-langarena/main.go:34.
func TestBotsPatternUsesDFA(t *testing.T) {
	// Exact pattern from regex-bench CI
	pattern := `(?i)bot|crawler|scanner|spider|indexing|crawl|robot|spider`

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	if engine.strategy != UseDFA && engine.strategy != UseBoth {
		t.Errorf("bots pattern: strategy=%s, want UseDFA or UseBoth",
			engine.strategy)
	}

	// Verify search performance is reasonable on ~2MB input
	input := []byte(strings.Repeat("GET /index.html HTTP/1.1 Mozilla/5.0\n", 50000))
	start := time.Now()
	_ = engine.IsMatch(input)
	elapsed := time.Since(start)

	if elapsed > 200*time.Millisecond {
		t.Errorf("bots pattern on %dB: %v (>200ms — likely fell to PikeVM)",
			len(input), elapsed)
	}
}
