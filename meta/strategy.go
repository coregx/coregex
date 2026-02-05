package meta

import (
	"regexp/syntax"

	"github.com/coregx/coregex/literal"
	"github.com/coregx/coregex/nfa"
	"github.com/coregx/coregex/prefilter"
)

// Strategy represents the execution strategy for regex matching.
//
// The meta-engine chooses between:
//   - UseNFA: use PikeVM exclusively (simple patterns, no cache needed)
//   - UseDFA: use Lazy DFA with NFA fallback (complex patterns, good literals)
//   - UseBoth: adaptive strategy (try DFA first, fallback to NFA on cache full)
//
// Strategy selection is automatic based on pattern analysis.
type Strategy int

const (
	// UseNFA uses only the NFA (PikeVM) engine.
	// Selected for:
	//   - Very small NFAs (< 20 states) where DFA overhead isn't worth it
	//   - Patterns without literals where DFA has no advantage
	//   - When EnableDFA is false in config
	UseNFA Strategy = iota

	// UseDFA uses Lazy DFA with NFA fallback on cache overflow.
	// Selected for:
	//   - Large NFAs (> 100 states) where DFA is essential
	//   - Patterns with good literals (prefilter + DFA is fastest)
	//   - Simple patterns (no alternations) where DFA doesn't blow up
	UseDFA

	// UseBoth uses adaptive strategy: try DFA, fallback to NFA if cache fills.
	// Selected for:
	//   - Medium-sized NFAs (20-100 states)
	//   - Patterns with some literals but complex structure
	//   - Default when pattern characteristics are unclear
	UseBoth

	// UseReverseAnchored uses reverse DFA search for patterns anchored at end.
	// Selected for:
	//   - Patterns with $ or \z anchor (end of text)
	//   - NOT also anchored at start (^)
	//   - Searches backward from end of haystack
	//   - Converts O(n*m) to O(m) for end-anchored patterns
	UseReverseAnchored

	// UseReverseSuffix uses suffix literal prefilter + reverse DFA search.
	// Selected for:
	//   - Patterns with literal suffix (e.g., `.*\.txt`)
	//   - NOT start-anchored (^)
	//   - Has good suffix literal for prefiltering
	//   - Speedup: 10-100x for patterns like `.*\.txt`
	UseReverseSuffix

	// UseOnePass uses one-pass DFA for anchored patterns with capture groups.
	// Selected for:
	//   - Pattern is always anchored (^ or implicit anchor)
	//   - Pattern is "one-pass" (no ambiguity in matching paths)
	//   - Pattern has capture groups (otherwise lazy DFA is faster)
	//   - Speedup: 10-20x over PikeVM for capture group extraction
	//   - Only used for FindSubmatch, not Find
	UseOnePass

	// UseReverseInner uses inner literal prefilter + bidirectional DFA search.
	// Selected for:
	//   - Patterns with inner literal (e.g., `prefix.*inner.*suffix`)
	//   - NOT start-anchored (^) or end-anchored ($)
	//   - Has good inner literal for prefiltering
	//   - NO good prefix or suffix literals (otherwise prefer UseDFA/UseReverseSuffix)
	//   - Has wildcards both before AND after inner literal
	//   - Speedup: 10-100x for patterns like `ERROR.*connection.*timeout`
	UseReverseInner

	// UseBoundedBacktracker uses bounded backtracking with bit-vector visited tracking.
	// Selected for:
	//   - Simple character class patterns (\d+, \w+, [a-z]+) without literals
	//   - Small enough input (states * inputLen <= threshold)
	//   - No prefilter benefit (no extractable literals)
	//   - Speedup: 2-4x over PikeVM for character class patterns
	UseBoundedBacktracker

	// UseTeddy uses Teddy multi-pattern prefilter directly without DFA.
	// Selected for:
	//   - Exact literal alternations like (foo|bar|baz)
	//   - All literals are complete (no regex engine verification needed)
	//   - 2-32 patterns, each >= 3 bytes
	//   - Speedup: 50-250x over PikeVM by skipping all DFA/NFA overhead
	//
	// This implements the "literal engine bypass" optimization from Rust regex:
	// when patterns are exact literals, the prefilter IS the engine.
	UseTeddy

	// UseReverseSuffixSet uses Teddy multi-pattern prefilter for suffix alternations.
	// Selected for:
	//   - Patterns like `.*\.(txt|log|md)` where suffix is an alternation
	//   - No common suffix (LCS is empty), but multiple suffix literals available
	//   - 2-32 suffix literals, each >= 3 bytes
	//   - Speedup: 5-10x over UseBoth by using Teddy for suffix candidates
	//
	// Algorithm:
	//   1. Teddy finds any of the suffix literals (e.g., ".txt", ".log", ".md")
	//   2. Reverse DFA scan from suffix position to find match start
	//   3. For `.*` prefix patterns, match starts at position 0 (skip reverse scan)
	//
	// This is an optimization NOT present in rust-regex (they fallback to Core).
	UseReverseSuffixSet

	// UseCharClassSearcher uses specialized lookup-table searcher for simple char_class+ patterns.
	// Selected for:
	//   - Patterns like `[\w]+`, `[a-z]+`, `\d+` (simple repeated character class)
	//   - NOT concatenations (those use BoundedBacktracker)
	//   - NOT patterns with capture groups
	//   - Speedup: 14-22x over stdlib, 14-17x over BoundedBacktracker
	//
	// Uses 256-byte membership table for O(1) byte classification instead of
	// NFA state tracking. Optimal for "find all words" type patterns.
	UseCharClassSearcher

	// UseCompositeSearcher uses sequential lookup tables for concatenated char class patterns.
	// Selected for:
	//   - Patterns like [a-zA-Z]+[0-9]+, \d+\s+\w+, [a-z]+[A-Z]+
	//   - Concatenation of 2+ quantified character classes
	//   - No anchors, captures, or alternations within
	//   - Speedup: 5-6x over BoundedBacktracker by using O(1) lookup tables
	//
	// Algorithm:
	//   1. Each char class part has [256]bool membership table
	//   2. Greedy matching: consume max chars for each part
	//   3. Backtrack if min requirement not met
	//
	// Reference: https://github.com/coregx/coregex/issues/72
	UseCompositeSearcher

	// UseBranchDispatch uses O(1) first-byte dispatch for anchored alternations.
	// Selected for:
	//   - Start-anchored patterns like ^(\d+|UUID|hex32)
	//   - Each alternation branch has distinct first bytes (no overlap)
	//   - Speedup: 2-3x on match, 10x+ on no-match by avoiding branch iteration
	//
	// Algorithm:
	//   1. Build [256]int8 dispatch table: first_byte → branch_index
	//   2. On search: dispatch[haystack[0]] gives branch to try
	//   3. Only execute that single branch instead of all branches
	//
	// Reference: https://github.com/coregx/coregex/issues/79
	UseBranchDispatch

	// UseDigitPrefilter uses SIMD digit scanning for patterns that must start with digits.
	// Selected for:
	//   - Patterns where ALL alternation branches must start with a digit [0-9]
	//   - Examples: IP address patterns, numeric validators
	//   - Pattern has no extractable prefix literals (due to alternation structure)
	//   - Speedup: 5-10x by skipping non-digit regions with SIMD
	//
	// Algorithm:
	//   1. SIMD scan haystack for digit sequences
	//   2. At each digit position, run lazy DFA to verify match
	//   3. Skip non-digit regions entirely (major speedup for sparse matches)
	//
	// This addresses Issue #50 (IP regex optimization) where alternations like
	// `25[0-5]|2[0-4][0-9]|...` produce no extractable prefix literals.
	UseDigitPrefilter

	// UseAhoCorasick uses Aho-Corasick automaton for large literal alternations.
	// Selected for:
	//   - Exact literal alternations with >32 patterns (beyond Teddy's limit)
	//   - All literals are complete (no regex engine verification needed)
	//   - Each pattern >= 1 byte
	//   - Speedup: 50-500x over PikeVM by using O(n) multi-pattern matching
	//
	// Uses github.com/coregx/ahocorasick library with:
	//   - Dense array transitions for O(1) state lookup
	//   - Byte class compression for memory efficiency
	//   - ~1.6 GB/s throughput
	//
	// This extends the "literal engine bypass" optimization for large pattern sets
	// where Teddy's SIMD approach becomes impractical.
	UseAhoCorasick

	// UseAnchoredLiteral uses specialized O(1) matching for anchored prefix.*suffix patterns.
	// Selected for:
	//   - Patterns matching: ^prefix.*[charclass+]suffix$
	//   - Both start (^) and end ($) anchored
	//   - Contains .* or .+ wildcard
	//   - Has literal suffix (e.g., ".php", ".txt")
	//   - Optional prefix literal and charclass bridge
	//   - Speedup: 50-90x over stdlib by avoiding NFA/DFA entirely
	//
	// Algorithm (all O(1) or O(k) operations):
	//   1. O(1) length check (MinLength)
	//   2. O(k) prefix check (bytes.HasPrefix equivalent)
	//   3. O(k) suffix check (bytes.HasSuffix equivalent)
	//   4. O(m) charclass bridge verification (if required)
	//
	// Examples:
	//   - ^/.*[\w-]+\.php$ → prefix="/", suffix=".php", charclass=[\w-]
	//   - ^.*\.txt$        → no prefix, suffix=".txt", no charclass
	//   - ^api/v1/.*\.json$ → prefix="api/v1/", suffix=".json"
	//
	// This is a specialized "literal engine bypass" for URL/path matching patterns
	// that are extremely common in web applications and routing tables.
	// Reference: https://github.com/coregx/coregex/issues/79
	UseAnchoredLiteral

	// UseMultilineReverseSuffix uses line-aware suffix search for multiline patterns.
	// Selected for:
	//   - Patterns with (?m) multiline flag AND line-start anchor (^)
	//   - Pattern has good suffix literal (e.g., `(?m)^/.*\.php`)
	//   - The ^ anchor matches at LINE start, not just input start
	//   - Speedup: 5-20x over stdlib by avoiding per-position forward NFA
	//
	// Algorithm (from Rust regex rebar benchmarks):
	//   1. Find suffix literal candidates using prefilter
	//   2. For each candidate, scan backward to find LINE start (\n or pos 0)
	//   3. Verify prefix pattern from line start to suffix
	//   4. Return first valid match
	//
	// Key difference from UseReverseSuffix:
	//   - ReverseSuffix: unanchored (.*suffix), match starts at pos 0
	//   - MultilineReverseSuffix: line-anchored ((?m)^.*suffix), match starts at line start
	//
	// This fixes Issue #97 where `(?m)^/.*[\w-]+\.php` was 24% SLOWER than stdlib
	// because UseReverseSuffix assumed match always starts at position 0.
	// Reference: https://github.com/coregx/coregex/issues/97
	UseMultilineReverseSuffix
)

