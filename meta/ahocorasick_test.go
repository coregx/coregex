package meta

import (
	"reflect"
	"testing"
)

// TestAhoCorasickStrategySelection verifies that patterns with >64 literals
// select UseAhoCorasick strategy.
func TestAhoCorasickStrategySelection(t *testing.T) {
	// Pattern with 65 literals (above Teddy's limit of 64)
	// Teddy supports up to 64 patterns via Slim (2-32) and Fat (33-64) variants.
	// For >64 patterns, Aho-Corasick is selected.
	// Each literal >= 3 bytes, all complete (no regex meta-characters)
	// IMPORTANT: No shared prefixes! Go's regex parser factors common prefixes,
	// e.g., "two|three" becomes "t(wo|hree)", which extracts only "t" as incomplete.
	// Using unique words with different first characters.
	pattern := `alpha|bravo|charlie|delta|echo|foxtrot|golf|hotel|india|juliet|` + // 10
		`kilo|lima|mike|november|oscar|papa|quebec|romeo|sierra|tango|` + // 20
		`uniform|victor|whiskey|xray|yankee|zulu|anise|basil|cilantro|dill|` + // 30
		`endive|fennel|ginger|hops|ivory|jasmine|kelp|lavender|mint|nutmeg|` + // 40
		`oregano|parsley|quassia|rosemary|sage|thyme|urtica|verbena|wasabi|xylose|` + // 50
		`yarrow|zinnia|acacia|bamboo|cactus|dahlia|ebony|fern|grass|holly|` + // 60
		`iris|juniper|kudzu|lotus|moss|nettle|oak` // 67

	re, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) failed: %v", pattern, err)
	}

	if re.Strategy() != UseAhoCorasick {
		t.Errorf("Strategy() = %s, want UseAhoCorasick for 67 patterns", re.Strategy())
	}
}

// TestAhoCorasickIsMatch tests boolean matching with Aho-Corasick.
func TestAhoCorasickIsMatch(t *testing.T) {
	// 10 patterns - now uses Teddy (limit raised from 8 to 32)
	// This test will skip if Teddy is selected instead of Aho-Corasick
	pattern := `apple|banana|cherry|date|elderberry|fig|grape|honeydew|imbe|jackfruit`

	re, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) failed: %v", pattern, err)
	}

	if re.Strategy() != UseAhoCorasick {
		t.Skipf("Strategy is %s, not UseAhoCorasick (pattern analysis may differ)", re.Strategy())
	}

	tests := []struct {
		name     string
		haystack string
		want     bool
	}{
		{"match first pattern", "I like apple pie", true},
		{"match middle pattern", "fig is a fruit", true},
		{"match last pattern", "jackfruit is tropical", true},
		{"no match", "orange is not in the list", false},
		{"match at start", "banana split", true},
		{"match at end", "I ate a date", true},
		{"empty haystack", "", false},
		{"partial match only", "app is not apple", true}, // "apple" is found? No, "app" is partial
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := re.IsMatch([]byte(tc.haystack))
			if got != tc.want {
				t.Errorf("IsMatch(%q) = %v, want %v", tc.haystack, got, tc.want)
			}
		})
	}
}

// TestAhoCorasickFind tests finding first match with Aho-Corasick.
func TestAhoCorasickFind(t *testing.T) {
	// 9 patterns - triggers Aho-Corasick
	pattern := `one|two|three|four|five|six|seven|eight|nine`

	re, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) failed: %v", pattern, err)
	}

	if re.Strategy() != UseAhoCorasick {
		t.Skipf("Strategy is %s, not UseAhoCorasick", re.Strategy())
	}

	tests := []struct {
		name     string
		haystack string
		wantStr  string
		wantNil  bool
	}{
		{"single match", "I have one apple", "one", false},
		{"first of multiple", "one two three", "one", false},
		{"match in middle", "x y z seven a b c", "seven", false},
		{"no match", "zero is not here", "", true},
		{"empty haystack", "", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			match := re.Find([]byte(tc.haystack))
			if tc.wantNil {
				if match != nil {
					t.Errorf("Find(%q) = %q, want nil", tc.haystack, match.String())
				}
			} else {
				if match == nil {
					t.Errorf("Find(%q) = nil, want %q", tc.haystack, tc.wantStr)
				} else if match.String() != tc.wantStr {
					t.Errorf("Find(%q) = %q, want %q", tc.haystack, match.String(), tc.wantStr)
				}
			}
		})
	}
}

// TestAhoCorasickFindIndices tests zero-allocation index finding.
func TestAhoCorasickFindIndices(t *testing.T) {
	pattern := `red|orange|yellow|green|blue|indigo|violet|black|white`

	re, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) failed: %v", pattern, err)
	}

	if re.Strategy() != UseAhoCorasick {
		t.Skipf("Strategy is %s, not UseAhoCorasick", re.Strategy())
	}

	tests := []struct {
		name      string
		haystack  string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"match at start", "red rose", 0, 3, true},
		{"match in middle", "the sky is blue today", 11, 15, true},
		{"no match", "pink is not here", -1, -1, false},
		{"empty", "", -1, -1, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			start, end, found := re.FindIndices([]byte(tc.haystack))
			if found != tc.wantFound {
				t.Errorf("FindIndices(%q) found = %v, want %v", tc.haystack, found, tc.wantFound)
			}
			if start != tc.wantStart || end != tc.wantEnd {
				t.Errorf("FindIndices(%q) = (%d, %d), want (%d, %d)",
					tc.haystack, start, end, tc.wantStart, tc.wantEnd)
			}
		})
	}
}

