package meta

import (
	"errors"
	"testing"
)

// TestDefaultConfigValues verifies DefaultConfig returns expected field values.
func TestDefaultConfigValues(t *testing.T) {
	c := DefaultConfig()

	if !c.EnableDFA {
		t.Error("EnableDFA should be true by default")
	}
	if !c.EnablePrefilter {
		t.Error("EnablePrefilter should be true by default")
	}
	if c.MaxDFAStates != 10000 {
		t.Errorf("MaxDFAStates = %d, want 10000", c.MaxDFAStates)
	}
	if c.DeterminizationLimit != 1000 {
		t.Errorf("DeterminizationLimit = %d, want 1000", c.DeterminizationLimit)
	}
	if c.MinLiteralLen != 1 {
		t.Errorf("MinLiteralLen = %d, want 1", c.MinLiteralLen)
	}
	if c.MaxLiterals != 256 {
		t.Errorf("MaxLiterals = %d, want 256", c.MaxLiterals)
	}
	if c.MaxRecursionDepth != 100 {
		t.Errorf("MaxRecursionDepth = %d, want 100", c.MaxRecursionDepth)
	}
	if !c.EnableASCIIOptimization {
		t.Error("EnableASCIIOptimization should be true by default")
	}
}

// TestDefaultConfigPassesValidation verifies DefaultConfig always validates.
func TestDefaultConfigPassesValidation(t *testing.T) {
	c := DefaultConfig()
	if err := c.Validate(); err != nil {
		t.Errorf("DefaultConfig().Validate() = %v, want nil", err)
	}
}

// TestConfigValidateMaxDFAStates tests MaxDFAStates validation boundaries.
func TestConfigValidateMaxDFAStates(t *testing.T) {
	tests := []struct {
		name         string
		maxDFAStates uint32
		wantErr      bool
		wantField    string
	}{
		{"zero is invalid", 0, true, "MaxDFAStates"},
		{"minimum valid (1)", 1, false, ""},
		{"typical value", 10000, false, ""},
		{"maximum valid (1M)", 1_000_000, false, ""},
		{"exceeds maximum", 1_000_001, true, "MaxDFAStates"},
		{"far exceeds maximum", 10_000_000, true, "MaxDFAStates"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := DefaultConfig()
			c.MaxDFAStates = tt.maxDFAStates
			err := c.Validate()

			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.wantField != "" {
				var cfgErr *ConfigError
				if !errors.As(err, &cfgErr) {
					t.Errorf("error type = %T, want *ConfigError", err)
				} else if cfgErr.Field != tt.wantField {
					t.Errorf("ConfigError.Field = %q, want %q", cfgErr.Field, tt.wantField)
				}
			}
		})
	}
}

// TestConfigValidateDeterminizationLimit tests DeterminizationLimit validation.
func TestConfigValidateDeterminizationLimit(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		valid bool
	}{
		{"below minimum (5)", 5, false},
		{"at minimum (10)", 10, true},
		{"typical (1000)", 1000, true},
		{"at maximum (100000)", 100_000, true},
		{"above maximum", 100_001, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := DefaultConfig()
			c.DeterminizationLimit = tt.limit
			err := c.Validate()

			if (err == nil) != tt.valid {
				t.Errorf("DeterminizationLimit=%d: Validate() error = %v, wantValid %v",
					tt.limit, err, tt.valid)
			}
		})
	}
}

// TestConfigValidateMinLiteralLen tests MinLiteralLen validation.
func TestConfigValidateMinLiteralLen(t *testing.T) {
	tests := []struct {
		name  string
		value int
		valid bool
	}{
		{"zero is invalid", 0, false},
		{"minimum valid (1)", 1, true},
		{"typical (2)", 2, true},
		{"maximum valid (64)", 64, true},
		{"above maximum", 65, false},
		{"negative", -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := DefaultConfig()
			c.MinLiteralLen = tt.value
			err := c.Validate()

			if (err == nil) != tt.valid {
				t.Errorf("MinLiteralLen=%d: Validate() error = %v, wantValid %v",
					tt.value, err, tt.valid)
			}
		})
	}
}

