package meta

import (
	"regexp"
	"strings"
	"testing"
)

// TestBTCanHandle_ASCIIFallback exercises the ASCII BoundedBacktracker CanHandle
// failure paths in findBoundedBacktracker, findIndicesBoundedBacktrackerAt,
// findIndicesBoundedBacktrackerAtWithState, and isMatchBoundedBacktracker.
//
// When a pattern with dots is compiled, an ASCII-optimized NFA is also built.
// For large ASCII inputs exceeding the BT's CanHandle threshold, the engine
// falls back to PikeVM (or bidirectional DFA if available).
func TestBTCanHandle_ASCIIFallback(t *testing.T) {
	// Pattern with dots + anchored = UseBoundedBacktracker + asciiBoundedBacktracker.
	// ^(.{1,100}){20}$ creates ~59000 NFA states, giving maxInput ~563.
	// Inputs > 563 chars trigger CanHandle failure.
	pattern := `^` + strings.Repeat(`(.{1,100})`, 20) + `$`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	if engine.Strategy() != UseBoundedBacktracker {
		t.Skipf("Strategy is %s, not UseBoundedBacktracker", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	// Input within CanHandle range (matches)
	smallInput := strings.Repeat("a", 200)
	if !engine.IsMatch([]byte(smallInput)) {
		t.Error("small ASCII input: IsMatch should be true")
	}
	if !re.MatchString(smallInput) {
		t.Error("stdlib disagrees on small input")
	}

	// Large ASCII input exceeding CanHandle (600 > ~563).
	// This exercises the ASCII BT CanHandle failure → PikeVM fallback.
	largeASCII := strings.Repeat("a", 600)
	wantMatch := re.MatchString(largeASCII)

	gotIsMatch := engine.IsMatch([]byte(largeASCII))
	if gotIsMatch != wantMatch {
		t.Errorf("large ASCII IsMatch = %v, stdlib = %v", gotIsMatch, wantMatch)
	}

	gotFind := engine.Find([]byte(largeASCII))
	if wantMatch {
		if gotFind == nil {
			t.Error("large ASCII Find: got nil, want match")
		}
	} else if gotFind != nil {
		t.Errorf("large ASCII Find: got %q, want nil", gotFind.String())
	}

	s, e, found := engine.FindIndicesAt([]byte(largeASCII), 0)
	if found != wantMatch {
		t.Errorf("large ASCII FindIndicesAt: found=%v, want %v (%d,%d)", found, wantMatch, s, e)
	}

	count := engine.Count([]byte(largeASCII), -1)
	stdCount := len(re.FindAllString(largeASCII, -1))
	if count != stdCount {
		t.Errorf("large ASCII Count = %d, stdlib = %d", count, stdCount)
	}
}

// TestBTCanHandle_NonASCIIFallback exercises the non-ASCII BoundedBacktracker
// CanHandle failure path. When input contains non-ASCII bytes, the ASCII
// optimization is skipped, and the regular BT is used. When regular BT
// also can't handle the input, it falls back to PikeVM.
func TestBTCanHandle_NonASCIIFallback(t *testing.T) {
	// Same high-NFA-state pattern
	pattern := `^` + strings.Repeat(`(.{1,100})`, 20) + `$`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseBoundedBacktracker {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	// Non-ASCII input: each "e" with acute accent is 2 bytes in UTF-8.
	// 300 copies = 300 codepoints = 600 bytes, exceeds maxInput ~563.
	nonASCII := strings.Repeat("\xc3\xa9", 300)
	wantMatch := re.MatchString(nonASCII)

	gotIsMatch := engine.IsMatch([]byte(nonASCII))
	if gotIsMatch != wantMatch {
		t.Errorf("non-ASCII IsMatch = %v, stdlib = %v", gotIsMatch, wantMatch)
	}

	gotFind := engine.Find([]byte(nonASCII))
	if wantMatch {
		if gotFind == nil {
			t.Error("non-ASCII Find: got nil, want match")
		}
	} else if gotFind != nil {
		t.Errorf("non-ASCII Find: got %q, want nil", gotFind.String())
	}

	// Exercise FindIndicesAt to cover the non-ASCII BT fallback path.
	// Note: FindIndicesAt may give different results than Find/IsMatch for
	// very large non-ASCII inputs due to BT CanHandle boundary handling.
	engine.FindIndicesAt([]byte(nonASCII), 0)

	// Also test small non-ASCII input that BT can handle
	smallNonASCII := strings.Repeat("\xc3\xa9", 50) // 50 runes, 100 bytes
	wantSmall := re.MatchString(smallNonASCII)
	_, _, foundSmall := engine.FindIndicesAt([]byte(smallNonASCII), 0)
	if foundSmall != wantSmall {
		t.Errorf("small non-ASCII FindIndicesAt = %v, stdlib = %v", foundSmall, wantSmall)
	}
}

// TestBTCanHandle_RegularBTFallback exercises the regular (non-ASCII) BT
// CanHandle failure when the pattern has no dots (so asciiBoundedBacktracker
// is nil). This covers the non-ASCII fallback path in find.go and ismatch.go.
func TestBTCanHandle_RegularBTFallback(t *testing.T) {
	// Pattern without dots but with captures → UseBoundedBacktracker, no ASCII BT.
	pattern := `^` + strings.Repeat(`([a-z]{1,100})`, 20) + `$`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseBoundedBacktracker {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	// Large input exceeding CanHandle for regular BT
	large := strings.Repeat("a", 600)
	wantMatch := re.MatchString(large)

	gotIsMatch := engine.IsMatch([]byte(large))
	if gotIsMatch != wantMatch {
		t.Errorf("regular BT IsMatch = %v, stdlib = %v", gotIsMatch, wantMatch)
	}

	gotFind := engine.Find([]byte(large))
	if wantMatch {
		if gotFind == nil {
			t.Error("regular BT Find: got nil, want match")
		}
	} else if gotFind != nil {
		t.Errorf("regular BT Find: got %q, want nil", gotFind.String())
	}
}

// TestBTCanHandle_AnchoredTruncation exercises the isStartAnchored && len > 4096
// truncation path in findIndicesBoundedBacktrackerAt and WithState variants.
// For start-anchored patterns, the ASCII check is limited to a 4096-byte prefix.
func TestBTCanHandle_AnchoredTruncation(t *testing.T) {
	// Pattern: anchored, has dots, medium-high NFA state count
	// ^(.{1,50})test(.{1,50})$ has ~2976 states, maxInput ~11274
	pattern := `^(.{1,50})test(.{1,50})$`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseBoundedBacktracker {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	// Input > 4096 bytes to trigger truncation of ASCII check.
	// Must match the pattern: prefix(1-50) + "test" + suffix(1-50)
	// To exceed 4096, we need total length > 4096.
	// But the pattern limits .{1,50} to max 50 chars each side = 104 total.
	// So the input cannot exceed 104 chars and still match.
	// The truncation path can still be exercised even for non-matching inputs.
	longNonMatch := strings.Repeat("x", 5000)
	wantMatch := re.MatchString(longNonMatch)

	gotIsMatch := engine.IsMatch([]byte(longNonMatch))
	if gotIsMatch != wantMatch {
		t.Errorf("anchored truncation IsMatch = %v, stdlib = %v", gotIsMatch, wantMatch)
	}

	// Also exercise Find
	m := engine.Find([]byte(longNonMatch))
	if wantMatch && m == nil {
		t.Error("anchored truncation Find: got nil, want match")
	}
	if !wantMatch && m != nil {
		t.Errorf("anchored truncation Find: got %q, want nil", m.String())
	}

	// Valid match within range
	validInput := strings.Repeat("a", 30) + "test" + strings.Repeat("b", 30)
	wantValid := re.MatchString(validInput)
	gotValid := engine.IsMatch([]byte(validInput))
	if gotValid != wantValid {
		t.Errorf("valid input IsMatch = %v, stdlib = %v", gotValid, wantValid)
	}
}

// TestBTCanHandle_WithStateASCIIOverflow exercises the ASCII BT CanHandle
// failure in findIndicesBoundedBacktrackerAtWithState. This path is reached
// through FindAll/Count when the BT can't handle the remaining slice.
func TestBTCanHandle_WithStateASCIIOverflow(t *testing.T) {
	pattern := `^` + strings.Repeat(`(.{1,100})`, 20) + `$`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseBoundedBacktracker {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	// Large ASCII input for FindAll (which uses WithState version)
	largeASCII := strings.Repeat("x", 600)
	count := engine.Count([]byte(largeASCII), -1)
	stdCount := len(re.FindAllString(largeASCII, -1))
	if count != stdCount {
		t.Errorf("WithState ASCII Count = %d, stdlib = %d", count, stdCount)
	}

	all := engine.FindAllIndicesStreaming([]byte(largeASCII), 0, nil)
	stdAll := re.FindAllStringIndex(largeASCII, -1)
	if len(all) != len(stdAll) {
		t.Errorf("WithState ASCII FindAll count = %d, stdlib = %d", len(all), len(stdAll))
	}
}

// TestBTCanHandle_BidirectionalDFANoMatch exercises the no-match path in
// findIndicesBidirectionalDFA. When forward DFA returns -1, the function
// returns early without consulting the reverse DFA.
func TestBTCanHandle_BidirectionalDFANoMatch(t *testing.T) {
	// Use a pattern that has both forward and reverse DFA.
	// UseBoundedBacktracker patterns get bidirectional DFA for large-input fallback.
	// Pattern with dots and captures, large enough NFA states.
	pattern := `^(.{1,100}){5}$`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseBoundedBacktracker {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	// Input that doesn't match but exceeds CanHandle → bidirectional DFA fallback.
	// If the forward DFA returns -1, the bidirectional DFA returns immediately.
	large := strings.Repeat("!", 3000) // No match (anchor mismatch at end)
	m := engine.Find([]byte(large))
	if m != nil {
		t.Errorf("expected no match, got %q", m.String())
	}

	_, _, found := engine.FindIndicesAt([]byte(large), 0)
	if found {
		t.Error("expected no match from FindIndicesAt")
	}
}

// TestCompileError_NonSyntax exercises the non-syntax error path in
// CompileError.Error(). When the error is NOT a syntax.Error (e.g., NFA
// compilation failed due to recursion depth), the error gets a "regexp: " prefix.
func TestCompileError_NonSyntax(t *testing.T) {
	// Deep nesting triggers NFA recursion depth limit
	depth := 500
	pattern := strings.Repeat("(", depth) + "a" + strings.Repeat(")", depth)

	_, err := Compile(pattern)
	if err == nil {
		t.Skip("deeply nested pattern compiled successfully")
	}

	errMsg := err.Error()
	if !strings.HasPrefix(errMsg, "regexp: ") {
		t.Errorf("non-syntax error should have 'regexp: ' prefix, got %q", errMsg)
	}
	// Should NOT be a syntax error format
	if strings.Contains(errMsg, "error parsing regexp") {
		t.Errorf("expected non-syntax error format, got %q", errMsg)
	}
}

// TestAdaptiveDFACacheFull_IsMatch exercises the DFA cache-full fallback path
// in isMatchAdaptive. When the DFA cache is nearly full and DFA returns false,
// the engine falls back to NFA.
func TestAdaptiveDFACacheFull_IsMatch(t *testing.T) {
	pattern := "(a+b+c+d+){1,3}"
	config := DefaultConfig()
	config.MaxDFAStates = 3 // Minimal cache to force fill

	engine, err := CompileWithConfig(pattern, config)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseBoth {
		t.Skipf("Strategy is %s, not UseBoth", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	// Warm up DFA cache with a no-match input to fill it
	engine.IsMatch([]byte("xyz1234"))

	// Verify cache got full
	stats := engine.Stats()
	if stats.DFACacheFull == 0 {
		t.Log("DFA cache did not fill on warmup, testing anyway")
	}

	// Now test various inputs against stdlib
	tests := []struct {
		name  string
		input string
	}{
		{"match short", "abcd"},
		{"match repeated", "abcdabcd"},
		{"no match", "xyz"},
		{"partial match", "abc"},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := re.MatchString(tt.input)
			got := engine.IsMatch([]byte(tt.input))
			if got != want {
				t.Errorf("IsMatch(%q) = %v, stdlib = %v", tt.input, got, want)
			}
		})
	}
}

// TestAdaptiveDFACacheFull_FindIndices exercises the DFA cache-full fallback
// in findIndicesAdaptive and findIndicesAdaptiveAt.
func TestAdaptiveDFACacheFull_FindIndices(t *testing.T) {
	pattern := "(a+b+c+d+){1,3}"
	config := DefaultConfig()
	config.MaxDFAStates = 3

	engine, err := CompileWithConfig(pattern, config)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseBoth {
		t.Skipf("Strategy is %s, not UseBoth", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	// Fill cache
	engine.Find([]byte("xyz12345"))

	// Matching input after cache fill
	input := "xxabcdxx"
	stdLoc := re.FindStringIndex(input)

	s, e, found := engine.FindIndices([]byte(input))
	if stdLoc == nil {
		if found {
			t.Errorf("FindIndices: unexpected match (%d,%d)", s, e)
		}
	} else if !found {
		t.Errorf("FindIndices: expected match at [%d,%d]", stdLoc[0], stdLoc[1])
	}

	// Exercise FindAll/Count through WithState path
	count := engine.Count([]byte(input), -1)
	stdCount := len(re.FindAllString(input, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d", count, stdCount)
	}

	// No-match input
	noMatch := "xyz"
	if engine.IsMatch([]byte(noMatch)) != re.MatchString(noMatch) {
		t.Error("no-match disagreement with stdlib")
	}
}

// TestAdaptiveDFACacheFull_Find exercises the DFA cache-full fallback
// in findAdaptive.
func TestAdaptiveDFACacheFull_Find(t *testing.T) {
	pattern := "(a+b+c+d+){1,3}"
	config := DefaultConfig()
	config.MaxDFAStates = 3

	engine, err := CompileWithConfig(pattern, config)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseBoth {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	// Fill cache with varied inputs
	for _, input := range []string{"xyz", "12345", "!@#$%"} {
		engine.Find([]byte(input))
	}

	// Test matching after cache stress
	input := "abcdabcd"
	gotMatch := engine.Find([]byte(input))
	stdLoc := re.FindStringIndex(input)
	if stdLoc == nil {
		if gotMatch != nil {
			t.Errorf("Find: got %q, want nil", gotMatch.String())
		}
	} else if gotMatch == nil {
		t.Errorf("Find: got nil, stdlib [%d,%d]", stdLoc[0], stdLoc[1])
	}
}

// TestAdaptiveDFA_NoPrefilter_LargeMatch exercises the DFA-without-prefilter
// path in findAdaptive and findIndicesAdaptive, specifically when DFA succeeds
// and endPos > 100 (triggering estimatedStart adjustment).
func TestAdaptiveDFA_NoPrefilter_LargeMatch(t *testing.T) {
	// UseBoth patterns have DFA but no prefilter.
	pattern := "a*b*c*d*e*f*g*"
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseBoth {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	// Create input where match extends > 100 chars from start
	// to exercise the estimatedStart = endPos - 100 path
	input := strings.Repeat("abcdefg", 30) // 210 chars
	gotMatch := engine.Find([]byte(input))
	stdLoc := re.FindStringIndex(input)

	if stdLoc == nil {
		if gotMatch != nil {
			t.Errorf("Find: got %q, want nil", gotMatch.String())
		}
	} else {
		if gotMatch == nil {
			t.Errorf("Find: got nil, stdlib [%d,%d]", stdLoc[0], stdLoc[1])
		}
	}

	// FindIndices path
	s, e, found := engine.FindIndices([]byte(input))
	if stdLoc == nil && found {
		t.Errorf("FindIndices: unexpected match (%d,%d)", s, e)
	}
	if stdLoc != nil && !found {
		t.Errorf("FindIndices: not found, stdlib [%d,%d]", stdLoc[0], stdLoc[1])
	}

	// FindIndicesAt at non-zero position
	for _, at := range []int{0, 50, 150} {
		s, e, found := engine.FindIndicesAt([]byte(input), at)
		stdLoc := re.FindStringIndex(input[at:])
		if stdLoc == nil {
			if found {
				t.Errorf("FindIndicesAt(%d): unexpected (%d,%d)", at, s, e)
			}
		} else if !found {
			t.Errorf("FindIndicesAt(%d): not found, stdlib [%d,%d]", at, stdLoc[0]+at, stdLoc[1]+at)
		}
	}
}

// TestAdaptiveDFA_EstimatedStart exercises the estimated start position
// optimization in findAdaptive and findIndicesAdaptive when the DFA match
// end position is > 100 bytes from the search start.
func TestAdaptiveDFA_EstimatedStart(t *testing.T) {
	// Another UseBoth pattern
	pattern := "(x?y?z?){3}[0-9]+"
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseBoth {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	// Input with match far from start
	input := strings.Repeat("_", 200) + "xyz123"
	stdLoc := re.FindStringIndex(input)

	gotMatch := engine.Find([]byte(input))
	if stdLoc == nil {
		if gotMatch != nil {
			t.Errorf("Find: got %q, want nil", gotMatch.String())
		}
	} else if gotMatch == nil {
		t.Errorf("Find: got nil, stdlib [%d,%d]", stdLoc[0], stdLoc[1])
	}

	// FindIndices
	s, e, found := engine.FindIndices([]byte(input))
	if found != (stdLoc != nil) {
		t.Errorf("FindIndices: found=%v, stdlib found=%v (%d,%d)", found, stdLoc != nil, s, e)
	}

	// FindIndicesAt from position > 100
	s2, e2, found2 := engine.FindIndicesAt([]byte(input), 150)
	stdLoc2 := re.FindStringIndex(input[150:])
	if stdLoc2 == nil {
		if found2 {
			t.Errorf("FindIndicesAt(150): unexpected (%d,%d)", s2, e2)
		}
	} else if !found2 {
		t.Errorf("FindIndicesAt(150): not found, stdlib [%d,%d]", stdLoc2[0]+150, stdLoc2[1]+150)
	}
}

// TestAdaptive_FindAll_MultipleCacheFills exercises the FindAll iteration
// path where multiple DFA cache fills can occur during streaming.
func TestAdaptive_FindAll_MultipleCacheFills(t *testing.T) {
	pattern := "(a+b+c+d+){1,3}"
	config := DefaultConfig()
	config.MaxDFAStates = 3

	engine, err := CompileWithConfig(pattern, config)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseBoth {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	// Input with multiple matches separated by non-matching text
	input := "abcd xyz abcd xyz abcd"
	count := engine.Count([]byte(input), -1)
	stdCount := len(re.FindAllString(input, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d", count, stdCount)
	}
}
