# destructive-command-guard-go

A Go implementation of a destructive command guard that analyzes shell commands
for destructive operations (rm -rf, git push --force, DROP TABLE, etc.) and
returns allow/deny/ask decisions based on configurable policies.

## Quick Start

```bash
make build          # Build the binary
make test           # Run fast unit tests
make test-integration  # Run heavy cross-cutting library tests
make test-external  # Run black-box binary subprocess tests
make test-all       # Run the three tiers plus benchmarks
make help           # See all available targets
```

## Project Structure

```
cmd/dcg-go/         CLI entry point (hook mode, test mode, packs mode)
guard/              Public API — guard.Evaluate(command, ...options)
internal/
  evalcore/         Shared types (Severity, Decision, Policy, Result, etc.)
  parse/            Tree-sitter shell command parsing and AST analysis
  eval/             Evaluation pipeline — matching, severity, policy decisions
  packs/            Pack definitions (git, filesystem, database, infra, k8s, etc.)
internal/integration/  Heavy cross-cutting library tests (property, fault, oracle, stress, mutation, comparison)
tests/external/        Black-box binary subprocess tests
scripts/            CI tier scripts
docs/plans/         Architecture and plan documents
```

## Test Categories

Tests follow a naming convention that maps to Makefile targets:

| Prefix | What it tests | Makefile target |
|--------|---------------|-----------------|
| `Test` (no special prefix) | Unit/integration logic | `make test` |
| `TestProperty*` | Invariants across random inputs | `make test-integration` |
| `TestFault*` | Error paths and edge cases | `make test-integration` |
| `TestOracle*` | Correctness via cross-checking | `make test-integration` |
| `TestGolden*` | Golden corpus validation | `make test-integration` |
| `TestDeterministic*` | Consistency across runs | `make test-integration` |
| `TestStress*` | Concurrency, load, memory | `make test-integration` |
| `TestSecurity*` | Evasion, isolation, heap bounds | `make test-integration` |
| `TestMutation*` | Mutation testing kill rates | `make test-integration` |
| `TestComparison*` | Upstream Rust comparison | `make test-comparison` |
| `TestExternal*` | Full binary subprocess tests | `make test-external` |
| `TestBenchmark*` | Benchmark stability validation | `make test` |
| `Benchmark*` | Go benchmarks | `make bench` |

## Rules for Adding Tests

**When adding new test files or test categories, always update the Makefile.**

Specifically:

1. **New test file in an existing category** — no Makefile change needed if it
   follows the naming convention above (e.g. a new `TestPropertyFoo` is
   automatically included in `make test`).

2. **New test category with a new prefix** — add a new Makefile target and
   document the prefix in the table above. Add it to `test-all` if it should
   run in the full suite.

3. **New heavy cross-cutting library test** — place it in `internal/integration/`.
   If it needs external dependencies, add install steps to `make deps`.

4. **New black-box binary test** — place it in `tests/external/` with a
   `TestExternal` prefix.

5. **New CI tier test** — update the appropriate `scripts/ci_tier*.sh` script
   AND document which tier it belongs to in the test file's comment header.

6. **New benchmark** — ensure `make bench` discovers it. If it's in a new
   package, add the package to the `bench` target in the Makefile.

7. **Tests requiring external tools or binaries** — add installation to
   `make deps` and document the requirement in this file.

## Coding Conventions

- **Packs**: each pack category gets its own file in `internal/packs/`. Rules
  use `MatchFunc` closures for matching logic.
- **Policy types**: `StrictPolicy()`, `InteractivePolicy()`, `PermissivePolicy()`
  in the `guard` package (public). Canonical definitions in `internal/evalcore/policy.go`.
- **No import cycles**: `internal/eval` tests must NOT import `guard` (which
  imports `internal/eval`). Use the internal API directly in `internal/eval` tests.
- **Golden files**: stored in `{package}/testdata/golden/`. Use TSV format for
  corpus files, text format for per-pack golden files.
- **Test helpers**: shared heavy-suite helpers go in `internal/integration/`.
  Shared binary-subprocess helpers go in `tests/external/`. Per-package helpers
  go in `{package}/helpers_test.go` or `{package}/pack_helpers_test.go`.
