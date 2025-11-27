# Contributing to coregex

Thank you for considering contributing to coregex! This document outlines the development workflow and guidelines.

## Git Workflow (Git-Flow)

This project uses Git-Flow branching model for development.

### Branch Structure

```
main                 # Production-ready code (tagged releases)
  └─ develop         # Integration branch for next release
       ├─ feature/*  # New features
       ├─ bugfix/*   # Bug fixes
       └─ hotfix/*   # Critical fixes from main
```

### Branch Purposes

- **main**: Production-ready code. Only releases are merged here.
- **develop**: Active development branch. All features merge here first.
- **feature/\***: New features. Branch from `develop`, merge back to `develop`.
- **bugfix/\***: Bug fixes. Branch from `develop`, merge back to `develop`.
- **hotfix/\***: Critical production fixes. Branch from `main`, merge to both `main` and `develop`.

### Workflow Commands

#### Starting a New Feature

```bash
# Create feature branch from develop
git checkout develop
git pull origin develop
git checkout -b feature/my-new-feature

# Work on your feature...
git add .
git commit -m "feat: add my new feature"

# When done, merge back to develop
git checkout develop
git merge --squash feature/my-new-feature  # Squash merge for clean history
git commit -m "feat: my new feature (squashed)"
git branch -d feature/my-new-feature
git push origin develop
```

#### Fixing a Bug

```bash
# Create bugfix branch from develop
git checkout develop
git pull origin develop
git checkout -b bugfix/fix-issue-123

# Fix the bug...
git add .
git commit -m "fix: resolve issue #123"

# Merge back to develop
git checkout develop
git merge --squash bugfix/fix-issue-123  # Squash merge for clean history
git commit -m "fix: resolve issue #123 (squashed)"
git branch -d bugfix/fix-issue-123
git push origin develop
```

#### Creating a Release

```bash
# Create release branch from develop
git checkout develop
git pull origin develop
git checkout -b release/v0.2.0

# Update version numbers, CHANGELOG, etc.
git add .
git commit -m "chore: prepare release v0.2.0"

# Merge to main and tag
git checkout main
git merge --no-ff release/v0.2.0
git tag -a v0.2.0 -m "Release v0.2.0"

# Merge back to develop
git checkout develop
git merge --no-ff release/v0.2.0

# Delete release branch
git branch -d release/v0.2.0

# Push everything
git push origin main develop --tags
```

#### Hotfix (Critical Production Bug)

```bash
# Create hotfix branch from main
git checkout main
git pull origin main
git checkout -b hotfix/critical-bug

# Fix the bug...
git add .
git commit -m "fix: critical production bug"

# Merge to main and tag
git checkout main
git merge --no-ff hotfix/critical-bug
git tag -a v0.1.1 -m "Hotfix v0.1.1"

# Merge to develop
git checkout develop
git merge --no-ff hotfix/critical-bug

# Delete hotfix branch
git branch -d hotfix/critical-bug

# Push everything
git push origin main develop --tags
```

## Commit Message Guidelines