// String returns a human-readable representation of the Strategy.
func (s Strategy) String() string {
	switch s {
	case UseNFA:
		return "UseNFA"
	case UseDFA:
		return "UseDFA"
	case UseBoth:
		return "UseBoth"
	case UseReverseAnchored:
		return "UseReverseAnchored"
	case UseReverseSuffix:
		return "UseReverseSuffix"
	case UseOnePass:
		return "UseOnePass"
	case UseReverseInner:
		return "UseReverseInner"
	case UseBoundedBacktracker:
		return "UseBoundedBacktracker"
	case UseTeddy:
		return "UseTeddy"
	case UseReverseSuffixSet:
		return "UseReverseSuffixSet"
	case UseCharClassSearcher:
		return "UseCharClassSearcher"
	case UseCompositeSearcher:
		return "UseCompositeSearcher"
	case UseBranchDispatch:
		return "UseBranchDispatch"
	case UseDigitPrefilter:
		return "UseDigitPrefilter"
	case UseAhoCorasick:
		return "UseAhoCorasick"
	case UseAnchoredLiteral:
		return "UseAnchoredLiteral"
	case UseMultilineReverseSuffix:
		return "UseMultilineReverseSuffix"
	default:
		return "Unknown"
	}
}

// hasWordBoundary recursively checks if a syntax.Regexp contains word boundary assertions.
// Returns true if the pattern contains \b or \B.
//
// Word boundary assertions don't work correctly with reverse DFA search because
// the boundary depends on both adjacent characters, which changes meaning in reverse.
func hasWordBoundary(re *syntax.Regexp) bool {
	if re == nil {
		return false
	}

	switch re.Op {
	case syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		return true
	case syntax.OpConcat, syntax.OpAlternate:
		for _, sub := range re.Sub {
			if hasWordBoundary(sub) {
				return true
			}
		}
	case syntax.OpCapture, syntax.OpStar, syntax.OpPlus, syntax.OpQuest, syntax.OpRepeat:
		for _, sub := range re.Sub {
			if hasWordBoundary(sub) {
				return true
			}
		}
	}
	return false
}

// isDigitOnlyClass returns true if the character class contains ONLY digits [0-9].
// The runes slice contains pairs: [lo1, hi1, lo2, hi2, ...] representing ranges.
//
// Examples:
//   - [0-9] → runes = [48, 57] → true
//   - [0-5] → runes = [48, 53] → true
//   - [0-9a-z] → runes = [48, 57, 97, 122] → false (includes letters)
//   - [a-z] → runes = [97, 122] → false (no digits)
func isDigitOnlyClass(runes []rune) bool {
	if len(runes) == 0 || len(runes)%2 != 0 {
		return false
	}

	for i := 0; i < len(runes); i += 2 {
		lo, hi := runes[i], runes[i+1]
		// Range must be within '0' (48) to '9' (57)
		if lo < '0' || hi > '9' {
			return false
		}
	}
	return true
}

