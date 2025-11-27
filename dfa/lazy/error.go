package lazy

import "fmt"

// Error types for Lazy DFA operations

// ErrCacheFull indicates that the DFA state cache has exceeded its maximum size.
// When this occurs, the DFA falls back to NFA execution (PikeVM) for the
// remainder of the search.
//
// This is not a fatal error - it's an expected condition when matching complex
// patterns that generate many states.
var ErrCacheFull = &DFAError{
	Kind:    CacheFull,
	Message: "DFA state cache is full",
}

// ErrStateLimitExceeded indicates that the DFA has reached the maximum number
// of allowed states during determinization.
//
// This prevents unbounded memory growth for pathological patterns.
var ErrStateLimitExceeded = &DFAError{
	Kind:    StateLimitExceeded,
	Message: "DFA state limit exceeded",
}

// ErrInvalidConfig indicates that the provided configuration is invalid.
// This is typically caught during DFA construction.
var ErrInvalidConfig = &DFAError{
	Kind:    InvalidConfig,
	Message: "invalid DFA configuration",
}

// ErrorKind classifies DFA errors into categories
type ErrorKind uint8

const (
	// CacheFull indicates the state cache reached its size limit
	CacheFull ErrorKind = iota

	// StateLimitExceeded indicates too many states were created
	StateLimitExceeded

	// InvalidConfig indicates configuration validation failed
	InvalidConfig

	// NFAFallback indicates DFA gave up and fell back to NFA
	// (not an error per se, but tracked for metrics)
	NFAFallback
)

// String returns a human-readable error kind name
func (k ErrorKind) String() string {
	switch k {
	case CacheFull:
		return "CacheFull"
	case StateLimitExceeded:
		return "StateLimitExceeded"
	case InvalidConfig:
		return "InvalidConfig"
	case NFAFallback:
		return "NFAFallback"
	default:
		return fmt.Sprintf("UnknownErrorKind(%d)", k)
	}
}

// DFAError represents an error that occurred during DFA operations
type DFAError struct {
	Kind    ErrorKind
	Message string
	Cause   error // Optional underlying error
}

// Error implements the error interface
func (e *DFAError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

// Unwrap returns the underlying error (for errors.Is/As)
func (e *DFAError) Unwrap() error {
	return e.Cause
}

// Is implements error comparison for errors.Is
func (e *DFAError) Is(target error) bool {
	t, ok := target.(*DFAError)
	if !ok {
		return false
	}
	return e.Kind == t.Kind
}
