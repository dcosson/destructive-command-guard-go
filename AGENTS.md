# Agent Guidelines — destructive-command-guard-go

Instructions for AI agents working on this project.

## Build & Test

Use the Makefile for all build and test operations:

```bash
make build              # Build the binary
make test               # Unit + integration tests — run this after every change
make test-e2e           # E2E subprocess tests
make test-all           # Full suite (unit + e2e + stress + security + mutation + bench)
make lint               # go vet + staticcheck
make help               # Full target listing
```

Always run `make test` before committing. Run `make test-e2e` if you changed
CLI behavior, hook mode, or the public API surface.

## Adding New Tests — Makefile Rules

**Every time you add a new kind of test, update the Makefile.**

This is a hard rule. The Makefile is the single entry point for running tests,
and CI tiers, developer workflows, and other agents all depend on it being
complete.

### Checklist when adding tests:

1. **Does your test follow an existing naming convention?**
   (TestProperty*, TestFault*, TestE2E*, TestStress*, TestSecurity*, etc.)
   - Yes → no Makefile change needed, the existing `-run` patterns will pick it up.
   - No → you must add a new target or update an existing `-run` pattern.

2. **Does your test belong in a CI tier?**
   - Tier 1 (commit, <5s): fast deterministic checks → update `scripts/ci_tier1.sh`
   - Tier 2 (PR, <30s): property + fault + oracle checks → update `scripts/ci_tier2.sh`
   - Tier 3 (nightly, <60m): stress + benchmark + security → update `scripts/ci_tier3.sh`

3. **Does your test need external dependencies?**
   - Add install steps to the `deps` target in the Makefile.
   - Document the dependency in CLAUDE.md under the test categories table.

4. **Is it a benchmark (func Benchmark*)?**
   - Ensure `make bench` runs it. If you added benchmarks in a new package,
     add that package to the `bench` and `bench-full` targets.

5. **Is it an E2E test that builds and runs the binary?**
   - Use the `TestE2E` prefix.
   - Place it in `internal/e2etest/` (current convention).
   - Ensure `make test-e2e` picks it up.

### Example: adding a new test category

If you create tests with a new prefix like `TestChaos*`:

```makefile
# In Makefile, add:
test-chaos:
	go test ./internal/e2etest -run '^TestChaos' -count=1 -v -timeout 30m

# Update test-all:
test-all: test test-e2e test-stress test-security test-mutation test-chaos bench
```

Then update the test categories table in CLAUDE.md.

## Package Structure

```
guard/              Public API (guard.Evaluate, policies, options)
internal/eval/      Evaluation pipeline (DO NOT import guard from tests here)
internal/packs/     Pack rule definitions
internal/parse/     Tree-sitter command parsing
internal/e2etest/           Shared black-box/E2E/stress/mutation/comparison test infrastructure
cmd/dcg-go/         CLI binary
```

## Import Rules

- `guard` imports `internal/eval`, `internal/packs`, `internal/parse`
- `internal/eval` tests must NOT import `guard` — this creates an import cycle
- `internal/e2etest` may import `guard` (it's a leaf test package)
- `cmd/dcg-go` imports `guard`

## Git Workflow

- Always commit after completing a chunk of work with passing tests.
- Push after committing on non-main branches.
- Use descriptive commit messages: `{package}: {what changed}`
- Run `make test` before every commit. Run `make test-e2e` if CLI behavior changed.
