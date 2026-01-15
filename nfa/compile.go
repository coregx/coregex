package nfa

import (
	"fmt"
	"regexp/syntax"

	"github.com/coregx/coregex/internal/conv"
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

	// ASCIIOnly when true, compiles '.' to match only ASCII bytes (0x00-0x7F).
	// This dramatically reduces NFA state count (1 state vs ~28 states) and
	// improves performance for patterns with '.' when input is known to be ASCII.
	//
	// When false (default), '.' compiles to match any valid UTF-8 codepoint,
	// requiring ~28 NFA states to handle all valid UTF-8 byte sequences.
	//
	// This is used for ASCII runtime detection optimization (V11-002):
	// compile both ASCII and UTF-8 NFAs, select at runtime based on input.
	ASCIIOnly bool

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
		return c.compileStar(re.Sub[0], re.Flags&syntax.NonGreedy != 0)
	case syntax.OpPlus:
		return c.compilePlus(re.Sub[0], re.Flags&syntax.NonGreedy != 0)
	case syntax.OpQuest:
		return c.compileQuest(re.Sub[0], re.Flags&syntax.NonGreedy != 0)
	case syntax.OpRepeat:
		return c.compileRepeat(re.Sub[0], re.Min, re.Max, re.Flags&syntax.NonGreedy != 0)
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
		// Empty character class (e.g., [^\S\s]) should never match
		return c.compileNoMatch()
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
		// Empty character class (e.g., [^\S\s]) should never match
		return c.compileNoMatch()
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
// by building UTF-8 automata for each Unicode range.
//
// Issue #91 fix: properly compile UTF-8 byte sequences for each Unicode range,
// instead of accepting any UTF-8 sequence.
//
// Optimization: if the non-ASCII part covers ALL of non-ASCII Unicode (like [^,]),
// use the efficient "any valid UTF-8" approach. Otherwise, build precise UTF-8 ranges.
func (c *Compiler) compileUnicodeClassLarge(ranges []rune) (start, end StateID, err error) {
	// Separate ASCII and non-ASCII ranges
	var asciiRanges []Transition
	var nonASCIIRanges [][2]rune

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
			nonASCIIRanges = append(nonASCIIRanges, [2]rune{lo, hi})
		default:
			// Mixed: split into ASCII and non-ASCII parts
			asciiRanges = append(asciiRanges, Transition{
				Lo:   byte(lo),
				Hi:   0x7F,
				Next: InvalidState,
			})
			nonASCIIRanges = append(nonASCIIRanges, [2]rune{0x80, hi})
		}
	}

	// Check if non-ASCII part covers ALL of non-ASCII Unicode
	// This is true for patterns like [^,], [^a], [^\n] where the excluded char is ASCII
	coversAllNonASCII := len(nonASCIIRanges) == 1 &&
		nonASCIIRanges[0][0] <= 0x80 &&
		nonASCIIRanges[0][1] >= 0x10FFFF

	// Create shared end state
	target := c.builder.AddEpsilon(InvalidState)
	var altStarts []StateID

	// Build ASCII part
	if len(asciiRanges) > 0 {
		for i := range asciiRanges {
			asciiRanges[i].Next = target
		}
		switch {
		case len(asciiRanges) == 1:
			// Single range - use ByteRange
			id := c.builder.AddByteRange(asciiRanges[0].Lo, asciiRanges[0].Hi, target)
			altStarts = append(altStarts, id)
		default:
			// Multiple ranges - use Sparse
			id := c.builder.AddSparse(asciiRanges)
			altStarts = append(altStarts, id)
		}
	}

	// Build non-ASCII part
	if len(nonASCIIRanges) > 0 {
		if coversAllNonASCII {
			// Optimization: use efficient "any valid UTF-8 multi-byte" approach
			// This is correct because we're matching ALL non-ASCII codepoints
			multiByteStarts := c.buildUTF8NonASCIIBranches(target)
			altStarts = append(altStarts, multiByteStarts...)

			// Also match invalid UTF-8 bytes for stdlib compatibility.
			// Go regexp treats invalid UTF-8 bytes as single characters that
			// match negated char classes like \D, \S, \W, [^x].
			// NOTE: We only add this for coversAllNonASCII case because:
			// 1. For classes like \D, [^x] - they match any non-digit/non-x, including invalid UTF-8
			// 2. For partial Unicode classes like \P{Han} - we can't add 0x80-0xFF
			//    because it would incorrectly match valid UTF-8 bytes (e.g., each byte
			//    of "中" separately instead of treating it as one Han character).
			// The multi-byte paths take precedence for valid UTF-8 (longer match wins).
			invalidUTF8 := c.builder.AddByteRange(0x80, 0xFF, target)
			altStarts = append(altStarts, invalidUTF8)
		} else {
			// Precise: build UTF-8 automata for specific ranges (Issue #91 fix)
			// For partial Unicode classes like \P{Han}, we DON'T add invalid UTF-8
			// handling because it would incorrectly match bytes of valid UTF-8.
			for _, rng := range nonASCIIRanges {
				rangeStarts := c.compileUTF8Range(rng[0], rng[1], target)
				altStarts = append(altStarts, rangeStarts...)
			}
		}
	}

	if len(altStarts) == 0 {
		return c.compileNoMatch()
	}

	if len(altStarts) == 1 {
		return altStarts[0], target, nil
	}

	// Build split chain for all alternatives
	split := c.buildSplitChain(altStarts)
	return split, target, nil
}

