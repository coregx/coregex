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
	config  CompilerConfig
	builder *Builder
	depth   int // current recursion depth
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

	// Compile the regex into NFA states
	// Returns (start, end) state IDs for the compiled fragment
	start, end, err := c.compileRegexp(re)
	if err != nil {
		return nil, err
	}

	// Add final match state
	matchID := c.builder.AddMatch()

	// Connect end to match state
	if err := c.builder.Patch(end, matchID); err != nil {
		// If patching fails, end might be a Split state - add epsilon
		epsilonID := c.builder.AddEpsilon(matchID)
		if patchErr := c.builder.Patch(end, epsilonID); patchErr != nil {
			return nil, &CompileError{
				Err: fmt.Errorf("failed to connect to match state: %w", patchErr),
			}
		}
	}

	// Set start state
	c.builder.SetStart(start)

	// Build the final NFA
	nfa, err := c.builder.Build(
		WithUTF8(c.config.UTF8),
		WithAnchored(c.config.Anchored),
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
		return c.compileLiteral(re.Rune)
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
		// For MVP, treat captures as non-capturing groups
		return c.compileRegexp(re.Sub[0])
	case syntax.OpBeginLine, syntax.OpBeginText:
		// Anchors - for MVP, just pass through (handled by anchored flag)
		// In future, implement as zero-width assertions
		return c.compileEmptyMatch()
	case syntax.OpEndLine, syntax.OpEndText:
		// End anchors - for MVP, just pass through
		return c.compileEmptyMatch()
	case syntax.OpEmptyMatch:
		return c.compileEmptyMatch()
	default:
		return InvalidState, InvalidState, &CompileError{
			Err: fmt.Errorf("unsupported regex operation: %v", re.Op),
		}
	}
}

// compileLiteral compiles a literal string (sequence of runes)
func (c *Compiler) compileLiteral(runes []rune) (start, end StateID, err error) {
	if len(runes) == 0 {
		return c.compileEmptyMatch()
	}

	// Convert runes to UTF-8 bytes
	var prev = InvalidState
	var first = InvalidState

	for _, r := range runes {
		// Convert rune to UTF-8 bytes
		buf := make([]byte, 4)
		n := encodeRune(buf, r)

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
	}

	return first, prev, nil
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

	// Build alternation of all characters in ranges
	var alts []*syntax.Regexp
	for i := 0; i < len(ranges); i += 2 {
		lo := ranges[i]
		hi := ranges[i+1]
		for r := lo; r <= hi; r++ {
			alts = append(alts, &syntax.Regexp{
				Op:   syntax.OpLiteral,
				Rune: []rune{r},
			})
			// Limit to avoid explosion
			if len(alts) > 256 {
				return InvalidState, InvalidState, &CompileError{
					Err: fmt.Errorf("character class too large (>256 characters)"),
				}
			}
		}
	}

	if len(alts) == 1 {
		return c.compileRegexp(alts[0])
	}

	return c.compileAlternate(alts)
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
	end = c.builder.AddEpsilon(InvalidState)
	split := c.builder.AddSplit(subStart, end)

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
	end = c.builder.AddEpsilon(InvalidState)
	split := c.builder.AddSplit(subStart, end)

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
	end = c.builder.AddEpsilon(InvalidState)
	split := c.builder.AddSplit(subStart, end)

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
//
//nolint:gosec // G602: Buffer bounds are checked by rune size, safe for UTF-8 encoding
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
