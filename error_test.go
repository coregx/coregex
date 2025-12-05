package coregex

import (
	"regexp"
	"strings"
	"testing"

	"github.com/coregx/coregex/meta"
)

// TestErrorMessagePrefix verifies that error messages use "regexp:" prefix
// for stdlib compatibility (issue #13).
func TestErrorMessagePrefix(t *testing.T) {
	tests := []struct {
		name           string
		pattern        string
		wantPrefix     string
		wantSubstrings []string
	}{
		{
			name:           "invalid bracket expression",
			pattern:        "[invalid",
			wantPrefix:     "regexp:",
			wantSubstrings: []string{"regexp:", "error parsing regexp"},
		},
		{
			name:           "invalid escape sequence",
			pattern:        `\`,
			wantPrefix:     "regexp:",
			wantSubstrings: []string{"regexp:"},
		},
		{
			name:           "unmatched parenthesis",
			pattern:        "(abc",
			wantPrefix:     "regexp:",
			wantSubstrings: []string{"regexp:"},
		},
		{
			name:           "invalid repetition",
			pattern:        "*abc",
			wantPrefix:     "regexp:",
			wantSubstrings: []string{"regexp:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Compile(tt.pattern)
			if err == nil {
				t.Fatalf("Compile(%q) expected error, got nil", tt.pattern)
			}

			errMsg := err.Error()

			// Check prefix
			if !strings.HasPrefix(errMsg, tt.wantPrefix) {
				t.Errorf("error message should start with %q, got: %s", tt.wantPrefix, errMsg)
			}

			// Check substrings
			for _, sub := range tt.wantSubstrings {
				if !strings.Contains(errMsg, sub) {
					t.Errorf("error message should contain %q, got: %s", sub, errMsg)
				}
			}
		})
	}
}

// TestMustCompilePanicFormat verifies MustCompile panic message matches stdlib format.
func TestMustCompilePanicFormat(t *testing.T) {
	pattern := "[invalid"

	// Get stdlib panic format for comparison
	var stdlibPanic string
	func() {
		defer func() {
			if r := recover(); r != nil {
				stdlibPanic = r.(string)
			}
		}()
		regexp.MustCompile(pattern)
	}()

	// Get our panic format
	var ourPanic string
	func() {
		defer func() {
			if r := recover(); r != nil {
				ourPanic = r.(string)
			}
		}()
		MustCompile(pattern)
	}()

	// Both should start with "regexp: Compile(`"
	wantPrefix := "regexp: Compile(`"
	if !strings.HasPrefix(stdlibPanic, wantPrefix) {
		t.Logf("Note: stdlib panic format: %s", stdlibPanic)
	}

	if !strings.HasPrefix(ourPanic, wantPrefix) {
		t.Errorf("MustCompile panic should start with %q, got: %s", wantPrefix, ourPanic)
	}

	// Should contain the pattern in backticks
	if !strings.Contains(ourPanic, "`"+pattern+"`") {
		t.Errorf("MustCompile panic should contain pattern in backticks, got: %s", ourPanic)
	}
}

// TestConfigErrorPrefix verifies config errors use "regexp:" prefix.
func TestConfigErrorPrefix(t *testing.T) {
	// Create invalid config
	config := meta.DefaultConfig()
	config.MaxDFAStates = 0 // Invalid: must be > 0

	_, err := CompileWithConfig("abc", config)
	if err == nil {
		t.Fatal("expected error for invalid config")
	}

	errMsg := err.Error()
	if !strings.HasPrefix(errMsg, "regexp:") {
		t.Errorf("config error should start with 'regexp:', got: %s", errMsg)
	}
}

// TestCompileErrorVsStdlib compares error behavior with stdlib.
func TestCompileErrorVsStdlib(t *testing.T) {
	invalidPatterns := []string{
		"[",
		"(",
		"*",
		`\`,
		"(?P<>abc)", // empty capture name
	}

	for _, pattern := range invalidPatterns {
		t.Run(pattern, func(t *testing.T) {
			_, stdlibErr := regexp.Compile(pattern)
			_, ourErr := Compile(pattern)

			// Both should error
			if stdlibErr == nil && ourErr == nil {
				return // Both accept it, OK
			}

			if stdlibErr != nil && ourErr == nil {
				t.Errorf("stdlib rejects %q but we accept it", pattern)
			}

			// Our error should use regexp: prefix like stdlib
			if ourErr != nil {
				if !strings.HasPrefix(ourErr.Error(), "regexp:") {
					t.Errorf("our error should use 'regexp:' prefix, got: %s", ourErr.Error())
				}
			}
		})
	}
}