// compileUTF8Range builds NFA states for a Unicode range [lo, hi].
// Returns a slice of start states that lead to endState when the range matches.
//
// UTF-8 encoding:
//   - 1-byte: U+0000-U+007F → 0x00-0x7F
//   - 2-byte: U+0080-U+07FF → 0xC2-0xDF, 0x80-0xBF
//   - 3-byte: U+0800-U+FFFF → 0xE0-0xEF, 0x80-0xBF, 0x80-0xBF
//   - 4-byte: U+10000-U+10FFFF → 0xF0-0xF4, 0x80-0xBF, 0x80-0xBF, 0x80-0xBF
func (c *Compiler) compileUTF8Range(lo, hi rune, endState StateID) []StateID {
	var starts []StateID

	// Split range by UTF-8 byte length boundaries
	// 1-byte: U+0000-U+007F
	if lo <= 0x7F {
		asciiHi := hi
		if asciiHi > 0x7F {
			asciiHi = 0x7F
		}
		s := c.compileUTF81ByteRange(lo, asciiHi, endState)
		starts = append(starts, s)
		lo = 0x80
	}

	if lo > hi {
		return starts
	}

	// 2-byte: U+0080-U+07FF
	if lo <= 0x7FF {
		twoByteHi := hi
		if twoByteHi > 0x7FF {
			twoByteHi = 0x7FF
		}
		s := c.compileUTF82ByteRange(lo, twoByteHi, endState)
		starts = append(starts, s...)
		lo = 0x800
	}

	if lo > hi {
		return starts
	}

	// 3-byte: U+0800-U+FFFF (excluding surrogates U+D800-U+DFFF)
	if lo <= 0xFFFF {
		threeByteHi := hi
		if threeByteHi > 0xFFFF {
			threeByteHi = 0xFFFF
		}
		s := c.compileUTF83ByteRange(lo, threeByteHi, endState)
		starts = append(starts, s...)
		lo = 0x10000
	}

	if lo > hi {
		return starts
	}

	// 4-byte: U+10000-U+10FFFF
	s := c.compileUTF84ByteRange(lo, hi, endState)
	starts = append(starts, s...)

	return starts
}

// compileUTF81ByteRange builds NFA for ASCII range [lo, hi] (U+0000-U+007F).
func (c *Compiler) compileUTF81ByteRange(lo, hi rune, endState StateID) StateID {
	return c.builder.AddByteRange(byte(lo), byte(hi), endState)
}

// compileUTF82ByteRange builds NFA for 2-byte UTF-8 range [lo, hi] (U+0080-U+07FF).
// 2-byte: lead 0xC2-0xDF, cont 0x80-0xBF
func (c *Compiler) compileUTF82ByteRange(lo, hi rune, endState StateID) []StateID {
	var starts []StateID

	// UTF-8 2-byte encoding: 110xxxxx 10xxxxxx
	// Lead byte: 0xC0 | (codepoint >> 6)
	// Cont byte: 0x80 | (codepoint & 0x3F)

	loLead := byte(0xC0 | (lo >> 6))
	loCont := byte(0x80 | (lo & 0x3F))
	hiLead := byte(0xC0 | (hi >> 6))
	hiCont := byte(0x80 | (hi & 0x3F))

	if loLead == hiLead {
		// Same lead byte - single sequence with cont range
		cont := c.builder.AddByteRange(loCont, hiCont, endState)
		lead := c.builder.AddByteRange(loLead, loLead, cont)
		starts = append(starts, lead)
	} else {
		// Different lead bytes - need multiple sequences
		// First: loLead with [loCont, 0xBF]
		cont1 := c.builder.AddByteRange(loCont, 0xBF, endState)
		lead1 := c.builder.AddByteRange(loLead, loLead, cont1)
		starts = append(starts, lead1)

		// Middle: [loLead+1, hiLead-1] with [0x80, 0xBF]
		if hiLead > loLead+1 {
			contM := c.builder.AddByteRange(0x80, 0xBF, endState)
			leadM := c.builder.AddByteRange(loLead+1, hiLead-1, contM)
			starts = append(starts, leadM)
		}

		// Last: hiLead with [0x80, hiCont]
		cont2 := c.builder.AddByteRange(0x80, hiCont, endState)
		lead2 := c.builder.AddByteRange(hiLead, hiLead, cont2)
		starts = append(starts, lead2)
	}

	return starts
}

