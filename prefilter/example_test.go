package prefilter_test

import (
	"fmt"
	"regexp/syntax"

	"github.com/coregx/coregex/literal"
	"github.com/coregx/coregex/prefilter"
)

// ExampleBuilder demonstrates building a prefilter from a regex pattern.
func ExampleBuilder() {
	// Parse the regex pattern
	re, _ := syntax.Parse("hello", syntax.Perl)

	// Extract prefix literals
	extractor := literal.New(literal.DefaultConfig())
	prefixes := extractor.ExtractPrefixes(re)

	// Build the prefilter
	builder := prefilter.NewBuilder(prefixes, nil)
	pf := builder.Build()

	if pf != nil {
		// Use the prefilter to find candidates
		haystack := []byte("foo hello world")
		pos := pf.Find(haystack, 0)
		fmt.Printf("Found candidate at position %d\n", pos)
	}

	// Output:
	// Found candidate at position 4
}

// ExampleBuilder_singleByte demonstrates prefilter selection for single byte patterns.
func ExampleBuilder_singleByte() {
	// Pattern with single byte literal
	re, _ := syntax.Parse("[a].*", syntax.Perl)

	extractor := literal.New(literal.DefaultConfig())
	prefixes := extractor.ExtractPrefixes(re)

	builder := prefilter.NewBuilder(prefixes, nil)
	pf := builder.Build()

	// Should select MemchrPrefilter for single byte
	haystack := []byte("xxxayyy")
	pos := pf.Find(haystack, 0)
	fmt.Printf("Found 'a' at position %d\n", pos)
	fmt.Printf("Heap usage: %d bytes\n", pf.HeapBytes())

	// Output:
	// Found 'a' at position 3
	// Heap usage: 0 bytes
}

// ExampleBuilder_substring demonstrates prefilter selection for substring patterns.
func ExampleBuilder_substring() {
	// Pattern with substring literal
	re, _ := syntax.Parse("pattern.*", syntax.Perl)

	extractor := literal.New(literal.DefaultConfig())
	prefixes := extractor.ExtractPrefixes(re)

	builder := prefilter.NewBuilder(prefixes, nil)
	pf := builder.Build()

	// Should select MemmemPrefilter for substring
	haystack := []byte("test pattern matching")
	pos := pf.Find(haystack, 0)
	fmt.Printf("Found 'pattern' at position %d\n", pos)
	fmt.Printf("Heap usage: %d bytes\n", pf.HeapBytes())

	// Output:
	// Found 'pattern' at position 5
	// Heap usage: 7 bytes
}

// ExampleBuilder_noPrefilter demonstrates patterns with no available prefilter.
func ExampleBuilder_noPrefilter() {
	// Pattern with no extractable literals (wildcard)
	re, _ := syntax.Parse(".*", syntax.Perl)

	extractor := literal.New(literal.DefaultConfig())
	prefixes := extractor.ExtractPrefixes(re)

	builder := prefilter.NewBuilder(prefixes, nil)
	pf := builder.Build()

	if pf == nil {
		fmt.Println("No prefilter available, must use full regex engine")
	}

	// Output:
	// No prefilter available, must use full regex engine
}

// ExampleBuilder_alternation demonstrates prefilter with alternations.
func ExampleBuilder_alternation() {
	// Pattern with alternation - Go parser may factorize common prefixes
	re, _ := syntax.Parse("(foo|foobar|food)", syntax.Perl)

	extractor := literal.New(literal.DefaultConfig())
	prefixes := extractor.ExtractPrefixes(re)

	// The Go parser factorizes to foo[dbr] or similar
	// After minimization, should extract "foo"
	builder := prefilter.NewBuilder(prefixes, nil)
	pf := builder.Build()

	if pf != nil {
		haystack := []byte("test foobar end")
		pos := pf.Find(haystack, 0)
		fmt.Printf("Found candidate at position %d\n", pos)
		fmt.Printf("Complete match: %v\n", pf.IsComplete())
	}

	// Output:
	// Found candidate at position 5
	// Complete match: false
}

// ExampleBuilder_withSuffixes demonstrates using suffixes when prefixes are empty.
func ExampleBuilder_withSuffixes() {
	// Pattern with suffix but no prefix
	re, _ := syntax.Parse(".*world", syntax.Perl)

	extractor := literal.New(literal.DefaultConfig())
	prefixes := extractor.ExtractPrefixes(re) // Will be empty
	suffixes := extractor.ExtractSuffixes(re) // Will have "world"

	// Builder will use suffixes when prefixes are empty
	builder := prefilter.NewBuilder(prefixes, suffixes)
	pf := builder.Build()

	if pf != nil {
		haystack := []byte("hello world")
		pos := pf.Find(haystack, 0)
		fmt.Printf("Found suffix at position %d\n", pos)
	}

	// Output:
	// Found suffix at position 6
}

// ExamplePrefilter_Find demonstrates searching with Find method.
func ExamplePrefilter_Find() {
	// Create a simple pattern
	re, _ := syntax.Parse("test", syntax.Perl)

	extractor := literal.New(literal.DefaultConfig())
	prefixes := extractor.ExtractPrefixes(re)

	builder := prefilter.NewBuilder(prefixes, nil)
	pf := builder.Build()

	haystack := []byte("first test, second test, third test")

	// Find all occurrences
	start := 0
	count := 0
	for {
		pos := pf.Find(haystack, start)
		if pos == -1 {
			break
		}
		count++
		fmt.Printf("Match %d at position %d\n", count, pos)
		start = pos + 1 // Move past this match
	}

	// Output:
	// Match 1 at position 6
	// Match 2 at position 19
	// Match 3 at position 31
}

// ExamplePrefilter_IsComplete demonstrates checking completeness.
func ExamplePrefilter_IsComplete() {
	// Complete pattern (exact literal)
	reComplete, _ := syntax.Parse("exact", syntax.Perl)
	extractorComplete := literal.New(literal.DefaultConfig())
	prefixesComplete := extractorComplete.ExtractPrefixes(reComplete)
	pfComplete := prefilter.NewBuilder(prefixesComplete, nil).Build()

	// Incomplete pattern (literal with wildcard)
	reIncomplete, _ := syntax.Parse("prefix.*", syntax.Perl)
	extractorIncomplete := literal.New(literal.DefaultConfig())
	prefixesIncomplete := extractorIncomplete.ExtractPrefixes(reIncomplete)
	pfIncomplete := prefilter.NewBuilder(prefixesIncomplete, nil).Build()

	fmt.Printf("Complete pattern needs verification: %v\n", !pfComplete.IsComplete())
	fmt.Printf("Incomplete pattern needs verification: %v\n", !pfIncomplete.IsComplete())

	// Output:
	// Complete pattern needs verification: false
	// Incomplete pattern needs verification: true
}
