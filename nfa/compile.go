package nfa

import (
	"fmt"
	"regexp/syntax"
)

// CompilerConfig configures NFA compilation behavior
type CompilerConfig struct {
	// UTF8 determines whether the NFA respects UTF-8 boundaries.
	// When true, empty matches that split UTF-8 sequences are avoided.
	UTF8 bool

	// Anchored forces the pattern to match only at the start of input
	Anchored bool

	// DotNewline determines whether '.' matches '\n'
	DotNewline bool

	// MaxRecursionDepth limits recursion during compilation to prevent stack overflow
	// Default: 100
	MaxRecursionDepth int
}

// DefaultCompilerConfig returns a compiler configuration with sensible defaults
func DefaultCompilerConfig() CompilerConfig {
	return CompilerConfig{
		UTF8:              true,
		Anchored:          false,
		DotNewline:        false,
		MaxRecursionDepth: 100,
	}
}

// Compiler compiles regexp/syntax.Regexp patterns into Thompson NFAs
type Compiler struct {
	config       CompilerConfig
	builder      *Builder
	depth        int      // current recursion depth
	captureCount int      // number of capture groups (1-based, group 0 is entire match)
	captureNames []string // names of capture groups (index 0 = "", rest from pattern)
}

// NewCompiler creates a new NFA compiler with the given configuration
func NewCompiler(config CompilerConfig) *Compiler {
	if config.MaxRecursionDepth == 0 {
		config.MaxRecursionDepth = 100
	}
	return &Compiler{
		config:  config,
		builder: NewBuilder(),
		depth:   0,
	}
}

// NewDefaultCompiler creates a new NFA compiler with default configuration
func NewDefaultCompiler() *Compiler {
	return NewCompiler(DefaultCompilerConfig())
}

// Compile compiles a regex pattern string into an NFA
func (c *Compiler) Compile(pattern string) (*NFA, error) {
	// Parse the pattern using regexp/syntax
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return nil, &CompileError{
			Pattern: pattern,
			Err:     err,
		}
	}

	return c.CompileRegexp(re)
}

// CompileRegexp compiles a parsed syntax.Regexp into an NFA
func (c *Compiler) CompileRegexp(re *syntax.Regexp) (*NFA, error) {
	c.builder = NewBuilder()
	c.depth = 0
	c.captureCount = 0
	c.captureNames = nil

	// Count capture groups and collect their names
	c.collectCaptureInfo(re)

	// Determine if pattern is inherently anchored (has ^ or \A prefix)
	allAnchored := c.isPatternAnchored(re)

	// Compile the actual pattern
	patternStart, patternEnd, err := c.compileRegexp(re)
	if err != nil {
		return nil, err
	}

	// Add final match state
	matchID := c.builder.AddMatch()

	// Connect pattern end to match state
	if err := c.builder.Patch(patternEnd, matchID); err != nil {
		// If patching fails, end might be a Split state - add epsilon
		epsilonID := c.builder.AddEpsilon(matchID)
		if patchErr := c.builder.Patch(patternEnd, epsilonID); patchErr != nil {
			return nil, &CompileError{
				Err: fmt.Errorf("failed to connect to match state: %w", patchErr),
			}
		}
	}

	// Anchored start always points to pattern
	anchoredStart := patternStart

	// Unanchored start: compile the (?s:.)*? prefix for DFA and other engines
	// that need it. PikeVM simulates this prefix in its search loop instead
	// (like Rust regex-automata) for correct startPos tracking.
	// If pattern is anchored, unanchored start equals anchored start.
	var unanchoredStart StateID
	if c.config.Anchored || allAnchored {
		unanchoredStart = anchoredStart
	} else {
		unanchoredStart = c.compileUnanchoredPrefix(patternStart)
	}

	// Set dual start states
	c.builder.SetStarts(anchoredStart, unanchoredStart)

	// Build the final NFA
	// captureCount + 1 because group 0 is the entire match
	nfa, err := c.builder.Build(
		WithUTF8(c.config.UTF8),
		WithAnchored(c.config.Anchored || allAnchored),
		WithCaptureCount(c.captureCount+1),
		WithCaptureNames(c.captureNames),
	)
	if err != nil {
		return nil, &CompileError{
			Err: err,
		}
	}

	return nfa, nil
}

