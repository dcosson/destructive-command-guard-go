# destructive-command-guard-go

A command-line tool and Go library that analyzes shell commands and returns
`allow`, `deny`, or `ask` decisions. It is designed to be used as a
[Claude Code hook](https://docs.anthropic.com/en/docs/claude-code/hooks) to
intercept risky commands before they execute.

It parses bash commands with tree-sitter, matches them against registered
rules, and evaluates the matching rules by category (destructive,
privacy-sensitive, or both) and severity (`Low`, `Medium`, `High`, or
`Critical`).

At a high level, the library:

1. Parses bash command text with tree-sitter.
2. Extracts command structure from the syntax tree.
3. Matches the command against registered rules, organized into packs.
4. Produces separate assessments for destructive risk and privacy risk.
5. Applies policy to turn those assessments into a final decision.

## Terminology

- A `rule` is one pattern the library knows how to recognize.
- A `pack` is a collection of related rules, such as Git, filesystem, or
  database operations.
- Each rule has a `RuleCategory`: `Destructive`, `Privacy`, or `Both`.
- Each rule also has a `Severity`: `Low`, `Medium`, `High`, or `Critical`.

In other words, this library does not just look for "dangerous commands" in the
abstract. It parses a bash command, matches it against typed rules, and then
evaluates the matched rules by category and severity.

## Policies

You set a separate policy for destructive rules and privacy rules. Each
policy converts a severity assessment into an `Allow`, `Deny`, or `Ask`
decision. The two decisions are then merged: the strictest one wins
(`Deny` > `Ask` > `Allow`).

This lets you configure different risk tolerances for each category. For
example, you might be comfortable with destructive commands (you know what
you're doing) but want strict protection against anything touching private
data.

| Severity | Allow All | Permissive | Moderate | Strict | Interactive (default) |
|--------|:---:|:---:|:---:|:---:|:---:|
| **Indeterminate** | Allow | Allow | Deny | Deny | Ask |
| **Low** | Allow | Allow | Allow | Deny | Allow |
| **Medium** | Allow | Allow | Allow | Deny | Ask |
| **High** | Allow | Allow | Deny | Deny | Ask |
| **Critical** | Allow | Deny | Deny | Deny | Deny |

Only **Interactive** ever returns `Ask`. All other policies return only
`Allow` or `Deny`.

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

# Override both category policies together
dcg-go test --policy permissive "git push --force"

# Override category policies independently
dcg-go test --destructive-policy permissive --privacy-policy strict "cat ~/.ssh/id_rsa"

# Pass caller environment for context-aware evaluation
dcg-go test --env "RAILS_ENV=production rails db:reset"
```

Exit codes: `0` = Allow, `1` = Error, `2` = Deny, `3` = Ask.

### List packs and rules

```bash
dcg-go list packs         # Human-readable pack summary
dcg-go list packs --json  # Machine-readable pack summary
dcg-go list rules         # Human-readable rules grouped by category
dcg-go list rules --json  # Machine-readable rule metadata
```

## Go Library

Use `guard.Evaluate` directly in Go code:

```go
import "github.com/dcosson/destructive-command-guard-go/guard"

result := guard.Evaluate("rm -rf /",
    guard.WithDestructivePolicy(guard.ModeratePolicy()),
    guard.WithPrivacyPolicy(guard.StrictPolicy()),
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

Each match includes the rule category and severity that contributed to the
decision. The result keeps destructive and privacy assessments separate,
then merges them into the final `Allow` / `Ask` / `Deny` decision (the
strictest decision from either category wins).

### Options

```go
guard.WithDestructivePolicy(p Policy)   // Set the destructive policy
guard.WithPrivacyPolicy(p Policy)       // Set the privacy policy
guard.WithAllowlist("git push *")       // Glob patterns to always allow
guard.WithBlocklist("rm -rf /*")        // Glob patterns to always deny
guard.WithPacks("core.git")             // Only evaluate specific packs
guard.WithDisabledPacks("frameworks")   // Skip specific packs
guard.WithEnv(os.Environ())             // Pass environment for context-aware rules
```

### Result

```go
type Result struct {
    Decision              Decision     // Allow, Deny, or Ask
    DestructiveAssessment *Assessment  // Aggregate destructive severity + confidence
    PrivacyAssessment     *Assessment  // Aggregate privacy severity + confidence
    Matches               []Match      // Individual matched rules
    Warnings              []Warning    // Parse warnings (partial parse, truncation, etc.)
    Command               string       // The original command
}
```

## Pattern Packs

Commands are matched against rule packs organized by domain. Packs contain rules
whose categories are destructive, privacy-sensitive, or both.

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
| `personal.*` | personal files and SSH material, including privacy-sensitive paths |
| `secrets.*` | secret-management tools and sensitive secret access patterns |
| `macos.privacy` | privacy-sensitive macOS automation and data access |

Each rule records a category, severity, reason, and remediation. Some packs
also include safe patterns that short-circuit evaluation for known
non-destructive cases.

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
(override with `DCG_CONFIG` env var):

```yaml
destructive_policy: moderate    # allow-all, permissive, moderate, strict, interactive
privacy_policy: strict
allowlist:
  - "git status *"
blocklist:
  - "rm -rf /*"
enabled_packs:
  - core.git
  - core.filesystem
disabled_packs:
  - frameworks
```

## License

TBD
