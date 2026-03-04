# destructive-command-guard-go

A command-line tool and Go library that analyzes shell commands for destructive
operations and returns allow/deny/ask decisions. Designed to be used as a
[Claude Code hook](https://docs.anthropic.com/en/docs/claude-code/hooks) to
intercept dangerous commands before they execute.

It parses commands using tree-sitter grammars (bash, zsh, fish), matches them
against a library of destructive patterns organized into packs, and applies a
configurable policy to produce a decision.

## Installation

Requires Go 1.24+.

```bash
make build
# Binary is at ./build/dcg-go
```

## Usage

### As a Claude Code hook

The primary use case. Configure `dcg-go` as a `PreToolUse` hook in your Claude
Code settings so it intercepts Bash commands before execution:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "/path/to/dcg-go"
          }
        ]
      }
    ]
  }
}
```

In hook mode, `dcg-go` reads a JSON event from stdin and writes a JSON response
to stdout with a `permissionDecision` of `"allow"`, `"deny"`, or `"ask"`.

### Test mode

Evaluate commands directly from the command line:

```bash
# Simple check
dcg-go test "rm -rf /tmp/important"
# Decision: Deny  Severity: Critical  Pack: core.filesystem

# JSON output
dcg-go test --json "git push --force origin main"

# With explanation
dcg-go test --explain "DROP TABLE users;"

# Override policy
dcg-go test --policy permissive "git push --force"

# Pass caller environment for context-aware evaluation
dcg-go test --env "RAILS_ENV=production rails db:reset"
```

Exit codes: `0` = Allow, `1` = Error, `2` = Deny, `3` = Ask.

### List packs

```bash
dcg-go packs          # Human-readable
dcg-go packs --json   # Machine-readable
```

## Go Library

Use `guard.Evaluate` directly in Go code:

```go
import "github.com/dcosson/destructive-command-guard-go/guard"

result := guard.Evaluate("rm -rf /",
    guard.WithPolicy(guard.InteractivePolicy()),
)

switch result.Decision {
case guard.Deny:
    fmt.Println("Blocked:", result.Matches[0].Reason)
case guard.Ask:
    fmt.Println("Needs confirmation")
case guard.Allow:
    fmt.Println("Safe to proceed")
}
```

### Options

```go
guard.WithPolicy(p Policy)              // Strict, Interactive (default), or Permissive
guard.WithAllowlist("git push *")       // Glob patterns to always allow
guard.WithBlocklist("rm -rf /*")        // Glob patterns to always deny
guard.WithPacks("core.git")             // Only evaluate specific packs
guard.WithDisabledPacks("frameworks")   // Skip specific packs
guard.WithEnv(os.Environ())             // Pass environment for context-aware rules
```

### Policies

| Policy | Indeterminate | Low | Medium | High | Critical |
|--------|:---:|:---:|:---:|:---:|:---:|
| **Strict** | Deny | Allow | Deny | Deny | Deny |
| **Interactive** (default) | Ask | Allow | Ask | Deny | Deny |
| **Permissive** | Allow | Allow | Allow | Ask | Deny |

### Result

```go
type Result struct {
    Decision   Decision     // Allow, Deny, or Ask
    Assessment *Assessment  // Aggregate severity + confidence
    Matches    []Match      // Individual pattern matches (pack, rule, reason, remediation)
    Warnings   []Warning    // Parse warnings (partial parse, truncation, etc.)
    Command    string       // The original command
}
```

## Pattern Packs

Commands are matched against rule packs organized by domain:

| Pack | Description |
|------|-------------|
| `core.git` | Force push, reset, clean, branch deletion |
| `core.filesystem` | rm -rf, dd, mkfs, shred, truncate |
| `database.postgresql` | DROP, TRUNCATE, pg_restore --clean, dropdb |
| `database.mysql` | DROP, TRUNCATE, mysqladmin drop |
| `database.sqlite` | DROP, .restore, vacuum into |
| `database.mongodb` | dropDatabase, dropCollection, mongorestore --drop |
| `database.redis` | FLUSHALL, FLUSHDB, CONFIG SET, DEBUG |
| `frameworks` | rails db:drop, db:reset in production |

Each pack contains safe patterns (known non-destructive commands) and
destructive patterns with severity ratings and remediation suggestions.

## Project Structure

```
cmd/dcg-go/             CLI entry point (hook, test, packs modes)
guard/                  Public API (Evaluate, policies, options, types)
internal/
  parse/                Tree-sitter shell parsing and AST analysis
  eval/                 Evaluation pipeline (matching, severity, policy)
  packs/                Pack definitions and registry
  internal/e2etest/              Test infrastructure (mutation, comparison, e2e, stress)
scripts/                CI tier scripts
docs/plans/             Architecture and plan documents
```

## Development

### Prerequisites

- Go 1.24+
- Optional: `make deps` installs staticcheck for linting

### Build and test

```bash
make build          # Build the binary
make test           # Fast unit suite
make test-unit      # Core packages only (~5s)
make test-e2e       # E2E subprocess tests (~2s)
make lint           # go vet + staticcheck
make help           # All available targets
```

### Extended test suites

```bash
make test-stress      # Concurrent eval, high-volume fuzz, memory pressure
make test-security    # Evasion, isolation, heap growth bounds
make test-mutation    # Mutation operator kill rates
make test-race        # Full suite with Go race detector
make bench            # Benchmarks (validation, single iteration)
make bench-full       # Benchmarks (full, 3s x 5 runs)
make test-all         # Everything above
```

### Comparison testing

If you have the upstream Rust implementation, run comparison tests to verify
behavioral equivalence:

```bash
make test-comparison UPSTREAM_BINARY=/path/to/upstream-dcg
```

### CI tiers

Curated test subsets for different CI stages:

| Tier | Target | Time budget | When |
|------|--------|-------------|------|
| 1 | `make test-ci-tier1` | <5s | Every commit |
| 2 | `make test-ci-tier2` | <30s | Every PR |
| 3 | `make test-ci-tier3` | <60m | Nightly |

### Test naming conventions

Tests follow a prefix-based naming convention that maps to Makefile targets:

| Prefix | What it tests | Target |
|--------|---------------|--------|
| `TestProperty*` | Invariants across random inputs | `make test-integration` / `internal/e2etest` |
| `TestFault*` | Error paths and edge cases | `make test-integration` / `internal/e2etest` |
| `TestOracle*` | Correctness via cross-checking | `make test` |
| `TestGolden*` | Golden corpus validation | `make test` |
| `TestDeterministic*` | Consistency across runs | `make test` |
| `TestE2E*` | Full binary subprocess tests | `make test-e2e` |
| `TestStress*` | Concurrency, load, memory | `make test-stress` |
| `TestSecurity*` | Evasion, isolation, heap bounds | `make test-security` |
| `TestMutation*` | Mutation testing kill rates | `make test-mutation` |
| `TestComparison*` | Upstream comparison | `make test-comparison` |
| `Benchmark*` | Go benchmarks | `make bench` |

### Configuration

`dcg-go` looks for a YAML config file at `~/.config/dcg-go/config.yaml`
(override with `DCG_CONFIG` env var). Config supports:

- Default policy selection
- Allowlist/blocklist patterns
- Pack enable/disable
- Environment variable passthrough

## License

TBD
