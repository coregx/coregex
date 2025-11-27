# Security Policy

## Supported Versions

coregex is currently in experimental release (v0.x.x). We provide security updates for the following versions:

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | :white_check_mark: |
| < 0.1.0 | :x:                |

Future stable releases (v1.0+) will follow semantic versioning with LTS support.

## Reporting a Vulnerability

We take security seriously. If you discover a security vulnerability in coregex, please report it responsibly.

### How to Report

**DO NOT** open a public GitHub issue for security vulnerabilities.

Instead, please report security issues by:

1. **Private Security Advisory** (preferred):
   https://github.com/coregx/coregex/security/advisories/new

2. **Email** to maintainers:
   Create a private GitHub issue or contact via discussions

### What to Include

Please include the following information in your report:

- **Description** of the vulnerability
- **Steps to reproduce** the issue (include malicious regex pattern if applicable)
- **Affected versions** (which versions are impacted)
- **Potential impact** (DoS, memory exhaustion, unexpected behavior, etc.)
- **Suggested fix** (if you have one)
- **Your contact information** (for follow-up questions)

### Response Timeline

- **Initial Response**: Within 48-72 hours
- **Triage & Assessment**: Within 1 week
- **Fix & Disclosure**: Coordinated with reporter

We aim to:
1. Acknowledge receipt within 72 hours
2. Provide an initial assessment within 1 week
3. Work with you on a coordinated disclosure timeline
4. Credit you in the security advisory (unless you prefer to remain anonymous)

## Security Considerations for Regex Engine

coregex is a regex engine that compiles and executes untrusted regex patterns. This introduces security risks that users should be aware of.

### 1. Malicious Regex Patterns (ReDoS)

**Risk**: Crafted regex patterns can cause excessive CPU usage or memory exhaustion.

**Attack Vectors**:
- **Catastrophic backtracking**: Patterns with nested quantifiers (e.g., `(a+)+b`)
- **DFA state explosion**: Patterns causing exponential DFA states
- **Memory exhaustion**: Patterns with large repetition counts
- **Pattern injection**: User-supplied regex patterns in web applications

**Mitigation in Library**:
- ✅ **No backtracking in DFA** - DFA search is O(n) time, immune to catastrophic backtracking
- ✅ **Lazy DFA with limits** - DFA state cache has configurable max size (default: 10,000 states)
- ✅ **NFA fallback** - Graceful degradation when DFA cache fills
- ✅ **Thompson's NFA** - PikeVM execution is O(n×m), bounded worst-case time
- ✅ **Determinization limit** - Prevents excessive NFA→DFA conversion (default: 1,000 states)
- 🔄 **Pattern complexity analysis** - Planned for v0.2.0

**User Recommendations**:
```go
// ❌ BAD - Don't compile untrusted patterns without limits
pattern := userInput // Could be "(a+)+b"
re, _ := coregex.Compile(pattern)
re.Match(largeInput) // Potential DoS

// ✅ GOOD - Use custom config with strict limits
config := coregex.DefaultConfig()
config.DFAMaxStates = 1000        // Limit DFA cache
config.DeterminizationLimit = 100 // Limit NFA→DFA complexity

re, err := coregex.CompileWithConfig(pattern, config)
if err != nil {
    // Pattern too complex or compilation failed
    return errors.New("invalid pattern")
}

// Match with timeout (application-level)
ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
defer cancel()

done := make(chan bool)
go func() {
    result := re.Match(input)
    done <- result
}()

select {
case <-done:
    // Match completed
case <-ctx.Done():
    // Timeout - potential DoS pattern
    return errors.New("match timeout")
}
```

### 2. Integer Overflow Vulnerabilities

**Risk**: Large repetition counts or pattern sizes can cause integer overflow.

**Example Attack**:
```
Pattern: a{4294967295}  // 2^32-1 repetitions
Compilation: May overflow when allocating NFA states
Result: Incorrect buffer allocation or panic
```