// isDigitLeadConcat checks if a concatenation pattern is digit-lead.
// For concatenation, we iterate through elements:
// - If an element is optional AND digit-only, we continue (it's fine either way)
// - If an element is optional but NOT digit-only, the pattern is NOT digit-lead
// - If an element is required, we check if it's digit-lead
func isDigitLeadConcat(subs []*syntax.Regexp) bool {
	for _, sub := range subs {
		if isOptionalElement(sub) {
			// Optional element - if it matches, must be digit-only
			if !isOptionalDigitOnly(sub) {
				// Could match non-digit character, so not digit-lead
				return false
			}
			// Optional and digit-only, continue to next element
			continue
		}
		// Required element - must be digit-lead
		return isDigitLeadPattern(sub)
	}
	// All elements were optional - pattern can match empty, not digit-lead
	return false
}

// isOptionalElement returns true if the syntax element can match zero characters.
// This includes Quest (?), Star (*), and Repeat with min=0.
func isOptionalElement(re *syntax.Regexp) bool {
	if re == nil {
		return false
	}
	switch re.Op {
	case syntax.OpQuest, syntax.OpStar:
		return true
	case syntax.OpRepeat:
		return re.Min == 0
	default:
		return false
	}
}

// isOptionalDigitOnly returns true if the optional element, when it matches,
// only matches digits. This is used for [1-9]? type patterns where we need
// to verify the element is safe to skip over in digit-lead detection.
func isOptionalDigitOnly(re *syntax.Regexp) bool {
	if re == nil || len(re.Sub) == 0 {
		return false
	}
	// Check if the sub-pattern (the thing being made optional) is digit-only
	sub := re.Sub[0]
	switch sub.Op {
	case syntax.OpCharClass:
		return isDigitOnlyClass(sub.Rune)
	case syntax.OpLiteral:
		// All runes must be digits
		for _, r := range sub.Rune {
			if r < '0' || r > '9' {
				return false
			}
		}
		return len(sub.Rune) > 0
	default:
		// For other cases, recursively check if digit-lead
		// (If it's digit-lead, any match starts with digit)
		return isDigitLeadPattern(sub)
	}
}

// isDigitLeadPattern returns true if ALL branches of the pattern must start with a digit [0-9].
// This is used to enable digit prefilter optimization for patterns like IP addresses.
//
// The function recursively analyzes the AST to determine if every possible match
// must begin with a digit character. This enables SIMD prefiltering to skip
// non-digit regions entirely.
//
// Examples that return true:
//   - \d+ (digit class with plus)
//   - [0-9]+ (explicit digit range)
//   - [0-9] (single digit required)
//   - 25[0-5]|2[0-4][0-9] (all branches start with digit literal)
//   - (?:25[0-5]|...) (non-capturing group)
//   - (\d+) (capture group wrapping digit pattern)
//   - [0-5][0-9] (concatenation starting with digit)
//
// Examples that return false:
//   - [a-z0-9]+ (may start with letter)
//   - a\d+ (starts with literal 'a')
//   - \d*foo (star can match zero - may start with 'f')
//   - \d?foo (quest can match zero - may start with 'f')
//   - [0-9]* (star can match zero)
//   - .*\d+ (dot-star matches anything)
//   - \w+ (word class includes letters)
func isDigitLeadPattern(re *syntax.Regexp) bool {
	if re == nil {
		return false
	}

	switch re.Op {
	case syntax.OpCharClass:
		// Character class must contain ONLY digits
		return isDigitOnlyClass(re.Rune)

	case syntax.OpLiteral:
		// First rune must be a digit
		return len(re.Rune) > 0 && re.Rune[0] >= '0' && re.Rune[0] <= '9'

	case syntax.OpAlternate:
		// ALL branches must start with digit
		if len(re.Sub) == 0 {
			return false
		}
		for _, sub := range re.Sub {
			if !isDigitLeadPattern(sub) {
				return false
			}
		}
		return true

	case syntax.OpConcat:
		// Delegate to helper to reduce cyclomatic complexity
		if len(re.Sub) == 0 {
			return false
		}
		return isDigitLeadConcat(re.Sub)

	case syntax.OpCapture:
		// Look through capture group
		if len(re.Sub) == 0 {
			return false
		}
		return isDigitLeadPattern(re.Sub[0])

	case syntax.OpPlus:
		// Plus requires at least one match, check the sub-pattern
		if len(re.Sub) == 0 {
			return false
		}
		return isDigitLeadPattern(re.Sub[0])

	case syntax.OpRepeat:
		// OpRepeat with min >= 1 guarantees at least one match
		if len(re.Sub) == 0 {
			return false
		}
		if re.Min >= 1 {
			return isDigitLeadPattern(re.Sub[0])
		}
		// min == 0 means could match zero times
		return false

	case syntax.OpStar, syntax.OpQuest:
		// Star (*) and Quest (?) can match zero times, so pattern might not start with digit
		return false

	case syntax.OpEmptyMatch:
		// Empty match doesn't require any character
		return false

	case syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		// Dot (.) matches any character, not specifically digits
		return false

	case syntax.OpBeginLine, syntax.OpEndLine, syntax.OpBeginText, syntax.OpEndText:
		// Anchors don't consume characters
		return false

	case syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		// Word boundaries don't consume characters
		return false

	default:
		return false
	}
}

// digitPrefilterMaxNFAStates is the maximum NFA state count for using digit prefilter.
// Set to 100 to include IP patterns (74 states) - digit prefilter + sliced haystack
// optimization provides good speedup by skipping non-digit positions.
const digitPrefilterMaxNFAStates = 100

// shouldUseDigitPrefilter checks if the pattern should use digit prefilter optimization.
// Returns true if:
//   - Pattern must start with a digit [0-9]
//   - DFA and prefilter are enabled
//   - Pattern is not too complex (NFA states <= digitPrefilterMaxNFAStates)
//   - Pattern is suitable for SIMD digit scanning
//
// This is used for simple digit-lead patterns where SIMD scanning is beneficial.
// Complex patterns like IP addresses (74 NFA states) should use plain DFA because
// the per-position verification overhead exceeds the SIMD scanning benefit.
func shouldUseDigitPrefilter(re *syntax.Regexp, nfaSize int, config Config) bool {
	if re == nil || !config.EnableDFA || !config.EnablePrefilter {
		return false
	}
	// Complex patterns have too much DFA overhead per digit position
	if nfaSize > digitPrefilterMaxNFAStates {
		return false
	}
	return isDigitLeadPattern(re)
}

