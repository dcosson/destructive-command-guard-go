# Destructive Command Guard (Go) — Architecture

**Module**: `github.com/dcosson/destructive-command-guard-go`
**Binary**: `dcgo`
**Source**: [Shaping doc](../shaping/shaping.md) | [Frame](../shaping/frame.md)

---

## 1. System Overview

A pure-Go library and CLI that evaluates shell commands for destructive
patterns. Designed as a mistake-preventer for LLM coding agents — not a
security boundary.

**Key design principles:**

- **Library-first**: Core logic is a stateless Go package with no I/O.
  The CLI and hook modes are thin wrappers.
- **AST-first**: Tree-sitter bash parsing provides structural understanding
  of commands, eliminating the false-positive classes that plague regex-on-raw-text
  approaches.
- **Assessment ≠ Decision**: Pattern matching produces severity + confidence
  assessments. A separate policy layer converts assessments to decisions
  (Allow/Deny/Ask). Callers control their own risk tolerance.
- **Fail-open**: Parse errors, unknown constructs, and timeouts result in
  Allow, not Deny. We never block valid workflows due to analysis limitations.

### Architectural Divergence from Upstream (Rust)

The upstream Rust version uses a **regex-first** approach: it matches patterns
against the raw command string, with context sanitization to mask string
literals and reduce false positives. It uses `aho-corasick` SIMD-accelerated
matching and lazy regex compilation for sub-millisecond latency.

Our Go version uses a **tree-sitter-first** approach: we parse the command
into a full bash AST, then extract structured command invocations (command
name, arguments, flags, inline env vars) and match patterns against those
extracted fields.

**What this gives us:**

- **No context sanitization needed.** String arguments inside commands are
  structurally separated by the parser. `echo "don't rm -rf /"` parses as
  `echo` with a string argument — we never see `rm -rf` as a command.
- **Compound command awareness.** Pipelines, subshells, command substitutions,
  and `&&`/`||` chains are structurally decomposed. Each command in a pipeline
  is evaluated independently.
- **Heredoc/inline script detection is structural.** The bash AST already
  identifies heredoc bodies and string arguments to commands like `bash -c`.
  We don't need a separate trigger-detection tier.
- **Higher accuracy for flag/argument analysis.** We can distinguish
  `git push --force` from `git push --force-with-lease` structurally rather
  than with increasingly specific regex patterns.

**What we still need from the Rust approach:**

- **Command normalization.** The AST faithfully preserves `/usr/bin/git` as
  the command name. We still need to strip path prefixes to normalize
  command names for matching.
- **Keyword pre-filter.** Before spending time on tree-sitter parsing, a
  fast string-contains check on pack keywords lets us skip parsing entirely
  for harmless commands. This is our equivalent of the Rust version's
  aho-corasick quick-reject.

---

## 2. Component Diagram

```mermaid
graph TB
    subgraph "Public API (guard package)"
        Evaluate["guard.Evaluate(cmd, opts...)"]
        Result["Result{Decision, Matches}"]
        Options["Options: Policy, Allowlist,<br/>Blocklist, Packs, Env"]
    end

    subgraph "Evaluation Pipeline (internal/eval)"
        PreFilter["Keyword Pre-Filter"]
        Parser["Bash Parser (tree-sitter)"]
        Extractor["Command Extractor"]
        Normalizer["Command Normalizer"]
        EnvDetector["Environment Detector"]
        Matcher["Pattern Matcher"]
        PolicyEngine["Policy Engine"]
    end

    subgraph "Pattern Packs (internal/packs)"
        Registry["Pack Registry"]
        CorePacks["core.git, core.filesystem"]
        DBPacks["database.postgresql, mysql,<br/>sqlite, mongodb, redis"]
        InfraPacks["infrastructure.terraform,<br/>pulumi, ansible"]
        CloudPacks["cloud.aws, gcp, azure"]
        K8sPacks["kubernetes.kubectl, helm"]
        ContainerPacks["containers.docker, compose"]
        OtherPacks["frameworks, remote.rsync,<br/>secrets.vault, platform.github"]
    end

    subgraph "Inline Script Analysis (internal/inline)"
        ScriptDetector["Script Detector"]
        LangParsers["Language Parsers<br/>(python, ruby, js, etc.)"]
    end

    subgraph "Tree-sitter (external dep)"
        TSParser["treesitter/parser"]
        BashGrammar["grammars/bash"]
        OtherGrammars["grammars/python,<br/>ruby, js, etc."]
    end

    subgraph "CLI (cmd/dcgo)"
        HookMode["Hook Mode (stdin JSON)"]
        TestMode["Test Mode (dcgo test)"]
        PacksMode["Packs Mode (dcgo packs)"]
        Config["Config File Loader"]
    end

    Evaluate --> PreFilter
    PreFilter -->|"keywords match"| Parser
    PreFilter -->|"no keywords"| PolicyEngine
    Parser --> Extractor
    Extractor --> Normalizer
    Extractor --> ScriptDetector
    ScriptDetector --> LangParsers
    LangParsers --> Matcher
    Normalizer --> EnvDetector
    EnvDetector --> Matcher
    Matcher --> Registry
    Registry --> CorePacks & DBPacks & InfraPacks & CloudPacks & K8sPacks & ContainerPacks & OtherPacks
    Matcher --> PolicyEngine
    PolicyEngine --> Result

    HookMode --> Evaluate
    TestMode --> Evaluate
    Config --> Options
    Options --> Evaluate

    Parser --> TSParser
    TSParser --> BashGrammar
    LangParsers --> OtherGrammars
```

