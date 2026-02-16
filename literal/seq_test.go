package literal

import (
	"bytes"
	"testing"
)

// TestLiteralBasic tests basic Literal type functionality
func TestLiteralBasic(t *testing.T) {
	tests := []struct {
		name     string
		bytes    []byte
		complete bool
		wantLen  int
		wantStr  string
	}{
		{
			name:     "simple complete literal",
			bytes:    []byte("hello"),
			complete: true,
			wantLen:  5,
			wantStr:  "literal{hello, complete=true}",
		},
		{
			name:     "incomplete literal",
			bytes:    []byte("test"),
			complete: false,
			wantLen:  4,
			wantStr:  "literal{test, complete=false}",
		},
		{
			name:     "empty literal",
			bytes:    []byte{},
			complete: true,
			wantLen:  0,
			wantStr:  "literal{, complete=true}",
		},
		{
			name:     "single byte",
			bytes:    []byte("x"),
			complete: true,
			wantLen:  1,
			wantStr:  "literal{x, complete=true}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lit := NewLiteral(tt.bytes, tt.complete)

			if got := lit.Len(); got != tt.wantLen {
				t.Errorf("Len() = %d, want %d", got, tt.wantLen)
			}

			if got := lit.String(); got != tt.wantStr {
				t.Errorf("String() = %q, want %q", got, tt.wantStr)
			}

			if lit.Complete != tt.complete {
				t.Errorf("Complete = %v, want %v", lit.Complete, tt.complete)
			}
		})
	}
}

// TestSeqCreation tests NewSeq with various inputs
func TestSeqCreation(t *testing.T) {
	tests := []struct {
		name     string
		literals []Literal
		wantLen  int
		isEmpty  bool
		isFinite bool
	}{
		{
			name:     "empty sequence",
			literals: []Literal{},
			wantLen:  0,
			isEmpty:  true,
			isFinite: false,
		},
		{
			name: "single literal",
			literals: []Literal{
				NewLiteral([]byte("test"), true),
			},
			wantLen:  1,
			isEmpty:  false,
			isFinite: true,
		},
		{
			name: "multiple literals",
			literals: []Literal{
				NewLiteral([]byte("foo"), true),
				NewLiteral([]byte("bar"), true),
				NewLiteral([]byte("baz"), true),
			},
			wantLen:  3,
			isEmpty:  false,
			isFinite: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seq := NewSeq(tt.literals...)

			if got := seq.Len(); got != tt.wantLen {
				t.Errorf("Len() = %d, want %d", got, tt.wantLen)
			}

			if got := seq.IsEmpty(); got != tt.isEmpty {
				t.Errorf("IsEmpty() = %v, want %v", got, tt.isEmpty)
			}

			if got := seq.IsFinite(); got != tt.isFinite {
				t.Errorf("IsFinite() = %v, want %v", got, tt.isFinite)
			}
		})
	}
}

// TestSeqGet tests Get method
func TestSeqGet(t *testing.T) {
	seq := NewSeq(
		NewLiteral([]byte("first"), true),
		NewLiteral([]byte("second"), false),
		NewLiteral([]byte("third"), true),
	)

	tests := []struct {
		index        int
		wantBytes    string
		wantComplete bool
	}{
		{0, "first", true},
		{1, "second", false},
		{2, "third", true},
	}

	for _, tt := range tests {
		lit := seq.Get(tt.index)
		if string(lit.Bytes) != tt.wantBytes {
			t.Errorf("Get(%d).Bytes = %q, want %q", tt.index, lit.Bytes, tt.wantBytes)
		}
		if lit.Complete != tt.wantComplete {
			t.Errorf("Get(%d).Complete = %v, want %v", tt.index, lit.Complete, tt.wantComplete)
		}
	}
}

