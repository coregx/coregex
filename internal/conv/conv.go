// Package conv provides safe integer conversion helpers for the regex engine.
//
// These functions perform bounds checking before narrowing integer conversions
// to prevent silent overflow. They panic on overflow since this indicates a
// programming error (e.g., regex pattern too large for internal limits).
package conv

import "math"

// IntToUint32 safely converts an int to uint32.
// Panics if n < 0 or n > math.MaxUint32.
//
//go:inline
func IntToUint32(n int) uint32 {
	// Use uint for comparison to avoid overflow on 32-bit platforms
	// where int cannot represent math.MaxUint32
	if n < 0 || uint(n) > math.MaxUint32 {
		panic("integer overflow: int value out of uint32 range")
	}
	return uint32(n)
}

// IntToUint16 safely converts an int to uint16.
// Panics if n < 0 or n > math.MaxUint16.
//
//go:inline
func IntToUint16(n int) uint16 {
	if n < 0 || n > math.MaxUint16 {
		panic("integer overflow: int value out of uint16 range")
	}
	return uint16(n)
}

// Uint64ToUint32 safely converts a uint64 to uint32.
// Panics if n > math.MaxUint32.
//
//go:inline
func Uint64ToUint32(n uint64) uint32 {
	if n > math.MaxUint32 {
		panic("integer overflow: uint64 value out of uint32 range")
	}
	return uint32(n)
}

// Uint64ToUint16 safely converts a uint64 to uint16.
// Panics if n > math.MaxUint16.
//
//go:inline
func Uint64ToUint16(n uint64) uint16 {
	if n > math.MaxUint16 {
		panic("integer overflow: uint64 value out of uint16 range")
	}
	return uint16(n)
}