// compileRegexp recursively compiles a syntax.Regexp node.
// Returns (start, end) state IDs for the compiled fragment.
// The 'end' state is a state that needs to be patched to continue the automaton.
func (c *Compiler) compileRegexp(re *syntax.Regexp) (start, end StateID, err error) {
	// Check recursion depth
	c.depth++
	if c.depth > c.config.MaxRecursionDepth {
		return InvalidState, InvalidState, &CompileError{
			Err: ErrTooComplex,
		}
	}
	defer func() { c.depth-- }()

	switch re.Op {
	case syntax.OpLiteral:
		return c.compileLiteral(re)
	case syntax.OpCharClass:
		return c.compileCharClass(re.Rune)
	case syntax.OpAnyChar:
		return c.compileAnyChar()
	case syntax.OpAnyCharNotNL:
		return c.compileAnyCharNotNL()
	case syntax.OpConcat:
		return c.compileConcat(re.Sub)
	case syntax.OpAlternate:
		return c.compileAlternate(re.Sub)
	case syntax.OpStar:
		return c.compileStar(re.Sub[0])
	case syntax.OpPlus:
		return c.compilePlus(re.Sub[0])
	case syntax.OpQuest:
		return c.compileQuest(re.Sub[0])
	case syntax.OpRepeat:
		return c.compileRepeat(re.Sub[0], re.Min, re.Max)
	case syntax.OpCapture:
		return c.compileCapture(re)
	case syntax.OpBeginText:
		// \A - only matches at start of input (not after newlines)
		// Used by ^ in non-multiline mode
		id := c.builder.AddLook(LookStartText, InvalidState)
		return id, id, nil
	case syntax.OpEndText:
		// \z - only matches at end of input (not before newlines)
		// Used by $ in non-multiline mode
		id := c.builder.AddLook(LookEndText, InvalidState)
		return id, id, nil
	case syntax.OpBeginLine:
		// ^ in multiline mode (?m) - matches at start of input OR after \n
		id := c.builder.AddLook(LookStartLine, InvalidState)
		return id, id, nil
	case syntax.OpEndLine:
		// $ in multiline mode (?m) - matches at end of input OR before \n
		id := c.builder.AddLook(LookEndLine, InvalidState)
		return id, id, nil
	case syntax.OpWordBoundary:
		// \b - word boundary (transition between word and non-word chars)
		id := c.builder.AddLook(LookWordBoundary, InvalidState)
		return id, id, nil
	case syntax.OpNoWordBoundary:
		// \B - non-word boundary (no transition between word and non-word chars)
		id := c.builder.AddLook(LookNoWordBoundary, InvalidState)
		return id, id, nil
	case syntax.OpEmptyMatch:
		return c.compileEmptyMatch()
	default:
		return InvalidState, InvalidState, &CompileError{
			Err: fmt.Errorf("unsupported regex operation: %v", re.Op),
		}
	}
}

// compileLiteral compiles a literal string (sequence of runes)
// Handles case-insensitive matching when FoldCase flag is set
func (c *Compiler) compileLiteral(re *syntax.Regexp) (start, end StateID, err error) {
	runes := re.Rune
	if len(runes) == 0 {
		return c.compileEmptyMatch()
	}

	// Check if case-insensitive matching is enabled
	foldCase := re.Flags&syntax.FoldCase != 0

	// Convert runes to UTF-8 bytes
	var prev = InvalidState
	var first = InvalidState

	for _, r := range runes {
		// For case-insensitive matching of ASCII letters, create alternation
		if foldCase && isASCIILetter(r) {
			nextState, err := c.compileFoldCaseRune(r, prev, &first)
			if err != nil {
				return InvalidState, InvalidState, err
			}
			prev = nextState
		} else {
			// Normal case-sensitive matching
			prev, err = c.compileCaseSensitiveRune(r, prev, &first)
			if err != nil {
				return InvalidState, InvalidState, err
			}
		}
	}

	return first, prev, nil
}

// compileFoldCaseRune compiles a case-insensitive ASCII letter
// by creating alternation between upper and lower case versions
func (c *Compiler) compileFoldCaseRune(r rune, prev StateID, first *StateID) (StateID, error) {
	upper := toUpperASCII(r)
	lower := toLowerASCII(r)

	// Build UTF-8 sequences for both cases
	upperStart, upperEnd, err := c.compileSingleRune(upper)
	if err != nil {
		return InvalidState, err
	}
	lowerStart, lowerEnd, err := c.compileSingleRune(lower)
	if err != nil {
		return InvalidState, err
	}

	// Create join state
	nextState := c.builder.AddEpsilon(InvalidState)

	// Connect both paths to join
	if err := c.builder.Patch(upperEnd, nextState); err != nil {
		return InvalidState, err
	}
	if err := c.builder.Patch(lowerEnd, nextState); err != nil {
		return InvalidState, err
	}

	// Create split state
	split := c.builder.AddSplit(upperStart, lowerStart)

	if prev == InvalidState {
		// First character - split becomes the start
		*first = split
	} else {
		// Subsequent character - connect from previous
		if err := c.builder.Patch(prev, split); err != nil {
			return InvalidState, err
		}
	}

	return nextState, nil
}