// TestSeqMinimize tests Minimize algorithm
func TestSeqMinimize(t *testing.T) {
	tests := []struct {
		name      string
		input     []Literal
		wantCount int
		wantBytes []string // expected remaining literals (order may vary due to sorting)
	}{
		{
			name: "prefix redundancy - foobar covered by foo",
			input: []Literal{
				NewLiteral([]byte("foo"), true),
				NewLiteral([]byte("foobar"), true),
			},
			wantCount: 1,
			wantBytes: []string{"foo"},
		},
		{
			name: "chain redundancy - a covers ab covers abc",
			input: []Literal{
				NewLiteral([]byte("a"), true),
				NewLiteral([]byte("ab"), true),
				NewLiteral([]byte("abc"), true),
			},
			wantCount: 1,
			wantBytes: []string{"a"},
		},
		{
			name: "no redundancy - different prefixes",
			input: []Literal{
				NewLiteral([]byte("hello"), true),
				NewLiteral([]byte("world"), true),
			},
			wantCount: 2,
			wantBytes: []string{"hello", "world"},
		},
		{
			name: "partial redundancy",
			input: []Literal{
				NewLiteral([]byte("test"), true),
				NewLiteral([]byte("testing"), true),
				NewLiteral([]byte("hello"), true),
			},
			wantCount: 2,
			wantBytes: []string{"test", "hello"},
		},
		{
			name:      "empty sequence",
			input:     []Literal{},
			wantCount: 0,
			wantBytes: []string{},
		},
		{
			name: "single literal",
			input: []Literal{
				NewLiteral([]byte("single"), true),
			},
			wantCount: 1,
			wantBytes: []string{"single"},
		},
		{
			name: "all same prefix",
			input: []Literal{
				NewLiteral([]byte("pre"), true),
				NewLiteral([]byte("prefix"), true),
				NewLiteral([]byte("prepare"), true),
			},
			wantCount: 1,
			wantBytes: []string{"pre"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seq := NewSeq(tt.input...)
			seq.Minimize()

			if got := seq.Len(); got != tt.wantCount {
				t.Errorf("Minimize() resulted in %d literals, want %d", got, tt.wantCount)
			}

			// Check that expected literals are present (order-independent)
			gotBytes := make(map[string]bool)
			for i := 0; i < seq.Len(); i++ {
				gotBytes[string(seq.Get(i).Bytes)] = true
			}

			for _, want := range tt.wantBytes {
				if !gotBytes[want] {
					t.Errorf("Minimize() missing expected literal %q", want)
				}
			}

			if len(gotBytes) != len(tt.wantBytes) {
				t.Errorf("Minimize() got %d unique literals, want %d", len(gotBytes), len(tt.wantBytes))
			}
		})
	}
}

