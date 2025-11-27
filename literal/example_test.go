package literal_test

import (
	"fmt"

	"github.com/coregx/coregex/literal"
)

// Example demonstrates basic usage of literal sequences
func Example() {
	// Create a sequence of literals from a regex alternation like /foo|bar|baz/
	seq := literal.NewSeq(
		literal.NewLiteral([]byte("foo"), true),
		literal.NewLiteral([]byte("bar"), true),
		literal.NewLiteral([]byte("baz"), true),
	)

	fmt.Printf("Sequence has %d literals\n", seq.Len())
	fmt.Printf("First literal: %s\n", seq.Get(0).Bytes)

	// Output:
	// Sequence has 3 literals
	// First literal: foo
}

// ExampleSeq_Minimize demonstrates removing redundant literals
func ExampleSeq_Minimize() {
	// For prefix matching, "foo" covers "foobar"
	seq := literal.NewSeq(
		literal.NewLiteral([]byte("foo"), true),
		literal.NewLiteral([]byte("foobar"), true),
	)

	fmt.Printf("Before minimize: %d literals\n", seq.Len())
	seq.Minimize()
	fmt.Printf("After minimize: %d literals\n", seq.Len())
	fmt.Printf("Remaining: %s\n", seq.Get(0).Bytes)

	// Output:
	// Before minimize: 2 literals
	// After minimize: 1 literals
	// Remaining: foo
}

// ExampleSeq_Minimize_chain demonstrates chain redundancy removal
func ExampleSeq_Minimize_chain() {
	// "a" covers "ab" which covers "abc"
	seq := literal.NewSeq(
		literal.NewLiteral([]byte("abc"), true),
		literal.NewLiteral([]byte("ab"), true),
		literal.NewLiteral([]byte("a"), true),
	)

	seq.Minimize()
	fmt.Printf("Literals after minimize: %d\n", seq.Len())
	fmt.Printf("Shortest literal wins: %s\n", seq.Get(0).Bytes)

	// Output:
	// Literals after minimize: 1
	// Shortest literal wins: a
}

// ExampleSeq_LongestCommonPrefix demonstrates finding common prefix
func ExampleSeq_LongestCommonPrefix() {
	seq := literal.NewSeq(
		literal.NewLiteral([]byte("hello"), true),
		literal.NewLiteral([]byte("help"), true),
		literal.NewLiteral([]byte("hero"), true),
	)

	prefix := seq.LongestCommonPrefix()
	fmt.Printf("Common prefix: %s\n", prefix)

	// Output:
	// Common prefix: he
}

// ExampleSeq_LongestCommonPrefix_none demonstrates no common prefix
func ExampleSeq_LongestCommonPrefix_none() {
	seq := literal.NewSeq(
		literal.NewLiteral([]byte("abc"), true),
		literal.NewLiteral([]byte("def"), true),
	)

	prefix := seq.LongestCommonPrefix()
	fmt.Printf("Common prefix length: %d\n", len(prefix))

	// Output:
	// Common prefix length: 0
}

// ExampleSeq_LongestCommonSuffix demonstrates finding common suffix
func ExampleSeq_LongestCommonSuffix() {
	seq := literal.NewSeq(
		literal.NewLiteral([]byte("cat"), true),
		literal.NewLiteral([]byte("bat"), true),
		literal.NewLiteral([]byte("rat"), true),
	)

	suffix := seq.LongestCommonSuffix()
	fmt.Printf("Common suffix: %s\n", suffix)

	// Output:
	// Common suffix: at
}

// ExampleSeq_Clone demonstrates deep copying
func ExampleSeq_Clone() {
	original := literal.NewSeq(
		literal.NewLiteral([]byte("test"), true),
	)

	clone := original.Clone()

	// Modify clone
	clone.Minimize() // This won't affect original

	fmt.Printf("Original length: %d\n", original.Len())
	fmt.Printf("Clone length: %d\n", clone.Len())

	// Output:
	// Original length: 1
	// Clone length: 1
}

// ExampleLiteral demonstrates basic Literal usage
func ExampleLiteral() {
	// Complete literal - represents entire match
	complete := literal.NewLiteral([]byte("hello"), true)
	fmt.Printf("%s, length=%d\n", complete.String(), complete.Len())

	// Incomplete literal - just a prefix
	incomplete := literal.NewLiteral([]byte("world"), false)
	fmt.Printf("%s, length=%d\n", incomplete.String(), incomplete.Len())

	// Output:
	// literal{hello, complete=true}, length=5
	// literal{world, complete=false}, length=5
}

// ExampleSeq_IsEmpty demonstrates empty sequence checks
func ExampleSeq_IsEmpty() {
	empty := literal.NewSeq()
	nonempty := literal.NewSeq(literal.NewLiteral([]byte("x"), true))

	fmt.Printf("Empty sequence: %v\n", empty.IsEmpty())
	fmt.Printf("Non-empty sequence: %v\n", nonempty.IsEmpty())

	// Output:
	// Empty sequence: true
	// Non-empty sequence: false
}

// ExampleSeq_IsFinite demonstrates finite language check
func ExampleSeq_IsFinite() {
	// A sequence with literals represents a finite language
	finite := literal.NewSeq(literal.NewLiteral([]byte("test"), true))

	// Empty sequence represents infinite/empty language
	empty := literal.NewSeq()

	fmt.Printf("Finite: %v\n", finite.IsFinite())
	fmt.Printf("Empty: %v\n", empty.IsFinite())

	// Output:
	// Finite: true
	// Empty: false
}
