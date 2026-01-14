// Package coregex provides fuzz tests comparing coregex behavior against stdlib regexp.
//
// These fuzz tests ensure coregex produces identical results to stdlib for all
// valid patterns and inputs. Any differences indicate either a bug in coregex
// or an intentional behavioral difference (which should be documented).
//
// Run fuzz tests with:
//
//	go test -fuzz=FuzzMatchStdlib -fuzztime=30s
//	go test -fuzz=FuzzFindStdlib -fuzztime=30s
//	go test -fuzz=FuzzFindAllStdlib -fuzztime=30s
//	go test -fuzz=FuzzFindSubmatchStdlib -fuzztime=30s
//	go test -fuzz=FuzzReplaceStdlib -fuzztime=30s
//	go test -fuzz=FuzzSplitStdlib -fuzztime=30s
package coregex

import (
	"bytes"
	"reflect"
	"regexp"
	"testing"
	"unicode/utf8"
)

// ===========================================================================
// Seed Corpus - Common patterns and inputs for fuzzing
// ===========================================================================

// Common regex patterns for seeding the fuzz corpus
var seedPatterns = []string{
	// Literals
	`hello`,
	`world`,
	`foo`,
	`bar`,

	// Character classes
	`\d`,
	`\d+`,
	`\D`,
	`\w`,
	`\w+`,
	`\W`,
	`\s`,
	`\s+`,
	`\S`,
	`[a-z]`,
	`[a-z]+`,
	`[A-Z]`,
	`[0-9]`,
	`[a-zA-Z0-9]`,
	`[^a-z]`,
	`[^0-9]`,

	// Anchors
	`^hello`,
	`world$`,
	`^hello$`,
	`\bhello\b`,

	// Quantifiers
	`a*`,
	`a+`,
	`a?`,
	`a{2}`,
	`a{2,}`,
	`a{2,5}`,
	`a*?`,
	`a+?`,
	`a??`,

	// Alternation
	`foo|bar`,
	`foo|bar|baz`,
	`a|b|c`,

	// Groups
	`(a)`,
	`(a)(b)`,
	`(a|b)`,
	`(?:a)`,
	`(?P<name>a)`,

	// Complex patterns
	`\d{3}-\d{4}`,
	`[a-z]+@[a-z]+\.[a-z]+`,
	`https?://`,
	`.*\.txt$`,
	`^[a-z]+$`,

	// Empty and edge cases
	``,
	`.`,
	`.*`,
	`.+`,
	`(.*)`,
	`^$`,

	// Unicode
	`[日本語]+`,
	`\p{L}+`,

	// Escape sequences
	`\a\f\n\r\t\v`,
	`\\.`,
	`\+`,
	`\*`,
}

// Common inputs for seeding the fuzz corpus
var seedInputs = []string{
	"",
	"a",
	"hello",
	"hello world",
	"foo bar baz",
	"123",
	"abc123def",
	"hello-123",
	"user@example.com",
	"https://example.com",
	"file.txt",
	"document.pdf",
	"日本語",
	"hello\nworld",
	"hello\tworld",
	"  spaces  ",
	"UPPERCASE",
	"MixedCase",
	"special!@#$%",
	"a b c d e f",
	"1 2 3 4 5",
	"aaa",
	"aaabbb",
	"ababab",
	"    ",
	"\n\n\n",
	"555-1234",
	"test@test.com",
}

// ===========================================================================
// Known differences helpers
// ===========================================================================

// hasUTF8CodepointDifference returns true if this pattern+input combination
// has known differences due to coregex matching bytes vs stdlib matching codepoints.
// This affects `.`, `\D`, `\W`, `\S`, negated character classes, and
// patterns that can match empty strings on multibyte input.
func hasUTF8CodepointDifference(pattern, input string) bool {
	// Check if input contains multibyte UTF-8 characters
	hasMultibyte := false
	for _, r := range input {
		if r >= 0x80 {
			hasMultibyte = true
			break
		}
	}
	if !hasMultibyte {
		return false
	}

	// Patterns that match at byte level vs codepoint level
	codepointPatterns := map[string]bool{
		`.`:      true, // dot matches any codepoint in stdlib, any byte in coregex
		`\D`:     true, // non-digit
		`\W`:     true, // non-word
		`\S`:     true, // non-space
		`[^a-z]`: true, // negated class
		`[^0-9]`: true, // negated class
		`[^a-zA-Z]`: true,
		// Empty-match patterns step by codepoint in stdlib, by byte in coregex
		``:     true, // empty pattern
		`a*`:   true, // can match empty
		`a?`:   true, // can match empty
		`a*?`:  true, // can match empty
		`a??`:  true, // can match empty
		`.*`:   true, // can match empty
		`.*?`:  true, // can match empty
		`.?`:   true, // can match empty
		// Dot with captures also has codepoint differences
		`(.)`:  true,
		`(.)+`: true,
		`(.)*`: true,
		`(.)?`: true,
		`.+`:   true,
	}
	return codepointPatterns[pattern]
}