---

## 3. Layer Decomposition

### Layer 0: External Dependencies

| Dependency | Purpose |
|-----------|---------|
| `github.com/treesitter-go/treesitter` | Pure-Go tree-sitter runtime |
| `github.com/treesitter-go/treesitter/grammars/bash` | Bash grammar (to be exported from tree-sitter-go) |
| `github.com/treesitter-go/treesitter/grammars/python` | Python grammar (for inline script detection) |
| (other grammars as needed) | Ruby, JS, etc. for inline script detection |

### Layer 1: Core Library (`guard` package — public API)

The top-level `guard` package is the public API surface. It exposes:

```go
package guard

// Evaluate analyzes a shell command for destructive patterns.
// Stateless, no I/O. Safe for concurrent use.
func Evaluate(command string, opts ...Option) *Result

// Result contains the evaluation outcome.
type Result struct {
    Decision   Decision       // Allow, Deny, or Ask
    Assessment *Assessment    // Raw severity + confidence (nil if no match)
    Matches    []Match        // All pattern matches found
    Command    string         // The original command
}

type Decision int
const (
    Allow Decision = iota
    Deny
    Ask
)

type Assessment struct {
    Severity   Severity   // Critical, High, Medium, Low
    Confidence Confidence // High, Medium, Low
}

type Match struct {
    Pack        string   // e.g. "core.git"
    Rule        string   // e.g. "git-reset-hard"
    Severity    Severity
    Confidence  Confidence
    Reason      string   // Why this is dangerous
    Remediation string   // Suggested safe alternative
    EnvEscalated bool    // Was severity escalated due to production env?
}

type Severity int
const (
    Low Severity = iota
    Medium
    High
    Critical
)

type Confidence int
const (
    ConfidenceLow Confidence = iota
    ConfidenceMedium
    ConfidenceHigh
)

// Option configures evaluation behavior.
type Option func(*evalConfig)

func WithPolicy(p Policy) Option          // Set decision policy
func WithAllowlist(patterns ...string) Option  // Allow specific commands/patterns
func WithBlocklist(patterns ...string) Option  // Block specific commands/patterns
func WithPacks(packs ...string) Option     // Enable only these packs
func WithDisabledPacks(packs ...string) Option // Disable specific packs
func WithEnv(env []string) Option          // Provide process env vars for env detection

// Policy converts an Assessment into a Decision.
type Policy interface {
    Decide(Assessment) Decision
}

// Built-in policies
func StrictPolicy() Policy       // Deny on Medium+, no Ask
func InteractivePolicy() Policy  // Ask on Medium, Deny on High+
func PermissivePolicy() Policy   // Ask on High, Deny on Critical only
```

### Layer 2: Evaluation Pipeline (`internal/eval`)

Orchestrates the analysis steps. This is the core internal engine.

**Pipeline steps:**

1. **Allowlist/Blocklist check** — If the command matches a caller-provided
   allowlist pattern, short-circuit to Allow. If blocklist, short-circuit
   to Deny.
2. **Keyword pre-filter** — Check if the command string contains any keyword
   from enabled packs. If no keywords match, return Allow (no parsing needed).
3. **Tree-sitter parse** — Parse the command string as bash. If parsing fails,
   fail-open (Allow).
