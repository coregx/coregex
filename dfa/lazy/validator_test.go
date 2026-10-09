package lazy

import (
	"testing"

	"github.com/coregx/coregex/nfa"
)

func TestValidatorTransition(t *testing.T) {
	tests := []struct {
		name     string
		vs       uint8
		b        byte
		wantNext uint8
		wantQuit bool
	}{
		// VS0: codepoint boundary
		{"ascii_at_boundary", VS0, 'a', VS0, false},
		{"orphan_continuation", VS0, 0x80, VS0, true},
		{"overlong_C0", VS0, 0xC0, VS0, true},
		{"overlong_C1", VS0, 0xC1, VS0, true},
		{"valid_2byte_lead", VS0, 0xC2, VS1, false},
		{"valid_2byte_lead_DF", VS0, 0xDF, VS1, false},
		{"E0_special", VS0, 0xE0, VS3, false},
		{"E1_normal", VS0, 0xE1, VS2, false},
		{"ED_surrogate", VS0, 0xED, VS4, false},
		{"EF_normal", VS0, 0xEF, VS2, false},
		{"F0_special", VS0, 0xF0, VS7, false},
		{"F1_normal", VS0, 0xF1, VS6, false},
		{"F4_restricted", VS0, 0xF4, VS8, false},
		{"F5_invalid", VS0, 0xF5, VS0, true},
		{"FF_invalid", VS0, 0xFF, VS0, true},

		// VS1: expecting continuation
		{"VS1_valid_cont", VS1, 0x80, VS0, false},
		{"VS1_valid_cont_BF", VS1, 0xBF, VS0, false},
		{"VS1_invalid_ascii", VS1, 'x', VS0, true},
		{"VS1_invalid_lead", VS1, 0xC2, VS0, true},

		// VS3: after E0, expecting A0-BF
		{"VS3_valid_A0", VS3, 0xA0, VS5, false},
		{"VS3_valid_BF", VS3, 0xBF, VS5, false},
		{"VS3_invalid_80", VS3, 0x80, VS0, true},
		{"VS3_invalid_9F", VS3, 0x9F, VS0, true},

		// VS4: after ED, expecting 80-9F
		{"VS4_valid_80", VS4, 0x80, VS5, false},
		{"VS4_valid_9F", VS4, 0x9F, VS5, false},
		{"VS4_invalid_A0", VS4, 0xA0, VS0, true},

		// VS7: after F0, expecting 90-BF
		{"VS7_valid_90", VS7, 0x90, VS6, false},
		{"VS7_invalid_80", VS7, 0x80, VS0, true},
		{"VS7_invalid_8F", VS7, 0x8F, VS0, true},

		// VS8: after F4, expecting 80-8F
		{"VS8_valid_80", VS8, 0x80, VS6, false},
		{"VS8_valid_8F", VS8, 0x8F, VS6, false},
		{"VS8_invalid_90", VS8, 0x90, VS0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next, quit := ValidatorTransition(tt.vs, tt.b)
			if next != tt.wantNext || quit != tt.wantQuit {
				t.Errorf("ValidatorTransition(VS%d, 0x%02X) = (VS%d, %v), want (VS%d, %v)",
					tt.vs, tt.b, next, quit, tt.wantNext, tt.wantQuit)
			}
		})
	}
}

// TestValidatorFullSequences verifies the validator tracks complete UTF-8 sequences.
func TestValidatorFullSequences(t *testing.T) {
	sequences := []struct {
		name  string
		bytes []byte
		valid bool
	}{
		{"ascii", []byte("hello"), true},
		{"2byte_К", []byte{0xD0, 0x9A}, true},
		{"3byte_日", []byte{0xE6, 0x97, 0xA5}, true},
		{"4byte_emoji", []byte{0xF0, 0x9F, 0x98, 0x80}, true},
		{"orphan_continuation", []byte{0x80}, false},
		{"truncated_2byte", []byte{0xC2}, false},
		{"overlong", []byte{0xC0, 0x80}, false},
		{"surrogate", []byte{0xED, 0xA0, 0x80}, false},
	}

	for _, tt := range sequences {
		t.Run(tt.name, func(t *testing.T) {
			vs := VS0
			allValid := true
			for _, b := range tt.bytes {
				next, quit := ValidatorTransition(vs, b)
				if quit {
					allValid = false
					break
				}
				vs = next
			}
			if tt.valid && !allValid {
				t.Errorf("valid sequence %q reported as invalid", tt.bytes)
			}
			if !tt.valid && allValid && vs == VS0 {
				t.Errorf("invalid sequence %q reported as valid", tt.bytes)
			}
		})
	}
}

// TestComputeStateKeyFull verifies validator state affects key identity.
func TestComputeStateKeyFull(t *testing.T) {
	nfaStates := []nfa.StateID{1, 2, 3}

	key0 := ComputeStateKeyFull(nfaStates, false, false, VS0)
	key1 := ComputeStateKeyFull(nfaStates, false, false, VS1)

	if key0 == key1 {
		t.Error("different validator states must produce different keys")
	}

	keySame := ComputeStateKeyFull(nfaStates, false, false, VS0)
	if key0 != keySame {
		t.Error("same inputs must produce same key")
	}
}