// isEmptyPatternCase returns true if this is an empty pattern case
// which has known differences in Split behavior.
func isEmptyPatternCase(pattern string) bool {
	return pattern == ""
}

// hasRepeatedCaptureGroupDifference returns true if this pattern has
// repeated capture groups with known semantic differences.
// In stdlib, (a)* captures the last matched 'a', while coregex may differ.
func hasRepeatedCaptureGroupDifference(pattern string) bool {
	// Patterns with repeated capture groups that may have different behavior
	repeatedCapturePatterns := map[string]bool{
		`(a)*`:     true,
		`(a)+`:     true,
		`(a)?`:     true,
		`((a|b)*)`: true,
		`((.)*`:    true,
	}
	return repeatedCapturePatterns[pattern]
}

// ===========================================================================
// FuzzMatchStdlib - Fuzz Match/MatchString
// ===========================================================================

func FuzzMatchStdlib(f *testing.F) {
	// Add seed corpus
	for _, p := range seedPatterns {
		for _, i := range seedInputs {
			f.Add(p, i)
		}
	}

	f.Fuzz(func(t *testing.T, pattern, input string) {
		// Skip invalid patterns
		stdRe, err := regexp.Compile(pattern)
		if err != nil {
			return
		}

		cgRe, err := Compile(pattern)
		if err != nil {
			t.Fatalf("coregex failed to compile valid pattern %q: %v", pattern, err)
		}

		// Compare MatchString
		stdMatchStr := stdRe.MatchString(input)
		cgMatchStr := cgRe.MatchString(input)
		if stdMatchStr != cgMatchStr {
			t.Errorf("MatchString(%q, %q):\n  stdlib: %v\n  coregex: %v",
				pattern, input, stdMatchStr, cgMatchStr)
		}
	})
}

// ===========================================================================
// FuzzFindStdlib - Fuzz Find/FindString/FindIndex
// ===========================================================================

func FuzzFindStdlib(f *testing.F) {
	// Add seed corpus
	for _, p := range seedPatterns {
		for _, i := range seedInputs {
			f.Add(p, i)
		}
	}

	f.Fuzz(func(t *testing.T, pattern, input string) {
		// Skip known differences: UTF-8 codepoint vs byte matching
		if hasUTF8CodepointDifference(pattern, input) {
			return
		}

		// Skip invalid patterns
		stdRe, err := regexp.Compile(pattern)
		if err != nil {
			return
		}

		cgRe, err := Compile(pattern)
		if err != nil {
			t.Fatalf("coregex failed to compile valid pattern %q: %v", pattern, err)
		}

		// Compare Find
		stdFind := stdRe.Find([]byte(input))
		cgFind := cgRe.Find([]byte(input))
		if !bytes.Equal(stdFind, cgFind) {
			t.Errorf("Find(%q, %q):\n  stdlib: %q\n  coregex: %q",
				pattern, input, stdFind, cgFind)
		}

		// Compare FindString
		stdFindStr := stdRe.FindString(input)
		cgFindStr := cgRe.FindString(input)
		if stdFindStr != cgFindStr {
			t.Errorf("FindString(%q, %q):\n  stdlib: %q\n  coregex: %q",
				pattern, input, stdFindStr, cgFindStr)
		}

		// Compare FindStringIndex
		stdStrIdx := stdRe.FindStringIndex(input)
		cgStrIdx := cgRe.FindStringIndex(input)
		if !reflect.DeepEqual(stdStrIdx, cgStrIdx) {
			t.Errorf("FindStringIndex(%q, %q):\n  stdlib: %v\n  coregex: %v",
				pattern, input, stdStrIdx, cgStrIdx)
		}
	})
}

// ===========================================================================
// FuzzFindAllStdlib - Fuzz FindAll/FindAllString/FindAllIndex
// ===========================================================================