// isSafeForReverseSuffix checks if a pattern is safe for UseReverseSuffix strategy.
// Returns true only for patterns where reverse search is proven to work correctly.
//
// Safe patterns (whitelist approach):
//   - `.*suffix` - AnyChar Star followed by literal
//   - `.+suffix` - AnyChar Plus followed by literal
//   - `[charclass]+suffix` - CharClass Plus followed by literal (e.g., `[^\s]+\.txt`)
//   - `prefix.*suffix` - literal, AnyChar Star, literal
//
// Unsafe patterns (blacklist - excluded):
//   - Quest (?) before suffix: `0?0`, `a?b` - reverse NFA bug with optional
//   - Internal anchors: `0?^0`, `a$b` - position constraints don't reverse
//   - Star of CharClass: `[^\s]*suffix` - zero-width match edge cases
func isSafeForReverseSuffix(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpConcat:
		if len(re.Sub) < 2 {
			return false
		}
		// Check for wildcard patterns: .*suffix, .+suffix, [charclass]+suffix
		// Look for AnyChar Star/Plus or CharClass Plus anywhere in concat
		hasWildcard := false
		for i := 0; i < len(re.Sub)-1; i++ {
			sub := re.Sub[i]
			// AnyChar Star/Plus: .* or .+
			if (sub.Op == syntax.OpStar || sub.Op == syntax.OpPlus) &&
				len(sub.Sub) > 0 &&
				(sub.Sub[0].Op == syntax.OpAnyChar || sub.Sub[0].Op == syntax.OpAnyCharNotNL) {
				hasWildcard = true
				break
			}
			// CharClass Plus: [^\s]+, [\w]+, etc. (requires at least one char match)
			if sub.Op == syntax.OpPlus && len(sub.Sub) > 0 && sub.Sub[0].Op == syntax.OpCharClass {
				hasWildcard = true
				break
			}
		}
		if !hasWildcard {
			return false // No wildcard pattern - not safe
		}
		// Check for internal anchors (^ or $ not at expected positions)
		for i := 1; i < len(re.Sub)-1; i++ {
			if containsAnchor(re.Sub[i]) {
				return false // Internal anchor - not safe
			}
		}
		return true

	case syntax.OpCapture:
		if len(re.Sub) > 0 {
			return isSafeForReverseSuffix(re.Sub[0])
		}
		return false

	default:
		return false
	}
}

// containsAnchor checks if AST contains any anchor (^, $, \A, \z)
func containsAnchor(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpBeginLine, syntax.OpEndLine, syntax.OpBeginText, syntax.OpEndText:
		return true
	case syntax.OpConcat, syntax.OpAlternate:
		for _, sub := range re.Sub {
			if containsAnchor(sub) {
				return true
			}
		}
	case syntax.OpCapture, syntax.OpStar, syntax.OpPlus, syntax.OpQuest, syntax.OpRepeat:
		if len(re.Sub) > 0 {
			return containsAnchor(re.Sub[0])
		}
	}
	return false
}

// isMultilineLineAnchored checks if a pattern has multiline line-start anchor (^).
// Returns true for patterns like `(?m)^.*suffix` where ^ matches at line starts.
//
// Detection criteria:
//   - Pattern contains OpBeginLine (^) - multiline start anchor
//   - Pattern is NOT truly start-anchored (IsAlwaysAnchored returns false)
//   - Pattern has .* or .+ wildcard
//   - Pattern has suffix literal
//
// This distinguishes:
//   - `(?m)^.*\.php` → true (multiline, line-anchored)
//   - `.*\.php` → false (unanchored, use UseReverseSuffix)
//   - `\A.*\.php` → false (text-anchored, not multiline)
func isMultilineLineAnchored(re *syntax.Regexp) bool {
	return containsLineStartAnchor(re) && containsWildcard(re)
}

// containsLineStartAnchor checks if AST contains OpBeginLine (^) but NOT OpBeginText (\A).
// Returns true only for multiline line-start anchors, not text-start anchors.
func containsLineStartAnchor(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpBeginLine:
		return true
	case syntax.OpBeginText:
		return false // Text anchor, not line anchor
	case syntax.OpConcat:
		// Check first element for start anchor
		if len(re.Sub) > 0 && re.Sub[0].Op == syntax.OpBeginLine {
			return true
		}
		// Also check nested structures
		for _, sub := range re.Sub {
			if containsLineStartAnchor(sub) {
				return true
			}
		}
	case syntax.OpAlternate:
		// All branches must have line-start anchor for pattern to be line-anchored
		if len(re.Sub) == 0 {
			return false
		}
		for _, sub := range re.Sub {
			if !containsLineStartAnchor(sub) {
				return false
			}
		}
		return true
	case syntax.OpCapture:
		if len(re.Sub) > 0 {
			return containsLineStartAnchor(re.Sub[0])
		}
	}
	return false
}

// containsWildcard checks if AST contains .* or .+ wildcard pattern.
func containsWildcard(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpStar, syntax.OpPlus:
		if len(re.Sub) > 0 {
			sub := re.Sub[0]
			if sub.Op == syntax.OpAnyChar || sub.Op == syntax.OpAnyCharNotNL {
				return true
			}
		}
	case syntax.OpConcat, syntax.OpAlternate:
		for _, sub := range re.Sub {
			if containsWildcard(sub) {
				return true
			}
		}
	case syntax.OpCapture, syntax.OpQuest, syntax.OpRepeat:
		if len(re.Sub) > 0 {
			return containsWildcard(re.Sub[0])
		}
	}
	return false
}

// isSafeForMultilineReverseSuffix checks if a pattern is safe for UseMultilineReverseSuffix.
// Returns true for patterns where line-aware reverse search is proven to work correctly.
//
// Safe patterns:
//   - `(?m)^.*suffix` - Line-anchored with any-char wildcard
//   - `(?m)^.+suffix` - Line-anchored with required any-char
//   - `(?m)^[charclass]+suffix` - Line-anchored with charclass
//
// Unsafe patterns:
//   - Patterns with internal line anchors
//   - Patterns where reverse search semantics differ
func isSafeForMultilineReverseSuffix(re *syntax.Regexp) bool {
	if !isMultilineLineAnchored(re) {
		return false
	}

	switch re.Op {
	case syntax.OpConcat:
		if len(re.Sub) < 2 {
			return false
		}
		// First element should be ^ (line start anchor)
		// Then .* or .+ or [charclass]+
		// Then suffix literal
		hasLineAnchor := false
		hasWildcard := false

		for i, sub := range re.Sub {
			if i == 0 && sub.Op == syntax.OpBeginLine {
				hasLineAnchor = true
				continue
			}
			if isWildcardOp(sub) {
				hasWildcard = true
				continue
			}
		}

		return hasLineAnchor && hasWildcard

	case syntax.OpCapture:
		if len(re.Sub) > 0 {
			return isSafeForMultilineReverseSuffix(re.Sub[0])
		}
		return false

	default:
		return false
	}
}

// isWildcardOp checks if the op is a wildcard pattern (.*, .+, or [charclass]+)
func isWildcardOp(re *syntax.Regexp) bool {
	if re.Op == syntax.OpStar || re.Op == syntax.OpPlus {
		if len(re.Sub) > 0 {
			sub := re.Sub[0]
			// .* or .+
			if sub.Op == syntax.OpAnyChar || sub.Op == syntax.OpAnyCharNotNL {
				return true
			}
			// [charclass]+
			if re.Op == syntax.OpPlus && sub.Op == syntax.OpCharClass {
				return true
			}
		}
	}
	return false
}

