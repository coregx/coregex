package coregex

import (
	"fmt"
	"math/rand"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// utf8BoundaryRunes are the code points where the UTF-8 encoding changes
// length or where a continuation byte wraps, plus neighbors. A class that
// is compiled to byte ranges imprecisely tends to fail at exactly these.
var utf8BoundaryRunes = []rune{
	0x7F, 0x80, 0x7FF, 0x800, 0xFFF, 0x1000, 0xD7FF, 0xE000, 0xFFFD, 0xFFFF,
	0x10000, 0x1003F, 0x10040, 0x10FFF, 0x11000, 0x1F600, 0x1FFFF, 0x20000,
	0x2A6DF, 0x2A6E0, 0x3FFFF, 0x40000, 0xFFFFF, 0x100000, 0x10FFFE, 0x10FFFF,
}

// tableProbes returns, for every range of tab, the code points lo-1, lo,
// lo+1, hi-1, hi and hi+1 (valid scalar values only), plus the UTF-8
// boundary runes. Ranges with a stride are probed at lo, lo+stride and hi.
func tableProbes(tab *unicode.RangeTable) []rune {
	set := make(map[rune]struct{})
	add := func(r rune) {
		if r >= 0 && r <= unicode.MaxRune && utf8.ValidRune(r) {
			set[r] = struct{}{}
		}
	}
	for _, r := range tab.R16 {
		lo, hi, s := rune(r.Lo), rune(r.Hi), rune(r.Stride)
		for _, p := range []rune{lo - 1, lo, lo + 1, lo + s, hi - 1, hi, hi + 1} {
			add(p)
		}
	}
	for _, r := range tab.R32 {
		lo, hi, s := rune(r.Lo), rune(r.Hi), rune(r.Stride)
		for _, p := range []rune{lo - 1, lo, lo + 1, lo + s, hi - 1, hi, hi + 1} {
			add(p)
		}
	}
	for _, r := range utf8BoundaryRunes {
		add(r)
	}
	out := make([]rune, 0, len(set))
	for r := range set {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// checkClassAgainstStdlib compares MatchString and FindStringIndex on every
// probe rune alone, and FindAllStringIndex on all probes joined by spaces.
func checkClassAgainstStdlib(t *testing.T, pattern string, probes []rune) {
	t.Helper()
	re, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q): %v", pattern, err)
	}
	std := regexp.MustCompile(pattern)

	fails := 0
	for _, r := range probes {
		s := string(r)
		if got, want := re.MatchString(s), std.MatchString(s); got != want {
			t.Errorf("%s MatchString(U+%04X) = %v, stdlib %v", pattern, r, got, want)
			fails++
		}
		if got, want := re.FindStringIndex(s), std.FindStringIndex(s); !reflect.DeepEqual(got, want) {
			t.Errorf("%s FindStringIndex(U+%04X) = %v, stdlib %v", pattern, r, got, want)
			fails++
		}
		if fails >= 10 {
			t.Fatalf("%s: too many failures", pattern)
		}
	}

	var b strings.Builder
	for _, r := range probes {
		b.WriteRune(r)
		b.WriteByte(' ')
	}
	joined := b.String()
	if got, want := re.FindAllStringIndex(joined, -1), std.FindAllStringIndex(joined, -1); !reflect.DeepEqual(got, want) {
		t.Errorf("%s FindAllStringIndex(joined probes): %d matches, stdlib %d", pattern, len(got), len(want))
	}
}

// TestUnicodeClassRangesMatchStdlib checks every Unicode script and category
// class, positive and negated, at the boundaries of each of its ranges.
// Before the fix, 4-byte ranges constrained only the lead byte, so \p{Han}
// matched "😀" and \P{Han} matched "𠀀".
func TestUnicodeClassRangesMatchStdlib(t *testing.T) {
	type named struct {
		name string
		tab  *unicode.RangeTable
	}
	var tables []named
	for name, tab := range unicode.Scripts {
		tables = append(tables, named{name, tab})
	}
	for name, tab := range unicode.Categories {
		tables = append(tables, named{name, tab})
	}
	sort.Slice(tables, func(i, j int) bool { return tables[i].name < tables[j].name })
	if testing.Short() {
		// Scripts and categories with 4-byte ranges, plus 2/3-byte ones.
		keep := map[string]bool{
			"Han": true, "Greek": true, "Cyrillic": true, "Hiragana": true,
			"Latin": true, "Common": true, "Arabic": true, "L": true, "Lu": true,
			"N": true, "So": true, "Co": true, "Cs": true,
		}
		short := tables[:0]
		for _, tb := range tables {
			if keep[tb.name] {
				short = append(short, tb)
			}
		}
		tables = short
	}

	for _, tb := range tables {
		probes := tableProbes(tb.tab)
		for _, pattern := range []string{
			`\p{` + tb.name + `}`,
			`\P{` + tb.name + `}`,
			`[^\p{` + tb.name + `}]`,
			`^\p{` + tb.name + `}+$`,
		} {
			t.Run(pattern, func(t *testing.T) {
				checkClassAgainstStdlib(t, pattern, probes)
			})
		}
	}
}

// TestUTF8FourByteRangesMatchStdlib covers explicit supplementary-plane
// ranges, the reported repros, and random ranges whose bounds sit on
// continuation-byte boundaries.
func TestUTF8FourByteRangesMatchStdlib(t *testing.T) {
	fixed := [][2]rune{
		{0x20000, 0x2A6DF}, // CJK Ext B
		{0x10000, 0x10FFF},
		{0x1F600, 0x1F64F}, // Emoticons
		{0x10000, 0x10FFFF},
		{0x10400, 0x1044F},
		{0x1003F, 0x10040},
		{0x3FFFF, 0x40000},
		{0xFFFFF, 0x100000},
		{0x10FFFF, 0x10FFFF},
		{0x800, 0x10FFFF},
		{0x80, 0x1FFFF},
		{0xE000, 0x10000},
	}
	const random = 40
	ranges := make([][2]rune, 0, len(fixed)+random)
	ranges = append(ranges, fixed...)
	rng := rand.New(rand.NewSource(20261017))
	for range random {
		lo := rune(0x10000 + rng.Intn(0x100000))
		hi := lo + rune(rng.Intn(0x10FFFF-int(lo)+1))
		// Snap some bounds onto continuation-byte boundaries.
		switch rng.Intn(4) {
		case 0:
			lo &^= 0x3F
		case 1:
			hi |= 0x3F
		case 2:
			lo &^= 0xFFF
		}
		if hi > unicode.MaxRune {
			hi = unicode.MaxRune
		}
		ranges = append(ranges, [2]rune{lo, hi})
	}

	for _, r := range ranges {
		lo, hi := r[0], r[1]
		probes := []rune{}
		for _, p := range []rune{lo - 1, lo, lo + 1, (lo + hi) / 2, hi - 1, hi, hi + 1} {
			if p >= 0 && utf8.ValidRune(p) {
				probes = append(probes, p)
			}
		}
		// Probe continuation-byte boundaries just outside and inside the range.
		for _, m := range []rune{0x3F, 0xFFF, 0x3FFFF} {
			for _, p := range []rune{lo &^ m, (lo &^ m) - 1, hi | m, (hi | m) + 1} {
				if p >= 0 && utf8.ValidRune(p) {
					probes = append(probes, p)
				}
			}
		}
		probes = append(probes, utf8BoundaryRunes...)
		for _, pattern := range []string{
			fmt.Sprintf(`[\x{%X}-\x{%X}]`, lo, hi),
			fmt.Sprintf(`[^\x{%X}-\x{%X}]`, lo, hi),
			fmt.Sprintf(`[a\x{%X}-\x{%X}]+`, lo, hi),
		} {
			t.Run(pattern, func(t *testing.T) {
				checkClassAgainstStdlib(t, pattern, probes)
			})
		}
	}
}