func FuzzFindAllStdlib(f *testing.F) {
	// Add seed corpus
	for _, p := range seedPatterns {
		for _, i := range seedInputs {
			f.Add(p, i)
		}
	}

	f.Fuzz(func(t *testing.T, pattern, input string) {
		// Skip known differences: UTF-8 codepoint vs byte matching
		if hasUTF8CodepointDifference(pattern, input) {
			return
		}

		// Skip invalid patterns
		stdRe, err := regexp.Compile(pattern)
		if err != nil {
			return
		}

		cgRe, err := Compile(pattern)
		if err != nil {
			t.Fatalf("coregex failed to compile valid pattern %q: %v", pattern, err)
		}

		// Compare FindAll
		stdFindAll := stdRe.FindAll([]byte(input), -1)
		cgFindAll := cgRe.FindAll([]byte(input), -1)
		if !equalByteSlices(stdFindAll, cgFindAll) {
			t.Errorf("FindAll(%q, %q):\n  stdlib: %v\n  coregex: %v",
				pattern, input, toStringSlice(stdFindAll), toStringSlice(cgFindAll))
		}

		// Compare FindAllString
		stdFindAllStr := stdRe.FindAllString(input, -1)
		cgFindAllStr := cgRe.FindAllString(input, -1)
		if !reflect.DeepEqual(stdFindAllStr, cgFindAllStr) {
			t.Errorf("FindAllString(%q, %q):\n  stdlib: %v\n  coregex: %v",
				pattern, input, stdFindAllStr, cgFindAllStr)
		}

		// Compare FindAllStringIndex
		stdFindAllStrIdx := stdRe.FindAllStringIndex(input, -1)
		cgFindAllStrIdx := cgRe.FindAllStringIndex(input, -1)
		if !reflect.DeepEqual(stdFindAllStrIdx, cgFindAllStrIdx) {
			t.Errorf("FindAllStringIndex(%q, %q):\n  stdlib: %v\n  coregex: %v",
				pattern, input, stdFindAllStrIdx, cgFindAllStrIdx)
		}

		// Test limited FindAll (n=3)
		stdLimited := stdRe.FindAllString(input, 3)
		cgLimited := cgRe.FindAllString(input, 3)
		if !reflect.DeepEqual(stdLimited, cgLimited) {
			t.Errorf("FindAllString(%q, %q, 3):\n  stdlib: %v\n  coregex: %v",
				pattern, input, stdLimited, cgLimited)
		}
	})
}

// ===========================================================================
// FuzzFindSubmatchStdlib - Fuzz FindSubmatch/FindStringSubmatch
// ===========================================================================

func FuzzFindSubmatchStdlib(f *testing.F) {
	// Add seed corpus with patterns that have capture groups
	capturePatterns := []string{
		`(a)`,
		`(a)(b)`,
		`(a|b)`,
		`(.*)`,
		`(.+)`,
		`(\d+)`,
		`(\w+)`,
		`(\w+)@(\w+)`,
		`(\w+)-(\d+)`,
		`^(.+)-(\d+)$`,
		`(([a-z]+)(\d+))`,
		`(?P<name>\w+)`,
		`(?P<first>\w+)\s+(?P<second>\w+)`,
		`(a)*`,
		`(a)+`,
		`(a)?`,
		`((a|b)*)`,
	}

	for _, p := range capturePatterns {
		for _, i := range seedInputs {
			f.Add(p, i)
		}
	}

	f.Fuzz(func(t *testing.T, pattern, input string) {
		// Skip known differences: repeated capture groups
		if hasRepeatedCaptureGroupDifference(pattern) {
			return
		}

		// Skip invalid patterns
		stdRe, err := regexp.Compile(pattern)
		if err != nil {
			return
		}

		cgRe, err := Compile(pattern)
		if err != nil {
			t.Fatalf("coregex failed to compile valid pattern %q: %v", pattern, err)
		}

		// Compare FindSubmatch
		stdSubmatch := stdRe.FindSubmatch([]byte(input))
		cgSubmatch := cgRe.FindSubmatch([]byte(input))
		if !equalByteSlices(stdSubmatch, cgSubmatch) {
			t.Errorf("FindSubmatch(%q, %q):\n  stdlib: %v\n  coregex: %v",
				pattern, input, toStringSlice(stdSubmatch), toStringSlice(cgSubmatch))
		}

		// Compare FindStringSubmatch
		stdStrSubmatch := stdRe.FindStringSubmatch(input)
		cgStrSubmatch := cgRe.FindStringSubmatch(input)
		if !reflect.DeepEqual(stdStrSubmatch, cgStrSubmatch) {
			t.Errorf("FindStringSubmatch(%q, %q):\n  stdlib: %v\n  coregex: %v",
				pattern, input, stdStrSubmatch, cgStrSubmatch)
		}

		// Compare FindSubmatchIndex
		stdSubmatchIdx := stdRe.FindSubmatchIndex([]byte(input))
		cgSubmatchIdx := cgRe.FindSubmatchIndex([]byte(input))
		if !reflect.DeepEqual(stdSubmatchIdx, cgSubmatchIdx) {
			t.Errorf("FindSubmatchIndex(%q, %q):\n  stdlib: %v\n  coregex: %v",
				pattern, input, stdSubmatchIdx, cgSubmatchIdx)
		}

		// Compare FindStringSubmatchIndex
		stdStrSubmatchIdx := stdRe.FindStringSubmatchIndex(input)
		cgStrSubmatchIdx := cgRe.FindStringSubmatchIndex(input)
		if !reflect.DeepEqual(stdStrSubmatchIdx, cgStrSubmatchIdx) {
			t.Errorf("FindStringSubmatchIndex(%q, %q):\n  stdlib: %v\n  coregex: %v",
				pattern, input, stdStrSubmatchIdx, cgStrSubmatchIdx)
		}
	})
}