// compileCaseSensitiveRune compiles a single rune in case-sensitive mode
// by converting it to UTF-8 bytes and chaining ByteRange states
func (c *Compiler) compileCaseSensitiveRune(r rune, prev StateID, first *StateID) (StateID, error) {
	// Convert rune to UTF-8 bytes
	buf := make([]byte, 4)
	n := encodeRune(buf, r)

	for i := 0; i < n; i++ {
		b := buf[i]
		id := c.builder.AddByteRange(b, b, InvalidState)
		if *first == InvalidState {
			*first = id
		}
		if prev != InvalidState {
			if err := c.builder.Patch(prev, id); err != nil {
				return InvalidState, err
			}
		}
		prev = id
	}

	return prev, nil
}

// compileSingleRune compiles a single rune to UTF-8 byte sequence
func (c *Compiler) compileSingleRune(r rune) (start, end StateID, err error) {
	buf := make([]byte, 4)
	n := encodeRune(buf, r)

	var prev = InvalidState
	var first = InvalidState

	for i := 0; i < n; i++ {
		b := buf[i]
		id := c.builder.AddByteRange(b, b, InvalidState)
		if first == InvalidState {
			first = id
		}
		if prev != InvalidState {
			if err := c.builder.Patch(prev, id); err != nil {
				return InvalidState, InvalidState, err
			}
		}
		prev = id
	}

	return first, prev, nil
}

// isASCIILetter checks if a rune is an ASCII letter (a-z, A-Z)
func isASCIILetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// toUpperASCII converts an ASCII letter to uppercase
func toUpperASCII(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - 'a' + 'A'
	}
	return r
}

// toLowerASCII converts an ASCII letter to lowercase
func toLowerASCII(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r - 'A' + 'a'
	}
	return r
}

// compileCharClass compiles a character class like [a-zA-Z0-9]
func (c *Compiler) compileCharClass(ranges []rune) (start, end StateID, err error) {
	if len(ranges) == 0 {
		return c.compileEmptyMatch()
	}

	// Character class ranges are pairs: [lo1, hi1, lo2, hi2, ...]
	// For UTF-8, we need to handle multi-byte sequences

	// Simple case: ASCII character class
	// Check if all ranges are ASCII
	allASCII := true
	for _, r := range ranges {
		if r > 127 {
			allASCII = false
			break
		}
	}

	if allASCII && len(ranges) >= 2 {
		// Build byte-level transitions
		var transitions []Transition
		for i := 0; i < len(ranges); i += 2 {
			lo := byte(ranges[i])
			hi := byte(ranges[i+1])
			transitions = append(transitions, Transition{
				Lo:   lo,
				Hi:   hi,
				Next: InvalidState, // Will be patched later
			})
		}

		if len(transitions) == 1 {
			// Single range - use ByteRange
			t := transitions[0]
			id := c.builder.AddByteRange(t.Lo, t.Hi, InvalidState)
			return id, id, nil
		}

		// Multiple ranges - use Sparse
		// For sparse, we need a target state
		// Create an epsilon state as the target
		target := c.builder.AddEpsilon(InvalidState)
		for i := range transitions {
			transitions[i].Next = target
		}
		id := c.builder.AddSparse(transitions)
		return id, target, nil
	}

	// For Unicode, we need to build a UTF-8 automaton
	// This is complex - for MVP, fall back to alternation
	return c.compileUnicodeClass(ranges)
}

