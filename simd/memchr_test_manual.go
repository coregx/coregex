// Manual test to verify build tags work correctly across platforms.
// This file can be run with: go run memchr_test_manual.go
// It verifies that the correct implementation is selected based on build tags.
//
// NOTE: This is NOT a unit test file. Real tests will be added in P1-005.
// This is just a sanity check for build tag architecture.

//go:build ignore

package main

import (
	"fmt"
	"runtime"

	"github.com/coregx/coregex/simd"
)

func main() {
	fmt.Printf("Testing memchr on %s/%s\n\n", runtime.GOOS, runtime.GOARCH)

	// Test data
	haystack := []byte("hello world, this is a test")

	// Test Memchr
	pos1 := simd.Memchr(haystack, 'w')
	fmt.Printf("Memchr('w'): %d (expected: 6)\n", pos1)

	pos2 := simd.Memchr(haystack, 'x')
	fmt.Printf("Memchr('x'): %d (expected: -1)\n", pos2)

	// Test Memchr2
	pos3 := simd.Memchr2(haystack, 'w', 't')
	fmt.Printf("Memchr2('w', 't'): %d (expected: 6)\n", pos3)

	// Test Memchr3
	pos4 := simd.Memchr3(haystack, 'x', 'y', 'z')
	fmt.Printf("Memchr3('x', 'y', 'z'): %d (expected: -1)\n", pos4)

	pos5 := simd.Memchr3(haystack, ' ', ',', '.')
	fmt.Printf("Memchr3(' ', ',', '.'): %d (expected: 5)\n", pos5)

	// Verify results
	allPassed := true
	if pos1 != 6 {
		fmt.Printf("FAIL: Memchr('w') returned %d, expected 6\n", pos1)
		allPassed = false
	}
	if pos2 != -1 {
		fmt.Printf("FAIL: Memchr('x') returned %d, expected -1\n", pos2)
		allPassed = false
	}
	if pos3 != 6 {
		fmt.Printf("FAIL: Memchr2('w', 't') returned %d, expected 6\n", pos3)
		allPassed = false
	}
	if pos4 != -1 {
		fmt.Printf("FAIL: Memchr3('x', 'y', 'z') returned %d, expected -1\n", pos4)
		allPassed = false
	}
	if pos5 != 5 {
		fmt.Printf("FAIL: Memchr3(' ', ',', '.') returned %d, expected 5\n", pos5)
		allPassed = false
	}

	if allPassed {
		fmt.Println("\nAll manual tests PASSED!")
	} else {
		fmt.Println("\nSome tests FAILED!")
	}
}