// compileUTF83ByteRange builds NFA for 3-byte UTF-8 range [lo, hi] (U+0800-U+FFFF).
// 3-byte: lead 0xE0-0xEF, cont1 0x80-0xBF, cont2 0x80-0xBF
// Note: surrogates U+D800-U+DFFF are invalid in UTF-8 and should be excluded.
func (c *Compiler) compileUTF83ByteRange(lo, hi rune, endState StateID) []StateID {
	var starts []StateID

	// Handle surrogate gap: skip U+D800-U+DFFF
	if lo <= 0xD7FF && hi >= 0xE000 {
		// Range spans surrogates - split into two
		s1 := c.compileUTF83ByteRangeSimple(lo, 0xD7FF, endState)
		starts = append(starts, s1...)
		s2 := c.compileUTF83ByteRangeSimple(0xE000, hi, endState)
		starts = append(starts, s2...)
		return starts
	}

	// Skip if entirely in surrogate range
	if lo >= 0xD800 && hi <= 0xDFFF {
		return starts
	}

	// Clamp to avoid surrogates
	if lo >= 0xD800 && lo <= 0xDFFF {
		lo = 0xE000
	}
	if hi >= 0xD800 && hi <= 0xDFFF {
		hi = 0xD7FF
	}

	if lo > hi {
		return starts
	}

	return c.compileUTF83ByteRangeSimple(lo, hi, endState)
}

// compileUTF83ByteRangeSimple builds NFA for 3-byte range without surrogate handling.
func (c *Compiler) compileUTF83ByteRangeSimple(lo, hi rune, endState StateID) []StateID {
	var starts []StateID

	// UTF-8 3-byte encoding: 1110xxxx 10xxxxxx 10xxxxxx
	// Lead byte: 0xE0 | (codepoint >> 12)
	// Cont1 byte: 0x80 | ((codepoint >> 6) & 0x3F)
	// Cont2 byte: 0x80 | (codepoint & 0x3F)

	loLead := byte(0xE0 | (lo >> 12))
	loCont1 := byte(0x80 | ((lo >> 6) & 0x3F))
	loCont2 := byte(0x80 | (lo & 0x3F))
	hiLead := byte(0xE0 | (hi >> 12))
	hiCont1 := byte(0x80 | ((hi >> 6) & 0x3F))
	hiCont2 := byte(0x80 | (hi & 0x3F))

	switch {
	case loLead == hiLead && loCont1 == hiCont1:
		// Same lead and cont1 - single sequence with cont2 range
		cont2 := c.builder.AddByteRange(loCont2, hiCont2, endState)
		cont1 := c.builder.AddByteRange(loCont1, loCont1, cont2)
		lead := c.builder.AddByteRange(loLead, loLead, cont1)
		starts = append(starts, lead)

	case loLead == hiLead:
		// Same lead byte - need to handle cont1 range
		for cont1Val := loCont1; cont1Val <= hiCont1; cont1Val++ {
			c2Lo := c.utf8Cont2Lo(cont1Val, loCont1, loCont2)
			c2Hi := c.utf8Cont2Hi(cont1Val, hiCont1, hiCont2)
			cont2 := c.builder.AddByteRange(c2Lo, c2Hi, endState)
			cont1 := c.builder.AddByteRange(cont1Val, cont1Val, cont2)
			lead := c.builder.AddByteRange(loLead, loLead, cont1)
			starts = append(starts, lead)
		}

	default:
		// Different lead bytes - enumerate each lead byte's range
		for leadVal := loLead; leadVal <= hiLead; leadVal++ {
			c1Lo := c.utf8Cont1Lo3Byte(leadVal, loLead, loCont1)
			c1Hi := c.utf8Cont1Hi3Byte(leadVal, hiLead, hiCont1)

			for cont1Val := c1Lo; cont1Val <= c1Hi; cont1Val++ {
				c2Lo := c.utf8Cont2LoFull(leadVal, cont1Val, loLead, loCont1, loCont2)
				c2Hi := c.utf8Cont2HiFull(leadVal, cont1Val, hiLead, hiCont1, hiCont2)
				cont2 := c.builder.AddByteRange(c2Lo, c2Hi, endState)
				cont1 := c.builder.AddByteRange(cont1Val, cont1Val, cont2)
				lead := c.builder.AddByteRange(leadVal, leadVal, cont1)
				starts = append(starts, lead)
			}
		}
	}

	return starts
}

// compileUTF84ByteRange builds NFA for 4-byte UTF-8 range [lo, hi] (U+10000-U+10FFFF).
// 4-byte: lead 0xF0-0xF4, cont1-3 0x80-0xBF
func (c *Compiler) compileUTF84ByteRange(lo, hi rune, endState StateID) []StateID {
	var starts []StateID

	// Clamp to valid Unicode range
	if hi > 0x10FFFF {
		hi = 0x10FFFF
	}
	if lo < 0x10000 {
		lo = 0x10000
	}
	if lo > hi {
		return starts
	}

	// UTF-8 4-byte encoding: 11110xxx 10xxxxxx 10xxxxxx 10xxxxxx
	// For simplicity, use a conservative approach: match any valid 4-byte sequence in range
	// This creates more states but is correct

	loLead := byte(0xF0 | (lo >> 18))
	hiLead := byte(0xF0 | (hi >> 18))

	for leadVal := loLead; leadVal <= hiLead; leadVal++ {
		// Determine cont1 range for this lead byte
		var c1Lo, c1Hi byte
		if leadVal == 0xF0 {
			c1Lo = 0x90 // F0 requires cont1 >= 0x90
		} else {
			c1Lo = 0x80
		}
		if leadVal == 0xF4 {
			c1Hi = 0x8F // F4 requires cont1 <= 0x8F
		} else {
			c1Hi = 0xBF
		}

		// Build states for each lead byte value
		cont3 := c.builder.AddByteRange(0x80, 0xBF, endState)
		cont2 := c.builder.AddByteRange(0x80, 0xBF, cont3)
		cont1 := c.builder.AddByteRange(c1Lo, c1Hi, cont2)
		lead := c.builder.AddByteRange(leadVal, leadVal, cont1)
		starts = append(starts, lead)
	}

	return starts
}

