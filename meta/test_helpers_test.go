package meta

// Shared test helper functions for meta package tests.

import (
	"testing"
)

func useBothPattern() string {
	return `(a|b|c|d|e|f|g|h)*(a|b|c|d|e|f|g|h)*(a|b|c|d|e|f|g|h)*(a|b|c|d|e|f|g|h)*z`
}

// requireStrategy skips the test if the engine does not use the expected strategy.

func requireStrategy(t *testing.T, engine *Engine, want Strategy) {
	t.Helper()
	if engine.Strategy() != want {
		t.Skipf("pattern uses %s, not %s", engine.Strategy(), want)
	}
}

// -----------------------------------------------------------------------------
// 1. findAdaptiveAt (find.go:348) — UseBoth strategy, non-zero positions
//    Also covers findAdaptive (find.go:226) branches.
//
// UseBoth is selected for medium NFA (20-100 states) without strong literals.
// The adaptive path tries DFA first, then NFA.
// The "At" variant is triggered when FindAll iterates with at > 0.
// -----------------------------------------------------------------------------

func decodeUTF8(b []byte) rune {
	if len(b) == 0 {
		return -1
	}
	if b[0] < 0x80 {
		return rune(b[0])
	}
	if b[0] < 0xE0 {
		return rune(b[0]&0x1F)<<6 | rune(b[1]&0x3F)
	}
	if b[0] < 0xF0 {
		return rune(b[0]&0x0F)<<12 | rune(b[1]&0x3F)<<6 | rune(b[2]&0x3F)
	}
	return rune(b[0]&0x07)<<18 | rune(b[1]&0x3F)<<12 | rune(b[2]&0x3F)<<6 | rune(b[3]&0x3F)
}

// -----------------------------------------------------------------------------
// 8. findDFAAt (find.go:307) — UseDFA at non-zero position (currently 40%)
// -----------------------------------------------------------------------------

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
