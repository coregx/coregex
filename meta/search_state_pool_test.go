package meta

import (
	"sync"
	"testing"
)

// TestSearchStatePoolConcurrency exercises the search state pool under concurrent load.
// Covers: search_state.go get/put, reset, pool.New
func TestSearchStatePoolConcurrency(t *testing.T) {
	engine, err := Compile(`(\w+)`)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	const goroutines = 16
	const iterations = 100

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				haystack := []byte("hello world test")
				match := engine.Find(haystack)
				if match == nil {
					t.Errorf("expected match, got nil")
					return
				}
				if match.String() != "hello" {
					t.Errorf("got %q, want %q", match.String(), "hello")
					return
				}
			}
		}()
	}

	wg.Wait()
}

// TestSearchStatePoolFindSubmatch exercises state pool with captures.
// Covers: search_state.go newSearchState (onepassSlots/onepassCache allocation),
//
//	reset (onepassSlots reset to -1)
func TestSearchStatePoolFindSubmatch(t *testing.T) {
	engine, err := Compile(`(\w+)@(\w+)\.(\w+)`)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		input    string
		wantNil  bool
		wantFull string
		wantG1   string
	}{
		{"user@example.com", false, "user@example.com", "user"},
		{"no-match-here", true, "", ""},
		{"a@b.c", false, "a@b.c", "a"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			m := engine.FindSubmatch([]byte(tt.input))
			if tt.wantNil {
				if m != nil {
					t.Errorf("expected nil, got match")
				}
				return
			}
			if m == nil {
				t.Fatal("expected match, got nil")
			}
			if m.GroupString(0) != tt.wantFull {
				t.Errorf("group(0) = %q, want %q", m.GroupString(0), tt.wantFull)
			}
			if m.GroupString(1) != tt.wantG1 {
				t.Errorf("group(1) = %q, want %q", m.GroupString(1), tt.wantG1)
			}
		})
	}
}

// TestSearchStatePoolConcurrentSubmatch exercises concurrent captures.
// Covers: search_state.go pool thread safety with onepass
func TestSearchStatePoolConcurrentSubmatch(t *testing.T) {
	engine, err := Compile(`(\d+)-(\d+)`)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	const goroutines = 8
	const iterations = 50

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				m := engine.FindSubmatch([]byte("prefix 123-456 suffix"))
				if m == nil {
					t.Errorf("expected match, got nil")
					return
				}
				if m.GroupString(0) != "123-456" {
					t.Errorf("group(0) = %q, want %q", m.GroupString(0), "123-456")
					return
				}
				if m.GroupString(1) != "123" {
					t.Errorf("group(1) = %q, want %q", m.GroupString(1), "123")
					return
				}
				if m.GroupString(2) != "456" {
					t.Errorf("group(2) = %q, want %q", m.GroupString(2), "456")
					return
				}
			}
		}()
	}

	wg.Wait()
}

// TestSearchStatePoolPutNil exercises put with nil state.
// Covers: search_state.go put nil guard
func TestSearchStatePoolPutNil(t *testing.T) {
	engine, err := Compile(`test`)
	if err != nil {
		t.Fatal(err)
	}

	// Should not panic
	engine.putSearchState(nil)
}

// TestSearchStatePoolFindAll exercises state reuse across FindAll iterations.
// Covers: search_state.go get/put cycle, findall.go findAllIndicesLoop state reuse
func TestSearchStatePoolFindAll(t *testing.T) {
	engine, err := Compile(`\w+`)
	if err != nil {
		t.Fatal(err)
	}

	input := []byte("one two three four five six seven eight nine ten")
	indices := engine.FindAllIndicesStreaming(input, 0, nil)
	if len(indices) != 10 {
		t.Errorf("expected 10 matches, got %d", len(indices))
	}

	// Verify all matches are correct
	expected := []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}
	for i, want := range expected {
		if i >= len(indices) {
			break
		}
		got := string(input[indices[i][0]:indices[i][1]])
		if got != want {
			t.Errorf("match[%d] = %q, want %q", i, got, want)
		}
	}

}