// isSafeForReverseInner checks if a pattern is safe for UseReverseInner strategy.
// Returns true for patterns where reverse search is proven to work correctly.
//
// Safe patterns:
//   - `.*keyword.*` - AnyChar Star on both sides
//   - `[\w]+@[\w]+` - CharClass Plus (email patterns)
//   - `.+keyword` - AnyChar Plus before
//
// Unsafe patterns:
//   - `A*20*` - Star of Literal (not AnyChar or CharClass)
//   - Patterns with Star that could match zero (zero-width issues)
func isSafeForReverseInner(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpConcat:
		if len(re.Sub) < 2 {
			return false
		}
		// Check for safe wildcard prefix at the beginning
		first := re.Sub[0]

		// AnyChar Star/Plus (.* or .+) - always safe
		if (first.Op == syntax.OpStar || first.Op == syntax.OpPlus) &&
			len(first.Sub) > 0 &&
			(first.Sub[0].Op == syntax.OpAnyChar || first.Sub[0].Op == syntax.OpAnyCharNotNL) {
			return true
		}

		// CharClass Plus ([\w]+ etc) - safe because Plus requires at least 1 char
		// Star of CharClass could be zero-width, so we only allow Plus
		if first.Op == syntax.OpPlus && len(first.Sub) > 0 && first.Sub[0].Op == syntax.OpCharClass {
			return true
		}

		return false // Literal Star like "A*" - not safe

	case syntax.OpCapture:
		if len(re.Sub) > 0 {
			return isSafeForReverseInner(re.Sub[0])
		}
		return false

	default:
		return false
	}
}

// shouldUseReverseSuffixSet checks if multiple suffix literals are available for Teddy prefilter.
// This handles patterns like `.*\.(txt|log|md)` where LCS is empty but individual suffixes are useful.
// Returns true if ReverseSuffixSet strategy should be used.
func shouldUseReverseSuffixSet(prefixLiterals, suffixLiterals *literal.Seq) bool {
	if suffixLiterals == nil || suffixLiterals.IsEmpty() {
		return false
	}

	// Skip if this is an exact literal alternation (would be better served by UseTeddy)
	// For exact alternations like `foo|bar|baz`:
	// - PREFIX literals = ["foo", "bar", "baz"]
	// - SUFFIX literals = ["foo", "bar", "baz"] (same!)
	// - All literals are complete
	// For suffix patterns like `.*\.(txt|log|md)`:
	// - PREFIX literals = [] or [""] (due to .*)
	// - SUFFIX literals = [".txt", ".log", ".md"]
	if prefixLiterals != nil && !prefixLiterals.IsEmpty() && prefixLiterals.AllComplete() {
		if prefixLiterals.Len() == suffixLiterals.Len() {
			return false // Exact alternation - use UseTeddy instead
		}
	}

	litCount := suffixLiterals.Len()
	if litCount < 2 || litCount > 32 {
		return false // Teddy requires 2-32 patterns
	}

	// Check if all suffix literals are long enough for efficient Teddy
	for i := 0; i < litCount; i++ {
		if len(suffixLiterals.Get(i).Bytes) < 2 { // Allow 2-byte suffixes for extensions
			return false
		}
	}

	return true
}

// hasFastPrefixPrefilter checks if the prefix literals would produce a "fast"
// SIMD-backed prefilter. Used to gate reverse optimizations.
//
// Returns true when:
//   - Prefix literals have a good LCP (Longest Common Prefix) that produces Memchr/Memmem
//   - Prefix literals would produce a fast Teddy prefilter (multiple patterns, minLen >= 3)
//
// Returns false when:
//   - No prefix literals exist
//   - Prefix literals are too short for effective prefiltering
//   - Only Aho-Corasick would be built (not considered fast enough to gate reverse)
func hasFastPrefixPrefilter(literals *literal.Seq, config Config) bool {
	if literals == nil || literals.IsEmpty() {
		return false
	}
	// Good LCP means a single-literal prefilter (Memchr or Memmem) which is always fast
	lcp := literals.LongestCommonPrefix()
	if len(lcp) >= config.MinLiteralLen {
		return true
	}
	// Check if a Teddy prefilter with good discrimination would be built
	return prefilter.WouldBeFast(literals)
}

