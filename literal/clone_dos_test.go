package literal

import (
	"regexp/syntax"
	"testing"
	"time"
)

// TestCloneRegexpNoExponentialBlowup verifies that cloneRegexp does not
// exhibit exponential growth on patterns where Sub aliases Sub0[:1].
// Bug: cloneRegexp cloned both Sub and Sub0, doubling each node with one child.
// Pattern `.(?:ac*d|ac)xy*` (15 chars) caused >3GB heap, >15s compile.
func TestCloneRegexpNoExponentialBlowup(t *testing.T) {
	patterns := []string{
		`.(?:ac*d|ac)xy*`,
		`.(?:ab*c|ab)de*`,
		`(?:a+b|a)c*d(?:e+f|e)`,
	}

	for _, p := range patterns {
		t.Run(p, func(t *testing.T) {
			re, err := syntax.Parse(p, syntax.Perl)
			if err != nil {
				t.Fatalf("Parse(%q): %v", p, err)
			}
			re = re.Simplify()

			start := time.Now()
			clone := cloneRegexp(re)
			elapsed := time.Since(start)

			if elapsed > 100*time.Millisecond {
				t.Errorf("cloneRegexp(%q) took %v — exponential blowup", p, elapsed)
			}
			if clone == nil {
				t.Error("cloneRegexp returned nil")
			}
		})
	}
}

// TestExtractPrefixesNoDoS tests the full extraction path with the DoS pattern.
func TestExtractPrefixesNoDoS(t *testing.T) {
	patterns := []string{
		`.(?:ac*d|ac)xy*`,
		`.(?:ab*c|ab)de*`,
	}

	for _, p := range patterns {
		t.Run(p, func(t *testing.T) {
			re, err := syntax.Parse(p, syntax.Perl)
			if err != nil {
				t.Fatalf("Parse(%q): %v", p, err)
			}
			re = re.Simplify()

			e := New(DefaultConfig())
			start := time.Now()
			_ = e.ExtractPrefixes(re)
			elapsed := time.Since(start)

			if elapsed > 500*time.Millisecond {
				t.Errorf("ExtractPrefixes(%q) took %v", p, elapsed)
			}
		})
	}
}
