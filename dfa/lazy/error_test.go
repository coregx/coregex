package lazy

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorKindString(t *testing.T) {
	tests := []struct {
		name string
		kind ErrorKind
		want string
	}{
		{name: "CacheFull", kind: CacheFull, want: "CacheFull"},
		{name: "CacheCleared", kind: CacheCleared, want: "CacheCleared"},
		{name: "StateLimitExceeded", kind: StateLimitExceeded, want: "StateLimitExceeded"},
		{name: "InvalidConfig", kind: InvalidConfig, want: "InvalidConfig"},
		{name: "NFAFallback", kind: NFAFallback, want: "NFAFallback"},
		{name: "unknown error kind 99", kind: ErrorKind(99), want: "UnknownErrorKind(99)"},
		{name: "unknown error kind 255", kind: ErrorKind(255), want: "UnknownErrorKind(255)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.kind.String()
			if got != tt.want {
				t.Errorf("ErrorKind(%d).String() = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestDFAErrorError(t *testing.T) {
	tests := []struct {
		name string
		err  *DFAError
		want string
	}{
		{
			name: "without cause",
			err:  &DFAError{Kind: CacheFull, Message: "cache is full"},
			want: "cache is full",
		},
		{
			name: "with cause",
			err:  &DFAError{Kind: InvalidConfig, Message: "invalid config", Cause: fmt.Errorf("max states is zero")},
			want: "invalid config: max states is zero",
		},
		{
			name: "sentinel ErrCacheFull",
			err:  ErrCacheFull,
			want: "DFA state cache is full",
		},
		{
			name: "sentinel errCacheCleared",
			err:  errCacheCleared,
			want: "DFA cache was cleared and rebuilt",
		},
		{
			name: "sentinel ErrStateLimitExceeded",
			err:  ErrStateLimitExceeded,
			want: "DFA state limit exceeded",
		},
		{
			name: "sentinel ErrInvalidConfig",
			err:  ErrInvalidConfig,
			want: "invalid DFA configuration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.want {
				t.Errorf("DFAError.Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDFAErrorUnwrap(t *testing.T) {
	tests := []struct {
		name      string
		err       *DFAError
		wantCause error
	}{
		{
			name:      "nil cause",
			err:       &DFAError{Kind: CacheFull, Message: "full"},
			wantCause: nil,
		},
		{
			name:      "with cause",
			err:       &DFAError{Kind: InvalidConfig, Message: "bad", Cause: fmt.Errorf("underlying")},
			wantCause: fmt.Errorf("underlying"),
		},
		{
			name:      "sentinel has no cause",
			err:       ErrCacheFull,
			wantCause: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Unwrap()
			if tt.wantCause == nil {
				if got != nil {
					t.Errorf("Unwrap() = %v, want nil", got)
				}
			} else {
				if got == nil {
					t.Errorf("Unwrap() = nil, want %v", tt.wantCause)
				} else if got.Error() != tt.wantCause.Error() {
					t.Errorf("Unwrap().Error() = %q, want %q", got.Error(), tt.wantCause.Error())
				}
			}
		})
	}
}

func TestDFAErrorIs(t *testing.T) {
	tests := []struct {
		name   string
		err    *DFAError
		target error
		want   bool
	}{
		{
			name:   "same kind matches",
			err:    &DFAError{Kind: CacheFull, Message: "custom message"},
			target: ErrCacheFull,
			want:   true,
		},
		{
			name:   "different kind does not match",
			err:    &DFAError{Kind: CacheFull, Message: "cache full"},
			target: ErrStateLimitExceeded,
			want:   false,
		},
		{
			name:   "non-DFAError target returns false",
			err:    ErrCacheFull,
			target: fmt.Errorf("not a DFA error"),
			want:   false,
		},
		{
			name:   "sentinel matches itself",
			err:    ErrCacheFull,
			target: ErrCacheFull,
			want:   true,
		},
		{
			name:   "CacheCleared kind matches",
			err:    errCacheCleared,
			target: &DFAError{Kind: CacheCleared, Message: "different message"},
			want:   true,
		},
		{
			name:   "InvalidConfig kind matches",
			err:    ErrInvalidConfig,
			target: &DFAError{Kind: InvalidConfig, Message: "other"},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Is(tt.target)
			if got != tt.want {
				t.Errorf("DFAError.Is(%v) = %v, want %v", tt.target, got, tt.want)
			}
		})
	}
}

func TestErrorsIsCompatibility(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		target error
		want   bool
	}{
		{
			name:   "errors.Is with same kind",
			err:    &DFAError{Kind: CacheFull, Message: "custom"},
			target: ErrCacheFull,
			want:   true,
		},
		{
			name:   "errors.Is with different kind",
			err:    &DFAError{Kind: StateLimitExceeded, Message: "limit"},
			target: ErrCacheFull,
			want:   false,
		},
		{
			name:   "errors.Is with sentinel",
			err:    ErrStateLimitExceeded,
			target: ErrStateLimitExceeded,
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errors.Is(tt.err, tt.target)
			if got != tt.want {
				t.Errorf("errors.Is(%v, %v) = %v, want %v", tt.err, tt.target, got, tt.want)
			}
		})
	}
}

func TestErrorsAsCompatibility(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantAs   bool
		wantKind ErrorKind
	}{
		{
			name:     "errors.As on ErrCacheFull",
			err:      ErrCacheFull,
			wantAs:   true,
			wantKind: CacheFull,
		},
		{
			name:     "errors.As on ErrStateLimitExceeded",
			err:      ErrStateLimitExceeded,
			wantAs:   true,
			wantKind: StateLimitExceeded,
		},
		{
			name:     "errors.As on wrapped DFAError",
			err:      fmt.Errorf("wrapped: %w", ErrInvalidConfig),
			wantAs:   true,
			wantKind: InvalidConfig,
		},
		{
			name:   "errors.As on non-DFA error",
			err:    fmt.Errorf("plain error"),
			wantAs: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dfaErr *DFAError
			got := errors.As(tt.err, &dfaErr)
			if got != tt.wantAs {
				t.Errorf("errors.As() = %v, want %v", got, tt.wantAs)
			}
			if got && dfaErr.Kind != tt.wantKind {
				t.Errorf("errors.As() kind = %v, want %v", dfaErr.Kind, tt.wantKind)
			}
		})
	}
}

func TestDFAErrorWithCauseChain(t *testing.T) {
	// Build a chain: DFAError wrapping another DFAError
	inner := &DFAError{Kind: CacheFull, Message: "inner cache full"}
	outer := &DFAError{Kind: NFAFallback, Message: "falling back to NFA", Cause: inner}

	// Unwrap should return inner
	unwrapped := outer.Unwrap()
	if unwrapped != inner {
		t.Errorf("Unwrap() should return inner error")
	}

	// errors.Is should find inner through Unwrap chain
	if !errors.Is(outer, inner) {
		t.Error("errors.Is(outer, inner) should be true through Unwrap chain")
	}

	// errors.Is should match by kind
	if !errors.Is(outer, &DFAError{Kind: NFAFallback}) {
		t.Error("errors.Is should match outer by kind NFAFallback")
	}

	// Error message should include cause
	want := "falling back to NFA: inner cache full"
	if got := outer.Error(); got != want {
		t.Errorf("outer.Error() = %q, want %q", got, want)
	}
}