// TestConfigValidateMaxLiterals tests MaxLiterals validation.
func TestConfigValidateMaxLiterals(t *testing.T) {
	tests := []struct {
		name  string
		value int
		valid bool
	}{
		{"zero is invalid", 0, false},
		{"minimum valid (1)", 1, true},
		{"typical (256)", 256, true},
		{"maximum valid (1000)", 1000, true},
		{"above maximum", 1001, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := DefaultConfig()
			c.MaxLiterals = tt.value
			err := c.Validate()

			if (err == nil) != tt.valid {
				t.Errorf("MaxLiterals=%d: Validate() error = %v, wantValid %v",
					tt.value, err, tt.valid)
			}
		})
	}
}

// TestConfigValidateMaxRecursionDepth tests MaxRecursionDepth validation.
func TestConfigValidateMaxRecursionDepth(t *testing.T) {
	tests := []struct {
		name  string
		value int
		valid bool
	}{
		{"below minimum (5)", 5, false},
		{"at minimum (10)", 10, true},
		{"typical (100)", 100, true},
		{"at maximum (1000)", 1000, true},
		{"above maximum", 1001, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := DefaultConfig()
			c.MaxRecursionDepth = tt.value
			err := c.Validate()

			if (err == nil) != tt.valid {
				t.Errorf("MaxRecursionDepth=%d: Validate() error = %v, wantValid %v",
					tt.value, err, tt.valid)
			}
		})
	}
}

// TestConfigValidateDFADisabled tests that DFA-specific fields are not validated
// when EnableDFA is false.
func TestConfigValidateDFADisabled(t *testing.T) {
	c := Config{
		EnableDFA:            false,
		MaxDFAStates:         0, // Would be invalid if DFA enabled
		DeterminizationLimit: 0, // Would be invalid if DFA enabled
		EnablePrefilter:      false,
		MaxRecursionDepth:    50,
	}

	if err := c.Validate(); err != nil {
		t.Errorf("DFA disabled with invalid DFA fields: Validate() = %v, want nil", err)
	}
}

// TestConfigValidatePrefilterDisabled tests that prefilter-specific fields are not
// validated when EnablePrefilter is false.
func TestConfigValidatePrefilterDisabled(t *testing.T) {
	c := Config{
		EnableDFA:            true,
		MaxDFAStates:         10000,
		DeterminizationLimit: 1000,
		EnablePrefilter:      false,
		MinLiteralLen:        0, // Would be invalid if prefilter enabled
		MaxLiterals:          0, // Would be invalid if prefilter enabled
		MaxRecursionDepth:    100,
	}

	if err := c.Validate(); err != nil {
		t.Errorf("Prefilter disabled with invalid prefilter fields: Validate() = %v, want nil", err)
	}
}

// TestConfigErrorFormat tests that ConfigError produces readable error messages.
func TestConfigErrorFormat(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		message string
		want    string
	}{
		{
			name:    "MaxDFAStates error",
			field:   "MaxDFAStates",
			message: "must be between 1 and 1,000,000",
			want:    "regexp: invalid config: MaxDFAStates: must be between 1 and 1,000,000",
		},
		{
			name:    "MinLiteralLen error",
			field:   "MinLiteralLen",
			message: "must be between 1 and 64",
			want:    "regexp: invalid config: MinLiteralLen: must be between 1 and 64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &ConfigError{Field: tt.field, Message: tt.message}
			got := err.Error()
			if got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestConfigErrorIsError verifies ConfigError satisfies the error interface.
func TestConfigErrorIsError(t *testing.T) {
	var err error = &ConfigError{Field: "Test", Message: "test message"}
	if err.Error() == "" {
		t.Error("ConfigError.Error() returned empty string")
	}

	// Verify errors.As works
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Error("errors.As failed to unwrap ConfigError")
	}
}

// TestConfigValidateMultipleErrors tests that the first invalid field causes error.
func TestConfigValidateMultipleErrors(t *testing.T) {
	c := Config{
		EnableDFA:            true,
		MaxDFAStates:         0, // invalid
		DeterminizationLimit: 0, // also invalid
		EnablePrefilter:      true,
		MinLiteralLen:        0,    // also invalid
		MaxLiterals:          0,    // also invalid
		MaxRecursionDepth:    5000, // also invalid
	}

	err := c.Validate()
	if err == nil {
		t.Fatal("expected error for config with multiple invalid fields")
	}

	// Should get the first field checked
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("error type = %T, want *ConfigError", err)
	}

	// The first field validated when DFA enabled is MaxDFAStates
	if cfgErr.Field != "MaxDFAStates" {
		t.Errorf("first error field = %q, want %q", cfgErr.Field, "MaxDFAStates")
	}
}