// TestLongestCommonPrefix tests LCP algorithm
func TestLongestCommonPrefix(t *testing.T) {
	tests := []struct {
		name  string
		input []Literal
		want  string
	}{
		{
			name: "common prefix - he",
			input: []Literal{
				NewLiteral([]byte("hello"), true),
				NewLiteral([]byte("help"), true),
				NewLiteral([]byte("hero"), true),
			},
			want: "he",
		},
		{
			name: "no common prefix",
			input: []Literal{
				NewLiteral([]byte("abc"), true),
				NewLiteral([]byte("def"), true),
			},
			want: "",
		},
		{
			name: "one literal - returns itself",
			input: []Literal{
				NewLiteral([]byte("single"), true),
			},
			want: "single",
		},
		{
			name:  "empty sequence",
			input: []Literal{},
			want:  "",
		},
		{
			name: "identical literals",
			input: []Literal{
				NewLiteral([]byte("same"), true),
				NewLiteral([]byte("same"), true),
			},
			want: "same",
		},
		{
			name: "one empty literal",
			input: []Literal{
				NewLiteral([]byte("hello"), true),
				NewLiteral([]byte{}, true),
			},
			want: "",
		},
		{
			name: "varying lengths with common prefix",
			input: []Literal{
				NewLiteral([]byte("test"), true),
				NewLiteral([]byte("testing"), true),
				NewLiteral([]byte("tester"), true),
			},
			want: "test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seq := NewSeq(tt.input...)
			got := seq.LongestCommonPrefix()

			if string(got) != tt.want {
				t.Errorf("LongestCommonPrefix() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestLongestCommonSuffix tests LCS algorithm
func TestLongestCommonSuffix(t *testing.T) {
	tests := []struct {
		name  string
		input []Literal
		want  string
	}{
		{
			name: "common suffix - at",
			input: []Literal{
				NewLiteral([]byte("cat"), true),
				NewLiteral([]byte("bat"), true),
				NewLiteral([]byte("rat"), true),
			},
			want: "at",
		},
		{
			name: "no common suffix",
			input: []Literal{
				NewLiteral([]byte("abc"), true),
				NewLiteral([]byte("def"), true),
			},
			want: "",
		},
		{
			name: "one literal - returns itself",
			input: []Literal{
				NewLiteral([]byte("single"), true),
			},
			want: "single",
		},
		{
			name:  "empty sequence",
			input: []Literal{},
			want:  "",
		},
		{
			name: "identical literals",
			input: []Literal{
				NewLiteral([]byte("same"), true),
				NewLiteral([]byte("same"), true),
			},
			want: "same",
		},
		{
			name: "one empty literal",
			input: []Literal{
				NewLiteral([]byte("hello"), true),
				NewLiteral([]byte{}, true),
			},
			want: "",
		},
		{
			name: "varying lengths with common suffix",
			input: []Literal{
				NewLiteral([]byte("testing"), true),
				NewLiteral([]byte("running"), true),
				NewLiteral([]byte("jumping"), true),
			},
			want: "ing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seq := NewSeq(tt.input...)
			got := seq.LongestCommonSuffix()

			if string(got) != tt.want {
				t.Errorf("LongestCommonSuffix() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSeqClone tests deep copy functionality
func TestSeqClone(t *testing.T) {
	t.Run("clone independence", func(t *testing.T) {
		original := NewSeq(
			NewLiteral([]byte("test"), true),
			NewLiteral([]byte("hello"), false),
		)

		clone := original.Clone()

		// Verify deep copy - lengths match
		if original.Len() != clone.Len() {
			t.Errorf("Clone() length mismatch: original=%d, clone=%d", original.Len(), clone.Len())
		}

		// Modify clone's first literal
		clone.literals[0].Bytes[0] = 'X'
		clone.literals[0].Complete = !clone.literals[0].Complete

		// Verify original is unchanged
		if original.Get(0).Bytes[0] != 't' {
			t.Errorf("Modifying clone affected original bytes")
		}
		if original.Get(0).Complete != true {
			t.Errorf("Modifying clone affected original Complete flag")
		}

		// Verify clone was modified
		if clone.Get(0).Bytes[0] != 'X' {
			t.Errorf("Clone modification didn't work")
		}
	})

	t.Run("nil sequence", func(t *testing.T) {
		var seq *Seq
		clone := seq.Clone()
		if clone != nil {
			t.Errorf("Clone() of nil sequence should return nil, got %v", clone)
		}
	})

	t.Run("empty sequence", func(t *testing.T) {
		seq := NewSeq()
		clone := seq.Clone()
		if clone.Len() != 0 {
			t.Errorf("Clone() of empty sequence should have length 0, got %d", clone.Len())
		}
	})
}

// TestSeqMethods tests various Seq methods together
func TestSeqMethods(t *testing.T) {
	t.Run("nil sequence behavior", func(t *testing.T) {
		var seq *Seq

		if seq.Len() != 0 {
			t.Errorf("nil.Len() = %d, want 0", seq.Len())
		}

		if !seq.IsEmpty() {
			t.Errorf("nil.IsEmpty() = false, want true")
		}

		if seq.IsFinite() {
			t.Errorf("nil.IsFinite() = true, want false")
		}
	})

	t.Run("operations preserve invariants", func(t *testing.T) {
		seq := NewSeq(
			NewLiteral([]byte("foo"), true),
			NewLiteral([]byte("foobar"), true),
			NewLiteral([]byte("football"), true),
		)

		// After minimize, should have only "foo"
		seq.Minimize()
		if seq.Len() != 1 {
			t.Errorf("After Minimize(), Len() = %d, want 1", seq.Len())
		}

		if string(seq.Get(0).Bytes) != "foo" {
			t.Errorf("After Minimize(), Get(0) = %q, want %q", seq.Get(0).Bytes, "foo")
		}

		// LCP of single element is the element itself
		lcp := seq.LongestCommonPrefix()
		if string(lcp) != "foo" {
			t.Errorf("LCP of single element = %q, want %q", lcp, "foo")
		}
	})
}

// TestHelperFunctions tests internal helper functions
func TestHelperFunctions(t *testing.T) {
	t.Run("isPrefix", func(t *testing.T) {
		tests := []struct {
			prefix []byte
			s      []byte
			want   bool
		}{
			{[]byte("hel"), []byte("hello"), true},
			{[]byte("hello"), []byte("hello"), true},
			{[]byte("hello"), []byte("hel"), false},
			{[]byte("abc"), []byte("def"), false},
			{[]byte{}, []byte("test"), true},
			{[]byte("test"), []byte{}, false},
		}

		for _, tt := range tests {
			got := isPrefix(tt.prefix, tt.s)
			if got != tt.want {
				t.Errorf("isPrefix(%q, %q) = %v, want %v", tt.prefix, tt.s, got, tt.want)
			}
		}
	})

	t.Run("commonPrefix", func(t *testing.T) {
		tests := []struct {
			a    []byte
			b    []byte
			want []byte
		}{
			{[]byte("hello"), []byte("help"), []byte("hel")},
			{[]byte("abc"), []byte("def"), []byte{}},
			{[]byte("test"), []byte("test"), []byte("test")},
			{[]byte("short"), []byte("sh"), []byte("sh")},
			{[]byte{}, []byte("test"), []byte{}},
		}

		for _, tt := range tests {
			got := commonPrefix(tt.a, tt.b)
			if !bytes.Equal(got, tt.want) {
				t.Errorf("commonPrefix(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
			}
		}
	})

	t.Run("commonSuffix", func(t *testing.T) {
		tests := []struct {
			a    []byte
			b    []byte
			want []byte
		}{
			{[]byte("cat"), []byte("bat"), []byte("at")},
			{[]byte("abc"), []byte("def"), []byte{}},
			{[]byte("test"), []byte("test"), []byte("test")},
			{[]byte("testing"), []byte("ing"), []byte("ing")},
			{[]byte{}, []byte("test"), []byte{}},
		}

		for _, tt := range tests {
			got := commonSuffix(tt.a, tt.b)
			if !bytes.Equal(got, tt.want) {
				t.Errorf("commonSuffix(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
			}
		}
	})
}

// TestCrossForward tests CrossForward method directly
func TestCrossForward(t *testing.T) {
	t.Run("empty left x non-empty right", func(t *testing.T) {
		left := NewSeq()
		right := NewSeq(NewLiteral([]byte("x"), true))
		left.CrossForward(right)

		if !left.IsEmpty() {
			t.Errorf("expected empty result, got %d literals", left.Len())
		}
	})

	t.Run("non-empty left x empty right", func(t *testing.T) {
		left := NewSeq(
			NewLiteral([]byte("ab"), true),
			NewLiteral([]byte("cd"), false),
		)
		right := NewSeq()
		left.CrossForward(right)

		// When right is empty, left remains unchanged
		if left.Len() != 2 {
			t.Fatalf("expected 2 literals, got %d", left.Len())
		}
		if string(left.Get(0).Bytes) != "ab" {
			t.Errorf("literal 0: expected %q, got %q", "ab", left.Get(0).Bytes)
		}
		if string(left.Get(1).Bytes) != "cd" {
			t.Errorf("literal 1: expected %q, got %q", "cd", left.Get(1).Bytes)
		}
	})

	t.Run("single x single both complete", func(t *testing.T) {
		left := NewSeq(NewLiteral([]byte("ab"), true))
		right := NewSeq(NewLiteral([]byte("cd"), true))
		left.CrossForward(right)

		if left.Len() != 1 {
			t.Fatalf("expected 1 literal, got %d", left.Len())
		}
		if string(left.Get(0).Bytes) != "abcd" {
			t.Errorf("expected %q, got %q", "abcd", left.Get(0).Bytes)
		}
		if !left.Get(0).Complete {
			t.Errorf("expected complete=true, got false")
		}
	})

	t.Run("complete left x complete right", func(t *testing.T) {
		left := NewSeq(NewLiteral([]byte("ab"), true))
		right := NewSeq(NewLiteral([]byte("cd"), true))
		left.CrossForward(right)

		if left.Len() != 1 {
			t.Fatalf("expected 1 literal, got %d", left.Len())
		}
		if string(left.Get(0).Bytes) != "abcd" {
			t.Errorf("expected %q, got %q", "abcd", left.Get(0).Bytes)
		}
		if !left.Get(0).Complete {
			t.Errorf("expected complete=true when both sides complete")
		}
	})

	t.Run("incomplete left x complete right", func(t *testing.T) {
		// Inexact (Complete=false) literals are kept as-is, not extended
		left := NewSeq(NewLiteral([]byte("ab"), false))
		right := NewSeq(NewLiteral([]byte("cd"), true))
		left.CrossForward(right)

		if left.Len() != 1 {
			t.Fatalf("expected 1 literal, got %d", left.Len())
		}
		if string(left.Get(0).Bytes) != "ab" {
			t.Errorf("expected %q (unchanged), got %q", "ab", left.Get(0).Bytes)
		}
		if left.Get(0).Complete {
			t.Errorf("expected complete=false for inexact literal")
		}
	})

	t.Run("cross-product 2x2", func(t *testing.T) {
		left := NewSeq(
			NewLiteral([]byte("a"), true),
			NewLiteral([]byte("b"), true),
		)
		right := NewSeq(
			NewLiteral([]byte("c"), true),
			NewLiteral([]byte("d"), true),
		)
		left.CrossForward(right)

		if left.Len() != 4 {
			t.Fatalf("expected 4 literals, got %d", left.Len())
		}

		expected := map[string]bool{"ac": true, "ad": true, "bc": true, "bd": true}
		for i := 0; i < left.Len(); i++ {
			got := string(left.Get(i).Bytes)
			if !expected[got] {
				t.Errorf("unexpected literal %q at index %d", got, i)
			}
			if !left.Get(i).Complete {
				t.Errorf("literal %q should be complete", got)
			}
		}
	})

	t.Run("complete left x incomplete right propagates incomplete", func(t *testing.T) {
		left := NewSeq(NewLiteral([]byte("ab"), true))
		right := NewSeq(NewLiteral([]byte("cd"), false))
		left.CrossForward(right)

		if left.Len() != 1 {
			t.Fatalf("expected 1 literal, got %d", left.Len())
		}
		if string(left.Get(0).Bytes) != "abcd" {
			t.Errorf("expected %q, got %q", "abcd", left.Get(0).Bytes)
		}
		if left.Get(0).Complete {
			t.Errorf("expected complete=false when right is incomplete")
		}
	})
}

// TestKeepFirstBytes tests KeepFirstBytes method directly
func TestKeepFirstBytes(t *testing.T) {
	t.Run("n=0 is no-op", func(t *testing.T) {
		seq := NewSeq(
			NewLiteral([]byte("abc"), true),
			NewLiteral([]byte("def"), true),
		)
		seq.KeepFirstBytes(0)

		// n=0 triggers early return, no changes
		if seq.Len() != 2 {
			t.Fatalf("expected 2 literals, got %d", seq.Len())
		}
		if string(seq.Get(0).Bytes) != "abc" {
			t.Errorf("literal 0: expected %q, got %q", "abc", seq.Get(0).Bytes)
		}
	})

	t.Run("n=1 truncates to first byte", func(t *testing.T) {
		seq := NewSeq(
			NewLiteral([]byte("abc"), true),
			NewLiteral([]byte("def"), true),
		)
		seq.KeepFirstBytes(1)

		if seq.Len() != 2 {
			t.Fatalf("expected 2 literals, got %d", seq.Len())
		}
		if string(seq.Get(0).Bytes) != "a" {
			t.Errorf("literal 0: expected %q, got %q", "a", seq.Get(0).Bytes)
		}
		if seq.Get(0).Complete {
			t.Errorf("literal 0: expected complete=false after truncation")
		}
		if string(seq.Get(1).Bytes) != "d" {
			t.Errorf("literal 1: expected %q, got %q", "d", seq.Get(1).Bytes)
		}
		if seq.Get(1).Complete {
			t.Errorf("literal 1: expected complete=false after truncation")
		}
	})

	t.Run("n > all literal lengths is no-op", func(t *testing.T) {
		seq := NewSeq(
			NewLiteral([]byte("ab"), true),
			NewLiteral([]byte("cde"), true),
		)
		seq.KeepFirstBytes(10)

		if seq.Len() != 2 {
			t.Fatalf("expected 2 literals, got %d", seq.Len())
		}
		if string(seq.Get(0).Bytes) != "ab" {
			t.Errorf("literal 0: expected %q, got %q", "ab", seq.Get(0).Bytes)
		}
		if !seq.Get(0).Complete {
			t.Errorf("literal 0: expected complete=true (not truncated)")
		}
		if string(seq.Get(1).Bytes) != "cde" {
			t.Errorf("literal 1: expected %q, got %q", "cde", seq.Get(1).Bytes)
		}
		if !seq.Get(1).Complete {
			t.Errorf("literal 1: expected complete=true (not truncated)")
		}
	})

	t.Run("n=4 truncates long keeps short", func(t *testing.T) {
		seq := NewSeq(
			NewLiteral([]byte("ab"), true),        // 2 bytes, keep as-is
			NewLiteral([]byte("abcdef"), true),     // 6 bytes, truncate to 4
			NewLiteral([]byte("xyz"), true),         // 3 bytes, keep as-is
			NewLiteral([]byte("longstring"), false), // 10 bytes, truncate to 4
		)
		seq.KeepFirstBytes(4)

		if seq.Len() != 4 {
			t.Fatalf("expected 4 literals, got %d", seq.Len())
		}

		// "ab" (2 bytes) - unchanged
		if string(seq.Get(0).Bytes) != "ab" || !seq.Get(0).Complete {
			t.Errorf("literal 0: expected {%q, complete=true}, got {%q, complete=%v}",
				"ab", seq.Get(0).Bytes, seq.Get(0).Complete)
		}

		// "abcdef" -> "abcd" (truncated, marked incomplete)
		if string(seq.Get(1).Bytes) != "abcd" || seq.Get(1).Complete {
			t.Errorf("literal 1: expected {%q, complete=false}, got {%q, complete=%v}",
				"abcd", seq.Get(1).Bytes, seq.Get(1).Complete)
		}

		// "xyz" (3 bytes) - unchanged
		if string(seq.Get(2).Bytes) != "xyz" || !seq.Get(2).Complete {
			t.Errorf("literal 2: expected {%q, complete=true}, got {%q, complete=%v}",
				"xyz", seq.Get(2).Bytes, seq.Get(2).Complete)
		}

		// "longstring" -> "long" (truncated, was already incomplete)
		if string(seq.Get(3).Bytes) != "long" || seq.Get(3).Complete {
			t.Errorf("literal 3: expected {%q, complete=false}, got {%q, complete=%v}",
				"long", seq.Get(3).Bytes, seq.Get(3).Complete)
		}
	})
}

// TestDedupCompleteFlag tests that Dedup keeps the first occurrence's Complete flag
func TestDedupCompleteFlag(t *testing.T) {
	t.Run("complete then incomplete keeps complete", func(t *testing.T) {
		seq := NewSeq(
			NewLiteral([]byte("abc"), true),
			NewLiteral([]byte("abc"), false),
		)
		seq.Dedup()

		if seq.Len() != 1 {
			t.Fatalf("expected 1 literal after dedup, got %d", seq.Len())
		}
		if string(seq.Get(0).Bytes) != "abc" {
			t.Errorf("expected %q, got %q", "abc", seq.Get(0).Bytes)
		}
		if !seq.Get(0).Complete {
			t.Errorf("expected complete=true (first occurrence was complete)")
		}
	})

	t.Run("incomplete then complete keeps incomplete", func(t *testing.T) {
		seq := NewSeq(
			NewLiteral([]byte("abc"), false),
			NewLiteral([]byte("abc"), true),
		)
		seq.Dedup()

		if seq.Len() != 1 {
			t.Fatalf("expected 1 literal after dedup, got %d", seq.Len())
		}
		if string(seq.Get(0).Bytes) != "abc" {
			t.Errorf("expected %q, got %q", "abc", seq.Get(0).Bytes)
		}
		if seq.Get(0).Complete {
			t.Errorf("expected complete=false (first occurrence was incomplete)")
		}
	})

	t.Run("no duplicates unchanged", func(t *testing.T) {
		seq := NewSeq(
			NewLiteral([]byte("abc"), true),
			NewLiteral([]byte("def"), false),
			NewLiteral([]byte("ghi"), true),
		)
		seq.Dedup()

		if seq.Len() != 3 {
			t.Fatalf("expected 3 literals (no duplicates), got %d", seq.Len())
		}
		if string(seq.Get(0).Bytes) != "abc" || !seq.Get(0).Complete {
			t.Errorf("literal 0 changed unexpectedly")
		}
		if string(seq.Get(1).Bytes) != "def" || seq.Get(1).Complete {
			t.Errorf("literal 1 changed unexpectedly")
		}
		if string(seq.Get(2).Bytes) != "ghi" || !seq.Get(2).Complete {
			t.Errorf("literal 2 changed unexpectedly")
		}
	})
}

// Benchmarks

func BenchmarkMinimize(b *testing.B) {
	b.ReportAllocs()

	// Worst case: many literals, all different (no redundancy)
	literals := make([]Literal, 100)
	for i := 0; i < 100; i++ {
		literals[i] = NewLiteral([]byte{byte(i), byte(i + 1)}, true)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		seq := NewSeq(literals...)
		seq.Minimize()
	}
}

func BenchmarkLongestCommonPrefix(b *testing.B) {
	b.ReportAllocs()

	seq := NewSeq(
		NewLiteral([]byte("hello_world_test_1"), true),
		NewLiteral([]byte("hello_world_test_2"), true),
		NewLiteral([]byte("hello_world_test_3"), true),
		NewLiteral([]byte("hello_world_test_4"), true),
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = seq.LongestCommonPrefix()
	}
}

func BenchmarkLongestCommonSuffix(b *testing.B) {
	b.ReportAllocs()

	seq := NewSeq(
		NewLiteral([]byte("testing_suffix"), true),
		NewLiteral([]byte("running_suffix"), true),
		NewLiteral([]byte("jumping_suffix"), true),
		NewLiteral([]byte("walking_suffix"), true),
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = seq.LongestCommonSuffix()
	}
}

func BenchmarkClone(b *testing.B) {
	b.ReportAllocs()

	seq := NewSeq(
		NewLiteral([]byte("literal_one"), true),
		NewLiteral([]byte("literal_two"), false),
		NewLiteral([]byte("literal_three"), true),
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = seq.Clone()
	}
}