// selectReverseStrategy selects reverse-based strategies (ReverseSuffix, ReverseInner).
// Returns 0 if no reverse strategy is suitable.
//
// This is a helper function to reduce cyclomatic complexity in SelectStrategy.
func selectReverseStrategy(n *nfa.NFA, re *syntax.Regexp, literals *literal.Seq, config Config) Strategy {
	// Only applicable if DFA and prefilter enabled, not anchored
	if re == nil || !config.EnableDFA || !config.EnablePrefilter {
		return 0
	}

	// Patterns with end anchor ($, \z) NOT at end position are impossible to match.
	// E.g., `$00` has $ followed by "00" - nothing can follow end-of-string.
	// These patterns should fall through to NFA which will correctly return no match.
	if nfa.HasImpossibleEndAnchor(re) {
		return 0
	}

	// Word boundary assertions (\b, \B) don't work correctly with reverse DFA search.
	if hasWordBoundary(re) {
		return 0
	}

	if n.IsAlwaysAnchored() || nfa.IsPatternEndAnchored(re) {
		return 0 // Anchored patterns use other strategies
	}

	// Create extractor early - needed for multiline check and suffix extraction
	extractor := literal.New(literal.ExtractorConfig{
		MaxLiterals:   config.MaxLiterals,
		MaxLiteralLen: 64,
		MaxClassSize:  10,
	})

	// MULTILINE CHECK FIRST (Issue #97): For patterns like `(?m)^/.*\.php`,
	// the ^ anchor matches at LINE start, not just position 0.
	// Prefix literals are NOT useful because match can occur on ANY line.
	// UseMultilineReverseSuffix handles line-aware matching correctly.
	// This check MUST come BEFORE the prefix literal check!
	if isSafeForMultilineReverseSuffix(re) {
		suffixLiterals := extractor.ExtractSuffixes(re)
		if suffixLiterals != nil && !suffixLiterals.IsEmpty() {
			lcs := suffixLiterals.LongestCommonSuffix()
			if len(lcs) >= config.MinLiteralLen {
				return UseMultilineReverseSuffix
			}
		}
	}

	// Check if prefix literals would produce a fast forward prefilter.
	// If so, skip reverse optimizations (the overhead is not worth it).
	//
	// Two conditions are checked:
	//   1. Good LCP (common prefix) -> Memchr/Memmem prefilter (always fast)
	//   2. WouldBeFast() -> Teddy prefilter with minLen >= 3 (fast SIMD)
	//
	// This comes AFTER multiline check because multiline patterns cannot
	// benefit from prefix literals (match can be at any line start).
	//
	// Reference: Rust regex-automata strategy.rs:1162 and strategy.rs:1555
	//   if core.pre.as_ref().map_or(false, |p| p.is_fast()) {
	//       debug!("skipping reverse optimization because we already have a fast prefilter");
	//       return Err(core);
	//   }
	if hasFastPrefixPrefilter(literals, config) {
		return 0 // Fast prefix prefilter available - skip reverse optimizations
	}

	// No good/fast prefix - check suffix and inner literals
	// Check suffix literals (for patterns like `.*\.txt`)
	suffixLiterals := extractor.ExtractSuffixes(re)
	if suffixLiterals != nil && !suffixLiterals.IsEmpty() {
		lcs := suffixLiterals.LongestCommonSuffix()
		if len(lcs) >= config.MinLiteralLen {
			// Use whitelist approach: only enable ReverseSuffix for patterns
			// where reverse search is proven to work correctly.
			// Patterns like "0?0", "0?^0" have bugs in reverse NFA.
			if !isSafeForReverseSuffix(re) {
				return 0 // Fall through to other strategies
			}
			return UseReverseSuffix // Good suffix literal available
		}
	}

	// No common suffix (LCS empty), but check if multiple suffix literals available
	// for Teddy multi-suffix prefilter. This handles patterns like `.*\.(txt|log|md)`.
	if shouldUseReverseSuffixSet(literals, suffixLiterals) {
		return UseReverseSuffixSet
	}

	// No prefix or suffix - try inner literal (for patterns like `.*keyword.*`)
	innerInfo := extractor.ExtractInnerForReverseSearch(re)
	if innerInfo != nil {
		lcp := innerInfo.Literals.LongestCommonPrefix()
		// Single-character inner literals like "@" can be effective for email patterns
		// because: (1) Match() is fast with memchr prefilter, (2) Find() uses
		// early return optimization. ReverseInner detects quadratic behavior
		// and falls back to Core when needed.
		//
		// EXCEPTION: For digit-lead patterns like `\d+\.\d+\.\d+`, single-byte
		// inner literals (like ".") have high frequency in typical text.
		// DigitPrefilter is 3.8x faster for these patterns (Issue #75).
		if len(lcp) == 1 && isDigitLeadPattern(re) {
			return 0 // Let DigitPrefilter handle digit-lead patterns
		}
		if len(lcp) >= 1 {
			// Use whitelist approach: only enable ReverseInner for patterns
			// where reverse search is proven to work correctly.
			// Patterns like "A*20*" have Star of Literal, not AnyChar wildcard.
			if !isSafeForReverseInner(re) {
				return 0 // Fall through to other strategies
			}
			return UseReverseInner // Inner literal available - use ReverseInner
		}
	}

	return 0 // No suitable reverse strategy
}

// isSimpleCharClass checks if a regexp is a simple character class pattern
// like [0-9], \d, \w, etc. that doesn't benefit from DFA overhead.
// Returns true for patterns that are just repeats of character classes.
//
// This also handles patterns with capture groups wrapping character classes,
// like (a|b|c)+ which Go's parser optimizes to Plus(Capture(CharClass)).
// BoundedBacktracker can handle capture groups efficiently (they're epsilon
// transitions in the NFA), and is 3-7x faster than PikeVM for these patterns.
func isSimpleCharClass(re *syntax.Regexp) bool {
	if re == nil {
		return false
	}

	switch re.Op {
	case syntax.OpCharClass:
		// Direct character class like [0-9] or \d
		return true
	case syntax.OpPlus, syntax.OpStar, syntax.OpQuest, syntax.OpRepeat:
		// Repeat of character class like [0-9]+ or \d*
		if len(re.Sub) == 1 {
			return isSimpleCharClass(re.Sub[0])
		}
	case syntax.OpConcat:
		// Allow concatenations of character classes like [0-9]+[a-z]+
		// but only if all are simple
		for _, sub := range re.Sub {
			if !isSimpleCharClass(sub) {
				return false
			}
		}
		return true
	case syntax.OpCapture:
		// Look through capture groups - (a|b|c)+ parses as Plus(Capture(CharClass))
		// BoundedBacktracker handles captures correctly (epsilon transitions)
		if len(re.Sub) == 1 {
			return isSimpleCharClass(re.Sub[0])
		}
	}
	return false
}

// literalAnalysis contains the results of analyzing literals for strategy selection.
type literalAnalysis struct {
	hasGoodLiterals        bool // Good prefix literal (LCP >= MinLiteralLen)
	hasTeddyLiterals       bool // Suitable for Teddy (2-32 patterns, each >= 3 bytes)
	hasAhoCorasickLiterals bool // Suitable for Aho-Corasick (>32 patterns, each >= 1 byte)
}

// selectLiteralStrategy selects strategy based on literal analysis.
// Returns 0 if no literal-based strategy is suitable.
// This is a helper function to reduce cyclomatic complexity in SelectStrategy.
func selectLiteralStrategy(literals *literal.Seq, litAnalysis literalAnalysis) Strategy {
	if literals == nil {
		return 0
	}

	// Exact literal alternations → use Teddy directly (literal engine bypass)
	// Patterns like "(foo|bar|baz)" where all literals are complete don't need
	// DFA verification - Teddy.Find() returns exact matches.
	// Speedup: 50-250x by skipping all DFA/NFA construction overhead.
	if litAnalysis.hasTeddyLiterals && literals.AllComplete() {
		return UseTeddy
	}

	// Large literal alternations → use Aho-Corasick (extends literal engine bypass)
	// Patterns with >32 literals exceed Teddy's capacity but Aho-Corasick handles
	// thousands of patterns with O(n) matching time.
	// Speedup: 50-500x by using dense array transitions (~1.6 GB/s throughput).
	if litAnalysis.hasAhoCorasickLiterals && literals.AllComplete() {
		return UseAhoCorasick
	}

	return 0
}

// analyzeLiterals checks if literals are suitable for prefiltering.
// This is a helper function to reduce cyclomatic complexity in SelectStrategy.
func analyzeLiterals(literals *literal.Seq, config Config) literalAnalysis {
	result := literalAnalysis{}

	if literals == nil || literals.IsEmpty() {
		return result
	}

	// Check longest common prefix (for single-literal prefilter like Memmem)
	lcp := literals.LongestCommonPrefix()
	if len(lcp) >= config.MinLiteralLen {
		result.hasGoodLiterals = true
	}

	// Check for Teddy prefilter suitability (2-64 literals, each >= 3 bytes)
	// Teddy doesn't need common prefix - it can search for multiple distinct literals.
	// This enables fast alternation pattern matching: (foo|bar|baz|qux)
	//
	// Slim Teddy (SSSE3, 8 buckets): 2-32 patterns - optimal, uses SIMD
	// Fat Teddy (AVX2, 16 buckets): 33-64 patterns - uses SIMD or scalar fallback
	//
	// For >64 patterns, use Aho-Corasick.
	litCount := literals.Len()
	if litCount >= 2 && litCount <= 64 {
		allLongEnough := true
		for i := 0; i < litCount; i++ {
			if len(literals.Get(i).Bytes) < 3 {
				allLongEnough = false
				break
			}
		}
		if allLongEnough {
			result.hasTeddyLiterals = true
		}
	}

	// Check for Aho-Corasick suitability (>64 literals, each >= 1 byte)
	// Aho-Corasick handles large pattern sets efficiently with O(n) matching.
	// This extends the "literal engine bypass" optimization beyond Teddy's 64 pattern limit.
	if litCount > 64 {
		allNonEmpty := true
		for i := 0; i < litCount; i++ {
			if len(literals.Get(i).Bytes) < 1 {
				allNonEmpty = false
				break
			}
		}
		if allNonEmpty {
			result.hasAhoCorasickLiterals = true
		}
	}

	return result
}

