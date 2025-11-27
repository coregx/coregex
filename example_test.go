package coregex_test

import (
	"fmt"

	"github.com/coregx/coregex"
)

// ExampleCompile demonstrates basic pattern compilation and matching.
func ExampleCompile() {
	re, err := coregex.Compile(`\d+`)
	if err != nil {
		panic(err)
	}

	fmt.Println(re.Match([]byte("hello 123")))
	// Output: true
}

// ExampleMustCompile demonstrates panic-on-error compilation.
func ExampleMustCompile() {
	re := coregex.MustCompile(`hello`)
	fmt.Println(re.MatchString("hello world"))
	// Output: true
}

// ExampleRegex_Find demonstrates finding the first match.
func ExampleRegex_Find() {
	re := coregex.MustCompile(`\d+`)
	match := re.Find([]byte("age: 42 years"))
	fmt.Println(string(match))
	// Output: 42
}

// ExampleRegex_FindString demonstrates finding a match in a string.
func ExampleRegex_FindString() {
	re := coregex.MustCompile(`\w+@\w+\.\w+`)
	email := re.FindString("Contact: user@example.com")
	fmt.Println(email)
	// Output: user@example.com
}

// ExampleRegex_FindIndex demonstrates finding match positions.
func ExampleRegex_FindIndex() {
	re := coregex.MustCompile(`\d+`)
	loc := re.FindIndex([]byte("age: 42"))
	fmt.Printf("Match at [%d:%d]\n", loc[0], loc[1])
	// Output: Match at [5:7]
}

// ExampleRegex_FindAll demonstrates finding all matches.
func ExampleRegex_FindAll() {
	re := coregex.MustCompile(`\d`)
	matches := re.FindAll([]byte("a1b2c3"), -1)
	for _, m := range matches {
		fmt.Print(string(m), " ")
	}
	fmt.Println()
	// Output: 1 2 3
}

// ExampleRegex_FindAllString demonstrates finding all string matches.
func ExampleRegex_FindAllString() {
	re := coregex.MustCompile(`\w+`)
	words := re.FindAllString("hello world test", -1)
	for _, word := range words {
		fmt.Print(word, " ")
	}
	fmt.Println()
	// Output: hello world test
}

// ExampleCompileWithConfig demonstrates custom configuration.
func ExampleCompileWithConfig() {
	config := coregex.DefaultConfig()
	config.MaxDFAStates = 50000 // Increase cache size

	re, err := coregex.CompileWithConfig("(a|b|c)*", config)
	if err != nil {
		panic(err)
	}

	fmt.Println(re.MatchString("abcabc"))
	// Output: true
}
