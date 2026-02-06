package lazy

import "fmt"

// Error types for Lazy DFA operations

// ErrCacheFull indicates that the DFA state cache has exceeded its maximum size
// AND the maximum number of cache clears has been reached.
// When this occurs, the DFA falls back to NFA execution (PikeVM) for the
// remainder of the search.
//
// This is not a fatal error - it's an expected condition when matching complex
// patterns that generate many states.
var ErrCacheFull = &DFAError{
	Kind:    CacheFull,
	Message: "DFA state cache is full",
}

// errCacheCleared is an internal sentinel error returned by determinize()
// when the cache was cleared and rebuilt. The search loop must re-obtain
// the current state from the start state at the current position and continue.
//
// This is NOT a real error - it signals that the search should restart
// DFA processing from the current position with a fresh cache.
var errCacheCleared = &DFAError{
	Kind:    CacheCleared,
	Message: "DFA cache was cleared and rebuilt",
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
	// and cannot be cleared further (max clears exceeded)
	CacheFull ErrorKind = iota

	// CacheCleared indicates the cache was cleared and rebuilt.
	// This is an internal signal, not a real error. The search loop
	// should re-obtain the current state and continue from the current position.
	CacheCleared

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
	case CacheCleared:
		return "CacheCleared"
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