4. **Command extraction** — Walk the AST to extract individual command
   invocations: `(name, args, flags, inlineEnvVars)`.
5. **Dataflow resolution** — Forward pass through the AST tracking variable
   assignments (`DIR=/`, `export RAILS_ENV=production`). Substitute known
   variable values into subsequent commands' arguments and env vars. See
   §8 Alien Artifacts for details.
6. **Normalization** — Strip path prefixes from command names
   (`/usr/bin/git` → `git`).
7. **Inline script detection** — For commands like `python -c "..."`,
   `bash -c "..."`, extract the script body and analyze it with the
   appropriate language grammar.
8. **Environment detection** — Check inline env vars from the AST,
   dataflow-resolved env vars, and caller-provided process env vars for
   production indicators.
9. **Pattern matching** — For each extracted command, check against enabled
   packs. Safe patterns are checked first (short-circuit to Allow for that
   command). Then destructive patterns are checked.
10. **Assessment aggregation** — If multiple commands in a pipeline match,
    take the highest severity.
11. **Policy application** — Convert the final assessment to a decision
    using the configured policy.

### Layer 3: Pattern Packs (`internal/packs`)

Each pack is a Go struct registered in a global registry:

```go
type Pack struct {
    ID          string       // e.g. "core.git"
    Name        string       // e.g. "Git"
    Description string
    Keywords    []string     // For pre-filter: ["git"]
    Safe        []SafePattern
    Destructive []DestructivePattern
}

type SafePattern struct {
    Name    string
    Match   CommandMatcher  // Structural matcher, not regex
}

type DestructivePattern struct {
    Name        string
    Match       CommandMatcher
    Severity    Severity
    Confidence  Confidence
    Reason      string
    Remediation string
    EnvSensitive bool       // Escalate severity in production?
}
```

**Key difference from upstream**: Because we extract structured commands from
the AST, our `CommandMatcher` can be a structural matcher rather than a regex.
A matcher specifies: command name, required flags/args, and optional
negative conditions (e.g., "rm -rf" but NOT if target is under /tmp).

```go
// CommandMatcher matches against extracted command invocations.
type CommandMatcher interface {
    Match(cmd ExtractedCommand) bool
}

// ExtractedCommand is a single command invocation extracted from the AST.
type ExtractedCommand struct {
    Name       string            // Normalized command name
    Args       []string          // Positional arguments
    Flags      map[string]string // Flag name → value (or "" for boolean flags)
    InlineEnv  map[string]string // Inline env var assignments
    RawText    string            // Original text span from source
    InPipeline bool              // Is this part of a pipeline?
    Negated    bool              // Preceded by !
}
```

### Layer 4: Tree-sitter Integration (`internal/parse`)

Wraps tree-sitter-go for our specific needs:

- **Bash parsing**: Parse command strings into ASTs
- **Command extraction**: Walk bash AST to find `simple_command` nodes
- **Inline script extraction**: Detect `python -c`, `bash -c`, heredocs, etc.
  and extract the embedded script text
- **Multi-language parsing**: Parse extracted scripts with appropriate grammars

### Layer 5: CLI (`cmd/dcgo`)

Thin binary with three modes:

- **Hook mode** (default, no subcommand): Read JSON from stdin, evaluate,
  write JSON to stdout. Initially supports Claude Code protocol only.
- **Test mode** (`dcgo test "cmd"`): Evaluate a command and print the result.
  `--explain` for detailed reasoning.
- **Packs mode** (`dcgo packs`): List available packs with descriptions.
- **Config**: Optional YAML/TOML config file for allowlists, blocklists,
  pack selection.

---

## 4. Data Flow: Evaluation Pipeline

