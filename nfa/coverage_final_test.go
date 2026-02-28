package nfa

import (
	"regexp/syntax"
	"testing"
)

// --- compile.go: anchor analysis functions (all 0% covered) ---

func TestIsPatternEndAnchored(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"abc$", true},
		{"abc", false},
		{"(a|b)$", true},
		{"(a$|b)", false},
		{"^abc$", true},
		{"abc\\z", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatal(err)
			}
			got := IsPatternEndAnchored(re)
			if got != tt.want {
				t.Errorf("IsPatternEndAnchored(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

func TestHasImpossibleEndAnchor(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"abc$", false},   // $ at end - valid
		{"abc", false},    // No $ at all
		{"(a$)", false},   // $ at end of group - valid
		{"^abc$", false},  // $ at end - valid
		{"abc\\z", false}, // \z at end - valid
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatal(err)
			}
			got := HasImpossibleEndAnchor(re)
			if got != tt.want {
				t.Errorf("HasImpossibleEndAnchor(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

func TestIsPatternStartAnchored(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"^abc", true},
		{"abc", false},
		{"(^a|b)", true},
		{"(a|^b)", true},
		{"(a|b)", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatal(err)
			}
			got := IsPatternStartAnchored(re)
			if got != tt.want {
				t.Errorf("IsPatternStartAnchored(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

// --- compile.go: compileNoMatch - triggered by empty char class ---

func TestCompileNoMatchPattern(t *testing.T) {
	// [^\s\S] matches nothing (complement of "any char")
	// Go syntax package normalizes this to OpNoMatch
	compiler := NewDefaultCompiler()
	n, err := compiler.Compile("[^\\s\\S]")
	if err != nil {
		t.Fatal(err)
	}
	if n == nil {
		t.Fatal("expected non-nil NFA")
	}

	// This pattern should never match
	vm := NewPikeVM(n)
	if vm.IsMatch([]byte("anything")) {
		t.Error("expected no match for impossible pattern")
	}
}

// --- compile.go: UTF-8 1-byte range (ASCII char class) ---

func TestCompileUTF81ByteRange(t *testing.T) {
	// [A-Z] is a pure ASCII char class -> compileUTF81ByteRange
	compiler := NewDefaultCompiler()
	n, err := compiler.Compile("[A-Z]+")
	if err != nil {
		t.Fatal(err)
	}

	vm := NewPikeVM(n)
	if !vm.IsMatch([]byte("HELLO")) {
		t.Error("expected match for [A-Z]+ on HELLO")
	}
	if vm.IsMatch([]byte("hello")) {
		t.Error("expected no match for [A-Z]+ on hello")
	}
}

// --- compile.go: non-greedy quantifiers (exercise else branches) ---

func TestCompileNonGreedyQuantifiers(t *testing.T) {
	tests := []struct {
		pattern  string
		input    string
		wantFind bool
	}{
		{"a*?b", "aab", true},  // Non-greedy star
		{"a+?b", "aab", true},  // Non-greedy plus
		{"a??b", "ab", true},   // Non-greedy quest
		{"a*?b", "b", true},    // Non-greedy star: zero a's
		{"a+?b", "b", false},   // Non-greedy plus: needs at least one a
		{"a??b", "b", true},    // Non-greedy quest: zero a's
		{"a*?b", "xxx", false}, // No b at all
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			n, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}
			vm := NewPikeVM(n)
			got := vm.IsMatch([]byte(tt.input))
			if got != tt.wantFind {
				t.Errorf("IsMatch(%q, %q) = %v, want %v", tt.pattern, tt.input, got, tt.wantFind)
			}
		})
	}
}

// --- pikevm.go: NewPikeVMState (0% covered) ---

func TestNewPikeVMState(t *testing.T) {
	state := NewPikeVMState()
	if state == nil {
		t.Fatal("expected non-nil PikeVMState")
	}
}

// --- pikevm.go: SearchBetween ---

func TestSearchBetween(t *testing.T) {
	compiler := NewDefaultCompiler()
	n, err := compiler.Compile("abc")
	if err != nil {
		t.Fatal(err)
	}
	vm := NewPikeVM(n)

	tests := []struct {
		name      string
		haystack  string
		startAt   int
		maxEnd    int
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"match_in_range", "xabcx", 0, 5, 1, 4, true},
		{"match_exact_range", "abc", 0, 3, 0, 3, true},
		{"no_match_range", "xyz", 0, 3, -1, -1, false},
		{"start_past_max", "abc", 3, 3, -1, -1, false},
		{"start_gt_haystack", "abc", 10, 20, -1, -1, false},
		{"maxend_gt_haystack", "xabcx", 0, 100, 1, 4, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, e, f := vm.SearchBetween([]byte(tt.haystack), tt.startAt, tt.maxEnd)
			if f != tt.wantFound || s != tt.wantStart || e != tt.wantEnd {
				t.Errorf("SearchBetween(%q, %d, %d) = (%d, %d, %v), want (%d, %d, %v)",
					tt.haystack, tt.startAt, tt.maxEnd, s, e, f,
					tt.wantStart, tt.wantEnd, tt.wantFound)
			}
		})
	}
}