// compileUnicodeClass handles Unicode character classes by building UTF-8 automata
func (c *Compiler) compileUnicodeClass(ranges []rune) (start, end StateID, err error) {
	// For MVP: convert to alternation of individual characters
	// This is inefficient but correct
	// Full implementation would use UTF-8 range compilation

	if len(ranges) == 0 {
		return c.compileEmptyMatch()
	}

	// Count total characters first to avoid explosion
	totalChars := int64(0)
	for i := 0; i < len(ranges); i += 2 {
		lo := ranges[i]
		hi := ranges[i+1]
		totalChars += int64(hi - lo + 1)
		if totalChars > 256 {
			// For large character classes (like negated [^,] with 1.1M chars),
			// we need a different approach - use UTF-8 byte ranges directly
			return c.compileUnicodeClassLarge(ranges)
		}
	}

	// Build alternation of all characters in ranges (small classes only)
	var alts []*syntax.Regexp
	for i := 0; i < len(ranges); i += 2 {
		lo := ranges[i]
		hi := ranges[i+1]
		for r := lo; r <= hi; r++ {
			alts = append(alts, &syntax.Regexp{
				Op:   syntax.OpLiteral,
				Rune: []rune{r},
			})
		}
	}

	if len(alts) == 1 {
		return c.compileRegexp(alts[0])
	}

	return c.compileAlternate(alts)
}

// compileUnicodeClassLarge handles large Unicode character classes (e.g., negated classes)
// by building transitions for UTF-8 byte ranges instead of expanding all codepoints.
// For example, [^,] expands to 1.1M codepoints but can be represented as byte ranges.
//
// This is a simplified MVP implementation that handles common negated ASCII cases.
// A full implementation would use proper UTF-8 range compilation algorithms.
func (c *Compiler) compileUnicodeClassLarge(ranges []rune) (start, end StateID, err error) {
	// For large character classes, especially negated ones, we use a different approach:
	// Instead of expanding all codepoints, we build a Sparse state with byte ranges.
	//
	// Strategy for negated ASCII classes (like [^,], [^\n], [^0-9]):
	// 1. Collect all ASCII ranges
	// 2. Build Sparse state with these ranges
	// 3. For non-ASCII part (0x80-0x10FFFF), accept any valid UTF-8 multi-byte sequence
	//
	// This handles the common case of negated single ASCII characters efficiently.

	// Separate ASCII and non-ASCII ranges
	var asciiRanges []Transition
	var hasNonASCII bool

	for i := 0; i < len(ranges); i += 2 {
		lo := ranges[i]
		hi := ranges[i+1]

		switch {
		case hi < 0x80:
			// Pure ASCII range
			asciiRanges = append(asciiRanges, Transition{
				Lo:   byte(lo),
				Hi:   byte(hi),
				Next: InvalidState,
			})
		case lo >= 0x80:
			// Pure non-ASCII range
			hasNonASCII = true
		default:
			// Mixed: split into ASCII and non-ASCII parts
			// ASCII part: [lo, 0x7F]
			asciiRanges = append(asciiRanges, Transition{
				Lo:   byte(lo),
				Hi:   0x7F,
				Next: InvalidState,
			})
			hasNonASCII = true
		}
	}

	// Build the automaton
	if !hasNonASCII {
		// Pure ASCII character class - use Sparse state
		if len(asciiRanges) == 0 {
			return c.compileEmptyMatch()
		}

		target := c.builder.AddEpsilon(InvalidState)
		for i := range asciiRanges {
			asciiRanges[i].Next = target
		}

		if len(asciiRanges) == 1 && asciiRanges[0].Lo == asciiRanges[0].Hi {
			// Single byte
			id := c.builder.AddByteRange(asciiRanges[0].Lo, asciiRanges[0].Hi, target)
			return id, target, nil
		}

		id := c.builder.AddSparse(asciiRanges)
		return id, target, nil
	}

	// Has non-ASCII ranges: build alternation of ASCII and UTF-8 multi-byte sequences
	// For MVP, we'll accept any valid UTF-8 multi-byte sequence (simplified approach)
	//
	// ASCII part: handled by Sparse state
	// Non-ASCII part: match any valid UTF-8 sequence starting with 0xC0-0xFF
	//
	// This is an approximation but handles common negated classes efficiently.

	// Create target state
	target := c.builder.AddEpsilon(InvalidState)

	// Build alternation between ASCII and multi-byte UTF-8
	var altStarts []StateID

	// ASCII alternatives (if any)
	if len(asciiRanges) > 0 {
		for i := range asciiRanges {
			asciiRanges[i].Next = target
		}
		if len(asciiRanges) == 1 && asciiRanges[0].Lo == asciiRanges[0].Hi {
			id := c.builder.AddByteRange(asciiRanges[0].Lo, asciiRanges[0].Hi, target)
			altStarts = append(altStarts, id)
		} else {
			id := c.builder.AddSparse(asciiRanges)
			altStarts = append(altStarts, id)
		}
	}

	// Multi-byte UTF-8 alternative
	// For simplicity, accept any sequence starting with 0xC0-0xFF followed by continuation bytes
	// This is a simplified approach that accepts any valid UTF-8 multi-byte character
	//
	// UTF-8 encoding:
	// 2-byte: 0xC0-0xDF, 0x80-0xBF
	// 3-byte: 0xE0-0xEF, 0x80-0xBF, 0x80-0xBF
	// 4-byte: 0xF0-0xF7, 0x80-0xBF, 0x80-0xBF, 0x80-0xBF

	// For MVP: accept any byte sequence starting with 0x80-0xFF
	// This is overly permissive but safe for negated classes
	multiByteStart := c.builder.AddByteRange(0x80, 0xFF, target)
	altStarts = append(altStarts, multiByteStart)

	// Build split chain for alternatives
	if len(altStarts) == 1 {
		return altStarts[0], target, nil
	}

	split := c.buildSplitChain(altStarts)
	return split, target, nil
}

