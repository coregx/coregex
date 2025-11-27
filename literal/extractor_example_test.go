package literal_test

import (
	"fmt"
	"regexp/syntax"

	"github.com/coregx/coregex/literal"
)

// ExampleExtractor_ExtractPrefixes demonstrates basic prefix extraction
// from a simple literal pattern.
func ExampleExtractor_ExtractPrefixes() {
	// Parse a simple pattern
	re, _ := syntax.Parse("hello", syntax.Perl)

	// Create extractor with default config
	extractor := literal.New(literal.DefaultConfig())

	// Extract prefixes
	prefixes := extractor.ExtractPrefixes(re)

	// Print results
	fmt.Printf("Found %d prefix(es):\n", prefixes.Len())
	for i := 0; i < prefixes.Len(); i++ {
		lit := prefixes.Get(i)
		fmt.Printf("  - %q\n", string(lit.Bytes))
	}

	// Output:
	// Found 1 prefix(es):
	//   - "hello"
}

// ExampleExtractor_ExtractPrefixes_alternates demonstrates prefix extraction
// from alternation patterns. Note: Go's regex parser may optimize patterns
// by factoring common prefixes (e.g., "bar|baz" becomes "ba[rz]").
func ExampleExtractor_ExtractPrefixes_alternates() {
	// Pattern with alternations (using distinct prefixes to avoid parser optimization)
	re, _ := syntax.Parse("(apple|banana|cherry)", syntax.Perl)

	extractor := literal.New(literal.DefaultConfig())
	prefixes := extractor.ExtractPrefixes(re)

	fmt.Printf("Found %d prefix(es):\n", prefixes.Len())
	for i := 0; i < prefixes.Len(); i++ {
		lit := prefixes.Get(i)
		fmt.Printf("  - %q\n", string(lit.Bytes))
	}

	// Output:
	// Found 3 prefix(es):
	//   - "apple"
	//   - "banana"
	//   - "cherry"
}

// ExampleExtractor_ExtractPrefixes_charClass demonstrates character class
// expansion for small classes.
func ExampleExtractor_ExtractPrefixes_charClass() {
	// Small character class: [abc]
	re, _ := syntax.Parse("[abc]", syntax.Perl)

	extractor := literal.New(literal.DefaultConfig())
	prefixes := extractor.ExtractPrefixes(re)

	fmt.Printf("Found %d prefix(es):\n", prefixes.Len())
	for i := 0; i < prefixes.Len(); i++ {
		lit := prefixes.Get(i)
		fmt.Printf("  - %q\n", string(lit.Bytes))
	}

	// Output:
	// Found 3 prefix(es):
	//   - "a"
	//   - "b"
	//   - "c"
}

// ExampleExtractor_ExtractSuffixes demonstrates suffix extraction
// from a pattern.
func ExampleExtractor_ExtractSuffixes() {
	// Pattern: hello.*world
	// Suffix should be "world"
	re, _ := syntax.Parse("hello.*world", syntax.Perl)

	extractor := literal.New(literal.DefaultConfig())
	suffixes := extractor.ExtractSuffixes(re)

	fmt.Printf("Found %d suffix(es):\n", suffixes.Len())
	for i := 0; i < suffixes.Len(); i++ {
		lit := suffixes.Get(i)
		fmt.Printf("  - %q\n", string(lit.Bytes))
	}

	// Output:
	// Found 1 suffix(es):
	//   - "world"
}

// ExampleExtractor_ExtractInner demonstrates inner literal extraction
// for patterns where literals can appear anywhere.
func ExampleExtractor_ExtractInner() {
	// Pattern: .*error.*
	// Inner literal should be "error"
	re, _ := syntax.Parse(".*error.*", syntax.Perl)

	extractor := literal.New(literal.DefaultConfig())
	inner := extractor.ExtractInner(re)

	fmt.Printf("Found %d inner literal(s):\n", inner.Len())
	for i := 0; i < inner.Len(); i++ {
		lit := inner.Get(i)
		fmt.Printf("  - %q\n", string(lit.Bytes))
	}

	// Output:
	// Found 1 inner literal(s):
	//   - "error"
}

// ExampleExtractorConfig demonstrates configuring extraction limits.
func ExampleExtractorConfig() {
	// Create custom config with stricter limits
	config := literal.DefaultConfig()
	config.MaxLiterals = 2    // Only extract 2 literals max
	config.MaxLiteralLen = 10 // Truncate literals > 10 bytes
	config.MaxClassSize = 3   // Only expand classes with ≤ 3 chars

	extractor := literal.New(config)

	// Pattern with many alternations
	re, _ := syntax.Parse("(one|two|three|four|five)", syntax.Perl)
	prefixes := extractor.ExtractPrefixes(re)

	// Should only get 2 literals due to MaxLiterals=2
	fmt.Printf("Extracted %d literals (limited to %d)\n", prefixes.Len(), config.MaxLiterals)

	// Output:
	// Extracted 2 literals (limited to 2)
}

// ExampleExtractor_ExtractPrefixes_httpMethods shows a real-world use case:
// extracting HTTP method literals for fast prefiltering in log parsers.
// Note: Parser may optimize "POST|PUT|PATCH" to "P(OST|UT|ATCH)".
func ExampleExtractor_ExtractPrefixes_httpMethods() {
	// HTTP method regex (using methods with distinct first letters to avoid parser optimization)
	re, _ := syntax.Parse("(GET|HEAD|DELETE|OPTIONS)", syntax.Perl)

	extractor := literal.New(literal.DefaultConfig())
	prefixes := extractor.ExtractPrefixes(re)

	fmt.Printf("HTTP methods extracted: %d\n", prefixes.Len())
	fmt.Println("Can use these for prefilter optimization:")
	for i := 0; i < prefixes.Len(); i++ {
		lit := prefixes.Get(i)
		fmt.Printf("  - %q\n", string(lit.Bytes))
	}

	// Output:
	// HTTP methods extracted: 4
	// Can use these for prefilter optimization:
	//   - "GET"
	//   - "HEAD"
	//   - "DELETE"
	//   - "OPTIONS"
}

// ExampleExtractor_ExtractPrefixes_noPrefix demonstrates a pattern
// with no extractable prefix (starts with wildcard).
func ExampleExtractor_ExtractPrefixes_noPrefix() {
	// Pattern starts with wildcard: .*error
	re, _ := syntax.Parse(".*error", syntax.Perl)

	extractor := literal.New(literal.DefaultConfig())
	prefixes := extractor.ExtractPrefixes(re)

	if prefixes.IsEmpty() {
		fmt.Println("No prefix literals found (pattern starts with wildcard)")
	} else {
		fmt.Printf("Found %d prefix(es)\n", prefixes.Len())
	}

	// Output:
	// No prefix literals found (pattern starts with wildcard)
}
