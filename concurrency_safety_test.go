package coregex

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// A compiled *Regex must be safe for concurrent use by multiple goroutines
// (README, godoc). These tests exercise every public API on one shared
// compiled Regex from many goroutines and compare each result with stdlib.
//
// Each case is first checked sequentially against stdlib (fresh and reused
// Regex). Only cases that pass that precondition are run concurrently, so a
// divergence under concurrency can only come from shared mutable search state,
// not from an unrelated matching bug.

type concurrencyAPI struct {
	name string
	co   func(*Regex, string) string
	std  func(*regexp.Regexp, string) string
}

func concurrencyAPIs() []concurrencyAPI {
	upper := strings.ToUpper
	return []concurrencyAPI{
		{"MatchString",
			func(r *Regex, s string) string { return fmt.Sprint(r.MatchString(s)) },
			func(r *regexp.Regexp, s string) string { return fmt.Sprint(r.MatchString(s)) }},
		{"FindStringIndex",
			func(r *Regex, s string) string { return fmt.Sprint(r.FindStringIndex(s)) },
			func(r *regexp.Regexp, s string) string { return fmt.Sprint(r.FindStringIndex(s)) }},
		{"FindString",
			func(r *Regex, s string) string { return fmt.Sprintf("%q", r.FindString(s)) },
			func(r *regexp.Regexp, s string) string { return fmt.Sprintf("%q", r.FindString(s)) }},
		{"FindAllStringIndex",
			func(r *Regex, s string) string { return fmt.Sprint(r.FindAllStringIndex(s, -1)) },
			func(r *regexp.Regexp, s string) string { return fmt.Sprint(r.FindAllStringIndex(s, -1)) }},
		{"FindStringSubmatchIndex",
			func(r *Regex, s string) string { return fmt.Sprint(r.FindStringSubmatchIndex(s)) },
			func(r *regexp.Regexp, s string) string { return fmt.Sprint(r.FindStringSubmatchIndex(s)) }},
		{"FindAllStringSubmatchIndex",
			func(r *Regex, s string) string { return fmt.Sprint(r.FindAllStringSubmatchIndex(s, -1)) },
			func(r *regexp.Regexp, s string) string { return fmt.Sprint(r.FindAllStringSubmatchIndex(s, -1)) }},
		{"ReplaceAllString",
			func(r *Regex, s string) string { return fmt.Sprintf("%q", r.ReplaceAllString(s, "<$0>")) },
			func(r *regexp.Regexp, s string) string { return fmt.Sprintf("%q", r.ReplaceAllString(s, "<$0>")) }},
		{"ReplaceAllLiteralString",
			func(r *Regex, s string) string { return fmt.Sprintf("%q", r.ReplaceAllLiteralString(s, "#")) },
			func(r *regexp.Regexp, s string) string { return fmt.Sprintf("%q", r.ReplaceAllLiteralString(s, "#")) }},
		{"ReplaceAllStringFunc",
			func(r *Regex, s string) string { return fmt.Sprintf("%q", r.ReplaceAllStringFunc(s, upper)) },
			func(r *regexp.Regexp, s string) string { return fmt.Sprintf("%q", r.ReplaceAllStringFunc(s, upper)) }},
		{"Split",
			func(r *Regex, s string) string { return fmt.Sprintf("%q", r.Split(s, -1)) },
			func(r *regexp.Regexp, s string) string { return fmt.Sprintf("%q", r.Split(s, -1)) }},
		{"CountString",
			func(r *Regex, s string) string { return fmt.Sprint(r.CountString(s, -1)) },
			func(r *regexp.Regexp, s string) string { return fmt.Sprint(len(r.FindAllStringIndex(s, -1))) }},
		{"AllStringIndex",
			func(r *Regex, s string) string {
				var out [][]int
				for m := range r.AllStringIndex(s) {
					out = append(out, []int{m[0], m[1]})
				}
				return fmt.Sprint(out)
			},
			func(r *regexp.Regexp, s string) string { return fmt.Sprint(r.FindAllStringIndex(s, -1)) }},
	}
}

type concurrencyCase struct {
	name    string // strategy family the case is meant to reach
	pattern string
	inputs  []string
}

