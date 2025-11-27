package simd_test

import (
	"fmt"

	"github.com/coregx/coregex/simd"
)

// Example demonstrates basic substring search
func ExampleMemmem() {
	haystack := []byte("hello world")
	needle := []byte("world")

	pos := simd.Memmem(haystack, needle)
	if pos != -1 {
		fmt.Printf("Found at position %d\n", pos)
	} else {
		fmt.Println("Not found")
	}
	// Output: Found at position 6
}

// Example_notFound demonstrates the case when needle is not present
func ExampleMemmem_notFound() {
	haystack := []byte("hello world")
	needle := []byte("xyz")

	pos := simd.Memmem(haystack, needle)
	if pos == -1 {
		fmt.Println("Not found")
	}
	// Output: Not found
}

// Example_httpHeader demonstrates searching in HTTP headers
func ExampleMemmem_httpHeader() {
	header := []byte("Content-Type: application/json\r\nContent-Length: 1234\r\n")
	needle := []byte("Content-Length:")

	pos := simd.Memmem(header, needle)
	if pos != -1 {
		fmt.Printf("Found header at position %d\n", pos)
	}
	// Output: Found header at position 32
}

// Example_jsonKey demonstrates searching for JSON keys
func ExampleMemmem_jsonKey() {
	json := []byte(`{"name":"John","age":30,"city":"New York"}`)
	needle := []byte(`"age"`)

	pos := simd.Memmem(json, needle)
	if pos != -1 {
		fmt.Printf("Found 'age' key at position %d\n", pos)
	}
	// Output: Found 'age' key at position 15
}

// Example_emptyNeedle demonstrates that empty needle matches at start
func ExampleMemmem_emptyNeedle() {
	haystack := []byte("hello")
	needle := []byte("")

	pos := simd.Memmem(haystack, needle)
	fmt.Printf("Empty needle found at position %d\n", pos)
	// Output: Empty needle found at position 0
}

// Example_repeatedPattern demonstrates finding patterns in repeated data
func ExampleMemmem_repeatedPattern() {
	dna := []byte("ATATATGCGCGC")
	pattern := []byte("GCGC")

	pos := simd.Memmem(dna, pattern)
	if pos != -1 {
		fmt.Printf("Pattern found at position %d\n", pos)
		fmt.Printf("Context: %s\n", dna[pos:pos+len(pattern)])
	}
	// Output:
	// Pattern found at position 6
	// Context: GCGC
}