```mermaid
sequenceDiagram
    participant Caller
    participant Guard as guard.Evaluate()
    participant Filter as Keyword Pre-Filter
    participant TS as Tree-sitter Parser
    participant Extract as Command Extractor
    participant Inline as Inline Script Detector
    participant Env as Environment Detector
    participant Match as Pattern Matcher
    participant Policy as Policy Engine

    Caller->>Guard: Evaluate("RAILS_ENV=prod rails db:reset", opts...)
    Guard->>Guard: Check allowlist/blocklist
    Guard->>Filter: Contains pack keywords?
    Filter-->>Guard: Yes ("rails" matches frameworks pack)
    Guard->>TS: Parse as bash
    TS-->>Guard: AST
    Guard->>Extract: Walk AST → extract commands
    Extract-->>Guard: [{name: "rails", args: ["db:reset"],<br/>inlineEnv: {RAILS_ENV: "prod"}}]
    Guard->>Extract: Normalize command names
    Guard->>Env: Check inline env + caller env
    Env-->>Guard: Production indicators: [RAILS_ENV=prod]
    Guard->>Match: Match against frameworks pack
    Match-->>Guard: Assessment{Severity: Critical, Confidence: High}<br/>(rails db:reset + production env → escalated)
    Guard->>Policy: Decide(assessment)
    Policy-->>Guard: Deny
    Guard-->>Caller: Result{Decision: Deny, ...}
```

### Data Flow: Inline Script Detection

```mermaid
sequenceDiagram
    participant Extract as Command Extractor
    participant Detect as Script Detector
    participant PyParser as Python Parser
    participant Match as Pattern Matcher

    Extract->>Extract: Found: python -c "import os; os.system('rm -rf /')"
    Extract->>Detect: Detect inline script
    Detect->>Detect: Command "python" + flag "-c" → Python script
    Detect->>PyParser: Parse "import os; os.system('rm -rf /')"
    PyParser-->>Detect: Python AST
    Detect->>Detect: Extract function calls from AST
    Detect-->>Match: [{name: "os.system", args: ["rm -rf /"]}]
    Note over Match: Recursively evaluate "rm -rf /" <br/>through the main pipeline
```

### Data Flow: Hook Mode (Claude Code)

```mermaid
sequenceDiagram
    participant CC as Claude Code
    participant DCGO as dcgo binary
    participant Guard as guard.Evaluate()

    CC->>DCGO: stdin JSON: {"tool": "Bash",<br/>"input": {"command": "git push --force"}}
    DCGO->>DCGO: Parse JSON, extract command
    DCGO->>DCGO: Load config (allowlist, blocklist, packs)
    DCGO->>Guard: Evaluate("git push --force",<br/>WithPolicy(InteractivePolicy()),<br/>WithEnv(os.Environ()))
    Guard-->>DCGO: Result{Decision: Ask, ...}
    DCGO-->>CC: stdout JSON: {"decision": "ask",<br/>"reason": "git push --force overwrites remote history"}
```

---

## 5. Package Structure

```
destructive-command-guard-go/
├── guard/                          # Layer 1: Public API
│   ├── guard.go                    #   Evaluate(), Result, Decision, types
│   ├── option.go                   #   Option funcs, evalConfig
│   ├── policy.go                   #   Policy interface, built-in policies
│   └── guard_test.go               #   Public API tests
│
├── internal/
│   ├── eval/                       # Layer 2: Evaluation pipeline
│   │   ├── pipeline.go             #   Pipeline orchestration
│   │   ├── prefilter.go            #   Keyword pre-filter
│   │   ├── pipeline_test.go
│   │   └── prefilter_test.go
│   │
│   ├── parse/                      # Layer 4: Tree-sitter integration
│   │   ├── bash.go                 #   Bash parsing + command extraction
│   │   ├── dataflow.go             #   Variable tracking + resolution (Alien Artifact)
│   │   ├── normalize.go            #   Command name normalization
│   │   ├── inline.go               #   Inline script detection
│   │   ├── command.go              #   ExtractedCommand type
│   │   ├── bash_test.go
│   │   ├── dataflow_test.go
│   │   ├── normalize_test.go
│   │   └── inline_test.go
│   │
│   ├── packs/                      # Layer 3: Pattern packs
│   │   ├── registry.go             #   Pack registry, lookup
│   │   ├── matcher.go              #   CommandMatcher interface + impls
│   │   ├── pack.go                 #   Pack, SafePattern, DestructivePattern types
│   │   ├── core/
│   │   │   ├── git.go
│   │   │   └── filesystem.go
│   │   ├── database/
│   │   │   ├── postgresql.go
│   │   │   ├── mysql.go
│   │   │   ├── sqlite.go
│   │   │   ├── mongodb.go
│   │   │   └── redis.go
│   │   ├── containers/
│   │   │   ├── docker.go
│   │   │   └── compose.go
│   │   ├── infrastructure/
│   │   │   ├── terraform.go
│   │   │   ├── pulumi.go
│   │   │   └── ansible.go
│   │   ├── cloud/
│   │   │   ├── aws.go
│   │   │   ├── gcp.go
│   │   │   └── azure.go
│   │   ├── kubernetes/
│   │   │   ├── kubectl.go
│   │   │   └── helm.go
│   │   ├── frameworks/
│   │   │   └── frameworks.go
│   │   ├── remote/
│   │   │   └── rsync.go
│   │   ├── secrets/
│   │   │   └── vault.go
│   │   ├── platform/
│   │   │   └── github.go
│   │   └── registry_test.go
│   │
│   └── envdetect/                  # Environment detection
│       ├── detect.go               #   Production indicator detection
│       └── detect_test.go
│
├── cmd/
│   └── dcgo/                       # Layer 5: CLI binary
│       ├── main.go                 #   Entry point, subcommand routing
│       ├── hook.go                 #   Hook mode (stdin JSON → stdout JSON)
│       ├── test.go                 #   Test mode (dcgo test "cmd")
│       ├── packs.go                #   Packs mode (dcgo packs)
│       └── config.go               #   Config file loading
│
├── docs/
│   ├── shaping/                    #   Shaping docs
│   └── plans/                      #   Architecture + plan docs
│
├── go.mod
└── go.sum
```