// SelectStrategy analyzes the NFA and literals to choose the best execution strategy.
//
// Algorithm:
//  1. If end-anchored ($ or \z) and not start-anchored → UseReverseAnchored
//  2. If DFA disabled in config → UseNFA
//  3. If NFA is tiny (< 20 states) → UseNFA (DFA overhead not worth it)
//  4. If simple character class pattern without literals → UseNFA (DFA overhead not worth it)
//  5. If good literals exist → UseDFA (prefilter + DFA is fastest)
//  6. If NFA is large (> 100 states) → UseDFA (essential for performance)
//  7. Otherwise → UseBoth (adaptive)
//
// "Good literals" means:
//   - At least one literal exists
//   - Longest common prefix (LCP) length >= MinLiteralLen
//   - This enables effective prefiltering
//
// Parameters:
//   - n: the compiled NFA to analyze
//   - re: the parsed regexp (for anchor detection, can be nil)
//   - literals: extracted prefix literals (can be nil)
//   - config: meta-engine configuration
//
// Example:
//
//	strategy := meta.SelectStrategy(nfa, re, literals, config)
//	switch strategy {
//	case meta.UseNFA:
//	    // Use PikeVM only
//	case meta.UseDFA:
//	    // Use Lazy DFA
//	case meta.UseReverseAnchored:
//	    // Use reverse search
//	case meta.UseBoth:
//	    // Adaptive
//	}
//
//nolint:cyclop // Strategy selection has many cases by design
func SelectStrategy(n *nfa.NFA, re *syntax.Regexp, literals *literal.Seq, config Config) Strategy {
	// Check for end-anchored patterns (highest priority optimization)
	// Pattern must:
	//   1. Be anchored at end ($ or \z)
	//   2. NOT be anchored at start (^ or \A)
	//   3. Have DFA enabled
	// This converts O(n*m) forward search to O(m) reverse search
	//
	// Note: We must avoid UseReverseAnchored for patterns that contain any start
	// anchor (^ or \A), even in alternations like `^a?$|^b?$`. The reverse DFA
	// cannot properly handle start anchors and would produce false positives.
	isStartAnchored := n.IsAlwaysAnchored()
	isEndAnchored := re != nil && nfa.IsPatternEndAnchored(re)
	hasStartAnchor := re != nil && nfa.IsPatternStartAnchored(re)

	if re != nil && config.EnableDFA && isEndAnchored && !isStartAnchored && !hasStartAnchor {
		// Perfect candidate for reverse search
		// Example: "pattern.*suffix$" on large haystack
		// Forward: O(n*m) tries, Reverse: O(m) one try
		return UseReverseAnchored
	}

	// START-ANCHORED OPTIMIZATION (Rust regex-automata approach)
	// For patterns anchored at start (^ or \A), skip Lazy DFA overhead.
	// Rationale: Only position 0 can match, so DFA construction overhead is wasteful.
	// Rust uses: OnePass → BoundedBacktracker → PikeVM for anchored patterns.
	//
	// Benefits:
	//   - Zero state construction overhead (vs Lazy DFA building states per byte)
	//   - BoundedBacktracker is optimized for single-position verification
	//   - 2-5x faster than Lazy DFA for anchored alternations like ^(a|b|c)
	//
	// Note on Issue #79: ^/.*[\w-]+\.php is slower than stdlib due to UTF-8 state
	// explosion (39 NFA states for `.`). This requires V11-002 (ASCII runtime detection)
	// or V11-003 (Daciuk's algorithm) to fix - not DFA (which is actually slower due
	// to lazy state construction overhead).
	//
	// Applies to BOTH:
	//   - Pure start-anchored: ^pattern (can match at pos 0 only)
	//   - Both-anchored: ^pattern$ (can match at pos 0, must end at end)
	// Both cases benefit from skipping DFA - match position is fully determined.
	if isStartAnchored {
		// HIGHEST PRIORITY: Check for anchored literal patterns (^prefix.*suffix$)
		// These patterns can use O(1) specialized matching (50-90x faster than stdlib).
		// Detection via DetectAnchoredLiteral analyzes AST for:
		//   - Both anchors (^ and $)
		//   - Wildcard (.* or .+)
		//   - Literal suffix (required)
		//   - Optional prefix and charclass bridge
		// Reference: https://github.com/coregx/coregex/issues/79
		if isEndAnchored && DetectAnchoredLiteral(re) != nil {
			return UseAnchoredLiteral
		}

		// Try branch dispatch for anchored alternations with distinct first bytes.
		// This gives O(1) branch selection instead of trying all branches.
		// Example: ^(\d+|UUID|hex32) → dispatch['0'-'9']=0, dispatch['U']=1, dispatch['h']=2
		if nfa.IsBranchDispatchPattern(re) {
			return UseBranchDispatch
		}
		return UseBoundedBacktracker
	}

	// Check for inner/suffix literal optimizations (second priority)
	// Delegated to helper function to reduce cyclomatic complexity
	if strategy := selectReverseStrategy(n, re, literals, config); strategy != 0 {
		return strategy
	}

	// If DFA disabled, always use NFA
	if !config.EnableDFA {
		return UseNFA
	}

	// Analyze NFA size and literals
	nfaSize := n.States()
	litAnalysis := analyzeLiterals(literals, config)

	// Check for simple char_class+ patterns (HIGHEST priority for character class patterns)
	// Patterns like [\w]+, [a-z]+, \d+ use CharClassSearcher: 14-17x faster than BoundedBacktracker
	// This must come BEFORE BoundedBacktracker check because CharClassSearcher is much faster
	// for the simple case (no concatenations, no capture groups).
	if !litAnalysis.hasGoodLiterals && !litAnalysis.hasTeddyLiterals && nfa.IsSimpleCharClassPlus(re) {
		return UseCharClassSearcher
	}

	// Check for concatenated char class patterns like [a-zA-Z]+[0-9]+
	// Uses sequential lookup tables for 5-6x speedup over BoundedBacktracker.
	// Must come AFTER CharClassSearcher (single char class) but BEFORE BoundedBacktracker.
	// Reference: https://github.com/coregx/coregex/issues/72
	if !litAnalysis.hasGoodLiterals && !litAnalysis.hasTeddyLiterals && nfa.IsCompositeCharClassPattern(re) {
		return UseCompositeSearcher
	}

	// Check for complex character class patterns (concatenations, captures) without literals
	// Patterns like [0-9]+[a-z]+ or (a|b|c)+ benefit from BoundedBacktracker:
	// 2-4x faster than PikeVM due to bit-vector visited tracking instead of SparseSet.
	if !litAnalysis.hasGoodLiterals && !litAnalysis.hasTeddyLiterals && isSimpleCharClass(re) {
		return UseBoundedBacktracker
	}

	// Check for exact literal alternations (Teddy, Aho-Corasick)
	// Delegated to helper function to reduce cyclomatic complexity.
	if strategy := selectLiteralStrategy(literals, litAnalysis); strategy != 0 {
		return strategy
	}

	// Tiny NFA with literals: use prefilter + NFA (like Rust)
	// For patterns like "j[a-z]+p", DFA construction overhead is not worth it
	// on small inputs. NFA with prefilter skip-ahead is faster.
	// The prefilter (memchr) jumps to candidates, NFA verifies in O(pattern) time.
	if nfaSize < 20 && litAnalysis.hasGoodLiterals {
		return UseNFA // findIndicesNFA now uses prefilter for skip-ahead
	}

	// Check for simple digit-lead patterns BEFORE tiny NFA fallback.
	// Patterns like `\d+\.\d+\.\d+` (14 NFA states) benefit more from
	// DigitPrefilter than plain NFA because SIMD digit scanning skips
	// non-digit regions entirely.
	if shouldUseDigitPrefilter(re, nfaSize, config) {
		return UseDigitPrefilter
	}

	// Tiny NFA without literals: use PikeVM directly (DFA overhead not worth it)
	// For patterns like "a", ".", "[0-9]", the DFA cache lookup and
	// determinization overhead exceeds the benefit.
	if nfaSize < 20 {
		return UseNFA
	}

	// Good literals on larger NFA → use prefilter + DFA (best performance)
	// Patterns like "ABXBYXCX" or "(foo|foobar)\d+" benefit massively from:
	//  1. Prefilter finds literal candidates quickly (5-50x speedup)
	//  2. DFA verifies with O(n) deterministic scan
	// Also covers Teddy multi-pattern prefilter for alternation patterns where
	// literals are not complete (e.g., "(foo|bar)\d+" needs DFA verification).
	if litAnalysis.hasGoodLiterals || litAnalysis.hasTeddyLiterals {
		return UseDFA
	}

	// Large NFA without literals → still use DFA
	// For patterns like "(a|b|c|d|e|f|g|h)*z", the DFA cache
	// prevents re-exploration of the same NFA state sets.
	// Even without prefilter, DFA's deterministic execution is faster
	// than NFA's parallel state tracking.
	if nfaSize > 100 {
		return UseDFA
	}

	// Medium NFA without strong characteristics → adaptive
	// Try DFA first (may hit cache), fallback to NFA if cache fills.
	// This handles patterns like "a*b*c*" where DFA may or may not help.
	return UseBoth
}

