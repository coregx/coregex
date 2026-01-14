package meta

import (
	"regexp"
	"testing"
)

// BranchDispatch benchmarks: anchored alternations like ^(\d+|UUID|hex32)
// These patterns benefit from O(1) first-byte dispatch optimization.

var branchDispatchCases = []struct {
	name  string
	input string
}{
	{"Digits", "12345"},
	{"UUID", "UUID-1234-5678"},
	{"Hex32", "hex32abcdef"},
	{"NoMatch", "xyz_no_match"},
}

func BenchmarkBranchDispatch_Stdlib(b *testing.B) {
	re := regexp.MustCompile(`^(\d+|UUID|hex32)`)
	for _, tc := range branchDispatchCases {
		b.Run(tc.name, func(b *testing.B) {
			input := []byte(tc.input)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				re.Match(input)
			}
		})
	}
}

func BenchmarkBranchDispatch_Coregex(b *testing.B) {
	re, err := Compile(`^(\d+|UUID|hex32)`)
	if err != nil {
		b.Fatal(err)
	}
	for _, tc := range branchDispatchCases {
		b.Run(tc.name, func(b *testing.B) {
			input := []byte(tc.input)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				re.IsMatch(input)
			}
		})
	}
}

// Additional anchored alternation patterns

func BenchmarkAnchoredAlt_ManyBranches_Stdlib(b *testing.B) {
	// Pattern with 5 distinct branches
	re := regexp.MustCompile(`^(GET|POST|PUT|DELETE|PATCH)`)
	inputs := []struct {
		name  string
		input []byte
	}{
		{"GET", []byte("GET /api/users")},
		{"POST", []byte("POST /api/users")},
		{"DELETE", []byte("DELETE /api/users/1")},
		{"NoMatch", []byte("OPTIONS /api")},
	}
	for _, tc := range inputs {
		b.Run(tc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				re.Match(tc.input)
			}
		})
	}
}

func BenchmarkAnchoredAlt_ManyBranches_Coregex(b *testing.B) {
	re, err := Compile(`^(GET|POST|PUT|DELETE|PATCH)`)
	if err != nil {
		b.Fatal(err)
	}
	inputs := []struct {
		name  string
		input []byte
	}{
		{"GET", []byte("GET /api/users")},
		{"POST", []byte("POST /api/users")},
		{"DELETE", []byte("DELETE /api/users/1")},
		{"NoMatch", []byte("OPTIONS /api")},
	}
	for _, tc := range inputs {
		b.Run(tc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				re.IsMatch(tc.input)
			}
		})
	}
}

// FirstByte prefilter benchmark (O(1) rejection)

func BenchmarkFirstByteReject_Stdlib(b *testing.B) {
	re := regexp.MustCompile(`^\d`)
	input := []byte("no digits here")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		re.Match(input)
	}
}

func BenchmarkFirstByteReject_Coregex(b *testing.B) {
	re, err := Compile(`^\d`)
	if err != nil {
		b.Fatal(err)
	}
	input := []byte("no digits here")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		re.IsMatch(input)
	}
}