**Import flow** (strictly layered — no upward imports):

```mermaid
graph TD
    CMD["cmd/dcgo"] --> GUARD["guard (public API)"]
    GUARD --> EVAL["internal/eval"]
    EVAL --> PARSE["internal/parse"]
    EVAL --> PACKS["internal/packs"]
    EVAL --> ENVDETECT["internal/envdetect"]
    PACKS --> PARSE
    PARSE --> TS["treesitter-go (external)"]
```

---

## 6. Key Architectural Decisions

### D1: Structural matching over regex

**Decision**: Pattern packs use `CommandMatcher` (structural matching against
extracted commands) rather than regex against raw command strings.

**Rationale**: Since we've already paid the cost of AST parsing, matching
against structured data is more accurate and easier to reason about. A
`CommandMatcher` that checks `name == "git" && hasFlag("--force")` is clearer
and less error-prone than a regex that tries to handle all the ways `--force`
could appear in a raw string.

**Trade-off**: Some patterns may still need raw-text matching for edge cases
(e.g., SQL statements passed as arguments). The `CommandMatcher` interface
allows both structural and text-based matchers.

### D2: No context sanitization

**Decision**: Omit the upstream's context sanitization pass.

**Rationale**: The upstream needs to mask string literals in the raw command
text before regex matching to avoid false positives (e.g., `echo "rm -rf /"`
would match `rm -rf` without sanitization). Our AST-first approach structurally
separates command invocations from their arguments, so string content is never
confused with command invocations. This is a direct advantage of the
tree-sitter-first architecture.

### D3: Single-pass keyword pre-filter

**Decision**: Use simple `strings.Contains` checks for keyword pre-filtering
rather than aho-corasick.

**Rationale**: With 21 packs and ~50 keywords, the pre-filter checks a small
number of short strings against the command. `strings.Contains` is fast enough
at this scale and avoids an external dependency. If benchmarks show this is a
bottleneck (unlikely), we can switch to aho-corasick later.

### D4: Fail-open on parse errors

**Decision**: If tree-sitter fails to parse a command (malformed bash, exotic
syntax), return Allow.

**Rationale**: This is a mistake preventer, not a security boundary. Blocking
commands we can't understand would create false denials that erode trust in
the tool. The upstream Rust version follows the same philosophy.

### D5: Assessment/Policy separation

**Decision**: Pattern matching produces raw assessments (severity + confidence).
A separate policy layer converts these to decisions (Allow/Deny/Ask).

**Rationale**: Different callers have different risk tolerances. A background
agent running autonomously should use `StrictPolicy` (no Ask — uncertain means
Deny). A user-facing interactive agent should use `InteractivePolicy` (uncertain
means Ask). The library ships sensible defaults but lets callers override.

### D6: Grammars exported from tree-sitter-go

**Decision**: Have tree-sitter-go export grammar packages publicly (e.g.,
`grammars/bash`) rather than vendoring grammar data into DCG.

**Rationale**: Keeps a single source of truth for grammar data. DCG imports
the grammars as a regular Go dependency. Requires a change to tree-sitter-go
to move grammars from `internal/testgrammars/` to a public `grammars/` package.

---

## 7. Cross-Cutting Concerns

### Performance