// buildUTF8NonASCIIBranches builds NFA branches for all valid UTF-8 multi-byte sequences.
// Each branch represents a complete UTF-8 codepoint (2, 3, or 4 bytes) that transitions to endState.
// Returns a slice of start states for each branch (to be combined with buildSplitChain).
func (c *Compiler) buildUTF8NonASCIIBranches(endState StateID) []StateID {
	var branches []StateID

	// Continuation byte helper: creates state matching 0x80-0xBF
	cont := func(next StateID) StateID {
		return c.builder.AddByteRange(0x80, 0xBF, next)
	}

	// 2-byte: 0xC2-0xDF, 0x80-0xBF
	{
		cont1 := cont(endState)
		lead := c.builder.AddByteRange(0xC2, 0xDF, cont1)
		branches = append(branches, lead)
	}

	// 3-byte sequences
	{
		// 0xE0, 0xA0-0xBF, 0x80-0xBF
		cont2 := cont(endState)
		cont1 := c.builder.AddByteRange(0xA0, 0xBF, cont2)
		lead := c.builder.AddByteRange(0xE0, 0xE0, cont1)
		branches = append(branches, lead)
	}
	{
		// 0xE1-0xEC, 0x80-0xBF, 0x80-0xBF
		cont2 := cont(endState)
		cont1 := cont(cont2)
		lead := c.builder.AddByteRange(0xE1, 0xEC, cont1)
		branches = append(branches, lead)
	}
	{
		// 0xED, 0x80-0x9F, 0x80-0xBF (avoid surrogates U+D800-U+DFFF)
		cont2 := cont(endState)
		cont1 := c.builder.AddByteRange(0x80, 0x9F, cont2)
		lead := c.builder.AddByteRange(0xED, 0xED, cont1)
		branches = append(branches, lead)
	}
	{
		// 0xEE-0xEF, 0x80-0xBF, 0x80-0xBF
		cont2 := cont(endState)
		cont1 := cont(cont2)
		lead := c.builder.AddByteRange(0xEE, 0xEF, cont1)
		branches = append(branches, lead)
	}

	// 4-byte sequences
	{
		// 0xF0, 0x90-0xBF, 0x80-0xBF, 0x80-0xBF
		cont3 := cont(endState)
		cont2 := cont(cont3)
		cont1 := c.builder.AddByteRange(0x90, 0xBF, cont2)
		lead := c.builder.AddByteRange(0xF0, 0xF0, cont1)
		branches = append(branches, lead)
	}
	{
		// 0xF1-0xF3, 0x80-0xBF, 0x80-0xBF, 0x80-0xBF
		cont3 := cont(endState)
		cont2 := cont(cont3)
		cont1 := cont(cont2)
		lead := c.builder.AddByteRange(0xF1, 0xF3, cont1)
		branches = append(branches, lead)
	}
	{
		// 0xF4, 0x80-0x8F, 0x80-0xBF, 0x80-0xBF
		cont3 := cont(endState)
		cont2 := cont(cont3)
		cont1 := c.builder.AddByteRange(0x80, 0x8F, cont2)
		lead := c.builder.AddByteRange(0xF4, 0xF4, cont1)
		branches = append(branches, lead)
	}

	return branches
}

// UTF-8 continuation byte helper functions for 3-byte range compilation.
// These are extracted to satisfy gocritic ifElseChain linter.

func (c *Compiler) utf8Cont2Lo(cont1Val, loCont1, loCont2 byte) byte {
	if cont1Val == loCont1 {
		return loCont2
	}
	return 0x80
}

func (c *Compiler) utf8Cont2Hi(cont1Val, hiCont1, hiCont2 byte) byte {
	if cont1Val == hiCont1 {
		return hiCont2
	}
	return 0xBF
}

//nolint:staticcheck // QF1002: can't use tagged switch - comparing to both constant and variable
func (c *Compiler) utf8Cont1Lo3Byte(leadVal, loLead, loCont1 byte) byte {
	switch {
	case leadVal == loLead:
		return loCont1
	case leadVal == 0xE0:
		return 0xA0 // Special case for E0
	default:
		return 0x80
	}
}

//nolint:staticcheck // QF1002: can't use tagged switch - comparing to both constant and variable
func (c *Compiler) utf8Cont1Hi3Byte(leadVal, hiLead, hiCont1 byte) byte {
	switch {
	case leadVal == hiLead:
		return hiCont1
	case leadVal == 0xED:
		return 0x9F // Special case for ED (avoid surrogates)
	default:
		return 0xBF
	}
}

func (c *Compiler) utf8Cont2LoFull(leadVal, cont1Val, loLead, loCont1, loCont2 byte) byte {
	if leadVal == loLead && cont1Val == loCont1 {
		return loCont2
	}
	return 0x80
}

