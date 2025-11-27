package prefilter

import (
	"regexp/syntax"
	"testing"

	"github.com/coregx/coregex/literal"
)

// Test helper: create a literal sequence from byte slices and complete flags
func makeSeq(lits ...struct {
	bytes    []byte
	complete bool
}) *literal.Seq {
	literals := make([]literal.Literal, len(lits))
	for i, lit := range lits {
		literals[i] = literal.NewLiteral(lit.bytes, lit.complete)
	}
	return literal.NewSeq(literals...)
}

// TestSelectPrefilter_Empty tests selection with empty literal sequences
func TestSelectPrefilter_Empty(t *testing.T) {
	tests := []struct {
		name     string
		prefixes *literal.Seq
		suffixes *literal.Seq
	}{
		{
			name:     "both nil",
			prefixes: nil,
			suffixes: nil,
		},
		{
			name:     "both empty",
			prefixes: literal.NewSeq(),
			suffixes: literal.NewSeq(),
		},
		{
			name:     "prefixes empty, suffixes nil",
			prefixes: literal.NewSeq(),
			suffixes: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pf := selectPrefilter(tt.prefixes, tt.suffixes)
			if pf != nil {
				t.Errorf("expected nil prefilter for empty sequences, got %T", pf)
			}
		})
	}
}

// TestSelectPrefilter_SingleByte tests selection of MemchrPrefilter
func TestSelectPrefilter_SingleByte(t *testing.T) {
	tests := []struct {
		name     string
		prefixes *literal.Seq
		suffixes *literal.Seq
		complete bool
	}{
		{
			name: "single byte in prefixes, complete",
			prefixes: makeSeq(
				struct {
					bytes    []byte
					complete bool
				}{[]byte("a"), true},
			),
			suffixes: nil,
			complete: true,
		},
		{
			name: "single byte in prefixes, incomplete",
			prefixes: makeSeq(
				struct {
					bytes    []byte
					complete bool
				}{[]byte("x"), false},
			),
			suffixes: nil,
			complete: false,
		},
		{
			name:     "single byte in suffixes (prefixes empty)",
			prefixes: literal.NewSeq(),
			suffixes: makeSeq(
				struct {
					bytes    []byte
					complete bool
				}{[]byte("z"), true},
			),
			complete: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pf := selectPrefilter(tt.prefixes, tt.suffixes)
			if pf == nil {
				t.Fatal("expected Memchr prefilter, got nil")
			}

			memchrPf, ok := pf.(*memchrPrefilter)
			if !ok {
				t.Fatalf("expected *memchrPrefilter, got %T", pf)
			}

			if memchrPf.IsComplete() != tt.complete {
				t.Errorf("IsComplete() = %v, want %v", memchrPf.IsComplete(), tt.complete)
			}

			if memchrPf.HeapBytes() != 0 {
				t.Errorf("HeapBytes() = %d, want 0", memchrPf.HeapBytes())
			}
		})
	}
}

// TestSelectPrefilter_SingleSubstring tests selection of MemmemPrefilter
func TestSelectPrefilter_SingleSubstring(t *testing.T) {
	tests := []struct {
		name     string
		prefixes *literal.Seq
		complete bool
		needle   []byte
	}{
		{
			name: "short substring",
			prefixes: makeSeq(
				struct {
					bytes    []byte
					complete bool
				}{[]byte("hello"), true},
			),
			complete: true,
			needle:   []byte("hello"),
		},
		{
			name: "long substring, incomplete",
			prefixes: makeSeq(
				struct {
					bytes    []byte
					complete bool
				}{[]byte("this is a longer pattern"), false},
			),
			complete: false,
			needle:   []byte("this is a longer pattern"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pf := selectPrefilter(tt.prefixes, nil)
			if pf == nil {
				t.Fatal("expected Memmem prefilter, got nil")
			}

			memmemPf, ok := pf.(*memmemPrefilter)
			if !ok {
				t.Fatalf("expected *memmemPrefilter, got %T", pf)
			}

			if memmemPf.IsComplete() != tt.complete {
				t.Errorf("IsComplete() = %v, want %v", memmemPf.IsComplete(), tt.complete)
			}

			expectedHeap := len(tt.needle)
			if memmemPf.HeapBytes() != expectedHeap {
				t.Errorf("HeapBytes() = %d, want %d", memmemPf.HeapBytes(), expectedHeap)
			}
		})
	}
}