// --- pikevm.go: SearchWithCapturesAt ---

func TestSearchWithCapturesAt(t *testing.T) {
	compiler := NewDefaultCompiler()
	n, err := compiler.Compile("(a+)(b+)")
	if err != nil {
		t.Fatal(err)
	}
	vm := NewPikeVM(n)

	m := vm.SearchWithCapturesAt([]byte("xaabbx"), 0)
	if m == nil {
		t.Fatal("expected match")
	}
	if m.Start != 1 || m.End != 5 {
		t.Errorf("expected match (1, 5), got (%d, %d)", m.Start, m.End)
	}

	// At offset
	m = vm.SearchWithCapturesAt([]byte("aaabb"), 2)
	if m == nil {
		t.Fatal("expected match at offset 2")
	}

	// At end of input (no match)
	m = vm.SearchWithCapturesAt([]byte("aabb"), 4)
	if m != nil {
		t.Error("expected nil at end of input")
	}

	// Past end
	m = vm.SearchWithCapturesAt([]byte("aabb"), 10)
	if m != nil {
		t.Error("expected nil past end")
	}
}

// --- pikevm.go: SearchWithSlotTableAt ---

func TestSearchWithSlotTableAt(t *testing.T) {
	compiler := NewDefaultCompiler()
	n, err := compiler.Compile("(a+)b")
	if err != nil {
		t.Fatal(err)
	}
	vm := NewPikeVM(n)

	s, e, found := vm.SearchWithSlotTableAt([]byte("xaabx"), 0, SearchModeFind)
	if !found {
		t.Fatal("expected match")
	}
	if s != 1 || e != 4 {
		t.Errorf("expected (1, 4), got (%d, %d)", s, e)
	}

	// Also test with captures mode
	s, e, found = vm.SearchWithSlotTableAt([]byte("xaabx"), 0, SearchModeCaptures)
	if !found {
		t.Fatal("expected match with captures mode")
	}
	if s != 1 || e != 4 {
		t.Errorf("captures mode: expected (1, 4), got (%d, %d)", s, e)
	}
}

// --- composite.go: IsCompositeCharClassPattern ---

