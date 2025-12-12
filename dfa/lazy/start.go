package lazy

import (
	"github.com/coregx/coregex/nfa"
)

// StartKind represents the look-behind context for start state selection.
//
// Different start states are needed to correctly handle assertions like:
//   - ^ (beginning of line/text)
//   - \b (word boundary)
//   - (?m) multiline mode
//
// The previous byte determines which assertions can match at the current position.
// This is inspired by RE2's start state caching (8 states) and rust-regex's
// StartByteMap (6 configurations).
type StartKind uint8

const (
	// StartNonWord - previous byte was non-word character (not [a-zA-Z0-9_])
	// or we're in the middle of input with a non-word char before
	StartNonWord StartKind = iota

	// StartWord - previous byte was a word character [a-zA-Z0-9_]
	StartWord

	// StartText - at the very beginning of input (for ^ in non-multiline mode)
	StartText

	// StartLineLF - previous byte was \n (for ^ in multiline mode)
	StartLineLF

	// StartLineCR - previous byte was \r (for CRLF handling)
	StartLineCR

	// startKindCount is the number of start kinds (not exported)
	startKindCount
)

// String returns a human-readable representation of the StartKind
func (k StartKind) String() string {
	switch k {
	case StartNonWord:
		return "NonWord"
	case StartWord:
		return "Word"
	case StartText:
		return "Text"
	case StartLineLF:
		return "LineLF"
	case StartLineCR:
		return "LineCR"
	default:
		return "Unknown"
	}
}

// StartTable holds cached start states for different look-behind contexts.
//
// For each (StartKind, anchored) combination, we cache the corresponding
// DFA start state. This avoids recomputing epsilon closures on every search.
//
// Total slots: startKindCount * 2 (anchored/unanchored) = 10 start states
type StartTable struct {
	// states[anchored][kind] -> StateID
	// anchored: 0 = unanchored, 1 = anchored
	states [2][startKindCount]StateID

	// byteMap maps each byte value to its StartKind
	// This allows O(1) lookup of the start kind for any previous byte
	byteMap [256]StartKind

	// initialized tracks which start states have been computed
	// Lazy initialization: only compute states when first needed
	initialized [2][startKindCount]bool
}

// NewStartTable creates a new StartTable with the byte map initialized.
// Start states themselves are computed lazily on first access.
func NewStartTable() *StartTable {
	st := &StartTable{}

	// Initialize byte map
	for b := 0; b < 256; b++ {
		switch {
		case b == '\n':
			st.byteMap[b] = StartLineLF
		case b == '\r':
			st.byteMap[b] = StartLineCR
		case isWordByte(byte(b)):
			st.byteMap[b] = StartWord
		default:
			st.byteMap[b] = StartNonWord
		}
	}

	// Initialize all state slots to InvalidState
	for i := 0; i < 2; i++ {
		for j := 0; j < int(startKindCount); j++ {
			st.states[i][j] = InvalidState
		}
	}

	return st
}

// GetKind returns the StartKind for the given previous byte.
// This is an O(1) lookup via the pre-computed byte map.
func (st *StartTable) GetKind(prevByte byte) StartKind {
	return st.byteMap[prevByte]
}

// GetKindForPosition returns the appropriate StartKind based on position.
// At position 0 (start of input), returns StartText.
// Otherwise, returns the kind based on the previous byte.
func (st *StartTable) GetKindForPosition(haystack []byte, pos int) StartKind {
	if pos == 0 {
		return StartText
	}
	return st.byteMap[haystack[pos-1]]
}

// Get returns the cached start state ID for the given configuration.
// Returns InvalidState if the state hasn't been initialized yet.
func (st *StartTable) Get(kind StartKind, anchored bool) StateID {
	anchoredIdx := 0
	if anchored {
		anchoredIdx = 1
	}
	return st.states[anchoredIdx][kind]
}

// Set stores a start state ID for the given configuration.
func (st *StartTable) Set(kind StartKind, anchored bool, stateID StateID) {
	anchoredIdx := 0
	if anchored {
		anchoredIdx = 1
	}
	st.states[anchoredIdx][kind] = stateID
	st.initialized[anchoredIdx][kind] = true
}

// IsInitialized returns true if the start state for the given configuration
// has been computed and cached.
func (st *StartTable) IsInitialized(kind StartKind, anchored bool) bool {
	anchoredIdx := 0
	if anchored {
		anchoredIdx = 1
	}
	return st.initialized[anchoredIdx][kind]
}