- **Benchmark suite**: Every pipeline stage benchmarked independently.
  Aggregate benchmarks for full evaluations of representative commands.
- **Pre-filter effectiveness**: Track what percentage of commands are
  rejected at the keyword stage (target: >90% of benign commands skip parsing).
- **No hard budget**: Performance should be invisible to users (LLM responses
  take seconds). Benchmark and optimize but don't enforce a strict timeout.

### Concurrency

- `guard.Evaluate()` is safe for concurrent use. No shared mutable state.
- Tree-sitter parsers are **not** safe for concurrent use. The evaluation
  pipeline creates a parser per call (or uses a pool). Parser creation is
  cheap — the grammar data is shared and read-only.

### Testing

- **Unit tests**: Every package has table-driven tests for its core logic.
- **Pack tests**: Each pack has tests for every destructive pattern AND every
  safe pattern, ensuring patterns match expected commands and don't
  false-positive on similar-but-safe commands.
- **Integration tests**: Full pipeline tests that exercise `guard.Evaluate()`
  with real commands and verify decisions.
- **Comparison tests**: Run the same commands through both the upstream Rust
  version and our Go version, compare results. Differences must be explained
  and intentional.
- **Benchmarks**: Go benchmarks for pre-filter, parsing, extraction, matching,
  and full pipeline.

### Extensibility

- Adding a new pack: Create a Go file in the appropriate `internal/packs/`
  subdirectory, register it in the registry. No other code changes needed.
- Adding a new language for inline script detection: Import the grammar from
  tree-sitter-go, add a detector entry in `internal/parse/inline.go`.

---

## 8. Alien Artifacts

### Intraprocedural Dataflow Analysis on Bash AST

Our highest-leverage advanced technique. Instead of analyzing each
`simple_command` node in isolation, we perform lightweight reaching-definitions
analysis across compound commands within a single command string.

**Problem it solves:**

```bash
# Pattern matching on isolated commands misses these:
DIR=/; rm -rf $DIR                    # Variable carries the danger
export RAILS_ENV=production && rails db:reset  # Env set in prior command
DB_HOST=prod-db.internal; psql -h $DB_HOST -c "DROP TABLE users"
```

In all three cases, the destructive operation and the dangerous context are
in separate `simple_command` nodes. Isolated extraction misses the connection.

**Technique:**