func concurrencyCases() []concurrencyCase {
	return []concurrencyCase{
		// DFA strategy, lazy quantifier: no reverse DFA, the Find family used
		// the engine-level PikeVM.
		{"dfa-lazy", `a+?b`, []string{"aaab", "xxaab yy ab", "bbb", "a b ab"}},
		{"dfa-lazy-digits", `x\d+?y`, []string{"x1y", "zz x123y and x9y", "xy x0", "x12 y"}},
		{"dfa-lazy-prefilter", `foo\d+?bar`, []string{"xx foo123bar yy", "foo1bar foo22bar", "no match here", "foofoo9bar"}},
		// Adaptive (UseBoth) strategy; the README example pattern.
		{"both-email", `(\w+)@(\w+)\.(\w+)`, []string{"user1@example.com and x", "a@b.c d@e.f", "none", "z z@q.io"}},
		// Inputs with invalid UTF-8 force the lazy DFA to fall back to its NFA.
		{"dfa-invalid-utf8", `k=[^;]+;`, []string{"k=\xff\xfe; k=ab;", "k=\xc3; x", "k=v;", "none \xff"}},
		{"dfa-dot-invalid-utf8", `id=.+?\.`, []string{"id=\xffab.", "id=x. id=\xc3y.", "id=", "zz id=1.2."}},
		// Anchored pattern with '.', BoundedBacktracker plus its ASCII variant.
		{"bt-anchored-dot", `^(\w+): (.+)$`, []string{"key: value", "a: b c", "nocolon", "k:  v"}},
		{"bt-charclass", `[0-9]+[a-z]+`, []string{"12ab 3c", "x9y", "none", "77zz88"}},
		// NFA strategy with word boundaries (issue #78 Count test pattern).
		{"nfa-word", `\bfoo\b`, []string{"foo bar foo baz foo", "foofoo", "a foo", ""}},
		// Digit prefilter (issue #78 mixed-operations test pattern).
		{"digit-prefilter", `(\d+)-(\d+)-(\d+)`, []string{"Date: 2024-01-15, Code: 123-456-789, ID: 999-888-777", "1-2-3", "none", "12-34"}},
		// Reverse strategies, single-line inputs.
		{"reverse-suffix", `.*\.txt`, []string{"file.txt", "a b.txt", "none", "x.txt.txt"}},
		{"reverse-inner", `.*error.*`, []string{"an error occurred", "noerr", "error", "x error y error z"}},
		{"reverse-suffix-set", `.*\.(?:txt|log)`, []string{"x.log", "y.txt", "none", "a.log b.txt"}},
		{"reverse-anchored", `\d+$`, []string{"abc 123", "456", "none", "7 8 9"}},
		{"multiline-reverse-suffix", `(?m)^/.*\.php`, []string{"/a.php", "/b/c.php", "none", "/x.php"}},
		// Literal and specialized strategies.
		{"teddy", `foo|bar|baz`, []string{"xfooxbarxbaz", "none", "bazbar", "fo ba"}},
		{"char-class", `\w+`, []string{"hello world", "  ", "a_b-c", ""}},
		{"composite", `[a-z]+[0-9]+`, []string{"abc123 de45", "none!", "x1", "99zz"}},
		{"anchored-literal", `^/.*\.php$`, []string{"/a.php", "/a.php5", "x/a.php", "/.php"}},
		// Captures on an anchored one-pass pattern.
		{"onepass", `^(\w+)=(\d+)`, []string{"k=1", "key=123 rest", "=1", "k=x"}},
	}
}

func TestConcurrentRegexMatchesStdlib(t *testing.T) {
	goroutines, rounds := 8, 25
	if testing.Short() {
		rounds = 8
	}
	apis := concurrencyAPIs()

	for _, tc := range concurrencyCases() {
		t.Run(tc.name, func(t *testing.T) {
			std := regexp.MustCompile(tc.pattern)
			want := make([][]string, len(tc.inputs))
			for i, in := range tc.inputs {
				want[i] = make([]string, len(apis))
				for j, api := range apis {
					want[i][j] = api.std(std, in)
				}
			}

			// Precondition: sequential results equal stdlib, with a fresh
			// Regex per call and with one reused Regex.
			reused := MustCompile(tc.pattern)
			for pass := 0; pass < 2; pass++ {
				for i, in := range tc.inputs {
					for j, api := range apis {
						if got := api.co(MustCompile(tc.pattern), in); got != want[i][j] {
							t.Fatalf("precondition (fresh): %s(%q) on %q = %s, stdlib %s", api.name, tc.pattern, in, got, want[i][j])
						}
						if got := api.co(reused, in); got != want[i][j] {
							t.Fatalf("precondition (reused): %s(%q) on %q = %s, stdlib %s", api.name, tc.pattern, in, got, want[i][j])
						}
					}
				}
			}

			shared := MustCompile(tc.pattern)
			var mismatches, panics atomic.Int64
			var firstMu sync.Mutex
			var first string
			record := func(msg string) {
				firstMu.Lock()
				if first == "" {
					first = msg
				}
				firstMu.Unlock()
			}

			var wg sync.WaitGroup
			for g := 0; g < goroutines; g++ {
				wg.Add(1)
				go func(g int) {
					defer wg.Done()
					for r := 0; r < rounds; r++ {
						for k := range tc.inputs {
							i := (k + g) % len(tc.inputs) // vary the order per goroutine
							for j, api := range apis {
								func() {
									defer func() {
										if p := recover(); p != nil {
											panics.Add(1)
											record(fmt.Sprintf("panic in %s on %q: %v", api.name, tc.inputs[i], p))
										}
									}()
									if got := api.co(shared, tc.inputs[i]); got != want[i][j] {
										mismatches.Add(1)
										record(fmt.Sprintf("%s on %q = %s, want %s", api.name, tc.inputs[i], got, want[i][j]))
									}
								}()
							}
						}
					}
				}(g)
			}
			wg.Wait()

			if n, p := mismatches.Load(), panics.Load(); n > 0 || p > 0 {
				t.Errorf("pattern %q under %d goroutines: %d wrong results, %d panics; first: %s",
					tc.pattern, goroutines, n, p, first)
			}
		})
	}
}