// compileAnyChar compiles '.' matching any character (including \n if DotNewline is true)
func (c *Compiler) compileAnyChar() (start, end StateID, err error) {
	if c.config.DotNewline {
		// Match any byte
		id := c.builder.AddByteRange(0, 255, InvalidState)
		return id, id, nil
	}
	return c.compileAnyCharNotNL()
}

// compileAnyCharNotNL compiles '.' matching any character except \n
func (c *Compiler) compileAnyCharNotNL() (start, end StateID, err error) {
	// Match any byte except '\n' (0x0A)
	// Use sparse transitions: [0x00-0x09], [0x0B-0xFF]
	target := c.builder.AddEpsilon(InvalidState)
	transitions := []Transition{
		{Lo: 0x00, Hi: 0x09, Next: target},
		{Lo: 0x0B, Hi: 0xFF, Next: target},
	}
	id := c.builder.AddSparse(transitions)
	return id, target, nil
}

// compileConcat compiles concatenation (e.g., "abc")
func (c *Compiler) compileConcat(subs []*syntax.Regexp) (start, end StateID, err error) {
	if len(subs) == 0 {
		return c.compileEmptyMatch()
	}
	if len(subs) == 1 {
		return c.compileRegexp(subs[0])
	}

	// Compile first sub-expression
	start, end, err = c.compileRegexp(subs[0])
	if err != nil {
		return InvalidState, InvalidState, err
	}

	// Chain the rest
	for i := 1; i < len(subs); i++ {
		nextStart, nextEnd, err := c.compileRegexp(subs[i])
		if err != nil {
			return InvalidState, InvalidState, err
		}
		// Connect current end to next start
		if err := c.builder.Patch(end, nextStart); err != nil {
			// If patch fails, insert epsilon
			epsilon := c.builder.AddEpsilon(nextStart)
			if err := c.builder.Patch(end, epsilon); err != nil {
				return InvalidState, InvalidState, err
			}
		}
		end = nextEnd
	}

	return start, end, nil
}

// compileAlternate compiles alternation (e.g., "a|b|c")
func (c *Compiler) compileAlternate(subs []*syntax.Regexp) (start, end StateID, err error) {
	if len(subs) == 0 {
		return c.compileEmptyMatch()
	}
	if len(subs) == 1 {
		return c.compileRegexp(subs[0])
	}

	// Compile all alternatives
	starts := make([]StateID, 0, len(subs))
	ends := make([]StateID, 0, len(subs))
	for _, sub := range subs {
		s, e, err := c.compileRegexp(sub)
		if err != nil {
			return InvalidState, InvalidState, err
		}
		starts = append(starts, s)
		ends = append(ends, e)
	}

	// Create split states to distribute to all alternatives
	// For n alternatives, we need n-1 split states
	split := c.buildSplitChain(starts)

	// Create a join epsilon state where all alternatives converge
	join := c.builder.AddEpsilon(InvalidState)
	for _, e := range ends {
		if err := c.builder.Patch(e, join); err != nil {
			// If patching fails, end might already be connected
			// This can happen with nested alternations
			continue
		}
	}

	return split, join, nil
}

// buildSplitChain builds a chain of split states for alternation
func (c *Compiler) buildSplitChain(targets []StateID) StateID {
	if len(targets) == 1 {
		return targets[0]
	}
	if len(targets) == 2 {
		return c.builder.AddSplit(targets[0], targets[1])
	}

	// For >2 alternatives, build a binary tree of splits
	// Split(alt1, Split(alt2, Split(alt3, ...)))
	right := c.buildSplitChain(targets[1:])
	return c.builder.AddSplit(targets[0], right)
}

