#!/bin/bash
set -euo pipefail

go build -o dcg-go ./cmd/dcg-go
export UPSTREAM_BINARY="${UPSTREAM_BINARY:-./upstream-dcg}"

UPSTREAM_BINARY="$UPSTREAM_BINARY" \
  go test -run TestComparisonAgainstUpstream ./internal/integration \
  -v -count=1
