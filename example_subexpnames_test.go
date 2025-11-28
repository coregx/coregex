package coregex_test

import (
	"fmt"

	"github.com/coregx/coregex"
)

// ExampleRegex_SubexpNames demonstrates named capture groups
func ExampleRegex_SubexpNames() {
	// Pattern with named and unnamed captures
	re := coregex.MustCompile(`(?P<year>\d{4})-(?P<month>\d{2})-(\d{2})`)

	// Get capture group names
	names := re.SubexpNames()
	fmt.Printf("Capture groups: %d\n", re.NumSubexp())
	fmt.Printf("Group 0 (full match): %q\n", names[0])
	fmt.Printf("Group 1 (year): %q\n", names[1])
	fmt.Printf("Group 2 (month): %q\n", names[2])
	fmt.Printf("Group 3 (day, unnamed): %q\n", names[3])

	// Output:
	// Capture groups: 4
	// Group 0 (full match): ""
	// Group 1 (year): "year"
	// Group 2 (month): "month"
	// Group 3 (day, unnamed): ""
}

// ExampleRegex_SubexpNames_matching shows using SubexpNames with matches
func ExampleRegex_SubexpNames_matching() {
	// Compile pattern with named captures
	re := coregex.MustCompile(`(?P<protocol>https?)://(?P<domain>\w+)`)

	// Find match and get submatch values
	match := re.FindStringSubmatch("Visit https://example for more")
	names := re.SubexpNames()

	// Print matches with their names
	for i, name := range names {
		if i < len(match) && match[i] != "" {
			if name != "" {
				fmt.Printf("%s: %s\n", name, match[i])
			} else if i == 0 {
				fmt.Printf("Full match: %s\n", match[i])
			}
		}
	}

	// Output:
	// Full match: https://example
	// protocol: https
	// domain: example
}