// TestSelectPrefilter_MultipleLiterals tests selection with multiple literals
func TestSelectPrefilter_MultipleLiterals(t *testing.T) {
	tests := []struct {
		name     string
		prefixes *literal.Seq
		wantNil  bool
		reason   string
	}{
		{
			name: "2 literals, len>=3 (Teddy)",
			prefixes: makeSeq(
				struct {
					bytes    []byte
					complete bool
				}{[]byte("foo"), true},
				struct {
					bytes    []byte
					complete bool
				}{[]byte("bar"), true},
			),
			wantNil: false,
			reason:  "Teddy handles 2-8 patterns with len>=3",
		},
		{
			name: "8 literals, len>=3 (Teddy)",
			prefixes: makeSeq(
				struct {
					bytes    []byte
					complete bool
				}{[]byte("aaa"), true},
				struct {
					bytes    []byte
					complete bool
				}{[]byte("bbb"), true},
				struct {
					bytes    []byte
					complete bool
				}{[]byte("ccc"), true},
				struct {
					bytes    []byte
					complete bool
				}{[]byte("ddd"), true},
				struct {
					bytes    []byte
					complete bool
				}{[]byte("eee"), true},
				struct {
					bytes    []byte
					complete bool
				}{[]byte("fff"), true},
				struct {
					bytes    []byte
					complete bool
				}{[]byte("ggg"), true},
				struct {
					bytes    []byte
					complete bool
				}{[]byte("hhh"), true},
			),
			wantNil: false,
			reason:  "Teddy handles 2-8 patterns with len>=3",
		},
		{
			name: "9 literals (future Aho-Corasick)",
			prefixes: makeSeq(
				struct {
					bytes    []byte
					complete bool
				}{[]byte("a"), true},
				struct {
					bytes    []byte
					complete bool
				}{[]byte("b"), true},
				struct {
					bytes    []byte
					complete bool
				}{[]byte("c"), true},
				struct {
					bytes    []byte
					complete bool
				}{[]byte("d"), true},
				struct {
					bytes    []byte
					complete bool
				}{[]byte("e"), true},
				struct {
					bytes    []byte
					complete bool
				}{[]byte("f"), true},
				struct {
					bytes    []byte
					complete bool
				}{[]byte("g"), true},
				struct {
					bytes    []byte
					complete bool
				}{[]byte("h"), true},
				struct {
					bytes    []byte
					complete bool
				}{[]byte("i"), true},
			),
			wantNil: true,
			reason:  "Aho-Corasick not yet implemented",
		},
		{
			name: "multiple literals, short (len<3)",
			prefixes: makeSeq(
				struct {
					bytes    []byte
					complete bool
				}{[]byte("ab"), true},
				struct {
					bytes    []byte
					complete bool
				}{[]byte("cd"), true},
			),
			wantNil: true,
			reason:  "too short for Teddy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pf := selectPrefilter(tt.prefixes, nil)

			// Handle nil case
			if tt.wantNil {
				if pf != nil {
					t.Errorf("expected nil (%s), got %T", tt.reason, pf)
				}
				return
			}

			// Handle non-nil case
			if pf == nil {
				t.Errorf("expected non-nil prefilter (%s), got nil", tt.reason)
				return
			}

			// For Teddy tests, verify it's actually Teddy
			if tt.name == "2 literals, len>=3 (Teddy)" || tt.name == "8 literals, len>=3 (Teddy)" {
				if _, ok := pf.(*Teddy); !ok {
					t.Errorf("expected *Teddy, got %T", pf)
				}
			}
		})
	}
}