func (c *Compiler) utf8Cont2HiFull(leadVal, cont1Val, hiLead, hiCont1, hiCont2 byte) byte {
	if leadVal == hiLead && cont1Val == hiCont1 {
		return hiCont2
	}
	return 0xBF
}

// compileAnyChar compiles '.' matching any character including newlines.
// This is used for OpAnyChar which the parser generates when DotNL flag is set
// (either globally via syntax.DotNL or locally via inline flag (?s:...)).
func (c *Compiler) compileAnyChar() (start, end StateID, err error) {
	// ASCII-only mode: match any single ASCII byte (0x00-0x7F)
	// This reduces ~28 UTF-8 states to just 1 state.
	if c.config.ASCIIOnly {
		return c.compileASCIIAny(true)
	}
	// Match any UTF-8 codepoint (1-4 bytes) including newlines
	return c.compileUTF8Any(true)
}

// compileAnyCharNotNL compiles '.' matching any character except \n
func (c *Compiler) compileAnyCharNotNL() (start, end StateID, err error) {
	// ASCII-only mode: match any single ASCII byte except newline
	// This reduces ~28 UTF-8 states to just 1-2 states.
	if c.config.ASCIIOnly {
		return c.compileASCIIAny(false)
	}
	// Match any UTF-8 codepoint (1-4 bytes) except newline
	return c.compileUTF8Any(false)
}

// compileASCIIAny compiles '.' for ASCII-only mode.
// This is a massive optimization: 1 state instead of ~28 UTF-8 states.
//
// When input is known to be ASCII (all bytes < 0x80), we can use this
// simplified automaton that matches any single byte in the ASCII range.
//
// Performance impact (Issue #79 fix):
//   - UTF-8 mode: ~28 states for '.', BoundedBacktracker walks all states per byte
//   - ASCII mode: 1-2 states for '.', dramatically faster execution
//
// Parameters:
//   - includeNL: if true, match all ASCII (0x00-0x7F)
//   - includeNL: if false, exclude newline (0x00-0x09, 0x0B-0x7F)
func (c *Compiler) compileASCIIAny(includeNL bool) (start, end StateID, err error) {
	endState := c.builder.AddEpsilon(InvalidState)

	if includeNL {
		// All ASCII bytes: 0x00-0x7F (single transition)
		ascii := c.builder.AddByteRange(0x00, 0x7F, endState)
		return ascii, endState, nil
	}

	// ASCII except newline: [0x00-0x09] | [0x0B-0x7F]
	asciiTrans := []Transition{
		{Lo: 0x00, Hi: 0x09, Next: endState},
		{Lo: 0x0B, Hi: 0x7F, Next: endState},
	}
	ascii := c.builder.AddSparse(asciiTrans)
	return ascii, endState, nil
}

