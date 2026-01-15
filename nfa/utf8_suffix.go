package nfa

// utf8SuffixCache caches UTF-8 suffix states for deduplication during NFA construction.
// This significantly reduces the number of states when compiling patterns with '.' (dot)
// or Unicode character classes by sharing common continuation byte states.
//
// Based on Rust regex-automata's Utf8SuffixMap (map.rs:187-296).
//
// The key insight is that UTF-8 sequences share common suffixes:
//   - E1-EC and EE-EF both end with [80-BF][80-BF]
//   - All multi-byte sequences end with [80-BF]
//
// By processing UTF-8 byte sequences in REVERSE order and caching
// (targetState, byteRange) -> stateID mappings, we can reuse states.
//
// Example for '.':
//
//	Without caching: 39 states (each [80-BF] duplicated)
//	With caching: ~13-15 states (shared continuation bytes)
type utf8SuffixCache struct {
	version  uint16
	capacity int
	entries  []utf8SuffixEntry
}

// utf8SuffixKey uniquely identifies a suffix transition.
// The key is (From, Start, End) where:
//   - From: the target state this byte range transitions TO
//   - Start, End: the byte range [Start, End]
type utf8SuffixKey struct {
	from  StateID
	start byte
	end   byte
}

type utf8SuffixEntry struct {
	version uint16
	key     utf8SuffixKey
	val     StateID
}

// defaultUtf8SuffixCacheCapacity is the initial cache size.
// Rust uses 1000, but for '.' we only need ~20 entries.
// Using a smaller size reduces memory and improves cache locality.
const defaultUtf8SuffixCacheCapacity = 64

// newUtf8SuffixCache creates a new suffix cache.
func newUtf8SuffixCache() *utf8SuffixCache {
	return &utf8SuffixCache{
		version:  1,
		capacity: defaultUtf8SuffixCacheCapacity,
		entries:  make([]utf8SuffixEntry, defaultUtf8SuffixCacheCapacity),
	}
}

// clear resets the cache for reuse without reallocating.
// Uses version increment for O(1) clearing.
func (c *utf8SuffixCache) clear() {
	c.version++
	if c.version == 0 {
		// Handle overflow by resetting all entries
		c.version = 1
		for i := range c.entries {
			c.entries[i].version = 0
		}
	}
}

// hash computes the cache index for a key using FNV-1a.
func (c *utf8SuffixCache) hash(key utf8SuffixKey) int {
	// FNV-1a hash - simple and fast for small keys
	h := uint64(14695981039346656037)
	h = (h ^ uint64(key.from)) * 1099511628211
	h = (h ^ uint64(key.start)) * 1099511628211
	h = (h ^ uint64(key.end)) * 1099511628211
	//nolint:gosec // capacity is always small (64), no overflow risk
	return int(h % uint64(c.capacity))
}

// get looks up a cached state for the given key.
// Returns (stateID, true) if found, (0, false) otherwise.
func (c *utf8SuffixCache) get(key utf8SuffixKey) (StateID, bool) {
	idx := c.hash(key)
	e := &c.entries[idx]
	if e.version == c.version && e.key == key {
		return e.val, true
	}
	return 0, false
}

// set stores a state in the cache.
// Note: This is a simple direct-mapped cache - collisions overwrite.
// For UTF-8 dot compilation, collisions are rare due to the small working set.
func (c *utf8SuffixCache) set(key utf8SuffixKey, val StateID) {
	idx := c.hash(key)
	c.entries[idx] = utf8SuffixEntry{
		version: c.version,
		key:     key,
		val:     val,
	}
}

// getOrCreate returns a cached state or creates a new one using the builder.
// This is the main API for suffix-sharing compilation.
//
// Parameters:
//   - builder: NFA builder to create new states
//   - targetState: the state this byte range should transition TO
//   - lo, hi: the byte range [lo, hi]
//
// Returns the StateID for a ByteRange state matching [lo, hi] -> targetState.
// If an identical state exists in the cache, it is reused.
func (c *utf8SuffixCache) getOrCreate(builder *Builder, targetState StateID, lo, hi byte) StateID {
	key := utf8SuffixKey{from: targetState, start: lo, end: hi}

	if cached, found := c.get(key); found {
		return cached
	}

	// Create new state and cache it
	newState := builder.AddByteRange(lo, hi, targetState)
	c.set(key, newState)
	return newState
}
