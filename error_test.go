package coregex

import (
	"regexp"
	"strings"
	"testing"

	"github.com/coregx/coregex/meta"
)

// TestErrorMessageFormat verifies that error messages match stdlib format.
// stdlib returns *syntax.Error directly with format: "error parsing regexp: ..."
func TestErrorMessageFormat(t *testing.T) {
	patterns := []string{
		"[invalid",
		`\`,
		"(abc",
		"*abc",
		`\8`,
	}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			_, stdlibErr := regexp.Compile(pattern)
			_, ourErr := Compile(pattern)

			if stdlibErr == nil {
				t.Skip("stdlib accepts this pattern")
			}

			if ourErr == nil {
				t.Fatalf("Compile(%q) expected error, got nil", pattern)
			}

			// Error messages should match exactly
			if ourErr.Error() != stdlibErr.Error() {
				t.Errorf("error message mismatch:\n  got:  %q\n  want: %q",
					ourErr.Error(), stdlibErr.Error())
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

			// Our error should match stdlib format exactly
			if ourErr != nil && stdlibErr != nil {
				if ourErr.Error() != stdlibErr.Error() {
					t.Errorf("error message mismatch:\n  got:  %q\n  want: %q",
						ourErr.Error(), stdlibErr.Error())
				}
			}
		})
	}
}
