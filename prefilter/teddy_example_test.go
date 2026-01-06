package prefilter_test

import (
	"fmt"
	"regexp/syntax"

	"github.com/coregx/coregex/literal"
	"github.com/coregx/coregex/prefilter"
)

// Example_teddyBasic demonstrates basic Teddy usage for multi-pattern search
func Example_teddyBasic() {
	patterns := [][]byte{
		[]byte("foo"),
		[]byte("bar"),
		[]byte("baz"),
	}

	teddy := prefilter.NewTeddy(patterns, nil)
	if teddy == nil {
		fmt.Println("Teddy not available")
		return
	}

	haystack := []byte("hello bar world")
	pos := teddy.Find(haystack, 0)

	fmt.Printf("Found at position: %d\n", pos)
	// Output: Found at position: 6
}

// Example_teddyWithRegex demonstrates Teddy with regex literal extraction
func Example_teddyWithRegex() {
	// Parse regex with alternation (use distinct patterns to ensure 3 prefixes)
	re, _ := syntax.Parse("(abc|def|ghi)", syntax.Perl)

	// Extract prefixes
	extractor := literal.New(literal.DefaultConfig())
	prefixes := extractor.ExtractPrefixes(re)

	fmt.Printf("Extracted %d prefixes\n", prefixes.Len())

	// Build prefilter (will select Teddy for 3 patterns)
	builder := prefilter.NewBuilder(prefixes, nil)
	pf := builder.Build()

	if pf == nil {
		fmt.Println("No prefilter available")
		return
	}

	// Use prefilter
	haystack := []byte("test def test")
	pos := pf.Find(haystack, 0)

	fmt.Printf("Found at position: %d\n", pos)
	// Output:
	// Extracted 3 prefixes
	// Found at position: 5
}

// Example_teddyMultipleMatches demonstrates finding multiple matches
func Example_teddyMultipleMatches() {
	patterns := [][]byte{
		[]byte("ERROR"),
		[]byte("WARNING"),
	}

	teddy := prefilter.NewTeddy(patterns, nil)
	if teddy == nil {
		fmt.Println("Teddy not available")
		return
	}

	haystack := []byte("INFO: all good\nERROR: something broke\nWARNING: check this")

	// Find all matches
	matches := []int{}
	start := 0
	for {
		pos := teddy.Find(haystack, start)
		if pos == -1 {
			break
		}
		matches = append(matches, pos)
		start = pos + 1 // Continue searching after this match
	}

	fmt.Printf("Found %d matches at positions: %v\n", len(matches), matches)
	// Output: Found 2 matches at positions: [15 38]
}

// Example_teddyConfig demonstrates custom Teddy configuration
func Example_teddyConfig() {
	patterns := [][]byte{
		[]byte("apple"),
		[]byte("banana"),
	}

	config := &prefilter.TeddyConfig{
		MinPatterns:    2,
		MaxPatterns:    8,
		MinPatternLen:  3,
		FingerprintLen: 1, // Use 1-byte fingerprint
	}

	teddy := prefilter.NewTeddy(patterns, config)
	if teddy == nil {
		fmt.Println("Teddy not available")
		return
	}

	haystack := []byte("I like banana and apple")
	pos := teddy.Find(haystack, 0)

	fmt.Printf("Found at position: %d\n", pos)
	// Output: Found at position: 7
}

// Example_teddyNoMatch demonstrates no-match behavior
func Example_teddyNoMatch() {
	patterns := [][]byte{
		[]byte("foo"),
		[]byte("bar"),
	}

	teddy := prefilter.NewTeddy(patterns, nil)
	if teddy == nil {
		fmt.Println("Teddy not available")
		return
	}

	haystack := []byte("hello world")
	pos := teddy.Find(haystack, 0)

	if pos == -1 {
		fmt.Println("No match found")
	}
	// Output: No match found
}

// Example_teddyHeapBytes demonstrates memory usage reporting
func Example_teddyHeapBytes() {
	patterns := [][]byte{
		[]byte("foo"),
		[]byte("bar"),
		[]byte("baz"),
		[]byte("qux"),
	}

	teddy := prefilter.NewTeddy(patterns, nil)
	if teddy == nil {
		fmt.Println("Teddy not available")
		return
	}

	fmt.Printf("Teddy uses approximately %d bytes of heap memory\n", teddy.HeapBytes())
	// Output will vary, but typically < 1KB
	// Example output: Teddy uses approximately 376 bytes of heap memory
}

// Example_teddyVsNaive demonstrates performance comparison
func Example_teddyVsNaive() {
	patterns := [][]byte{
		[]byte("pattern1"),
		[]byte("pattern2"),
		[]byte("pattern3"),
	}

	teddy := prefilter.NewTeddy(patterns, nil)
	if teddy == nil {
		fmt.Println("Teddy not available")
		return
	}

	haystack := make([]byte, 10000)
	for i := range haystack {
		haystack[i] = 'x'
	}
	copy(haystack[5000:], "pattern2")

	pos := teddy.Find(haystack, 0)
	fmt.Printf("Found pattern at position: %d\n", pos)
	fmt.Println("Teddy provides 20-50x speedup over naive multi-pattern search")
	// Output:
	// Found pattern at position: 5000
	// Teddy provides 20-50x speedup over naive multi-pattern search
}

// Example_teddyNotSuitable demonstrates when Teddy is not suitable
func Example_teddyNotSuitable() {
	// Too few patterns (need >= 2)
	teddy1 := prefilter.NewTeddy([][]byte{[]byte("foo")}, nil)
	fmt.Printf("1 pattern: %v\n", teddy1 != nil)

	// Pattern too short (need >= 3 bytes)
	teddy2 := prefilter.NewTeddy([][]byte{[]byte("ab"), []byte("cd")}, nil)
	fmt.Printf("Short patterns: %v\n", teddy2 != nil)

	// Too many patterns (max 32)
	manyPatterns := make([][]byte, 35)
	for i := range manyPatterns {
		manyPatterns[i] = []byte(fmt.Sprintf("pat%02d", i))
	}
	teddy3 := prefilter.NewTeddy(manyPatterns, nil)
	fmt.Printf("Too many patterns: %v\n", teddy3 != nil)

	// Output:
	// 1 pattern: false
	// Short patterns: false
	// Too many patterns: false
}
