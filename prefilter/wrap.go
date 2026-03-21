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