// compileStar compiles a* (zero or more)
func (c *Compiler) compileStar(sub *syntax.Regexp) (start, end StateID, err error) {
	subStart, subEnd, err := c.compileRegexp(sub)
	if err != nil {
		return InvalidState, InvalidState, err
	}

	// Create split: either enter sub or skip
	// split -> [sub, end]
	// sub -> split (loop back)
	// Use AddQuantifierSplit - quantifier splits don't affect priority
	end = c.builder.AddEpsilon(InvalidState)
	split := c.builder.AddQuantifierSplit(subStart, end)

	// Connect sub end back to split (loop)
	if err := c.builder.Patch(subEnd, split); err != nil {
		epsilon := c.builder.AddEpsilon(split)
		if err := c.builder.Patch(subEnd, epsilon); err != nil {
			return InvalidState, InvalidState, err
		}
	}

	return split, end, nil
}

// compilePlus compiles a+ (one or more)
func (c *Compiler) compilePlus(sub *syntax.Regexp) (start, end StateID, err error) {
	subStart, subEnd, err := c.compileRegexp(sub)
	if err != nil {
		return InvalidState, InvalidState, err
	}

	// Must match at least once
	// sub -> split -> [sub, end]
	// Use AddQuantifierSplit - quantifier splits don't affect priority
	end = c.builder.AddEpsilon(InvalidState)
	split := c.builder.AddQuantifierSplit(subStart, end)

	// Connect sub end to split (loop)
	if err := c.builder.Patch(subEnd, split); err != nil {
		epsilon := c.builder.AddEpsilon(split)
		if err := c.builder.Patch(subEnd, epsilon); err != nil {
			return InvalidState, InvalidState, err
		}
	}

	return subStart, end, nil
}

// compileQuest compiles a? (zero or one)
func (c *Compiler) compileQuest(sub *syntax.Regexp) (start, end StateID, err error) {
	subStart, subEnd, err := c.compileRegexp(sub)
	if err != nil {
		return InvalidState, InvalidState, err
	}

	// Either match sub or skip
	// Use AddQuantifierSplit - quantifier splits don't affect priority
	end = c.builder.AddEpsilon(InvalidState)
	split := c.builder.AddQuantifierSplit(subStart, end)

	// Connect sub end to end
	if err := c.builder.Patch(subEnd, end); err != nil {
		epsilon := c.builder.AddEpsilon(end)
		if err := c.builder.Patch(subEnd, epsilon); err != nil {
			return InvalidState, InvalidState, err
		}
	}

	return split, end, nil
}

// compileRepeat compiles a{m,n} (min to max repetitions)
func (c *Compiler) compileRepeat(sub *syntax.Regexp, minCount, maxCount int) (start, end StateID, err error) {
	if maxCount == -1 {
		// a{m,} = aaa...a* (minCount copies + star)
		return c.compileRepeatMin(sub, minCount)
	}
	if minCount == maxCount {
		// a{n} = aaa...a (exactly n copies)
		return c.compileRepeatExact(sub, minCount)
	}
	// a{m,n} = aaa...a(a?a?a?...) (minCount copies + (maxCount-minCount) optional copies)
	return c.compileRepeatRange(sub, minCount, maxCount)
}

// compileRepeatExact compiles a{n}
func (c *Compiler) compileRepeatExact(sub *syntax.Regexp, n int) (start, end StateID, err error) {
	if n == 0 {
		return c.compileEmptyMatch()
	}
	if n == 1 {
		return c.compileRegexp(sub)
	}

	// Concatenate n copies
	var subs []*syntax.Regexp
	for i := 0; i < n; i++ {
		subs = append(subs, sub)
	}
	return c.compileConcat(subs)
}

// compileRepeatMin compiles a{m,}
func (c *Compiler) compileRepeatMin(sub *syntax.Regexp, minCount int) (start, end StateID, err error) {
	if minCount == 0 {
		return c.compileStar(sub)
	}

	// Concatenate minCount copies + star
	var subs []*syntax.Regexp
	for i := 0; i < minCount; i++ {
		subs = append(subs, sub)
	}
	subs = append(subs, &syntax.Regexp{
		Op:  syntax.OpStar,
		Sub: []*syntax.Regexp{sub},
	})
	return c.compileConcat(subs)
}