func TestIsCompositeCharClassPatternCoverage(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"[a-z]+[0-9]+", true},
		{"[A-Z]+[a-z]+", true},
		{"abc", false},           // Not a concat of char classes
		{"[a-z]+", false},        // Only one part
		{"[a-z]+|[0-9]+", false}, // Alternation, not concat
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatal(err)
			}
			got := IsCompositeCharClassPattern(re)
			if got != tt.want {
				t.Errorf("IsCompositeCharClassPattern(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

// --- composite_dfa.go: IsCompositeSequenceDFAPattern ---

func TestIsCompositeSequenceDFAPattern(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"[a-z]+[0-9]+", true},
		{"abc", false},
		{"[a-z]*[0-9]+", false}, // Star has minMatch=0
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatal(err)
			}
			got := IsCompositeSequenceDFAPattern(re)
			if got != tt.want {
				t.Errorf("IsCompositeSequenceDFAPattern(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

// --- branch_dispatch.go: IsBranchDispatchPattern ---

func TestIsBranchDispatchPattern(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"^(abc|def)", true},
		{"abc|def", false}, // No start anchor
		{"^abc", false},    // No alternation
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatal(err)
			}
			got := IsBranchDispatchPattern(re)
			if got != tt.want {
				t.Errorf("IsBranchDispatchPattern(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

// --- backtrack.go: runeWidth ---

func TestRuneWidth(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  int
	}{
		{"empty", []byte{}, 0},
		{"ascii", []byte("a"), 1},
		{"two_byte", []byte{0xC2, 0xA3}, 2},              // £
		{"three_byte", []byte{0xE4, 0xB8, 0xAD}, 3},      // 中
		{"four_byte", []byte{0xF0, 0x9F, 0x98, 0x80}, 4}, // 😀
		{"invalid_continuation", []byte{0x80}, 1},        // Invalid lead byte
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runeWidth(tt.input)
			if got != tt.want {
				t.Errorf("runeWidth(%v) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// --- backtrack.go: backtrackFindLongestWithState ---

func TestBoundedBacktrackerFindLongest(t *testing.T) {
	compiler := NewDefaultCompiler()
	n, err := compiler.Compile("a+")
	if err != nil {
		t.Fatal(err)
	}

	bt := NewBoundedBacktracker(n)
	if bt == nil {
		t.Fatal("expected non-nil BoundedBacktracker")
	}

	// Use Longest mode via state
	bt.SetLongest(true)

	// Search with longest mode
	s, e, found := bt.SearchAt([]byte("aaab"), 0)
	if !found {
		t.Fatal("expected match")
	}
	if s != 0 || e != 3 {
		t.Errorf("expected (0, 3), got (%d, %d)", s, e)
	}

	// No match
	s, e, found = bt.SearchAt([]byte("bbb"), 0)
	if found {
		t.Errorf("expected no match, got (%d, %d)", s, e)
	}
}

// --- slot_table.go: SlotsPerState ---

func TestSlotTableSlotsPerState(t *testing.T) {
	st := NewSlotTable(10, 4)
	if st.SlotsPerState() != 4 {
		t.Errorf("expected SlotsPerState=4, got %d", st.SlotsPerState())
	}
}

// --- reverse.go: ReverseAnchored ---

func TestReverseAnchored(t *testing.T) {
	compiler := NewDefaultCompiler()
	fwdNFA, err := compiler.Compile("abc$")
	if err != nil {
		t.Fatal(err)
	}

	revNFA := ReverseAnchored(fwdNFA)
	if revNFA == nil {
		t.Fatal("expected non-nil reverse NFA")
	}
	if !revNFA.IsAnchored() {
		t.Error("expected reverse NFA to be anchored")
	}
}

// --- pikevm.go: PikeVM InitState with external state ---

func TestPikeVMInitState(t *testing.T) {
	compiler := NewDefaultCompiler()
	n, err := compiler.Compile("(a+)b")
	if err != nil {
		t.Fatal(err)
	}
	vm := NewPikeVM(n)

	state := NewPikeVMState()
	vm.InitState(state)

	// State should be usable now
	if state.Queue == nil {
		t.Error("expected Queue to be initialized")
	}
}

// --- pikevm.go: searchAtWithCaptures (anchored pattern) ---

func TestSearchWithCapturesAnchored(t *testing.T) {
	compiler := NewDefaultCompiler()
	n, err := compiler.Compile("^(a+)(b+)")
	if err != nil {
		t.Fatal(err)
	}
	vm := NewPikeVM(n)

	m := vm.SearchWithCapturesAt([]byte("aabb"), 0)
	if m == nil {
		t.Fatal("expected match for anchored pattern")
	}
	if m.Start != 0 || m.End != 4 {
		t.Errorf("expected (0, 4), got (%d, %d)", m.Start, m.End)
	}

	// At offset 1 should not match (anchored at ^)
	m = vm.SearchWithCapturesAt([]byte("xaabb"), 1)
	if m != nil {
		t.Error("expected no match at offset 1 for ^-anchored pattern")
	}
}

// --- compile.go: capture groups ---

func TestCompileCapture(t *testing.T) {
	compiler := NewDefaultCompiler()
	n, err := compiler.Compile("(a)(b)(c)")
	if err != nil {
		t.Fatal(err)
	}

	if n.CaptureCount() < 3 {
		t.Errorf("expected at least 3 capture groups, got %d", n.CaptureCount())
	}

	vm := NewPikeVM(n)
	m := vm.SearchWithCaptures([]byte("abc"))
	if m == nil {
		t.Fatal("expected match")
	}
	if len(m.Captures) < 4 { // group 0 + 3 capture groups
		t.Errorf("expected at least 4 capture groups, got %d", len(m.Captures))
	}
}

// --- compile.go: repeat patterns ---

func TestCompileRepeatExact(t *testing.T) {
	compiler := NewDefaultCompiler()
	n, err := compiler.Compile("a{3}")
	if err != nil {
		t.Fatal(err)
	}
	vm := NewPikeVM(n)

	if !vm.IsMatch([]byte("aaa")) {
		t.Error("expected match for a{3} on 'aaa'")
	}
	if vm.IsMatch([]byte("aa")) {
		t.Error("expected no match for a{3} on 'aa'")
	}
}

func TestCompileRepeatRange(t *testing.T) {
	compiler := NewDefaultCompiler()
	n, err := compiler.Compile("a{2,4}")
	if err != nil {
		t.Fatal(err)
	}
	vm := NewPikeVM(n)

	if vm.IsMatch([]byte("a")) {
		t.Error("expected no match for a{2,4} on 'a'")
	}
	if !vm.IsMatch([]byte("aaa")) {
		t.Error("expected match for a{2,4} on 'aaa'")
	}
}

// --- compile.go: Unicode char class (3-byte and wide ranges) ---

func TestCompileUnicodeCharClass(t *testing.T) {
	compiler := NewDefaultCompiler()
	// \p{Han} triggers 3-byte UTF-8 compilation
	n, err := compiler.Compile("\\p{Han}+")
	if err != nil {
		t.Fatal(err)
	}
	vm := NewPikeVM(n)

	if !vm.IsMatch([]byte("中文")) {
		t.Error("expected match for \\p{Han} on Chinese chars")
	}
	if vm.IsMatch([]byte("abc")) {
		t.Error("expected no match for \\p{Han} on ASCII")
	}
}

// --- pikevm.go: SetLongest ---

func TestPikeVMSetLongest(t *testing.T) {
	compiler := NewDefaultCompiler()
	n, err := compiler.Compile("a|aa")
	if err != nil {
		t.Fatal(err)
	}
	vm := NewPikeVM(n)

	// Default (leftmost-first): should match "a"
	s, e, found := vm.SearchAt([]byte("aa"), 0)
	if !found || s != 0 || e != 1 {
		t.Errorf("leftmost-first: expected (0, 1), got (%d, %d)", s, e)
	}

	// Set longest
	vm.SetLongest(true)
	s, e, found = vm.SearchAt([]byte("aa"), 0)
	if !found || s != 0 || e != 2 {
		t.Errorf("leftmost-longest: expected (0, 2), got (%d, %d)", s, e)
	}
}

// --- compile.go: case folding ---

func TestCompileFoldCase(t *testing.T) {
	compiler := NewDefaultCompiler()
	n, err := compiler.Compile("(?i)abc")
	if err != nil {
		t.Fatal(err)
	}
	vm := NewPikeVM(n)

	if !vm.IsMatch([]byte("ABC")) {
		t.Error("expected case-insensitive match")
	}
	if !vm.IsMatch([]byte("AbC")) {
		t.Error("expected case-insensitive match for mixed case")
	}
}

// --- pikevm.go: look assertions ---

func TestPikeVMLookAssertions(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    bool
	}{
		{`\bword\b`, "a word here", true},
		{`\bword\b`, "awordhere", false},
		{`\Bord\B`, "sword", false},
		{`^start`, "start of line", true},
		{`^start`, "not start", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			n, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}
			vm := NewPikeVM(n)
			got := vm.IsMatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatch(%q, %q) = %v, want %v", tt.pattern, tt.input, got, tt.want)
			}
		})
	}
}
