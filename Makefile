.PHONY: build clean deps lint test test-integration test-e2e test-stress \
       test-security test-mutation test-comparison \
       test-ci-tier1 test-ci-tier2 test-ci-tier3 test-all \
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

# Install optional tooling used by lint, e2e, and comparison targets.
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
# Lint
# --------------------------------------------------------------------------- #

lint:
	go vet ./...
	@if command -v staticcheck >/dev/null 2>&1; then \
		staticcheck ./...; \
	else \
		echo "staticcheck not installed — run 'make deps' to install"; \
	fi

# --------------------------------------------------------------------------- #
# Tests — primary targets
# --------------------------------------------------------------------------- #

# Fast unit tests. This is what you run most often.
# Excludes e2e/stress/security/mutation/comparison suites in e2etest and
# integration-tagged internal/eval tests.
test:
	go test ./cmd/dcg-go ./guard ./internal/envdetect ./internal/eval ./internal/parse ./internal/packs/... -count=1

# Integration tests (non-black-box) that are intentionally excluded from
# the unit loop. Currently includes heavy internal/eval corpus/property suites.
test-integration:
	go test -tags=e2e ./internal/eval -count=1

# Same as test but with -race detector enabled. Slower but catches data races.
test-race:
	go test ./cmd/dcg-go ./guard ./internal/envdetect ./internal/eval ./internal/parse ./internal/packs/... -count=1 -race

# --------------------------------------------------------------------------- #
# Tests — E2E (builds binary, runs subprocess tests)
# --------------------------------------------------------------------------- #

# End-to-end tests that build the dcg-go binary and exercise it as a
# subprocess. These test the full CLI surface: hook mode, test mode, packs
# mode, and real-world scenario evaluation.
test-e2e:
	go test ./e2etest -run '^TestE2E' -count=1 -v

# --------------------------------------------------------------------------- #
# Tests — stress, security, benchmarks (longer-running)
# --------------------------------------------------------------------------- #

# Stress tests: concurrent evaluation, high-volume fuzzing, memory pressure,
# mutation timeouts. These are CPU and memory intensive.
test-stress:
	go test ./e2etest -run '^TestStress' -count=1 -v -timeout 30m
	go test ./guard -run '^TestStress' -count=1 -v -timeout 10m
	go test ./cmd/dcg-go -run '^TestStress' -count=1 -v -timeout 10m

# Security tests: fuzz corpus cleanliness, golden file non-execution,
# subprocess isolation, heap growth bounds, env sensitivity, evasion checks.
test-security:
	go test ./e2etest -run '^TestSecurity' -count=1 -v -timeout 15m
	go test ./guard -run '^TestSecurity' -count=1 -v -timeout 15m
	go test ./cmd/dcg-go -run '^TestSecurity' -count=1 -v -timeout 15m

# Mutation testing harness: verifies mutation operators and kill rates.
test-mutation:
	go test ./e2etest -run '^Test(Mutation|DeterministicKnownMutationKill)' -count=1 -v

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
	UPSTREAM_BINARY=$(UPSTREAM_BINARY) go test ./e2etest -run '^TestComparison' -count=1 -v
	UPSTREAM_BINARY=$(UPSTREAM_BINARY) go test ./guard -run '^TestOracle.*Upstream' -count=1 -v
endif

# --------------------------------------------------------------------------- #
# Tests — CI tiers (curated subsets for commit / PR / nightly)
# --------------------------------------------------------------------------- #

# Tier 1: Commit-level smoke tests. Target: <5s.
test-ci-tier1:
	bash scripts/ci_tier1.sh

# Tier 2: PR-level tests. Target: <30s.
test-ci-tier2:
	bash scripts/ci_tier2.sh

# Tier 3: Nightly comprehensive tests + benchmarks. Target: <60m.
test-ci-tier3:
	bash scripts/ci_tier3.sh

# --------------------------------------------------------------------------- #
# Benchmarks
# --------------------------------------------------------------------------- #

# Run all benchmarks with a single iteration (validation mode).
bench:
	go test ./guard -run '^$$' -bench 'Benchmark' -benchtime=1x -count=1
	go test ./cmd/dcg-go -run '^$$' -bench 'Benchmark' -benchtime=1x -count=1
	go test -tags=e2e ./internal/eval -run '^$$' -bench 'Benchmark' -benchtime=1x -count=1
	go test ./e2etest -run '^$$' -bench 'Benchmark' -benchtime=1x -count=1

# Run benchmarks with full iterations for performance measurement.
bench-full:
	go test ./guard -run '^$$' -bench 'Benchmark' -benchtime=3s -count=5
	go test ./cmd/dcg-go -run '^$$' -bench 'Benchmark' -benchtime=3s -count=5
	go test -tags=e2e ./internal/eval -run '^$$' -bench 'Benchmark' -benchtime=3s -count=5
	go test ./e2etest -run '^$$' -bench 'Benchmark' -benchtime=3s -count=5

# --------------------------------------------------------------------------- #
# Aggregate targets
# --------------------------------------------------------------------------- #

# Run everything: unit, integration, e2e, stress, security, mutation, bench.
# Does NOT include comparison tests (needs UPSTREAM_BINARY).
test-all: test test-integration test-e2e test-stress test-security test-mutation bench

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
	@echo "  make lint               Run go vet + staticcheck"
	@echo ""
	@echo "Test (primary):"
	@echo "  make test               Fast unit tests"
	@echo "  make test-integration   Heavy integration tests (internal/eval, tagged)"
	@echo "  make test-race          Full tests with race detector"
	@echo ""
	@echo "Test (extended):"
	@echo "  make test-e2e           E2E tests (builds binary, subprocess tests)"
	@echo "  make test-stress        Stress tests (concurrent, memory, timeouts)"
	@echo "  make test-security      Security tests (evasion, heap, isolation)"
	@echo "  make test-mutation      Mutation testing harness"
	@echo "  make test-comparison    Comparison vs upstream (needs UPSTREAM_BINARY)"
	@echo ""
	@echo "Test (CI tiers):"
	@echo "  make test-ci-tier1      Commit-level smoke (<5s)"
	@echo "  make test-ci-tier2      PR-level tests (<30s)"
	@echo "  make test-ci-tier3      Nightly comprehensive (<60m)"
	@echo ""
	@echo "Benchmarks:"
	@echo "  make bench              Benchmarks (validation, 1 iteration)"
	@echo "  make bench-full         Benchmarks (full, 3s x 5 runs)"
	@echo ""
	@echo "Aggregate:"
	@echo "  make test-all           Everything except comparison tests"