// compileRepeatRange compiles a{m,n}
func (c *Compiler) compileRepeatRange(sub *syntax.Regexp, minCount, maxCount int) (start, end StateID, err error) {
	if minCount > maxCount {
		return InvalidState, InvalidState, &CompileError{
			Err: fmt.Errorf("invalid repeat range {%d,%d}", minCount, maxCount),
		}
	}

	// Concatenate minCount copies + (maxCount-minCount) optional copies
	var subs []*syntax.Regexp
	for i := 0; i < minCount; i++ {
		subs = append(subs, sub)
	}
	for i := 0; i < maxCount-minCount; i++ {
		subs = append(subs, &syntax.Regexp{
			Op:  syntax.OpQuest,
			Sub: []*syntax.Regexp{sub},
		})
	}
	return c.compileConcat(subs)
}

// compileEmptyMatch compiles an epsilon transition (matches without consuming input)
func (c *Compiler) compileEmptyMatch() (start, end StateID, err error) {
	id := c.builder.AddEpsilon(InvalidState)
	return id, id, nil
}

// encodeRune encodes a rune as UTF-8 into buf and returns the number of bytes written
// buf must have capacity >= 4
func encodeRune(buf []byte, r rune) int {
	if r < 0x80 {
		buf[0] = byte(r)
		return 1
	}
	if r < 0x800 {
		buf[0] = byte(0xC0 | (r >> 6))
		buf[1] = byte(0x80 | (r & 0x3F))
		return 2
	}
	if r < 0x10000 {
		buf[0] = byte(0xE0 | (r >> 12))
		buf[1] = byte(0x80 | ((r >> 6) & 0x3F))
		buf[2] = byte(0x80 | (r & 0x3F))
		return 3
	}
	buf[0] = byte(0xF0 | (r >> 18))
	buf[1] = byte(0x80 | ((r >> 12) & 0x3F))
	buf[2] = byte(0x80 | ((r >> 6) & 0x3F))
	buf[3] = byte(0x80 | (r & 0x3F))
	return 4
}

// compileUnanchoredPrefix creates the unanchored prefix (?s:.)*? for O(n) unanchored search.
//
// Deprecated: This function is no longer used by PikeVM. Instead, unanchored search
// simulates the prefix explicitly in the search loop (matching Rust regex-automata
// and Go stdlib approach) to ensure correct startPos tracking.
//
// The prefix is a non-greedy loop that matches any byte zero or more times:
//
//	     +---(any byte [0x00-0xFF])---+
//	     |                             |
//	     v                             |
//	[SPLIT] --------------------------(loop back)
//	   |
//	   +---(epsilon)---> [patternStart]
//
// The Split state has two epsilon transitions:
//  1. Left (preferred): epsilon to patternStart (try to match pattern)
//  2. Right: any byte transition that loops back (consume input and retry)
//
// This is non-greedy (.*?) because we prefer the pattern match over consuming more input.
//
// Returns the StateID of the Split state (the unanchored start).
func (c *Compiler) compileUnanchoredPrefix(patternStart StateID) StateID {
	// Create any-byte transition [0x00-0xFF]
	// This will loop back to the split state
	anyByte := c.builder.AddByteRange(0x00, 0xFF, InvalidState)

	// Create split state: prefer pattern (left) over consuming byte (right)
	// For non-greedy .*?, we want to try the pattern first
	split := c.builder.AddSplit(patternStart, anyByte)

	// Make the any-byte transition loop back to split
	if err := c.builder.Patch(anyByte, split); err != nil {
		// This should never fail for a ByteRange state, but handle gracefully
		// Fall back to pattern start without prefix
		return patternStart
	}

	return split
}

// compileCapture compiles a capture group (re.Op == OpCapture)
// Creates opening capture state -> sub-expression -> closing capture state
func (c *Compiler) compileCapture(re *syntax.Regexp) (start, end StateID, err error) {
	if len(re.Sub) == 0 {
		return c.compileEmptyMatch()
	}

	// Compile the sub-expression first
	subStart, subEnd, err := c.compileRegexp(re.Sub[0])
	if err != nil {
		return InvalidState, InvalidState, err
	}

	// Create closing capture state (records end position)
	// Note: we create closing first to get the ID, then opening points to subStart
	closeCapture := c.builder.AddCapture(uint32(re.Cap), false, InvalidState)

	// Connect sub-expression end to closing capture
	if err := c.builder.Patch(subEnd, closeCapture); err != nil {
		// If patching fails, insert epsilon
		epsilon := c.builder.AddEpsilon(closeCapture)
		if err := c.builder.Patch(subEnd, epsilon); err != nil {
			return InvalidState, InvalidState, err
		}
	}

	// Create opening capture state (records start position)
	openCapture := c.builder.AddCapture(uint32(re.Cap), true, subStart)

	return openCapture, closeCapture, nil
}

