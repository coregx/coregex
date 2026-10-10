package meta

import (
	"sync"

	"github.com/coregx/coregex/nfa"
)

// pikeVMPool hands out PikeVMs that run on one NFA.
//
// A PikeVM keeps mutable search state (thread queues, visited set, slot
// tables). Searchers are shared by every goroutine using a compiled pattern,
// so they must not hold a PikeVM and search with it directly: concurrent
// searches would corrupt each other's state. Instead a search takes a PikeVM
// from the pool for the duration of the call and returns it afterwards.
type pikeVMPool struct {
	pool sync.Pool
}

// newPikeVMPool creates a pool of PikeVMs for n. PikeVMs are created lazily,
// so a pattern that never falls back to the NFA allocates none.
func newPikeVMPool(n *nfa.NFA) *pikeVMPool {
	p := &pikeVMPool{}
	p.pool.New = func() any { return nfa.NewPikeVMLazy(n) }
	return p
}

// searchAt runs an unanchored PikeVM search starting at at, on a PikeVM taken
// from the pool. It returns the match bounds as absolute positions.
func (p *pikeVMPool) searchAt(haystack []byte, at int) (int, int, bool) {
	pv := p.pool.Get().(*nfa.PikeVM)
	defer p.pool.Put(pv)
	return pv.SearchAt(haystack, at)
}