// compileUTF8Any compiles an NFA that matches any single UTF-8 codepoint.
// If includeNL is false, newline (0x0A) is excluded.
//
// This implementation uses UTF-8 suffix sharing to minimize state count.
// By processing byte sequences in REVERSE order and caching common suffixes
// (like [80-BF]), we reduce states from ~39 to ~15.
//
// Based on Rust regex-automata's approach (compiler.rs:1531-1568).
//
// UTF-8 encoding:
//   - 1-byte: 0x00-0x7F (ASCII)
//   - 2-byte: 0xC2-0xDF, 0x80-0xBF
//   - 3-byte: 0xE0, 0xA0-0xBF, 0x80-0xBF
//     0xE1-0xEC, 0x80-0xBF, 0x80-0xBF
//     0xED, 0x80-0x9F, 0x80-0xBF
//     0xEE-0xEF, 0x80-0xBF, 0x80-0xBF
//   - 4-byte: 0xF0, 0x90-0xBF, 0x80-0xBF, 0x80-0xBF
//     0xF1-0xF3, 0x80-0xBF, 0x80-0xBF, 0x80-0xBF
//     0xF4, 0x80-0x8F, 0x80-0xBF, 0x80-0xBF
func (c *Compiler) compileUTF8Any(includeNL bool) (start, end StateID, err error) {
	// Shared end state for all branches
	endState := c.builder.AddEpsilon(InvalidState)

	// Suffix cache for sharing common continuation byte states
	cache := newUtf8SuffixCache()

	// UTF-8 multi-byte sequences as byte ranges.
	// Each sequence is processed in REVERSE order for suffix sharing.
	// Format: []struct{lo, hi byte} from lead byte to final continuation
	type byteRange struct{ lo, hi byte }
	sequences := [][]byteRange{
		// 2-byte: 0xC2-0xDF, 0x80-0xBF
		{{0xC2, 0xDF}, {0x80, 0xBF}},
		// 3-byte sequences
		{{0xE0, 0xE0}, {0xA0, 0xBF}, {0x80, 0xBF}},
		{{0xE1, 0xEC}, {0x80, 0xBF}, {0x80, 0xBF}},
		{{0xED, 0xED}, {0x80, 0x9F}, {0x80, 0xBF}}, // avoid surrogates
		{{0xEE, 0xEF}, {0x80, 0xBF}, {0x80, 0xBF}},
		// 4-byte sequences
		{{0xF0, 0xF0}, {0x90, 0xBF}, {0x80, 0xBF}, {0x80, 0xBF}},
		{{0xF1, 0xF3}, {0x80, 0xBF}, {0x80, 0xBF}, {0x80, 0xBF}},
		{{0xF4, 0xF4}, {0x80, 0x8F}, {0x80, 0xBF}, {0x80, 0xBF}},
	}

	// Build alternation of all UTF-8 patterns
	var branches []StateID

	// 1-byte ASCII (0x00-0x7F), excluding newline if needed
	if includeNL {
		// All ASCII including newline
		ascii := c.builder.AddByteRange(0x00, 0x7F, endState)
		branches = append(branches, ascii)
	} else {
		// ASCII except newline: [0x00-0x09] | [0x0B-0x7F]
		asciiTrans := []Transition{
			{Lo: 0x00, Hi: 0x09, Next: endState},
			{Lo: 0x0B, Hi: 0x7F, Next: endState},
		}
		ascii := c.builder.AddSparse(asciiTrans)
		branches = append(branches, ascii)
	}

	// Multi-byte UTF-8 sequences with suffix sharing.
	// Process each sequence in REVERSE order to maximize cache hits.
	// Example: for [E1-EC][80-BF][80-BF], process [80-BF] last byte first,
	// which allows sharing with [EE-EF][80-BF][80-BF].
	for _, seq := range sequences {
		target := endState
		// Process bytes in REVERSE order (from last to first)
		for i := len(seq) - 1; i >= 0; i-- {
			br := seq[i]
			// getOrCreate returns cached state or creates new one
			target = cache.getOrCreate(c.builder, target, br.lo, br.hi)
		}
		branches = append(branches, target)
	}

	// Invalid UTF-8 bytes - match as single bytes for stdlib compatibility.
	// Go regexp's . matches invalid UTF-8 bytes as single characters.
	// Invalid bytes: 0x80-0xBF (standalone continuation), 0xC0-0xC1 (overlong),
	// 0xF5-0xFF (out of range for Unicode).
	//
	// NOTE: We don't add 0xC2-0xF4 (valid lead bytes) here because:
	// 1. Adding them causes capture group bugs with zero-width matches ((.*)on "")
	// 2. The multi-byte paths already handle these bytes in valid sequences
	// 3. When these bytes appear standalone, the NFA won't match them (correct behavior
	//    differs from stdlib, but preserves capture group correctness which is more important)
	invalidTrans := []Transition{
		{Lo: 0x80, Hi: 0xBF, Next: endState}, // standalone continuation bytes
		{Lo: 0xC0, Hi: 0xC1, Next: endState}, // overlong 2-byte encodings
		{Lo: 0xF5, Hi: 0xFF, Next: endState}, // out of Unicode range
	}
	invalidUTF8 := c.builder.AddSparse(invalidTrans)
	branches = append(branches, invalidUTF8)

	// Create split state for alternation
	startState := c.buildSplitChain(branches)

	return startState, endState, nil
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

// compileStar compiles a* (greedy) or a*? (non-greedy)
func (c *Compiler) compileStar(sub *syntax.Regexp, nonGreedy bool) (start, end StateID, err error) {
	subStart, subEnd, err := c.compileRegexp(sub)
	if err != nil {
		return InvalidState, InvalidState, err
	}

	// Create split: either enter sub or skip
	// split -> [sub, end]
	// sub -> split (loop back)
	end = c.builder.AddEpsilon(InvalidState)
	// For greedy: prefer continue (left=subStart) over exit (right=end)
	//   Use AddQuantifierSplit - no priority change, longer match wins
	// For non-greedy: prefer exit (left=end) over continue (right=subStart)
	//   Use AddSplit so priority favors exit path (shorter match wins)
	var split StateID
	if nonGreedy {
		split = c.builder.AddSplit(end, subStart)
	} else {
		split = c.builder.AddQuantifierSplit(subStart, end)
	}

	// Connect sub end back to split (loop)
	if err := c.builder.Patch(subEnd, split); err != nil {
		epsilon := c.builder.AddEpsilon(split)
		if err := c.builder.Patch(subEnd, epsilon); err != nil {
			return InvalidState, InvalidState, err
		}
	}

	return split, end, nil
}

// compilePlus compiles a+ (greedy) or a+? (non-greedy)
func (c *Compiler) compilePlus(sub *syntax.Regexp, nonGreedy bool) (start, end StateID, err error) {
	subStart, subEnd, err := c.compileRegexp(sub)
	if err != nil {
		return InvalidState, InvalidState, err
	}

	// Must match at least once
	// sub -> split -> [sub, end]
	end = c.builder.AddEpsilon(InvalidState)
	// For greedy: prefer continue (left=subStart) over exit (right=end)
	//   Use AddQuantifierSplit - no priority change, longer match wins
	// For non-greedy: prefer exit (left=end) over continue (right=subStart)
	//   Use AddSplit so priority favors exit path (shorter match wins)
	var split StateID
	if nonGreedy {
		split = c.builder.AddSplit(end, subStart)
	} else {
		split = c.builder.AddQuantifierSplit(subStart, end)
	}

	// Connect sub end to split (loop)
	if err := c.builder.Patch(subEnd, split); err != nil {
		epsilon := c.builder.AddEpsilon(split)
		if err := c.builder.Patch(subEnd, epsilon); err != nil {
			return InvalidState, InvalidState, err
		}
	}

	return subStart, end, nil
}

// compileQuest compiles a? (greedy) or a?? (non-greedy)
func (c *Compiler) compileQuest(sub *syntax.Regexp, nonGreedy bool) (start, end StateID, err error) {
	subStart, subEnd, err := c.compileRegexp(sub)
	if err != nil {
		return InvalidState, InvalidState, err
	}

	// Either match sub or skip
	end = c.builder.AddEpsilon(InvalidState)
	// For greedy: prefer match (left=subStart) over skip (right=end)
	//   Use AddQuantifierSplit - no priority change, longer match wins
	// For non-greedy: prefer skip (left=end) over match (right=subStart)
	//   Use AddSplit so priority favors skip path (shorter match wins)
	var split StateID
	if nonGreedy {
		split = c.builder.AddSplit(end, subStart)
	} else {
		split = c.builder.AddQuantifierSplit(subStart, end)
	}

	// Connect sub end to end
	if err := c.builder.Patch(subEnd, end); err != nil {
		epsilon := c.builder.AddEpsilon(end)
		if err := c.builder.Patch(subEnd, epsilon); err != nil {
			return InvalidState, InvalidState, err
		}
	}

	return split, end, nil
}

// compileRepeat compiles a{m,n} (greedy) or a{m,n}? (non-greedy)
func (c *Compiler) compileRepeat(sub *syntax.Regexp, minCount, maxCount int, nonGreedy bool) (start, end StateID, err error) {
	if maxCount == -1 {
		// a{m,} = aaa...a* (minCount copies + star)
		return c.compileRepeatMin(sub, minCount, nonGreedy)
	}
	if minCount == maxCount {
		// a{n} = aaa...a (exactly n copies) - greedy/non-greedy doesn't matter
		return c.compileRepeatExact(sub, minCount)
	}
	// a{m,n} = aaa...a(a?a?a?...) (minCount copies + (maxCount-minCount) optional copies)
	return c.compileRepeatRange(sub, minCount, maxCount, nonGreedy)
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

// compileRepeatMin compiles a{m,} (greedy) or a{m,}? (non-greedy)
func (c *Compiler) compileRepeatMin(sub *syntax.Regexp, minCount int, nonGreedy bool) (start, end StateID, err error) {
	if minCount == 0 {
		return c.compileStar(sub, nonGreedy)
	}

	// Concatenate minCount copies + star
	var subs []*syntax.Regexp
	for i := 0; i < minCount; i++ {
		subs = append(subs, sub)
	}
	// Create synthetic star with correct NonGreedy flag
	starFlags := syntax.Flags(0)
	if nonGreedy {
		starFlags |= syntax.NonGreedy
	}
	subs = append(subs, &syntax.Regexp{
		Op:    syntax.OpStar,
		Flags: starFlags,
		Sub:   []*syntax.Regexp{sub},
	})
	return c.compileConcat(subs)
}

// compileRepeatRange compiles a{m,n} (greedy) or a{m,n}? (non-greedy)
func (c *Compiler) compileRepeatRange(sub *syntax.Regexp, minCount, maxCount int, nonGreedy bool) (start, end StateID, err error) {
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
	// Create synthetic quest nodes with correct NonGreedy flag
	questFlags := syntax.Flags(0)
	if nonGreedy {
		questFlags |= syntax.NonGreedy
	}
	for i := 0; i < maxCount-minCount; i++ {
		subs = append(subs, &syntax.Regexp{
			Op:    syntax.OpQuest,
			Flags: questFlags,
			Sub:   []*syntax.Regexp{sub},
		})
	}
	return c.compileConcat(subs)
}

// compileEmptyMatch compiles an epsilon transition (matches without consuming input)
func (c *Compiler) compileEmptyMatch() (start, end StateID, err error) {
	id := c.builder.AddEpsilon(InvalidState)
	return id, id, nil
}

// compileNoMatch compiles an NFA fragment that never matches.
// This is used for empty character classes like [^\S\s] which logically match nothing.
// The resulting NFA has a start state with no transitions to the end state,
// making it impossible to reach a match state.
func (c *Compiler) compileNoMatch() (start, end StateID, err error) {
	// Create start and end states that are not connected
	// The start state has no transitions, so the NFA can never progress
	start = c.builder.AddEpsilon(InvalidState)
	end = c.builder.AddEpsilon(InvalidState)
	// Don't connect start to end - this makes it impossible to match
	return start, end, nil
}

// encodeRune encodes a rune as UTF-8 into buf and returns the number of bytes written.
// buf must have capacity >= 4.
//
//nolint:gosec // G602: buf capacity is guaranteed by caller contract (see comment above)
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
	closeCapture := c.builder.AddCapture(conv.IntToUint32(re.Cap), false, InvalidState)

	// Connect sub-expression end to closing capture
	if err := c.builder.Patch(subEnd, closeCapture); err != nil {
		// If patching fails, insert epsilon
		epsilon := c.builder.AddEpsilon(closeCapture)
		if err := c.builder.Patch(subEnd, epsilon); err != nil {
			return InvalidState, InvalidState, err
		}
	}

	// Create opening capture state (records start position)
	openCapture := c.builder.AddCapture(conv.IntToUint32(re.Cap), true, subStart)

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
//
// IMPORTANT: This also checks for internal end anchors (like in `(a$)b$`).
// If there's an end anchor that's NOT at the very end, ReverseAnchored won't work.
func IsPatternEndAnchored(re *syntax.Regexp) bool {
	// First check if pattern ends with $
	if !isEndAnchored(re) {
		return false
	}
	// Then check for internal end anchors (which would make ReverseAnchored incorrect)
	if hasInternalEndAnchor(re) {
		return false
	}
	return true
}

// isEndAnchored checks if the pattern ends with $ (without checking for internal anchors)
func isEndAnchored(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpEndText: // Only OpEndText is truly end-anchored, not OpEndLine (multiline $)
		return true
	case syntax.OpConcat:
		if len(re.Sub) > 0 {
			// Check the last sub-expression
			return isEndAnchored(re.Sub[len(re.Sub)-1])
		}
	case syntax.OpCapture:
		if len(re.Sub) > 0 {
			return isEndAnchored(re.Sub[0])
		}
	case syntax.OpAlternate:
		// All alternatives must be end-anchored
		if len(re.Sub) == 0 {
			return false
		}
		for _, sub := range re.Sub {
			if !isEndAnchored(sub) {
				return false
			}
		}
		return true
	}
	return false
}

// hasInternalEndAnchor checks if there's an end anchor ($ or \z) that's NOT at the
// very end of the pattern. Such patterns like `(a$)b$` have contradictory anchors.
func hasInternalEndAnchor(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpConcat:
		// In a concatenation, check all elements EXCEPT the last one for end anchors
		// The last one is allowed to have an end anchor (that's the "end" anchor)
		for i := 0; i < len(re.Sub)-1; i++ {
			if containsEndAnchor(re.Sub[i]) {
				return true
			}
		}
		// Also check if the last element has internal anchors
		if len(re.Sub) > 0 {
			if hasInternalEndAnchor(re.Sub[len(re.Sub)-1]) {
				return true
			}
		}
	case syntax.OpCapture:
		if len(re.Sub) > 0 {
			return hasInternalEndAnchor(re.Sub[0])
		}
	case syntax.OpAlternate:
		for _, sub := range re.Sub {
			if hasInternalEndAnchor(sub) {
				return true
			}
		}
	}
	return false
}

