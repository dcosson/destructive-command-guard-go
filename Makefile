.PHONY: build clean deps fmt fmt-check check check-nofix \
       test test-integration test-external test-comparison test-all \
       bench bench-full test-race help

# Build output directory
BUILD_DIR ?= ./build

# Default binary name
BINARY ?= dcg-go

# Upstream binary for comparison tests (set to path of original Rust implementation)
UPSTREAM_BINARY ?=

# --------------------------------------------------------------------------- #
# Build
# --------------------------------------------------------------------------- #

build:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY) ./cmd/dcg-go

clean:
	rm -rf $(BUILD_DIR)
	go clean -testcache

# --------------------------------------------------------------------------- #
# Dependencies
# --------------------------------------------------------------------------- #

# Install optional tooling used by lint, external tests, and comparison targets.
# The core test suite (make test) has no external dependencies beyond the
# Go toolchain — this target is only needed for extended testing.
deps:
	@echo "==> Installing staticcheck (lint)..."
	go install honnef.co/go/tools/cmd/staticcheck@latest
	@echo "==> Go toolchain and module dependencies..."
	go mod download
	@echo ""
	@echo "All deps installed."
	@echo ""
	@echo "For comparison tests, you also need the upstream Rust binary."
	@echo "Set UPSTREAM_BINARY=/path/to/upstream-dcg when running make test-comparison."

# --------------------------------------------------------------------------- #
# Formatting & checks
# --------------------------------------------------------------------------- #

# Format all Go source files in place.
fmt:
	gofmt -w .

# Check formatting without modifying files (fails if unformatted).
fmt-check:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && echo "above files are not formatted" && exit 1)

# Format, then run vet + staticcheck.
check: fmt
	@echo "==> go vet"
	go vet ./...
	@echo "==> staticcheck"
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...

# CI version: check formatting without fixing, then vet + staticcheck.
check-nofix: fmt-check
	@echo "==> go vet"
	go vet ./...
	@echo "==> staticcheck"
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...

# --------------------------------------------------------------------------- #
# Tests — primary targets
# --------------------------------------------------------------------------- #

# Fast unit tests. This is what you run most often.
# Runs all packages except internal/integration/, which contains the heavy
# cross-cutting library suites (property, fault, security, oracle, stress,
# benchmark, fuzz, mutation, comparison).
test:
	go test $$(go list ./... | grep -v internal/integration | grep -v tests/external) -count=1

# Heavy integration suites: internal/integration package + build-tagged
# tests in cmd/dcg-go (fuzz, oracle, stress, security, property tests).
test-integration:
	go test ./internal/integration -count=1 -timeout 30m
	go test -tags=integration ./cmd/dcg-go -count=1 -timeout 10m

# Unit test target with -race detector enabled (same package set as `make test`).
# Does not include integration/external/comparison targets.
test-race:
	go test $$(go list ./... | grep -v internal/integration | grep -v tests/external) -count=1 -race

# --------------------------------------------------------------------------- #
# Tests — external (builds binary, tests CLI as black box)
# --------------------------------------------------------------------------- #

test-external:
	go test ./tests/external -count=1 -v

# --------------------------------------------------------------------------- #
# Tests — comparison (requires UPSTREAM_BINARY)
# --------------------------------------------------------------------------- #

# Run comparison tests against the upstream Rust implementation.
# Requires UPSTREAM_BINARY to be set to the path of the original binary.
#
#   make test-comparison UPSTREAM_BINARY=./upstream-dcg
#
test-comparison:
ifndef UPSTREAM_BINARY
	@echo "UPSTREAM_BINARY is not set. Skipping comparison tests."
	@echo "Usage: make test-comparison UPSTREAM_BINARY=/path/to/upstream-dcg"
else
	UPSTREAM_BINARY=$(UPSTREAM_BINARY) go test ./internal/integration -run '^TestComparison|^TestOracle.*Upstream' -count=1 -v
endif

# --------------------------------------------------------------------------- #
# Benchmarks
# --------------------------------------------------------------------------- #

# Run all benchmarks with a single iteration (validation mode).
bench:
	go test ./cmd/dcg-go -run '^$$' -bench 'Benchmark' -benchtime=1x -count=1
	go test -tags=e2e ./internal/eval -run '^$$' -bench 'Benchmark' -benchtime=1x -count=1
	go test ./internal/integration -run '^$$' -bench 'Benchmark' -benchtime=1x -count=1

# Run benchmarks with full iterations for performance measurement.
bench-full:
	go test ./cmd/dcg-go -run '^$$' -bench 'Benchmark' -benchtime=3s -count=5
	go test -tags=e2e ./internal/eval -run '^$$' -bench 'Benchmark' -benchtime=3s -count=5
	go test ./internal/integration -run '^$$' -bench 'Benchmark' -benchtime=3s -count=5

# --------------------------------------------------------------------------- #
# Aggregate targets
# --------------------------------------------------------------------------- #

# Run everything: unit, integration, external, and benchmarks.
# Does NOT include comparison tests (needs UPSTREAM_BINARY).
test-all: test test-integration test-external bench

# --------------------------------------------------------------------------- #
# Help
# --------------------------------------------------------------------------- #

help:
	@echo "destructive-command-guard-go"
	@echo ""
	@echo "Build:"
	@echo "  make build              Build the dcg-go binary"
	@echo "  make clean              Remove build artifacts and test cache"
	@echo "  make deps               Install optional tooling (staticcheck)"
	@echo "  make fmt                Format all Go source files"
	@echo "  make check              Format + vet + staticcheck"
	@echo "  make check-nofix        Check formatting + vet + staticcheck (CI, no edits)"
	@echo ""
	@echo "Test (primary):"
	@echo "  make test               Fast unit tests"
	@echo "  make test-integration   Heavy library integration tests"
	@echo "  make test-external      Black-box binary subprocess tests"
	@echo "  make test-race          Unit tests with race detector (same scope as make test)"
	@echo ""
	@echo "Test (extended):"
	@echo "  make test-comparison    Comparison vs upstream (needs UPSTREAM_BINARY)"
	@echo ""
	@echo "Benchmarks:"
	@echo "  make bench              Benchmarks (validation, 1 iteration)"
	@echo "  make bench-full         Benchmarks (full, 3s x 5 runs)"
	@echo ""
	@echo "Aggregate:"
	@echo "  make test-all           Everything except comparison tests"