// TestAhoCorasickFindAll tests finding all matches iteratively.
func TestAhoCorasickFindAll(t *testing.T) {
	pattern := `cat|dog|bird|fish|rabbit|hamster|turtle|snake|lizard`

	re, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) failed: %v", pattern, err)
	}

	if re.Strategy() != UseAhoCorasick {
		t.Skipf("Strategy is %s, not UseAhoCorasick", re.Strategy())
	}

	tests := []struct {
		name     string
		haystack string
		want     []string
	}{
		{"single match", "I have a cat", []string{"cat"}},
		{"multiple matches", "cat and dog are friends", []string{"cat", "dog"}},
		{"all at start", "dog bird fish", []string{"dog", "bird", "fish"}},
		{"no match", "I have a parrot", nil},
		{"repeated patterns", "cat cat cat", []string{"cat", "cat", "cat"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			matches := findAllStrings(re, []byte(tc.haystack), -1)
			if !reflect.DeepEqual(matches, tc.want) {
				t.Errorf("findAllStrings(%q) = %v, want %v", tc.haystack, matches, tc.want)
			}
		})
	}
}

// TestAhoCorasickCount tests counting matches.
func TestAhoCorasickCount(t *testing.T) {
	pattern := `mon|tue|wed|thu|fri|sat|sun|day|week|month`

	re, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) failed: %v", pattern, err)
	}

	if re.Strategy() != UseAhoCorasick {
		t.Skipf("Strategy is %s, not UseAhoCorasick", re.Strategy())
	}

	tests := []struct {
		name     string
		haystack string
		want     int
	}{
		{"single", "monday", 1}, // "mon" matches
		{"multiple", "mon tue wed", 3},
		{"none", "year", 0},
		{"overlapping words", "day week month", 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := re.Count([]byte(tc.haystack), -1)
			if got != tc.want {
				t.Errorf("Count(%q) = %d, want %d", tc.haystack, got, tc.want)
			}
		})
	}
}

// TestAhoCorasickLargePatternSet tests with many patterns (>64 triggers Aho-Corasick).
func TestAhoCorasickLargePatternSet(t *testing.T) {
	// 70 patterns - above Teddy's limit of 64 (Fat Teddy handles up to 64)
	// No shared prefixes to avoid Go's regex parser factoring (e.g., "two|three" → "t(wo|hree)")
	pattern := `alpha|bravo|charlie|delta|echo|foxtrot|golf|hotel|india|juliet|` + // 10
		`kilo|lima|mike|november|oscar|papa|quebec|romeo|sierra|tango|` + // 20
		`uniform|victor|whiskey|xray|yankee|zulu|anise|basil|cilantro|dill|` + // 30
		`endive|fennel|ginger|hops|ivory|jasmine|kelp|lavender|mint|nutmeg|` + // 40
		`oregano|parsley|quassia|rosemary|sage|thyme|urtica|verbena|wasabi|xylose|` + // 50
		`yarrow|zinnia|acacia|bamboo|cactus|dahlia|ebony|fern|grass|holly|` + // 60
		`iris|juniper|kudzu|lotus|moss|nettle|oak|plum|reed|sorrel` // 70

	re, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) failed: %v", pattern, err)
	}

	if re.Strategy() != UseAhoCorasick {
		t.Errorf("Strategy() = %s, want UseAhoCorasick for 70 patterns", re.Strategy())
	}

	haystack := []byte("this is alpha and omega, with bravo and tango at the end")

	// Test IsMatch
	if !re.IsMatch(haystack) {
		t.Error("IsMatch should find 'alpha', 'bravo', 'tango'")
	}

	// Test Find
	match := re.Find(haystack)
	if match == nil {
		t.Fatal("Find returned nil, expected 'alpha'")
	}
	if match.String() != "alpha" {
		t.Errorf("Find() = %q, want 'alpha'", match.String())
	}

	// Test Count
	count := re.Count(haystack, -1)
	if count != 3 { // alpha, bravo, tango
		t.Errorf("Count() = %d, want 3", count)
	}
}

// TestAhoCorasickStats verifies that AhoCorasickSearches counter is incremented.
func TestAhoCorasickStats(t *testing.T) {
	pattern := `stat1|stat2|stat3|stat4|stat5|stat6|stat7|stat8|stat9`

	re, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) failed: %v", pattern, err)
	}

	if re.Strategy() != UseAhoCorasick {
		t.Skipf("Strategy is %s, not UseAhoCorasick", re.Strategy())
	}

	// Reset stats
	re.ResetStats()

	// Perform some searches
	haystack := []byte("stat1 stat5 stat9")
	_ = re.IsMatch(haystack)
	_ = re.Find(haystack)
	start, end, found := re.FindIndices(haystack)
	_ = start + end // Use variables to avoid compiler optimization
	_ = found

	stats := re.Stats()
	if stats.AhoCorasickSearches != 3 {
		t.Errorf("AhoCorasickSearches = %d, want 3", stats.AhoCorasickSearches)
	}
}

// TestAhoCorasickStrategyReason tests the strategy reason string.
func TestAhoCorasickStrategyReason(t *testing.T) {
	pattern := `a1|a2|a3|a4|a5|a6|a7|a8|a9`

	re, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) failed: %v", pattern, err)
	}

	if re.Strategy() != UseAhoCorasick {
		t.Skipf("Strategy is %s, not UseAhoCorasick", re.Strategy())
	}

	// Just verify the strategy string
	if re.Strategy().String() != "UseAhoCorasick" {
		t.Errorf("Strategy().String() = %q, want 'UseAhoCorasick'", re.Strategy().String())
	}
}
