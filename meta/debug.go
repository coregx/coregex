package meta

import (
	"fmt"
	"os"
	"regexp/syntax"
	"strings"

	"github.com/coregx/coregex/literal"
	"github.com/coregx/coregex/prefilter"
)

// debugLevel controls compile-time diagnostic output.
// Set COREGEX_DEBUG=1 for strategy + engine summary,
// COREGEX_DEBUG=2 for literal extraction details.
// Zero cost when disabled — env var checked once at init.
var debugLevel int

func init() {
	switch os.Getenv("COREGEX_DEBUG") {
	case "1":
		debugLevel = 1
	case "2":
		debugLevel = 2
	case "3":
		debugLevel = 3
	}
}

// debugStrategy logs strategy selection details at compile time.
func debugStrategy(pattern string, strategy Strategy, nfaStates int, lits *literal.Seq, reason string) {
	if debugLevel < 1 {
		return
	}
	litCount := 0
	litComplete := false
	if lits != nil {
		litCount = lits.Len()
		litComplete = lits.AllComplete()
	}
	fmt.Fprintf(os.Stderr, "[coregex] pattern=%q strategy=%s nfa_states=%d literals=%d",
		pattern, strategy, nfaStates, litCount)
	if litCount > 0 {
		fmt.Fprintf(os.Stderr, " complete=%v", litComplete)
	}
	if reason != "" {
		fmt.Fprintf(os.Stderr, " reason=%q", reason)
	}
	fmt.Fprintln(os.Stderr)
}

// debugLiterals logs extracted literal sequences at compile time.
func debugLiterals(label string, lits *literal.Seq) {
	if debugLevel < 2 || lits == nil {
		return
	}
	n := lits.Len()
	if n == 0 {
		fmt.Fprintf(os.Stderr, "[coregex] %s: (empty)\n", label)
		return
	}
	var samples []string
	limit := n
	if limit > 8 {
		limit = 8
	}
	for i := 0; i < limit; i++ {
		lit := lits.Get(i)
		samples = append(samples, fmt.Sprintf("%q", string(lit.Bytes)))
	}
	more := ""
	if n > 8 {
		more = fmt.Sprintf(" ... (%d more)", n-8)
	}
	fmt.Fprintf(os.Stderr, "[coregex] %s: [%s%s] complete=%v\n",
		label, strings.Join(samples, ", "), more, lits.AllComplete())
}

// debugSuffixes extracts and logs suffix literals (only at level 2+).
func debugSuffixes(re *syntax.Regexp, config Config, isStartAnchored bool) {
	if debugLevel < 2 || !config.EnablePrefilter || isStartAnchored {
		return
	}
	extractor := literal.New(literal.ExtractorConfig{
		MaxLiterals:   config.MaxLiterals,
		MaxLiteralLen: 64,
		MaxClassSize:  10,
	})
	suffixes := extractor.ExtractSuffixes(re)
	debugLiterals("suffixes", suffixes)
}

// debugPrefilter logs prefilter type selection at compile time.
func debugPrefilter(pf prefilter.Prefilter) {
	if debugLevel < 1 || pf == nil {
		return
	}
	// Determine prefilter type name from concrete type
	name := fmt.Sprintf("%T", pf)
	switch pf.(type) {
	case *prefilter.Teddy:
		name = "Teddy (SSSE3 slim)"
	case *prefilter.FatTeddy:
		name = "FatTeddy (AVX2 fat)"
	case *prefilter.AhoCorasickPrefilter:
		name = "AhoCorasick (DFA)"
	}
	// Extract short name from *prefilter.memchrPrefilter → memchr
	if strings.Contains(name, "memchr") {
		name = "memchr"
	} else if strings.Contains(name, "memmem") {
		name = "memmem"
	}
	fmt.Fprintf(os.Stderr, "[coregex] prefilter=%s complete=%v\n", name, pf.IsComplete())
}

// debugEngine logs which engines were built or skipped.
func debugEngine(name string, built bool, reason string) {
	if debugLevel < 1 {
		return
	}
	if built {
		fmt.Fprintf(os.Stderr, "[coregex] %s built\n", name)
	} else if reason != "" {
		fmt.Fprintf(os.Stderr, "[coregex] skipping %s: %s\n", name, reason)
	}
}