// TestMemchrPrefilter_Find tests MemchrPrefilter.Find functionality
func TestMemchrPrefilter_Find(t *testing.T) {
	tests := []struct {
		name     string
		needle   byte
		haystack []byte
		start    int
		want     int
	}{
		{
			name:     "found at start",
			needle:   'h',
			haystack: []byte("hello world"),
			start:    0,
			want:     0,
		},
		{
			name:     "found in middle",
			needle:   'o',
			haystack: []byte("hello world"),
			start:    0,
			want:     4,
		},
		{
			name:     "found at end",
			needle:   'd',
			haystack: []byte("hello world"),
			start:    0,
			want:     10,
		},
		{
			name:     "not found",
			needle:   'x',
			haystack: []byte("hello world"),
			start:    0,
			want:     -1,
		},
		{
			name:     "empty haystack",
			needle:   'a',
			haystack: []byte(""),
			start:    0,
			want:     -1,
		},
		{
			name:     "start beyond bounds",
			needle:   'h',
			haystack: []byte("hello"),
			start:    10,
			want:     -1,
		},
		{
			name:     "start exactly at end",
			needle:   'h',
			haystack: []byte("hello"),
			start:    5,
			want:     -1,
		},
		{
			name:     "second occurrence",
			needle:   'l',
			haystack: []byte("hello world"),
			start:    3,
			want:     3,
		},
		{
			name:     "skip first, find second",
			needle:   'o',
			haystack: []byte("hello world"),
			start:    5,
			want:     7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pf := newMemchrPrefilter(tt.needle, false)
			got := pf.Find(tt.haystack, tt.start)
			if got != tt.want {
				t.Errorf("Find() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestMemmemPrefilter_Find tests MemmemPrefilter.Find functionality
func TestMemmemPrefilter_Find(t *testing.T) {
	tests := []struct {
		name     string
		needle   []byte
		haystack []byte
		start    int
		want     int
	}{
		{
			name:     "found at start",
			needle:   []byte("hello"),
			haystack: []byte("hello world"),
			start:    0,
			want:     0,
		},
		{
			name:     "found in middle",
			needle:   []byte("world"),
			haystack: []byte("hello world"),
			start:    0,
			want:     6,
		},
		{
			name:     "found at end",
			needle:   []byte("bar"),
			haystack: []byte("foobar"),
			start:    0,
			want:     3,
		},
		{
			name:     "not found",
			needle:   []byte("xyz"),
			haystack: []byte("hello world"),
			start:    0,
			want:     -1,
		},
		{
			name:     "empty haystack",
			needle:   []byte("test"),
			haystack: []byte(""),
			start:    0,
			want:     -1,
		},
		{
			name:     "start beyond bounds",
			needle:   []byte("hello"),
			haystack: []byte("hello world"),
			start:    20,
			want:     -1,
		},
		{
			name:     "start exactly at end",
			needle:   []byte("test"),
			haystack: []byte("testing"),
			start:    7,
			want:     -1,
		},
		{
			name:     "second occurrence",
			needle:   []byte("ab"),
			haystack: []byte("ababab"),
			start:    1,
			want:     2,
		},
		{
			name:     "skip first, find second",
			needle:   []byte("test"),
			haystack: []byte("test test test"),
			start:    5,
			want:     5,
		},
		{
			name:     "overlapping patterns",
			needle:   []byte("aaa"),
			haystack: []byte("aaaaa"),
			start:    0,
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pf := newMemmemPrefilter(tt.needle, false)
			got := pf.Find(tt.haystack, tt.start)
			if got != tt.want {
				t.Errorf("Find() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestBuilder_Integration tests full integration with literal.Extractor
func TestBuilder_Integration(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		haystack    []byte
		wantType    string // "memchr", "memmem", "nil"
		wantPos     int
		wantHeapMin int // minimum expected heap bytes
	}{
		{
			name:        "simple literal",
			pattern:     "hello",
			haystack:    []byte("foo hello bar"),
			wantType:    "memmem",
			wantPos:     4,
			wantHeapMin: 5,
		},
		{
			name:        "alternation (Go parser factorizes)",
			pattern:     "(foo|foobar)",
			haystack:    []byte("prefix foobar suffix"),
			wantType:    "memmem",
			wantPos:     7,
			wantHeapMin: 3, // "foo" after minimization
		},
		{
			name:        "character class single char",
			pattern:     "[a]test",
			haystack:    []byte("xxxatestyyy"),
			wantType:    "memmem",
			wantPos:     3,
			wantHeapMin: 5,
		},
		{
			name:        "no literals (.*)",
			pattern:     ".*",
			haystack:    []byte("anything"),
			wantType:    "nil",
			wantPos:     -1,
			wantHeapMin: 0,
		},
		{
			name:        "prefix with wildcard",
			pattern:     "start.*",
			haystack:    []byte("start here"),
			wantType:    "memmem",
			wantPos:     0,
			wantHeapMin: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse pattern
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("failed to parse pattern: %v", err)
			}

			// Extract literals
			extractor := literal.New(literal.DefaultConfig())
			prefixes := extractor.ExtractPrefixes(re)

			// Build prefilter
			builder := NewBuilder(prefixes, nil)
			pf := builder.Build()

			// Check type
			switch tt.wantType {
			case "nil":
				if pf != nil {
					t.Errorf("expected nil prefilter, got %T", pf)
				}
				return
			case "memchr":
				if pf == nil {
					t.Fatal("expected Memchr prefilter, got nil")
				}
				if _, ok := pf.(*memchrPrefilter); !ok {
					t.Errorf("expected *memchrPrefilter, got %T", pf)
				}
			case "memmem":
				if pf == nil {
					t.Fatal("expected Memmem prefilter, got nil")
				}
				if _, ok := pf.(*memmemPrefilter); !ok {
					t.Errorf("expected *memmemPrefilter, got %T", pf)
				}
			}

			// Test Find
			if pf != nil {
				got := pf.Find(tt.haystack, 0)
				if got != tt.wantPos {
					t.Errorf("Find() = %d, want %d", got, tt.wantPos)
				}

				// Check heap bytes
				heap := pf.HeapBytes()
				if heap < tt.wantHeapMin {
					t.Errorf("HeapBytes() = %d, want >= %d", heap, tt.wantHeapMin)
				}
			}
		})
	}
}

// TestMinLen tests the minLen helper function
func TestMinLen(t *testing.T) {
	tests := []struct {
		name string
		seq  *literal.Seq
		want int
	}{
		{
			name: "empty sequence",
			seq:  literal.NewSeq(),
			want: int(^uint(0) >> 1), // max int
		},
		{
			name: "single literal",
			seq: makeSeq(
				struct {
					bytes    []byte
					complete bool
				}{[]byte("hello"), true},
			),
			want: 5,
		},
		{
			name: "multiple literals, different lengths",
			seq: makeSeq(
				struct {
					bytes    []byte
					complete bool
				}{[]byte("a"), true},
				struct {
					bytes    []byte
					complete bool
				}{[]byte("hello"), true},
				struct {
					bytes    []byte
					complete bool
				}{[]byte("world"), true},
			),
			want: 1,
		},
		{
			name: "multiple literals, same length",
			seq: makeSeq(
				struct {
					bytes    []byte
					complete bool
				}{[]byte("foo"), true},
				struct {
					bytes    []byte
					complete bool
				}{[]byte("bar"), true},
				struct {
					bytes    []byte
					complete bool
				}{[]byte("baz"), true},
			),
			want: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := minLen(tt.seq)
			if got != tt.want {
				t.Errorf("minLen() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestPrefilter_EdgeCases tests edge cases for all prefilters
func TestPrefilter_EdgeCases(t *testing.T) {
	t.Run("memchr negative start", func(t *testing.T) {
		pf := newMemchrPrefilter('a', false)
		got := pf.Find([]byte("abc"), -1)
		if got != -1 {
			t.Errorf("Find() with negative start = %d, want -1", got)
		}
	})

	t.Run("memmem negative start", func(t *testing.T) {
		pf := newMemmemPrefilter([]byte("ab"), false)
		got := pf.Find([]byte("abc"), -1)
		if got != -1 {
			t.Errorf("Find() with negative start = %d, want -1", got)
		}
	})

	t.Run("memchr complete flag", func(t *testing.T) {
		pfComplete := newMemchrPrefilter('a', true)
		pfIncomplete := newMemchrPrefilter('a', false)

		if !pfComplete.IsComplete() {
			t.Error("complete prefilter should return IsComplete() = true")
		}
		if pfIncomplete.IsComplete() {
			t.Error("incomplete prefilter should return IsComplete() = false")
		}
	})

	t.Run("memmem needle aliasing", func(t *testing.T) {
		original := []byte("test")
		pf := newMemmemPrefilter(original, false)

		// Modify original
		original[0] = 'X'

		// Prefilter should still search for "test", not "Xest"
		got := pf.Find([]byte("test"), 0)
		if got != 0 {
			t.Errorf("Find() = %d, want 0 (needle should be copied)", got)
		}
	})
}

// BenchmarkPrefilter_Memchr benchmarks MemchrPrefilter
func BenchmarkPrefilter_Memchr(b *testing.B) {
	b.ReportAllocs()

	sizes := []int{64, 1024, 4096, 65536}
	pf := newMemchrPrefilter('x', false)

	for _, size := range sizes {
		haystack := make([]byte, size)
		for i := range haystack {
			haystack[i] = 'a'
		}
		// Put needle at 3/4 position
		haystack[size*3/4] = 'x'

		b.Run(formatSize(size), func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				pos := pf.Find(haystack, 0)
				if pos == -1 {
					b.Fatal("expected to find needle")
				}
			}
		})
	}
}

// BenchmarkPrefilter_Memmem benchmarks MemmemPrefilter
func BenchmarkPrefilter_Memmem(b *testing.B) {
	b.ReportAllocs()

	sizes := []int{64, 1024, 4096, 65536}
	needle := []byte("pattern")
	pf := newMemmemPrefilter(needle, false)

	for _, size := range sizes {
		haystack := make([]byte, size)
		for i := range haystack {
			haystack[i] = 'a'
		}
		// Put needle at 3/4 position
		copy(haystack[size*3/4:], needle)

		b.Run(formatSize(size), func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				pos := pf.Find(haystack, 0)
				if pos == -1 {
					b.Fatal("expected to find needle")
				}
			}
		})
	}
}

// formatSize formats byte size for benchmark names
func formatSize(size int) string {
	if size < 1024 {
		return string(rune(size)) + "B"
	}
	return string(rune(size/1024)) + "KB"
}
