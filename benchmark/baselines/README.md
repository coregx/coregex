# Benchmark Baselines

This directory stores benchmark results for version comparison using `benchstat`.

## Usage

```bash
# Run benchmarks and save as current version
./scripts/bench.sh

# Run benchmarks for specific version
./scripts/bench.sh v0.6.0

# Compare two versions
./scripts/bench.sh --compare v0.5.0 v0.6.0

# Compare current with latest baseline
./scripts/bench.sh --compare

# List available baselines
./scripts/bench.sh --list
```

## File Format

Each `.txt` file contains Go benchmark output with 10 iterations for statistical significance:

```
# coregex internal benchmarks - v0.6.0
# Generated: 2025-11-28T12:00:00+00:00

BenchmarkReverseSuffix/IsMatch/1KB-12    10    307.2 ns/op    0 B/op    0 allocs/op
...
```

## Best Practices

1. **Run on same machine** - benchmarks are machine-specific
2. **Close other programs** - reduce noise from background processes
3. **Use 10+ iterations** - for statistical significance (default: 10)
4. **Tag releases** - run `./scripts/bench.sh vX.Y.Z` before each release

## Interpreting Results

```
                        │  v0.5.0  │           v0.6.0            │
                        │  sec/op  │  sec/op   vs base           │
ReverseSuffix/1KB-12      100.0µ     307.2n  -99.69% (p=0.000)
```

- `~` = no significant difference (likely noise)
- `-99.69%` = 99.69% faster (improvement)
- `+50.00%` = 50% slower (regression)
- `p=0.000` = statistically significant