// isWordByte returns true if the byte is a word character [a-zA-Z0-9_]
func isWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '_'
}

// StartConfig holds the configuration for computing a start state.
// This includes NFA start state selection and look-behind context.
type StartConfig struct {
	Kind     StartKind
	Anchored bool
}

// ComputeStartState computes the DFA start state for the given configuration.
// This is called lazily when a start state is first needed.
//
// The builder and NFA are used to:
//  1. Get the appropriate NFA start state (anchored or unanchored)
//  2. Compute the epsilon closure with appropriate look assertions
//  3. Create the DFA state with word context
//
// The StartKind determines:
//   - Which look assertions are satisfied at the start position:
//   - StartText: Both \A (LookStartText) and ^ (LookStartLine) are satisfied
//   - StartLineLF: Only ^ (LookStartLine) is satisfied (multiline mode after \n)
//   - Other kinds: No line-start assertions are satisfied
//   - The isFromWord context (for \b/\B word boundary handling):
//   - StartWord: isFromWord = true (previous byte was word char)
//   - All others: isFromWord = false
//
// This enables correct handling of patterns like "^abc" and "\bword".
func ComputeStartState(builder *Builder, n *nfa.NFA, config StartConfig) (*State, StateKey) {
	return ComputeStartStateWithStride(builder, n, config, defaultStride)
}

// ComputeStartStateWithStride computes the DFA start state with explicit stride.
// The stride should be ByteClasses.AlphabetLen() for memory efficiency.
func ComputeStartStateWithStride(builder *Builder, n *nfa.NFA, config StartConfig, stride int) (*State, StateKey) {
	// Select NFA start state based on anchored flag
	var nfaStart nfa.StateID
	if config.Anchored {
		nfaStart = n.StartAnchored()
	} else {
		nfaStart = n.StartUnanchored()
	}

	// Determine which look assertions are satisfied at this start position.
	// This is the key to correct anchor handling in the DFA.
	//
	// For pattern "^abc":
	//   - At StartText (pos 0): LookStartLine satisfied → StateLook traversed → match possible
	//   - At StartLineLF (after \n): LookStartLine satisfied → StateLook traversed → match possible
	//   - At other positions: LookStartLine NOT satisfied → StateLook NOT traversed → no match
	//
	// Note: Word boundary assertions (\b, \B) are NOT resolved here because they depend
	// on BOTH the previous byte (known) AND the current byte (unknown until move()).
	// Word boundary StateLook states remain in the closure and are resolved in move().
	lookHave := LookSetFromStartKind(config.Kind)

	// Compute epsilon closure from NFA start state with look assertions
	startStateSet := builder.epsilonClosure([]nfa.StateID{nfaStart}, lookHave)

	// Check if start state is a match state
	isMatch := builder.containsMatchState(startStateSet)

	// Determine isFromWord based on StartKind.
	// This is critical for \b/\B word boundary handling:
	//   - StartWord: previous byte was a word char → isFromWord = true
	//   - StartText: no previous byte → isFromWord = false
	//   - StartLineLF/CR: newline is not a word char → isFromWord = false
	//   - StartNonWord: previous byte was non-word → isFromWord = false
	isFromWord := config.Kind == StartWord

	// Compute state key for caching
	// The key now includes word context - states with same NFA states but
	// different isFromWord are DIFFERENT DFA states!
	key := ComputeStateKeyWithWord(startStateSet, isFromWord)

	// Create DFA state with word context and stride (ID will be assigned by caller)
	state := NewStateWithStride(InvalidState, startStateSet, isMatch, isFromWord, stride)

	return state, key
}

// AllStartConfigs returns all possible start configurations.
// This is useful for pre-computing all start states during DFA construction.
func AllStartConfigs() []StartConfig {
	configs := make([]StartConfig, 0, int(startKindCount)*2)
	for anchored := 0; anchored < 2; anchored++ {
		for kind := StartKind(0); kind < startKindCount; kind++ {
			configs = append(configs, StartConfig{
				Kind:     kind,
				Anchored: anchored == 1,
			})
		}
	}
	return configs
}

// DefaultStartConfig returns the default start configuration for unanchored search
// at the beginning of input.
func DefaultStartConfig() StartConfig {
	return StartConfig{
		Kind:     StartText,
		Anchored: false,
	}
}