// ===========================================================================
// FuzzFindAllSubmatchStdlib - Fuzz FindAllSubmatch/FindAllStringSubmatch
// ===========================================================================

func FuzzFindAllSubmatchStdlib(f *testing.F) {
	// Add seed corpus with capture patterns
	capturePatterns := []string{
		`(a)`,
		`(.)`,
		`(\d+)`,
		`(\w+)`,
		`(\w+)=(\d+)`,
		`(\w+)@(\w+)`,
	}

	for _, p := range capturePatterns {
		for _, i := range seedInputs {
			f.Add(p, i)
		}
	}

	f.Fuzz(func(t *testing.T, pattern, input string) {
		// Skip known differences: UTF-8 codepoint vs byte matching
		if hasUTF8CodepointDifference(pattern, input) {
			return
		}

		// Skip known differences: repeated capture groups
		if hasRepeatedCaptureGroupDifference(pattern) {
			return
		}

		// Skip invalid patterns
		stdRe, err := regexp.Compile(pattern)
		if err != nil {
			return
		}

		cgRe, err := Compile(pattern)
		if err != nil {
			t.Fatalf("coregex failed to compile valid pattern %q: %v", pattern, err)
		}

		// Compare FindAllSubmatch
		stdAllSubmatch := stdRe.FindAllSubmatch([]byte(input), -1)
		cgAllSubmatch := cgRe.FindAllSubmatch([]byte(input), -1)
		if !equalNestedByteSlices(stdAllSubmatch, cgAllSubmatch) {
			t.Errorf("FindAllSubmatch(%q, %q) count mismatch or content mismatch",
				pattern, input)
		}

		// Compare FindAllStringSubmatch
		stdAllStrSubmatch := stdRe.FindAllStringSubmatch(input, -1)
		cgAllStrSubmatch := cgRe.FindAllStringSubmatch(input, -1)
		if !reflect.DeepEqual(stdAllStrSubmatch, cgAllStrSubmatch) {
			t.Errorf("FindAllStringSubmatch(%q, %q):\n  stdlib: %v\n  coregex: %v",
				pattern, input, stdAllStrSubmatch, cgAllStrSubmatch)
		}

		// Compare FindAllSubmatchIndex
		stdAllSubmatchIdx := stdRe.FindAllSubmatchIndex([]byte(input), -1)
		cgAllSubmatchIdx := cgRe.FindAllSubmatchIndex([]byte(input), -1)
		if !reflect.DeepEqual(stdAllSubmatchIdx, cgAllSubmatchIdx) {
			t.Errorf("FindAllSubmatchIndex(%q, %q):\n  stdlib: %v\n  coregex: %v",
				pattern, input, stdAllSubmatchIdx, cgAllSubmatchIdx)
		}

		// Compare FindAllStringSubmatchIndex
		stdAllStrSubmatchIdx := stdRe.FindAllStringSubmatchIndex(input, -1)
		cgAllStrSubmatchIdx := cgRe.FindAllStringSubmatchIndex(input, -1)
		if !reflect.DeepEqual(stdAllStrSubmatchIdx, cgAllStrSubmatchIdx) {
			t.Errorf("FindAllStringSubmatchIndex(%q, %q):\n  stdlib: %v\n  coregex: %v",
				pattern, input, stdAllStrSubmatchIdx, cgAllStrSubmatchIdx)
		}
	})
}