Follow [Conventional Commits](https://www.conventionalcommits.org/) specification:

```
<type>(<scope>): <description>

[optional body]

[optional footer]
```

### Types

- **feat**: New feature
- **fix**: Bug fix
- **docs**: Documentation changes
- **style**: Code style changes (formatting, etc.)
- **refactor**: Code refactoring
- **test**: Adding or updating tests
- **chore**: Maintenance tasks (build, dependencies, etc.)
- **perf**: Performance improvements

### Examples

```bash
feat: add Unicode property class support
fix: correct epsilon-closure in NFA compilation
docs: update README with performance benchmarks
refactor: simplify DFA state cache implementation
test: add fuzz tests for memchr edge cases
chore: update go.mod to Go 1.25.4
perf: optimize Teddy SIMD prefilter for AVX2
```

## Code Quality Standards

### Before Committing

1. **Format code**:
   ```bash
   go fmt ./...
   ```

2. **Run linter**:
   ```bash
   golangci-lint run
   ```

3. **Run tests**:
   ```bash
   go test ./...
   ```

4. **Run tests with race detector**:
   ```bash
   go test -race ./...
   ```

5. **All-in-one** (use pre-release script):
   ```bash
   bash scripts/pre-release-check.sh
   ```

### Pull Request Requirements

- [ ] Code is formatted (`go fmt ./...`)
- [ ] Linter passes (`golangci-lint run` - 0 issues)
- [ ] All tests pass (`go test ./...`)
- [ ] Race detector passes (`go test -race ./...`)
- [ ] New code has tests (minimum 70% coverage)
- [ ] Documentation updated (if applicable)
- [ ] Commit messages follow conventions
- [ ] No sensitive data (credentials, tokens, etc.)
- [ ] Benchmarks added for performance-critical code

## Development Setup

### Prerequisites

- Go 1.25 or later
- golangci-lint
- GCC or Clang (for race detector)
- Optional: WSL2 with Go (for Windows users without GCC)

### Install Dependencies

```bash
# Clone repository
git clone https://github.com/coregx/coregex.git
cd coregex

# Download dependencies
go mod download

# Install golangci-lint
# See: https://golangci-lint.run/welcome/install/
```

### Running Tests

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...

# Run with race detector
go test -race ./...

# Run benchmarks
go test -bench=. -benchmem ./simd/
go test -bench=. -benchmem ./prefilter/
```

### Running Linter

```bash
# Run linter
golangci-lint run

# Run with verbose output
golangci-lint run -v

# Verify config
golangci-lint config verify
```

## Project Structure

```
coregex/
├── .github/              # GitHub workflows and templates
│   ├── CODEOWNERS       # Code ownership
│   └── workflows/       # CI/CD pipelines
├── simd/                 # SIMD primitives (PUBLIC)
│   ├── memchr.go        # Fast byte search
│   ├── memchr_amd64.s   # AVX2/SSE4.2 assembly
│   ├── memmem.go        # Fast substring search
│   └── *_test.go        # Tests and benchmarks
├── literal/              # Literal extraction (PUBLIC)
│   ├── seq.go           # Literal sequences
│   ├── extractor.go     # Pattern literal extraction
│   └── *_test.go        # Tests
├── prefilter/            # Prefilter system (PUBLIC)
│   ├── prefilter.go     # Prefilter interface
│   ├── teddy.go         # Teddy multi-pattern SIMD
│   ├── teddy_ssse3_amd64.s  # SSSE3 assembly
│   └── *_test.go        # Tests and benchmarks
├── nfa/                  # NFA engine (PUBLIC)
│   ├── nfa.go           # NFA data structure
│   ├── compile.go       # Thompson's construction
│   ├── pikevm.go        # PikeVM execution
│   └── *_test.go        # Tests
├── dfa/                  # DFA engines (PUBLIC)
│   └── lazy/            # Lazy DFA
│       ├── lazy.go      # Main DFA search
│       ├── builder.go   # DFA compiler
│       ├── cache.go     # State cache
│       └── *_test.go    # Tests
├── meta/                 # Meta engine (PUBLIC)
│   ├── meta.go          # Engine orchestration
│   ├── strategy.go      # Strategy selection
│   └── *_test.go        # Tests
├── internal/             # Private implementation (INTERNAL)
│   └── sparse/          # Sparse set data structure
├── regex.go              # Public API
├── *_test.go             # Root package tests
├── example_test.go       # Runnable examples
├── docs/                 # Documentation
│   └── dev/             # Developer documentation
├── scripts/              # Development scripts
│   └── pre-release-check.sh  # Release validation
├── CHANGELOG.md          # Version history
├── LICENSE               # MIT License
└── README.md             # Main documentation
```

## Adding New Features

1. Check if issue exists, if not create one
2. Discuss approach in the issue
3. Create feature branch from `develop`
4. Implement feature with tests
5. Update documentation
6. Run quality checks (`bash scripts/pre-release-check.sh`)
7. Create pull request to `develop`
8. Wait for code review
9. Address feedback
10. Merge when approved

## Code Style Guidelines

### General Principles

- Follow Go conventions and idioms
- Write self-documenting code
- Add comments for complex logic (especially SIMD/assembly code)
- Keep functions small and focused
- Use meaningful variable names
- Optimize for clarity first, performance second (except in hot paths)

### Naming Conventions

- **Public types/functions**: `PascalCase` (e.g., `Compile`, `Memchr`)
- **Private types/functions**: `camelCase` (e.g., `epsilonClosure`, `selectPrefilter`)
- **Constants**: `PascalCase` with context prefix (e.g., `UseNFA`, `UseDFA`)
- **Test functions**: `Test*` (e.g., `TestLazyDFAFind`)
- **Benchmark functions**: `Benchmark*` (e.g., `BenchmarkMemchr`)
- **Example functions**: `Example*` (e.g., `ExampleCompile`)

### Error Handling

- Always check and handle errors
- Use descriptive error variables (`ErrCacheFull`, `ErrInvalidPattern`)
- Return errors immediately, don't wrap unnecessarily
- Validate inputs before processing

### Testing

- Use table-driven tests when appropriate
- Test both success and error cases
- Include test data in `testdata/` if needed
- Test edge cases (empty input, boundaries, alignment)
- Add fuzz tests for parsers and matchers
- Compare with stdlib regexp for correctness
- **Benchmarks are mandatory** for performance-critical code

## SIMD/Assembly Implementation Patterns

### Assembly Guidelines

- Use `NOSPLIT` for leaf functions
- Call `VZEROUPPER` before returning from AVX2 functions
- Handle misaligned inputs with scalar fallback
- Align loops to cache line boundaries when possible
- Comment assembly extensively (explain each block)

### Platform Support

- Always provide pure Go fallback (`*_generic.go`)
- Use build tags for platform-specific code (`// +build amd64`)
- Test on all platforms (Linux, macOS, Windows)
- Handle CPU feature detection properly

### Performance Validation

```go
// Benchmarks must include:
// - Multiple input sizes (16B, 64B, 4KB, 64KB)
// - Comparison with stdlib equivalent
// - Memory allocation tracking (0 allocs/op expected)
// - Throughput calculation (GB/s)

func BenchmarkMemchr(b *testing.B) {
    for _, size := range []int{16, 64, 4096, 65536} {
        input := make([]byte, size)
        b.Run(fmt.Sprintf("size_%d", size), func(b *testing.B) {
            b.SetBytes(int64(size))
            for i := 0; i < b.N; i++ {
                _ = Memchr(input, 'x')
            }
        })
    }
}
```

## NFA/DFA Implementation Patterns

### NFA Guidelines

- Use Thompson's construction (no backtracking)
- Implement epsilon-closure correctly
- Use SparseSet for O(1) state tracking
- Support capture groups where applicable

### DFA Guidelines

- Build states on-demand (lazy construction)
- Implement thread-safe caching
- Set reasonable state limits (prevent memory explosion)
- Provide NFA fallback for complex patterns

### Strategy Selection

- Measure pattern characteristics (NFA size, literal quality)
- Choose appropriate engine (NFA vs DFA vs Both)
- Document heuristics clearly
- Allow manual override via configuration

## Getting Help

- Check existing issues and discussions
- Read ROADMAP.md for project roadmap and progress
- Ask questions in GitHub Issues
- Reference implementation guides in `docs/dev/`

## Performance Expectations

- SIMD primitives: 5-100x faster than stdlib (depending on input size)
- Prefilters: 10-100x reduction in candidates
- Overall: 5-50x faster than stdlib for patterns with literals
- Zero allocations in steady state (hot paths)
- O(n) time complexity for DFA search

## License

By contributing, you agree that your contributions will be licensed under the Apache 2.0 License.

---

**Thank you for contributing to coregex!** 🎉
