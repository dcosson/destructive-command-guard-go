# destructive-command-guard-go

A Go implementation of a destructive command guard that analyzes shell commands
for destructive operations (rm -rf, git push --force, DROP TABLE, etc.) and
returns allow/deny/ask decisions based on configurable policies.

## Quick Start

```bash
make build          # Build the binary
make test           # Run unit + integration tests
make test-e2e       # Run E2E tests (builds binary, tests via subprocess)
make test-all       # Run everything
make help           # See all available targets
```

## Project Structure

```
cmd/dcg-go/         CLI entry point (hook mode, test mode, packs mode)
guard/              Public API — guard.Evaluate(command, ...options)
internal/
  parse/            Tree-sitter shell command parsing and AST analysis
  eval/             Evaluation pipeline — matching, severity, policy decisions
  packs/            Pack definitions (git, filesystem, database, infra, k8s, etc.)
  testharness/      Advanced test infrastructure (mutation, comparison, e2e, stress)
scripts/            CI tier scripts
docs/plans/         Architecture and plan documents
```

## Test Categories

Tests follow a naming convention that maps to Makefile targets:

| Prefix | What it tests | Makefile target |
|--------|---------------|-----------------|
| `Test` (no special prefix) | Unit/integration logic | `make test` |
| `TestProperty*` | Invariants across random inputs | `make test` |
| `TestFault*` | Error paths and edge cases | `make test` |
| `TestOracle*` | Correctness via cross-checking | `make test` |
| `TestGolden*` | Golden corpus validation | `make test` |
| `TestDeterministic*` | Consistency across runs | `make test` |
| `TestE2E*` | Full binary subprocess tests | `make test-e2e` |
| `TestStress*` | Concurrency, load, memory | `make test-stress` |
| `TestSecurity*` | Evasion, isolation, heap bounds | `make test-security` |
| `TestMutation*` | Mutation testing kill rates | `make test-mutation` |
| `TestComparison*` | Upstream Rust comparison | `make test-comparison` |
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

3. **New E2E test** — place it in `internal/testharness/` with a `TestE2E`
   prefix, or in a dedicated `e2etests/` directory. If it needs external
   dependencies, add install steps to `make deps`.

4. **New CI tier test** — update the appropriate `scripts/ci_tier*.sh` script
   AND document which tier it belongs to in the test file's comment header.

5. **New benchmark** — ensure `make bench` discovers it. If it's in a new
   package, add the package to the `bench` target in the Makefile.

6. **Tests requiring external tools or binaries** — add installation to
   `make deps` and document the requirement in this file.

## Coding Conventions

- **Packs**: each pack category gets its own file in `internal/packs/`. Rules
  use `MatchFunc` closures for matching logic.
- **Policy types**: `StrictPolicy()`, `InteractivePolicy()`, `PermissivePolicy()`
  in the `guard` package (public). Internal equivalents in `internal/eval/policy.go`.
- **No import cycles**: `internal/eval` tests must NOT import `guard` (which
  imports `internal/eval`). Use the internal API directly in `internal/eval` tests.
- **Golden files**: stored in `{package}/testdata/golden/`. Use TSV format for
  corpus files, text format for per-pack golden files.
- **Test helpers**: shared test utilities go in `internal/testharness/`. Per-package
  helpers go in `{package}/helpers_test.go` or `{package}/pack_helpers_test.go`.