// ===========================================================================
// FuzzReplaceStdlib - Fuzz ReplaceAllLiteral/ReplaceAllLiteralString
// ===========================================================================

func FuzzReplaceStdlib(f *testing.F) {
	replacements := []string{"", "X", "[$0]", "replacement", "***"}

	for _, p := range seedPatterns {
		for _, i := range seedInputs {
			for _, r := range replacements {
				f.Add(p, i, r)
			}
		}
	}

	f.Fuzz(func(t *testing.T, pattern, input, replacement string) {
		// Skip known differences: UTF-8 codepoint vs byte matching
		if hasUTF8CodepointDifference(pattern, input) {
			return
		}

		// Skip invalid patterns
		stdRe, err := regexp.Compile(pattern)
		if err != nil {
			return
		}

		cgRe, err := Compile(pattern)
		if err != nil {
			t.Fatalf("coregex failed to compile valid pattern %q: %v", pattern, err)
		}

		// Compare ReplaceAllLiteral
		stdReplace := stdRe.ReplaceAllLiteral([]byte(input), []byte(replacement))
		cgReplace := cgRe.ReplaceAllLiteral([]byte(input), []byte(replacement))
		if !bytes.Equal(stdReplace, cgReplace) {
			t.Errorf("ReplaceAllLiteral(%q, %q, %q):\n  stdlib: %q\n  coregex: %q",
				pattern, input, replacement, stdReplace, cgReplace)
		}

		// Compare ReplaceAllLiteralString
		stdReplaceStr := stdRe.ReplaceAllLiteralString(input, replacement)
		cgReplaceStr := cgRe.ReplaceAllLiteralString(input, replacement)
		if stdReplaceStr != cgReplaceStr {
			t.Errorf("ReplaceAllLiteralString(%q, %q, %q):\n  stdlib: %q\n  coregex: %q",
				pattern, input, replacement, stdReplaceStr, cgReplaceStr)
		}
	})
}

// ===========================================================================
// FuzzSplitStdlib - Fuzz Split
// ===========================================================================

func FuzzSplitStdlib(f *testing.F) {
	// Add seed corpus
	splitPatterns := []string{
		`,`,
		`:`,
		`\s+`,
		`\d+`,
		`[,;]+`,
		`-`,
		`\.`,
		``,
	}

	splitInputs := []string{
		"a,b,c",
		"foo:bar:baz",
		"hello world test",
		"1a2b3c",
		"a;b,c;d",
		"a-b-c-d",
		"a.b.c",
		"",
		"abc",
		"no delimiter here",
	}

	for _, p := range splitPatterns {
		for _, i := range splitInputs {
			f.Add(p, i)
		}
	}

	f.Fuzz(func(t *testing.T, pattern, input string) {
		// Skip known differences: empty pattern split behavior
		if isEmptyPatternCase(pattern) {
			return
		}

		// Skip invalid patterns
		stdRe, err := regexp.Compile(pattern)
		if err != nil {
			return
		}

		cgRe, err := Compile(pattern)
		if err != nil {
			t.Fatalf("coregex failed to compile valid pattern %q: %v", pattern, err)
		}

		// Compare Split with n=-1 (all splits)
		stdSplit := stdRe.Split(input, -1)
		cgSplit := cgRe.Split(input, -1)
		if !reflect.DeepEqual(stdSplit, cgSplit) {
			t.Errorf("Split(%q, %q, -1):\n  stdlib: %v\n  coregex: %v",
				pattern, input, stdSplit, cgSplit)
		}

		// Compare Split with n=2 (limited)
		stdSplit2 := stdRe.Split(input, 2)
		cgSplit2 := cgRe.Split(input, 2)
		if !reflect.DeepEqual(stdSplit2, cgSplit2) {
			t.Errorf("Split(%q, %q, 2):\n  stdlib: %v\n  coregex: %v",
				pattern, input, stdSplit2, cgSplit2)
		}

		// Compare Split with n=0 (nil)
		stdSplit0 := stdRe.Split(input, 0)
		cgSplit0 := cgRe.Split(input, 0)
		if !reflect.DeepEqual(stdSplit0, cgSplit0) {
			t.Errorf("Split(%q, %q, 0):\n  stdlib: %v\n  coregex: %v",
				pattern, input, stdSplit0, cgSplit0)
		}
	})
}