// strategyReasons maps simple strategies to their reason strings.
// This reduces cyclomatic complexity by avoiding switch cases for constant-return strategies.
var strategyReasons = map[Strategy]string{
	UseBoth:                   "adaptive strategy (medium complexity pattern)",
	UseReverseAnchored:        "reverse search for end-anchored pattern (O(m) instead of O(n*m))",
	UseReverseSuffix:          "suffix literal prefilter + reverse DFA (10-100x for patterns like .*\\.txt)",
	UseOnePass:                "one-pass DFA for anchored pattern with captures (10-20x over PikeVM)",
	UseReverseInner:           "inner literal prefilter + bidirectional DFA (10-100x for patterns like ERROR.*connection.*timeout)",
	UseTeddy:                  "Teddy multi-pattern prefilter for exact literal alternation (50-250x by skipping DFA)",
	UseReverseSuffixSet:       "Teddy multi-suffix prefilter for suffix alternation (5-10x for patterns like .*\\.(txt|log|md))",
	UseCharClassSearcher:      "specialized lookup-table searcher for char_class+ patterns (14-17x faster than BoundedBacktracker)",
	UseCompositeSearcher:      "sequential lookup tables for concatenated char classes (5-6x faster than BoundedBacktracker)",
	UseBranchDispatch:         "O(1) first-byte dispatch for anchored alternations (2-3x faster on match, 10x+ on no-match)",
	UseDigitPrefilter:         "SIMD digit scanner for digit-lead alternation patterns (5-10x for IP address patterns)",
	UseAhoCorasick:            "Aho-Corasick automaton for large literal alternations (50-500x for >32 pattern sets)",
	UseAnchoredLiteral:        "O(1) specialized matching for ^prefix.*suffix$ patterns (50-90x faster than stdlib)",
	UseMultilineReverseSuffix: "line-aware suffix prefilter for multiline patterns (5-20x for (?m)^.*\\.php patterns)",
}

// StrategyReason provides a human-readable explanation for strategy selection.
//
// This is useful for debugging and performance tuning.
//
// Example:
//
//	strategy := meta.SelectStrategy(nfa, literals, config)
//	reason := meta.StrategyReason(strategy, nfa, literals, config)
//	log.Printf("Using %s: %s", strategy, reason)
func StrategyReason(strategy Strategy, n *nfa.NFA, literals *literal.Seq, config Config) string {
	// Check simple strategies first (constant-return)
	if reason, ok := strategyReasons[strategy]; ok {
		return reason
	}

	// Handle strategies that need context-dependent reasons
	return strategyReasonComplex(strategy, n, literals, config)
}

// strategyReasonComplex handles strategies with context-dependent reason strings.
// This is a helper function to reduce cyclomatic complexity in StrategyReason.
func strategyReasonComplex(strategy Strategy, n *nfa.NFA, literals *literal.Seq, config Config) string {
	nfaSize := n.States()

	switch strategy {
	case UseNFA:
		if !config.EnableDFA {
			return "DFA disabled in configuration"
		}
		if nfaSize < 20 {
			return "tiny NFA (< 20 states), DFA overhead not worth it"
		}
		return "no good literals and small NFA"

	case UseDFA:
		if literals != nil && !literals.IsEmpty() {
			lcp := literals.LongestCommonPrefix()
			if len(lcp) >= config.MinLiteralLen {
				return "good literals available for prefilter + DFA"
			}
		}
		if nfaSize > 100 {
			return "large NFA (> 100 states), DFA essential"
		}
		return "DFA selected for performance"

	case UseBoundedBacktracker:
		if n.IsAlwaysAnchored() {
			return "bounded backtracker for start-anchored pattern (Rust approach: skip DFA for single-position check)"
		}
		return "bounded backtracker for simple character class pattern (2-4x faster than PikeVM)"

	default:
		return "unknown strategy"
	}
}
