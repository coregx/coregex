#!/bin/bash
# Benchmark runner with versioning support for benchstat comparison
# Usage:
#   ./scripts/bench.sh              # Run and save as current version
#   ./scripts/bench.sh v0.6.0       # Run and save as specific version
#   ./scripts/bench.sh --compare    # Compare current with latest baseline
#   ./scripts/bench.sh --compare v0.5.0 v0.6.0  # Compare two versions

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BASELINES_DIR="$PROJECT_DIR/benchmark/baselines"
BENCH_COUNT=10  # Number of iterations for statistical significance

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Get current version from git tag or default
get_version() {
    if [ -n "$1" ]; then
        echo "$1"
    else
        git describe --tags --always 2>/dev/null || echo "dev"
    fi
}

# Run benchmarks and save to file
run_benchmarks() {
    local version="$1"
    local output_file="$BASELINES_DIR/${version}.txt"

    echo -e "${GREEN}Running benchmarks for version: ${version}${NC}"
    echo -e "${YELLOW}Count: ${BENCH_COUNT} iterations${NC}"

    mkdir -p "$BASELINES_DIR"

    cd "$PROJECT_DIR"

    # Run coregex-only benchmarks (internal)
    echo "# coregex internal benchmarks - ${version}" > "$output_file"
    echo "# Generated: $(date -Iseconds)" >> "$output_file"
    echo "" >> "$output_file"

    # Meta package benchmarks (most important)
    go test -bench=. -benchmem -count="$BENCH_COUNT" -benchtime=100ms ./meta/... 2>/dev/null >> "$output_file" || true

    # SIMD benchmarks
    go test -bench=. -benchmem -count="$BENCH_COUNT" -benchtime=100ms ./simd/... 2>/dev/null >> "$output_file" || true

    # DFA benchmarks
    go test -bench=. -benchmem -count="$BENCH_COUNT" -benchtime=100ms ./dfa/... 2>/dev/null >> "$output_file" || true

    # Prefilter benchmarks
    go test -bench=. -benchmem -count="$BENCH_COUNT" -benchtime=100ms ./prefilter/... 2>/dev/null >> "$output_file" || true

    echo -e "${GREEN}Saved to: ${output_file}${NC}"
    echo ""
}

# Compare two benchmark files using benchstat
compare_benchmarks() {
    local old_version="$1"
    local new_version="$2"

    local old_file="$BASELINES_DIR/${old_version}.txt"
    local new_file="$BASELINES_DIR/${new_version}.txt"

    if [ ! -f "$old_file" ]; then
        echo -e "${RED}Error: Baseline not found: ${old_file}${NC}"
        echo "Available baselines:"
        ls -1 "$BASELINES_DIR"/*.txt 2>/dev/null | xargs -n1 basename | sed 's/.txt$//'
        exit 1
    fi

    if [ ! -f "$new_file" ]; then
        echo -e "${RED}Error: Baseline not found: ${new_file}${NC}"
        exit 1
    fi

    echo -e "${GREEN}Comparing ${old_version} vs ${new_version}${NC}"
    echo ""

    benchstat "$old_file" "$new_file"
}

# Get latest baseline version
get_latest_baseline() {
    ls -1t "$BASELINES_DIR"/*.txt 2>/dev/null | head -1 | xargs basename 2>/dev/null | sed 's/.txt$//'
}

# Main
case "${1:-}" in
    --compare|-c)
        if [ -n "$2" ] && [ -n "$3" ]; then
            compare_benchmarks "$2" "$3"
        else
            # Compare latest baseline with current
            latest=$(get_latest_baseline)
            if [ -z "$latest" ]; then
                echo -e "${RED}No baselines found. Run benchmarks first.${NC}"
                exit 1
            fi
            current=$(get_version)
            if [ "$latest" = "$current" ]; then
                echo -e "${YELLOW}Current version is the latest baseline.${NC}"
                echo "Run with specific versions: $0 --compare v0.5.0 v0.6.0"
                exit 0
            fi
            run_benchmarks "$current"
            compare_benchmarks "$latest" "$current"
        fi
        ;;
    --list|-l)
        echo "Available baselines:"
        ls -1 "$BASELINES_DIR"/*.txt 2>/dev/null | xargs -n1 basename | sed 's/.txt$//' || echo "No baselines found."
        ;;
    --help|-h)
        echo "Usage:"
        echo "  $0              Run benchmarks and save as current version"
        echo "  $0 v0.6.0       Run benchmarks and save as specific version"
        echo "  $0 --compare    Compare current with latest baseline"
        echo "  $0 --compare v0.5.0 v0.6.0  Compare two versions"
        echo "  $0 --list       List available baselines"
        ;;
    *)
        version=$(get_version "$1")
        run_benchmarks "$version"
        ;;
esac