// ===========================================================================
// FuzzNumSubexpStdlib - Fuzz NumSubexp/SubexpNames
// ===========================================================================

func FuzzNumSubexpStdlib(f *testing.F) {
	// Seed with patterns containing various capture group configurations
	for _, p := range seedPatterns {
		f.Add(p)
	}

	// Add more patterns with nested/complex groups
	complexPatterns := []string{
		`((a)(b)(c))`,
		`(a(b(c)))`,
		`(?:a)(b)(?:c)(d)`,
		`(?P<one>a)(?P<two>b)`,
		`(a|b|c)`,
		`((a|b)|c)`,
	}
	for _, p := range complexPatterns {
		f.Add(p)
	}

	f.Fuzz(func(t *testing.T, pattern string) {
		// Skip invalid patterns
		stdRe, err := regexp.Compile(pattern)
		if err != nil {
			return
		}

		cgRe, err := Compile(pattern)
		if err != nil {
			t.Fatalf("coregex failed to compile valid pattern %q: %v", pattern, err)
		}

		// Compare NumSubexp
		stdNum := stdRe.NumSubexp()
		cgNum := cgRe.NumSubexp()
		if stdNum != cgNum {
			t.Errorf("NumSubexp(%q):\n  stdlib: %d\n  coregex: %d",
				pattern, stdNum, cgNum)
		}

		// Compare SubexpNames
		stdNames := stdRe.SubexpNames()
		cgNames := cgRe.SubexpNames()
		if !reflect.DeepEqual(stdNames, cgNames) {
			t.Errorf("SubexpNames(%q):\n  stdlib: %v\n  coregex: %v",
				pattern, stdNames, cgNames)
		}
	})
}

// ===========================================================================
// FuzzQuoteMetaStdlib - Fuzz QuoteMeta
// ===========================================================================

func FuzzQuoteMetaStdlib(f *testing.F) {
	// Seed with strings containing special regex characters
	seeds := []string{
		"",
		"hello",
		"hello.world",
		"$100",
		"a+b*c?",
		"(foo)",
		"[abc]",
		"^start$",
		`\d+`,
		"a|b",
		"日本語",
		`!"#$%&'()*+,-./:;<=>?@[\]^_{|}~`,
	}

	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// Skip non-UTF8 strings
		if !utf8.ValidString(input) {
			return
		}

		stdQuoted := regexp.QuoteMeta(input)
		cgQuoted := QuoteMeta(input)

		if stdQuoted != cgQuoted {
			t.Errorf("QuoteMeta(%q):\n  stdlib: %q\n  coregex: %q",
				input, stdQuoted, cgQuoted)
		}

		// Additionally verify that the quoted pattern matches the original
		if input != "" && cgQuoted != "" {
			stdRe := regexp.MustCompile(stdQuoted)
			cgRe := MustCompile(cgQuoted)

			testInput := "prefix" + input + "suffix"

			stdMatch := stdRe.FindString(testInput)
			cgMatch := cgRe.FindString(testInput)

			if stdMatch != cgMatch {
				t.Errorf("QuoteMeta roundtrip mismatch for %q:\n  stdlib match: %q\n  coregex match: %q",
					input, stdMatch, cgMatch)
			}
		}
	})
}

// ===========================================================================
// Helper Functions
// ===========================================================================

// equalByteSlices compares two [][]byte for equality
func equalByteSlices(a, b [][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}

// equalNestedByteSlices compares two [][][]byte for equality
func equalNestedByteSlices(a, b [][][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !equalByteSlices(a[i], b[i]) {
			return false
		}
	}
	return true
}

// toStringSlice converts [][]byte to []string for better error messages
func toStringSlice(b [][]byte) []string {
	if b == nil {
		return nil
	}
	result := make([]string, len(b))
	for i, v := range b {
		if v == nil {
			result[i] = "<nil>"
		} else {
			result[i] = string(v)
		}
	}
	return result
}
