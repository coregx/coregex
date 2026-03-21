package prefilter

// incompleteWrapper wraps a prefilter and overrides IsComplete() to return false.
// Used when literals are nominally complete but the pattern has anchors or other
// constraints that require DFA/NFA verification after prefilter candidate finding.
type incompleteWrapper struct {
	inner Prefilter
}

// WrapIncomplete wraps a prefilter to force IsComplete()=false.
// The underlying prefilter is used for candidate finding, but the caller
// must verify each candidate with the full regex engine.
func WrapIncomplete(pf Prefilter) Prefilter {
	return &incompleteWrapper{inner: pf}
}

func (w *incompleteWrapper) Find(haystack []byte, start int) int {
	return w.inner.Find(haystack, start)
}

func (w *incompleteWrapper) IsComplete() bool {
	return false
}

func (w *incompleteWrapper) LiteralLen() int {
	return 0
}

func (w *incompleteWrapper) HeapBytes() int {
	return w.inner.HeapBytes()
}

func (w *incompleteWrapper) IsFast() bool {
	return w.inner.IsFast()
}

// lineAnchorWrapper wraps a prefilter and adds (?m)^ line-start verification.
// Instead of marking complete=false (which forces expensive NFA verification),
// this wrapper checks that each candidate is at line start (pos==0 || haystack[pos-1]=='\n').
// This gives O(1) verification per candidate vs O(n*states) NFA.
type lineAnchorWrapper struct {
	inner Prefilter
}

// WrapLineAnchor wraps a complete prefilter to add (?m)^ line-start checking.
// The wrapper's Find skips candidates not at line boundaries, and IsComplete
// returns true (no NFA verification needed — line check is sufficient).
func WrapLineAnchor(pf Prefilter) Prefilter {
	return &lineAnchorWrapper{inner: pf}
}

func (w *lineAnchorWrapper) Find(haystack []byte, start int) int {
	pos := start
	for {
		candidate := w.inner.Find(haystack, pos)
		if candidate == -1 {
			return -1
		}
		// Verify (?m)^ — candidate must be at start of line
		if candidate == 0 || haystack[candidate-1] == '\n' {
			return candidate
		}
		// Not at line start — skip to next candidate
		pos = candidate + 1
	}
}

func (w *lineAnchorWrapper) IsComplete() bool {
	return w.inner.IsComplete()
}

func (w *lineAnchorWrapper) LiteralLen() int {
	return w.inner.LiteralLen()
}

func (w *lineAnchorWrapper) HeapBytes() int {
	return w.inner.HeapBytes()
}

func (w *lineAnchorWrapper) IsFast() bool {
	return w.inner.IsFast()
}