**Mitigation**:
- ✅ Pattern length validation (via Go's regexp/syntax parser)
- ✅ Repetition count limits enforced by regexp/syntax
- ✅ Safe integer arithmetic in state counting
- ✅ NFA state limit enforcement

**Current Limits**:
- Max pattern length: Limited by regexp/syntax parser
- Max repetition count: Limited by regexp/syntax parser
- Max NFA states: Limited by determinization limit
- Max DFA states: Configurable (default: 10,000)

### 3. DFA Cache Exhaustion

**Risk**: Complex patterns can fill DFA state cache, causing performance degradation.

**Attack Vectors**:
- Patterns with large character classes and alternations
- Unicode patterns with many possible transitions
- Patterns designed to maximize DFA states

**Mitigation**:
- ✅ Configurable DFA cache size (MaxStates)
- ✅ Automatic NFA fallback when cache full
- ✅ Thread-safe cache with hit/miss statistics
- ✅ Cache clear method for manual reset

**Cache Configuration**:
```go
// Default config (production-ready)
config := coregex.DefaultConfig()
config.DFAMaxStates = 10000 // 10K states (~1-2MB memory)

// Restricted config (untrusted patterns)
config.DFAMaxStates = 100 // Only 100 states, faster fallback to NFA

// Permissive config (trusted patterns, performance-critical)
config.DFAMaxStates = 100000 // 100K states (~10-20MB memory)
```

### 4. Memory Exhaustion

**Risk**: Regex compilation or execution can allocate large amounts of memory.

**Attack Vectors**:
- NFA with thousands of states
- DFA cache growing to max size
- Large input strings with many matches
- FindAll with n=-1 on pathological patterns

**Mitigation**:
- ✅ Lazy DFA construction (only builds states needed)
- ✅ Configurable limits on cache size
- ✅ Bounded NFA state allocation
- ✅ Streaming input processing (no full input buffering)

**User Best Practices**:
```go
// ❌ BAD - Unbounded FindAll on untrusted input
matches := re.FindAll(hugeInput, -1) // May allocate huge slice

// ✅ GOOD - Limit number of matches
matches := re.FindAll(input, 100) // Max 100 matches

// ✅ GOOD - Validate input size first
if len(input) > maxInputSize {
    return errors.New("input too large")
}
```

### 5. Pattern Injection Attacks

**Risk**: User-supplied regex patterns in web applications can be exploited.

**Attack Vectors**:
- Search functionality with user-provided patterns
- Filter expressions using regex
- Template systems with regex validation

**Mitigation (Application Level)**:
```go
// ❌ BAD - Direct user input as pattern
pattern := r.URL.Query().Get("search") // Untrusted!
re, _ := coregex.Compile(pattern)

// ✅ GOOD - Whitelist allowed patterns
allowedPatterns := map[string]string{
    "email": `\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`,
    "phone": `\d{3}-\d{3}-\d{4}`,
}

patternName := r.URL.Query().Get("type")
pattern, ok := allowedPatterns[patternName]
if !ok {
    return errors.New("invalid pattern type")
}

// ✅ GOOD - Escape user input for literal matching
searchTerm := regexp.QuoteMeta(r.URL.Query().Get("search"))
pattern := fmt.Sprintf(`\b%s\b`, searchTerm)
```

### 6. SIMD Assembly Vulnerabilities

**Risk**: coregex uses hand-written AVX2/SSSE3 assembly for SIMD acceleration.

**Attack Vectors**:
- Buffer overflows in assembly code
- Unaligned memory access causing crashes
- VZEROUPPER omission causing performance penalties

**Mitigation**:
- ✅ Extensive bounds checking in assembly
- ✅ Alignment handling (aligned + unaligned paths)
- ✅ VZEROUPPER called before all AVX2 returns
- ✅ Comprehensive tests including alignment edge cases
- ✅ Fuzz testing for assembly code paths
- ✅ Pure Go fallback for non-AMD64 platforms

**Current Assembly Functions**:
- `memchrAVX2` - Single byte search (AVX2)
- `memchr2AVX2` - Two-byte search (AVX2)
- `memchr3AVX2` - Three-byte search (AVX2)
- `teddySSSE3` - Multi-pattern search (SSSE3)

All have extensive validation and bounds checking.

## Security Best Practices for Users

### Input Validation

Always validate regex patterns from untrusted sources:

```go
// Validate pattern complexity before compilation
if len(pattern) > maxPatternLength {
    return errors.New("pattern too long")
}

// Try to compile with strict limits
config := coregex.DefaultConfig()
config.DFAMaxStates = 1000
config.DeterminizationLimit = 100

re, err := coregex.CompileWithConfig(pattern, config)
if err != nil {
    // Pattern failed validation - potentially malicious
    log.Printf("Failed to compile pattern: %v", err)
    return err
}
```

### Resource Limits

Set limits when processing untrusted patterns or input:

```go
// Limit input size
const maxInputSize = 10 * 1024 * 1024 // 10MB
if len(input) > maxInputSize {
    return errors.New("input too large")
}

// Limit number of matches
const maxMatches = 1000
matches := re.FindAll(input, maxMatches)

// Use timeout for execution
ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
defer cancel()

done := make(chan []byte)
go func() {
    match := re.Find(input)
    done <- match
}()

select {
case result := <-done:
    // Success
case <-ctx.Done():
    // Timeout
    return errors.New("regex execution timeout")
}
```

### Error Handling

Always check errors - compilation failures may indicate malicious patterns:

```go
// ❌ BAD - Ignoring errors
re, _ := coregex.Compile(pattern)
matches := re.FindAll(input, -1)

// ✅ GOOD - Proper error handling
re, err := coregex.Compile(pattern)
if err != nil {
    return fmt.Errorf("pattern compilation failed: %w", err)
}

match := re.Find(input)
if match == nil {
    log.Printf("No match found")
    return nil
}

// Process match...
```

### Whitelisting Patterns

Use pattern whitelists instead of user-provided patterns:

```go
// ✅ Pre-compile trusted patterns
var (
    emailPattern = coregex.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
    phonePattern = coregex.MustCompile(`\d{3}-\d{3}-\d{4}`)
    datePattern  = coregex.MustCompile(`\d{4}-\d{2}-\d{2}`)
)

// Select pattern by type, not user input
func validateField(fieldType string, value string) bool {
    var pattern *coregex.Regex
    switch fieldType {
    case "email":
        pattern = emailPattern
    case "phone":
        pattern = phonePattern
    case "date":
        pattern = datePattern
    default:
        return false
    }

    return pattern.Match([]byte(value))
}
```

## Known Security Considerations

### 1. Thompson's NFA Complexity

**Status**: Mitigated by O(n×m) worst-case guarantee.

**Risk Level**: Low

**Description**: Thompson's NFA construction ensures no backtracking. PikeVM execution is bounded by O(n×m) where n=input length, m=NFA states.

**Mitigation**:
- ✅ Thompson's construction (no backtracking)
- ✅ SparseSet for O(1) state tracking
- ✅ Determinization limits prevent m from growing unbounded

### 2. DFA State Explosion

**Status**: Mitigated by lazy construction + cache limits.

**Risk Level**: Medium

**Description**: Certain patterns can cause exponential DFA states. Lazy DFA only builds states encountered during search.

**Mitigation**:
- ✅ Lazy construction (on-demand)
- ✅ Configurable max states limit
- ✅ Automatic NFA fallback
- ✅ Cache hit/miss statistics for monitoring

### 3. Dependency Security

coregex dependencies:

- `golang.org/x/sys` (minimal) - CPU feature detection for SIMD
- No other runtime dependencies

**Monitoring**:
- ✅ Minimal dependency surface (only 1 dependency)
- ✅ Standard library dependency (golang.org/x)
- 🔄 Dependabot enabled (planned when public)

## Security Testing

### Current Testing

- ✅ Unit tests with edge cases (empty input, alignment, boundaries)
- ✅ Fuzz tests for SIMD primitives
- ✅ Comparison tests vs stdlib regexp (correctness)
- ✅ Benchmarks for performance validation
- ✅ Race detector (0 data races)
- ✅ golangci-lint with 34+ linters

### Planned for v1.0

- 🔄 Fuzzing for pattern compilation
- 🔄 ReDoS vulnerability scanning
- 🔄 Static analysis with gosec
- 🔄 SAST/DAST scanning in CI
- 🔄 Comparison fuzzing against multiple regex engines

## Security Disclosure History

### v0.1.0 (2025-01-26)

**Initial release** - No security issues reported yet.

coregex v0.1.0 is a new project with production-quality code but experimental API stability.

**Recommendation**: Use with caution in production. API may change in v0.2+.

## Security Contact

- **GitHub Security Advisory**: https://github.com/coregx/coregex/security/advisories/new
- **Public Issues** (for non-sensitive bugs): https://github.com/coregx/coregex/issues
- **Discussions**: https://github.com/coregx/coregex/discussions

## Bug Bounty Program

coregex does not currently have a bug bounty program. We rely on responsible disclosure from the security community.

If you report a valid security vulnerability:
- ✅ Public credit in security advisory (if desired)
- ✅ Acknowledgment in CHANGELOG
- ✅ Our gratitude and recognition in README
- ✅ Priority review and quick fix

---

**Thank you for helping keep coregex secure!** 🔒

*Security is a journey, not a destination. We continuously improve our security posture with each release.*