Reaching-definitions analysis is a classic dataflow analysis from compiler
theory. We implement a lightweight version scoped to a single command string
(intraprocedural — we don't analyze across separate invocations):

1. Walk the bash AST in execution order (respecting `&&`, `||`, `;`, pipes)
2. At each `variable_assignment` or `export` command, record the definition:
   `{variable_name → value, scope: local|export}`
3. At each `simple_command`, substitute known variable values into the
   extracted command's arguments and env vars
4. Feed the resolved command into pattern matching and environment detection

This is not full abstract interpretation — we don't handle control flow
(if/then/else), loops, or function definitions. We handle the linear and
`&&`/`||` cases that cover >95% of real-world LLM-generated commands.

**Complexity**: O(n) in the number of AST nodes — a single forward pass. No
fixpoint iteration needed because bash command strings are acyclic (no loops
in the common case).

**Where it lives**: `internal/parse` as part of command extraction. The
extractor already walks the AST; dataflow tracking is an additional accumulator
carried through the walk.

---

## 9. Unreasonably Robust Programming (URP)

### Mutation Testing on Pattern Packs

Every condition in every `CommandMatcher` must be load-bearing. We build an
automated mutation testing harness that:

1. For each destructive pattern, systematically mutates one condition at a time
   (remove a flag check, change the command name, relax an argument constraint)
2. Reruns the pack's test suite after each mutation
3. Verifies that at least one test fails for every mutation

If a mutation doesn't cause a test failure, either the condition is redundant
(remove it) or the test suite has a gap (add a test). This guarantees our test
suite exercises every dimension of every pattern.

**Measurement**: Mutation kill rate per pack. Target: 100%.

### Golden File Corpus

A version-controlled corpus of ~500+ real-world commands with expected
evaluation results (decision, matched pack, severity). Every CI run verifies
the corpus. Any behavior change on a golden file must be explicitly
acknowledged in the diff — no silent regressions.

The corpus includes:

- All pack pattern examples (both destructive and safe variants)
- Edge cases: unusual flag ordering, quoted arguments, path-prefixed binaries
- Compound commands: pipelines, `&&` chains, subshells
- Inline scripts: `python -c`, `bash -c`, heredocs
- False-positive traps: commands that look dangerous but aren't
- Dataflow cases: variable assignments feeding into destructive commands

**Measurement**: Corpus size and coverage of pack patterns. Every pattern must
have at least 3 golden file entries (1 match, 1 near-miss, 1 safe variant).

### Fuzz Testing

Fuzz the full evaluation pipeline with randomly generated inputs:

- Random bash-like strings (valid and invalid syntax)
- Mutations of known commands (swap flags, change arguments)
- Extremely long inputs, empty inputs, unicode edge cases

**Invariants verified under fuzzing**:

1. Never panics (always returns a valid Result)
2. Parse errors → Allow (fail-open)
3. Empty/whitespace commands → Allow
4. Result.Decision is always one of {Allow, Deny, Ask}
5. If Result.Assessment is nil, Result.Decision is Allow
6. If Result.Matches is non-empty, Result.Assessment is non-nil

**Measurement**: Fuzz corpus size, time-to-first-crash (target: never).

### Comparison Testing Against Upstream

Run the same command corpus through both the Rust upstream and our Go version.
Produce a diff report categorizing every difference as:

- **Intentional improvement** (we catch something the Rust version misses, or
  we correctly allow something it false-positives on)
- **Intentional divergence** (different design choice, documented)
- **Bug** (we miss something the Rust version catches — fix it)

**Measurement**: Comparison pass rate. Unexplained divergences = 0.

### Grammar-Derived Coverage Analysis

Use the tree-sitter bash grammar's node types to enumerate the structural
contexts a command can appear in (simple_command, pipeline, subshell,
command_substitution, if_statement body, for loop body, etc.). Verify our
command extractor handles all of them.

For each pack's command names, generate synthetic commands in every structural
context and verify extraction + matching works. This catches extractor blind
spots like "we handle pipelines but not subshells in arithmetic expansion."

**Measurement**: Structural context coverage per pack. Target: 100% of
bash grammar command-bearing node types.

---

## 10. Extreme Optimization

After analysis, we conclude that extreme optimization (SIMD, assembly, GPU
offload) is **not applicable** to this workload in a meaningful way.

The inputs are short strings (typically < 1KB), processed one at a time, at
the frequency of LLM tool invocations (seconds between calls). The dominant
cost is tree-sitter parsing, which is already well-optimized. The keyword
pre-filter operates on ~50 short strings against a short command —
`strings.Contains` completes in nanoseconds at this scale.

Engineering SIMD or assembly for this workload would be optimization theater —
impressive-looking work that produces no measurable user-facing improvement.

**What we do instead**: Focus optimization effort on what matters:

- **Parser pooling**: Avoid allocating a new tree-sitter parser per call.
  Pool parsers with `sync.Pool` since grammar data is shared and read-only.
- **Pre-filter effectiveness**: Ensure >90% of benign commands skip parsing
  entirely. This is the highest-leverage optimization — avoiding work entirely.
- **Lazy pack initialization**: Packs are registered at init time but their
  matchers are only compiled on first use.
- **Benchmark-driven**: All optimizations are validated by benchmarks. No
  speculative optimization.

---

## 11. Fit Check: Architecture × Requirements

| Req | Requirement | Arch Coverage |
|-----|-------------|---------------|
| R0 | Assessment/policy separation | D5: Assessment/Policy separation, Policy interface |
| R1 | Pure Go, no cgo | tree-sitter-go is pure Go, all deps are pure Go |
| R2 | Public Go API | Layer 1: `guard` package with `Evaluate()` |
| R3 | Tree-sitter structural analysis | Layer 4: `internal/parse`, D1: structural matching |
| R4 | Cover destructive command categories | Layer 3: 21 packs across all categories |
| R5 | Benchmarked performance | Cross-cutting: benchmark suite |
| R6 | Standalone hook binary | Layer 5: `cmd/dcgo` hook mode |
| R7 | Other agent protocols | Not in v1 — library is protocol-agnostic, easy to add |
| R8 | Allowlists/blocklists | `WithAllowlist`/`WithBlocklist` options |
| R9 | Config file | Layer 5: `cmd/dcgo/config.go` |
| R10 | CLI test/packs commands | Layer 5: test mode, packs mode |
| R11 | Environment awareness | `internal/envdetect`, `WithEnv` option |
