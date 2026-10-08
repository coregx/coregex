package meta

import (
	"testing"
)

// TestBotsPatternUsesDFA verifies that the LangArena bots pattern routes
// to UseDFA. FIX-3 (SimpleFold orbits) grew NFA from 196→202 states,
// crossing the old threshold at 200. Threshold raised to 250.
//
// Pattern copied from regex-bench/go-coregex-langarena/main.go:34.
func TestBotsPatternUsesDFA(t *testing.T) {
	pattern := `(?i)bot|crawler|scanner|spider|indexing|crawl|robot|spider`

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	if engine.strategy != UseDFA && engine.strategy != UseBoth {
		t.Errorf("bots pattern: strategy=%s, want UseDFA or UseBoth (NFA states: %d)",
			engine.strategy, engine.nfa.States())
	}
}
