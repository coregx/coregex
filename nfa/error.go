// Package nfa provides a Thompson NFA (Non-deterministic Finite Automaton)
// implementation for regex matching.
//
// This package implements the core Thompson NFA algorithm along with a PikeVM
// execution engine. The NFA is compiled from regexp/syntax.Regexp patterns and
// can be used for matching with full support for capturing groups (future).
package nfa

import (
	"errors"
	"fmt"
)

// Common NFA errors
var (
	// ErrInvalidState indicates an invalid NFA state ID was encountered
	ErrInvalidState = errors.New("invalid NFA state")

	// ErrInvalidPattern indicates the regex pattern is invalid or unsupported
	ErrInvalidPattern = errors.New("invalid regex pattern")

	// ErrTooComplex indicates the pattern is too complex to compile
	ErrTooComplex = errors.New("pattern too complex")

	// ErrCompilation indicates a general NFA compilation failure
	ErrCompilation = errors.New("NFA compilation failed")

	// ErrInvalidConfig indicates invalid configuration was provided
	ErrInvalidConfig = errors.New("invalid NFA configuration")

	// ErrNoMatch indicates no match was found (not an error, used internally)
	ErrNoMatch = errors.New("no match found")
)

// CompileError wraps compilation errors with additional context
type CompileError struct {
	Pattern string
	Err     error
}

// Error implements the error interface
func (e *CompileError) Error() string {
	if e.Pattern != "" {
		return fmt.Sprintf("NFA compilation failed for pattern %q: %v", e.Pattern, e.Err)
	}
	return fmt.Sprintf("NFA compilation failed: %v", e.Err)
}

// Unwrap returns the underlying error
func (e *CompileError) Unwrap() error {
	return e.Err
}

// BuildError represents an error during NFA construction via the Builder API
type BuildError struct {
	Message string
	StateID StateID
}

// Error implements the error interface
func (e *BuildError) Error() string {
	if e.StateID != InvalidState {
		return fmt.Sprintf("NFA build error at state %d: %s", e.StateID, e.Message)
	}
	return fmt.Sprintf("NFA build error: %s", e.Message)
}
