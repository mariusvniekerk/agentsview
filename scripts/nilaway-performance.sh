#!/bin/bash
# Compare standalone NilAway with the golangci-lint module plugin build.
set -euo pipefail

CUSTOM_GCL="${CUSTOM_GCL:-./custom-gcl}"
NILAWAY_INCLUDE_PKGS="${NILAWAY_INCLUDE_PKGS:-github.com/wesm/agentsview}"
NILAWAY_PERF_RUNS="${NILAWAY_PERF_RUNS:-3}"

if ! command -v nilaway >/dev/null 2>&1; then
    echo "nilaway not found. Install with: make lint-tools" >&2
    exit 1
fi
if [ ! -x "$CUSTOM_GCL" ]; then
    echo "$CUSTOM_GCL not found. Build it with: make nilaway-golangci-build" >&2
    exit 1
fi
if ! [[ "$NILAWAY_PERF_RUNS" =~ ^[0-9]+$ ]] || [ "$NILAWAY_PERF_RUNS" -lt 1 ]; then
    echo "NILAWAY_PERF_RUNS must be a positive integer" >&2
    exit 1
fi

measure() {
    local label="$1"
    shift

    printf '%s\n' "$label"
    for run in $(seq 1 "$NILAWAY_PERF_RUNS"); do
        printf '  run %s/%s: ' "$run" "$NILAWAY_PERF_RUNS"
        /usr/bin/time -p "$@" >/dev/null
    done
}

measure "standalone nilaway" \
    nilaway -test=false -include-pkgs="$NILAWAY_INCLUDE_PKGS" ./...
measure "golangci-lint nilaway module" \
    "$CUSTOM_GCL" run --config .golangci.nilaway.yml ./...