// HasImpossibleEndAnchor checks if the pattern contains an end anchor ($, \z)
// that is NOT at the end of the pattern. Such patterns like `$00` can never match
// because nothing can follow the end-of-string assertion.
//
// Examples:
//   - `$00` → true ($ followed by "00" - impossible)
//   - `00$` → false ($ at end - valid end-anchored pattern)
//   - `a$b` → true ($ followed by "b" - impossible)
//   - `(a$)` → false ($ at end of group - valid)
func HasImpossibleEndAnchor(re *syntax.Regexp) bool {
	return containsEndAnchor(re) && !isEndAnchored(re)
}

// containsEndAnchor checks if the AST contains any end anchor ($ or \z)
func containsEndAnchor(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpEndText, syntax.OpEndLine:
		return true
	case syntax.OpConcat, syntax.OpAlternate:
		for _, sub := range re.Sub {
			if containsEndAnchor(sub) {
				return true
			}
		}
	case syntax.OpCapture, syntax.OpStar, syntax.OpPlus, syntax.OpQuest, syntax.OpRepeat:
		if len(re.Sub) > 0 {
			return containsEndAnchor(re.Sub[0])
		}
	}
	return false
}

// IsPatternStartAnchored checks if ANY branch of the pattern starts with ^ or \A.
// This is used to prevent UseReverseAnchored strategy selection for patterns like
// `^a?$|^b?$` where the start anchor constrains matching positions.
//
// Unlike IsPatternEndAnchored which requires ALL branches to be end-anchored,
// this function returns true if ANY branch has a start anchor, because reverse
// search cannot properly handle partial start anchoring in alternations.
func IsPatternStartAnchored(re *syntax.Regexp) bool {
	return containsStartAnchor(re)
}

// containsStartAnchor checks if the AST contains any start anchor (^ or \A)
func containsStartAnchor(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpBeginText, syntax.OpBeginLine:
		return true
	case syntax.OpConcat:
		// Check all parts of concatenation (start anchor could be in first position)
		for _, sub := range re.Sub {
			if containsStartAnchor(sub) {
				return true
			}
		}
	case syntax.OpAlternate:
		// Check all alternatives - if ANY has start anchor, we need to be careful
		for _, sub := range re.Sub {
			if containsStartAnchor(sub) {
				return true
			}
		}
	case syntax.OpCapture, syntax.OpStar, syntax.OpPlus, syntax.OpQuest, syntax.OpRepeat:
		if len(re.Sub) > 0 {
			return containsStartAnchor(re.Sub[0])
		}
	}
	return false
}