// collectCaptureInfo counts the number of capture groups and collects their names.
// This must be called before compilation to know the total count and names.
// After calling this:
//   - c.captureCount contains the highest capture group number
//   - c.captureNames is initialized with length captureCount+1
//   - c.captureNames[0] = "" (entire match)
//   - c.captureNames[i] = name or "" for group i
func (c *Compiler) collectCaptureInfo(re *syntax.Regexp) {
	// First pass: count captures
	c.countCapturesRecursive(re)

	// Initialize captureNames slice (index 0 = entire match "")
	c.captureNames = make([]string, c.captureCount+1)

	// Second pass: collect names
	c.collectNamesRecursive(re)
}

// countCapturesRecursive counts capture groups recursively
func (c *Compiler) countCapturesRecursive(re *syntax.Regexp) {
	switch re.Op {
	case syntax.OpCapture:
		if re.Cap > c.captureCount {
			c.captureCount = re.Cap
		}
		for _, sub := range re.Sub {
			c.countCapturesRecursive(sub)
		}
	case syntax.OpConcat, syntax.OpAlternate:
		for _, sub := range re.Sub {
			c.countCapturesRecursive(sub)
		}
	case syntax.OpStar, syntax.OpPlus, syntax.OpQuest, syntax.OpRepeat:
		if len(re.Sub) > 0 {
			c.countCapturesRecursive(re.Sub[0])
		}
	}
}

// collectNamesRecursive collects capture group names recursively
func (c *Compiler) collectNamesRecursive(re *syntax.Regexp) {
	switch re.Op {
	case syntax.OpCapture:
		// Store the name (may be empty string for unnamed captures)
		if re.Cap >= 0 && re.Cap < len(c.captureNames) {
			c.captureNames[re.Cap] = re.Name
		}
		for _, sub := range re.Sub {
			c.collectNamesRecursive(sub)
		}
	case syntax.OpConcat, syntax.OpAlternate:
		for _, sub := range re.Sub {
			c.collectNamesRecursive(sub)
		}
	case syntax.OpStar, syntax.OpPlus, syntax.OpQuest, syntax.OpRepeat:
		if len(re.Sub) > 0 {
			c.collectNamesRecursive(re.Sub[0])
		}
	}
}

// isPatternAnchored checks if a pattern is inherently anchored (starts with ^ or \A).
//
// A pattern is anchored if it begins with:
//   - OpBeginLine (^)
//   - OpBeginText (\A)
//   - A Concat that starts with an anchor
//
// For anchored patterns, the unanchored start state equals the anchored start state.
// Note: OpBeginLine (^) is NOT truly anchored because in multiline mode it matches
// after each newline. Only OpBeginText (\A) is truly anchored to input start.
func (c *Compiler) isPatternAnchored(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpBeginText: // Only \A is truly anchored, not ^ (OpBeginLine)
		return true
	case syntax.OpConcat:
		if len(re.Sub) > 0 {
			return c.isPatternAnchored(re.Sub[0])
		}
	case syntax.OpCapture:
		if len(re.Sub) > 0 {
			return c.isPatternAnchored(re.Sub[0])
		}
	}
	return false
}

// IsPatternEndAnchored checks if a pattern is inherently anchored at end (ends with \z).
//
// A pattern is end-anchored if it ends with:
//   - OpEndText (\z or non-multiline $) - only matches at EOF
//
// Note: OpEndLine (multiline $ with (?m)) is NOT considered end-anchored because
// it can match at multiple positions (before each \n and at EOF). Using reverse
// search for multiline $ would miss matches before \n characters.
//
// This is used to select the ReverseAnchored strategy which searches backward
// from the end of haystack for O(m) instead of O(n*m) performance.
func IsPatternEndAnchored(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpEndText: // Only OpEndText is truly end-anchored, not OpEndLine (multiline $)
		return true
	case syntax.OpConcat:
		if len(re.Sub) > 0 {
			// Check the last sub-expression
			return IsPatternEndAnchored(re.Sub[len(re.Sub)-1])
		}
	case syntax.OpCapture:
		if len(re.Sub) > 0 {
			return IsPatternEndAnchored(re.Sub[0])
		}
	case syntax.OpAlternate:
		// All alternatives must be end-anchored
		if len(re.Sub) == 0 {
			return false
		}
		for _, sub := range re.Sub {
			if !IsPatternEndAnchored(sub) {
				return false
			}
		}
		return true
	}
	return false
}
