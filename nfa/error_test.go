package nfa

import (
	"errors"
	"testing"
)

func TestCompileError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *CompileError
		wantSub  string
		wantFull string
	}{
		{
			name:     "with pattern",
			err:      &CompileError{Pattern: `[a-z]+`, Err: ErrTooComplex},
			wantSub:  `[a-z]+`,
			wantFull: `NFA compilation failed for pattern "[a-z]+": pattern too complex`,
		},
		{
			name:     "empty pattern",
			err:      &CompileError{Pattern: "", Err: ErrCompilation},
			wantSub:  "NFA compilation failed:",
			wantFull: "NFA compilation failed: NFA compilation failed",
		},
		{
			name:     "with wrapped sentinel",
			err:      &CompileError{Pattern: "abc", Err: ErrInvalidPattern},
			wantSub:  `"abc"`,
			wantFull: `NFA compilation failed for pattern "abc": invalid regex pattern`,
		},
		{
			name:     "nil inner error",
			err:      &CompileError{Pattern: "x", Err: nil},
			wantSub:  `NFA compilation failed for pattern "x": <nil>`,
			wantFull: `NFA compilation failed for pattern "x": <nil>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.wantFull {
				t.Errorf("Error() = %q, want %q", got, tt.wantFull)
			}
		})
	}
}

func TestCompileError_Unwrap(t *testing.T) {
	tests := []struct {
		name    string
		err     *CompileError
		wantErr error
	}{
		{
			name:    "unwrap ErrTooComplex",
			err:     &CompileError{Pattern: "a+", Err: ErrTooComplex},
			wantErr: ErrTooComplex,
		},
		{
			name:    "unwrap ErrInvalidPattern",
			err:     &CompileError{Pattern: "[", Err: ErrInvalidPattern},
			wantErr: ErrInvalidPattern,
		},
		{
			name:    "unwrap nil",
			err:     &CompileError{Pattern: "x", Err: nil},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Unwrap()
			if !errors.Is(got, tt.wantErr) {
				t.Errorf("Unwrap() = %v, want %v", got, tt.wantErr)
			}
		})
	}
}

func TestCompileError_ErrorsIs(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		target error
		want   bool
	}{
		{
			name:   "Is ErrTooComplex via CompileError",
			err:    &CompileError{Pattern: "a{999}", Err: ErrTooComplex},
			target: ErrTooComplex,
			want:   true,
		},
		{
			name:   "Is ErrInvalidPattern via CompileError",
			err:    &CompileError{Pattern: "[", Err: ErrInvalidPattern},
			target: ErrInvalidPattern,
			want:   true,
		},
		{
			name:   "Is ErrCompilation - not matching",
			err:    &CompileError{Pattern: "a", Err: ErrInvalidPattern},
			target: ErrCompilation,
			want:   false,
		},
		{
			name:   "Is ErrNoMatch - not matching",
			err:    &CompileError{Pattern: "a", Err: ErrTooComplex},
			target: ErrNoMatch,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errors.Is(tt.err, tt.target)
			if got != tt.want {
				t.Errorf("errors.Is() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompileError_ErrorsAs(t *testing.T) {
	err := error(&CompileError{Pattern: `\d+`, Err: ErrTooComplex})

	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Fatal("errors.As failed to extract CompileError")
	}
	if ce.Pattern != `\d+` {
		t.Errorf("Pattern = %q, want %q", ce.Pattern, `\d+`)
	}
	if !errors.Is(ce.Err, ErrTooComplex) {
		t.Errorf("Err = %v, want %v", ce.Err, ErrTooComplex)
	}
}

func TestBuildError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *BuildError
		wantFull string
	}{
		{
			name:     "with valid state ID",
			err:      &BuildError{Message: "invalid next state 99", StateID: StateID(5)},
			wantFull: "NFA build error at state 5: invalid next state 99",
		},
		{
			name:     "with InvalidState",
			err:      &BuildError{Message: "anchored start state not set", StateID: InvalidState},
			wantFull: "NFA build error: anchored start state not set",
		},
		{
			name:     "with state ID 0",
			err:      &BuildError{Message: "some issue", StateID: StateID(0)},
			wantFull: "NFA build error at state 0: some issue",
		},
		{
			name:     "with large state ID",
			err:      &BuildError{Message: "out of bounds", StateID: StateID(1000)},
			wantFull: "NFA build error at state 1000: out of bounds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.wantFull {
				t.Errorf("Error() = %q, want %q", got, tt.wantFull)
			}
		})
	}
}

func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"ErrInvalidState", ErrInvalidState, "invalid NFA state"},
		{"ErrInvalidPattern", ErrInvalidPattern, "invalid regex pattern"},
		{"ErrTooComplex", ErrTooComplex, "pattern too complex"},
		{"ErrCompilation", ErrCompilation, "NFA compilation failed"},
		{"ErrInvalidConfig", ErrInvalidConfig, "invalid NFA configuration"},
		{"ErrNoMatch", ErrNoMatch, "no match found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSentinelErrors_AreDistinct(t *testing.T) {
	sentinels := []error{
		ErrInvalidState,
		ErrInvalidPattern,
		ErrTooComplex,
		ErrCompilation,
		ErrInvalidConfig,
		ErrNoMatch,
	}

	for i, a := range sentinels {
		for j, b := range sentinels {
			if i == j {
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("sentinel errors %q and %q should be distinct but errors.Is returned true", a, b)
			}
		}
	}
}
