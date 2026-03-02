# 02: Matching Framework — Evaluation Pipeline, Packs, Policy & Environment Detection

**Batch**: 2 (Matching Framework)
**Depends On**: [01-treesitter-integration](./01-treesitter-integration.md)
**Blocks**: [03a-packs-core](./03a-packs-core.md), [04-api-and-cli](./04-api-and-cli.md)
**Architecture**: [00-architecture.md](./00-architecture.md) (§3 Layers 1–3, §4 Pipeline, §6 D1–D3)
**Plan Index**: [00-plan-index.md](./00-plan-index.md)

---

## 1. Summary

This plan covers **everything between parsing and the public API** — the
evaluation engine that transforms parsed `ExtractedCommand` values into
`Result{Decision, Matches}`. It is the primary consumer of plan 01's
`ParseResult` and the primary producer consumed by plan 04's public API.

**Scope**:

1. **Pack type definitions** — `Pack`, `SafePattern`, `DestructivePattern`
   (`internal/packs/pack.go`)
2. **CommandMatcher interface and built-in matchers** — `NameMatcher`,
   `FlagMatcher`, `ArgMatcher`, `ArgContentMatcher`, `EnvMatcher`,
   `CompositeMatcher`, `NegativeMatcher` (`internal/packs/matcher.go`)
3. **Pack registry** — Registration, lookup, keyword aggregation
   (`internal/packs/registry.go`)
4. **Keyword pre-filter** — Aho-Corasick automaton, dynamic pack selection
   caching (`internal/eval/prefilter.go`)
5. **Evaluation pipeline orchestration** — The full 12-step pipeline from
   architecture §4 (`internal/eval/pipeline.go`)
6. **Policy engine** — `Policy` interface, `StrictPolicy`, `InteractivePolicy`,
   `PermissivePolicy`, `Assessment` → `Decision` (`guard/policy.go`)
7. **Allowlist/blocklist matching** — Glob patterns against raw command,
   blocklist-first precedence (`internal/eval/allowlist.go`)
8. **Environment detection** — Production indicators from inline env vars,
   dataflow-resolved exports, and caller-provided process env
   (`internal/envdetect/detect.go`)
9. **Golden file infrastructure** — Test framework and initial seed corpus
   (`internal/eval/testdata/golden/`)
10. **Test pack** — Minimal `core.git` with 2 safe + 5 destructive patterns
    to validate framework end-to-end (`internal/packs/core/git.go`)

**Key input type** (from plan 01):

```go
// Consumed from internal/parse
type ExtractedCommand struct {
    Name             string
    RawName          string
    Args             []string
    RawArgs          []string
    Flags            map[string]string
    InlineEnv        map[string]string
    RawText          string
    InPipeline       bool
    Negated          bool
    DataflowResolved bool
    StartByte        uint32
    EndByte          uint32
}

type ParseResult struct {
    Commands     []ExtractedCommand
    Warnings     []Warning
    HasError     bool
    ExportedVars map[string][]string // Exported variables from dataflow analysis
}
```

**Key output types** (produced for plan 04):

```go
// Produced by internal/eval, surfaced via guard package
type Result struct {
    Decision   Decision
    Assessment *Assessment
    Matches    []Match
    Warnings   []Warning
    Command    string
}

type Assessment struct {
    Severity   Severity
    Confidence Confidence
}

type Match struct {
    Pack         string
    Rule         string
    Severity     Severity
    Confidence   Confidence
    Reason       string
    Remediation  string
    EnvEscalated bool
}
```

---

## 2. Component Diagram

```mermaid
graph TB
    subgraph "guard package (plan 04 — thin wrapper)"
        Evaluate["guard.Evaluate(cmd, opts...)"]
    end

    subgraph "internal/eval"
        Pipeline["Pipeline<br/>(pipeline.go)"]
        PreFilter["KeywordPreFilter<br/>(prefilter.go)"]
        AllowBlock["AllowBlockChecker<br/>(allowlist.go)"]
        Aggregator["AssessmentAggregator<br/>(pipeline.go)"]
    end

    subgraph "internal/parse (plan 01)"
        BashParser["BashParser.ParseAndExtract()"]
    end

    subgraph "internal/packs"
        Registry["Registry<br/>(registry.go)"]
        Matcher["CommandMatcher<br/>(matcher.go)"]
        PackTypes["Pack, SafePattern,<br/>DestructivePattern<br/>(pack.go)"]
        TestPack["core.git (test pack)<br/>(core/git.go)"]
    end

    subgraph "internal/envdetect"
        EnvDetector["Detector<br/>(detect.go)"]
    end

    subgraph "guard package (policy)"
        PolicyEngine["Policy Interface<br/>(policy.go)"]
        Strict["StrictPolicy"]
        Interactive["InteractivePolicy"]
        Permissive["PermissivePolicy"]
    end

    Evaluate --> Pipeline
    Pipeline --> AllowBlock
    Pipeline --> PreFilter
    PreFilter --> Registry
    Pipeline --> BashParser
    Pipeline --> EnvDetector
    Pipeline --> Matcher
    Matcher --> PackTypes
    Pipeline --> Aggregator
    Pipeline --> PolicyEngine
    Registry --> TestPack
```

---

## 3. Sequence Diagram: Full Evaluation Pipeline

```mermaid
sequenceDiagram
    participant Caller
    participant P as Pipeline
    participant AB as AllowBlockChecker
    participant KF as KeywordPreFilter
    participant BP as BashParser
    participant ED as EnvDetector
    participant R as Registry
    participant PM as PackMatcher
    participant Agg as Aggregator
    participant Pol as Policy

    Caller->>P: Evaluate("RAILS_ENV=prod rails db:reset", opts...)

    Note over P: Step 1: Input validation
    P->>P: len(cmd) > 0, len(cmd) <= 128KB ✓

    Note over P: Step 2: Blocklist/Allowlist
    P->>AB: Check(cmd, blocklist, allowlist)
    AB-->>P: NoMatch

    Note over P: Step 3: Keyword pre-filter
    P->>KF: Contains(cmd, enabledPacks)?
    KF-->>P: Yes (["rails"] → frameworks pack)

    Note over P: Step 4–6: Parse + Extract + Normalize
    P->>BP: ParseAndExtract(ctx, cmd, 0)
    BP-->>P: ParseResult{Commands: [{Name:"rails", Args:["db:reset"],<br/>InlineEnv:{RAILS_ENV:"prod"}}]}

    Note over P: Step 8: Inline script detection
    P->>P: No inline scripts detected

    Note over P: Step 9: Environment detection
    P->>ED: Detect(cmd.InlineEnv, exportedVars, callerEnv)
    ED-->>P: ProductionIndicators: [{Var:"RAILS_ENV", Val:"prod"}]

    Note over P: Step 10: Pattern matching
    P->>R: GetCandidatePacks(matchedKeywords)
    R-->>P: [frameworks]
    loop Each extracted command × each candidate pack
        P->>PM: MatchSafe(cmd, pack.Safe)
        PM-->>P: No safe match
        P->>PM: MatchDestructive(cmd, pack.Destructive)
        PM-->>P: Match! rails-db-destructive (High, ConfidenceHigh)
    end

    Note over P: Step 9b: Env escalation
    P->>P: Production detected → High → Critical

    Note over P: Step 11: Assessment aggregation
    P->>Agg: Aggregate(matches)
    Agg-->>P: Assessment{Critical, ConfidenceHigh}

    Note over P: Step 12: Policy application
    P->>Pol: Decide(Assessment{Critical, High})
    Pol-->>P: Deny

    P-->>Caller: Result{Decision:Deny, Assessment:..., Matches:[...]}
```

---

## 4. Package Structure

```
internal/eval/
├── pipeline.go             # Pipeline orchestration (RunPipeline)
├── prefilter.go            # Aho-Corasick keyword pre-filter
├── allowlist.go            # Allowlist/blocklist glob matching
├── config.go               # evalConfig: pack selection, policy, env, lists
├── pipeline_test.go        # Pipeline integration tests
├── prefilter_test.go       # Pre-filter unit tests
├── allowlist_test.go       # Allowlist/blocklist unit tests
└── testdata/
    └── golden/
        ├── golden_test.go  # Golden file test runner
        ├── README.md       # Golden file format documentation
        └── corpus/
            ├── core_git.txt         # Golden entries for core.git
            ├── safe_commands.txt    # Commands that must produce Allow
            ├── edge_cases.txt       # Unusual syntax, quoting, etc.
            └── env_escalation.txt   # Environment-sensitive cases

internal/packs/
├── pack.go                 # Pack, SafePattern, DestructivePattern types
├── matcher.go              # CommandMatcher interface + all built-in matchers
├── registry.go             # Pack registry (Register, Get, All, Keywords)
├── matcher_test.go         # Matcher unit tests
├── registry_test.go        # Registry unit tests
└── core/
    └── git.go              # Test pack: core.git (2-3 patterns)

internal/envdetect/
├── detect.go               # Production indicator detection
└── detect_test.go          # Env detection unit tests

guard/
├── policy.go               # Policy interface + built-in policies
└── policy_test.go          # Policy unit tests
```

**Import flow** (strictly layered — no upward imports):

```mermaid
graph TD
    GUARD["guard (policy.go)"] --> EVAL["internal/eval"]
    EVAL --> PARSE["internal/parse (plan 01)"]
    EVAL --> PACKS["internal/packs"]
    EVAL --> ENVDETECT["internal/envdetect"]
    PACKS --> PARSE
```

Notes:
- `internal/packs` imports `internal/parse` for the `ExtractedCommand` type.
  Matchers receive `parse.ExtractedCommand` values.
- `internal/eval` imports all three internal packages to orchestrate the pipeline.
- `guard/policy.go` defines the `Policy` interface and types (`Decision`,
  `Assessment`, `Severity`, `Confidence`). These types live in the `guard`
  package because they are part of the public API. `internal/eval` imports
  `guard` for these types.
- `internal/eval` does NOT export pipeline details — it exposes a single
  `RunPipeline(command string, cfg *evalConfig) guard.Result` function.

**Circular import avoidance**: `guard` → `internal/eval` → `guard` would be
circular. We break this by having `guard/policy.go` define only types (no
imports from `internal/`). `guard/guard.go` (plan 04) imports `internal/eval`
and calls `RunPipeline`. The types that both packages need (`Severity`,
`Confidence`, `Decision`, `Assessment`, `Match`, `Warning`, `Result`) live
in `guard` and are imported by `internal/eval`.

---

## 5. Detailed Design

### 5.1 Pack Type Definitions (`internal/packs/pack.go`)

```go
package packs

import "github.com/dcosson/destructive-command-guard-go/internal/parse"

// Pack is a collection of safe and destructive patterns for a tool/domain.
// Packs are registered at init time and are immutable after registration.
type Pack struct {
    ID          string              // Unique identifier, e.g. "core.git"
    Name        string              // Human-readable name, e.g. "Git"
    Description string              // Short description for --packs output
    Keywords    []string            // Pre-filter keywords: presence of ANY triggers this pack
    Safe        []SafePattern       // Safe patterns (checked first, short-circuit)
    Destructive []DestructivePattern // Destructive patterns (checked if no safe match)
}

// SafePattern short-circuits destructive matching for a specific command.
// If a safe pattern matches, destructive patterns in this pack are skipped
// for that command. Other packs still evaluate independently.
type SafePattern struct {
    Name  string          // Unique within pack, e.g. "git-push-no-force"
    Match CommandMatcher  // Structural matcher
}

// DestructivePattern identifies a destructive command.
type DestructivePattern struct {
    Name         string          // Unique within pack, e.g. "git-push-force"
    Match        CommandMatcher  // Structural matcher
    Severity     Severity        // Base severity (before env escalation)
    Confidence   Confidence      // How confident are we this is destructive?
    Reason       string          // Human-readable explanation of the danger
    Remediation  string          // Suggested safe alternative
    EnvSensitive bool            // If true, severity escalated in production env
}

// Severity levels for assessments.
type Severity int

const (
    Indeterminate Severity = iota // Cannot analyze (parse failure, oversized input)
    Low                           // Minor risk
    Medium                        // Moderate risk
    High                          // Significant risk
    Critical                      // Maximum risk
)

// Confidence levels for assessments.
type Confidence int

const (
    ConfidenceLow    Confidence = iota
    ConfidenceMedium
    ConfidenceHigh
)
```

**Note on type location**: `Severity` and `Confidence` are defined here in
`internal/packs` AND re-exported from the `guard` package. Plan 04 will
address the exact mechanism — either type aliases (`type Severity = packs.Severity`)
or the types move to `guard` and `packs` imports them. For plan 02
implementation, they live in `internal/packs` since that's where they're
first needed. The circular import resolution in §4 above describes the
final architecture.

**Revision**: After further consideration of the import flow, `Severity`,
`Confidence`, `Decision`, `Assessment`, `Match`, `Result`, and `Warning`
types should live in the `guard` package from the start, since they are
public API types and `internal/eval` needs them. `internal/packs` imports
`guard` for `Severity` and `Confidence`. This avoids a type-alias hop and
keeps the source of truth in the public package.

```go
// In guard/types.go (created as part of plan 02)
package guard

type Severity int
const (
    Indeterminate Severity = iota
    Low
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

type Decision int
const (
    Allow Decision = iota
    Deny
    Ask
)

type Assessment struct {
    Severity   Severity
    Confidence Confidence
}

type Match struct {
    Pack         string
    Rule         string
    Severity     Severity
    Confidence   Confidence
    Reason       string
    Remediation  string
    EnvEscalated bool
}

type Warning struct {
    Code    WarningCode
    Message string
}

type WarningCode int
const (
    WarnPartialParse        WarningCode = iota
    WarnInlineDepthExceeded
    WarnInputTruncated
    WarnExpansionCapped
    WarnExtractorPanic
    WarnCommandSubstitution
    WarnMatcherPanic
    WarnUnknownPackID               // Pack ID in config not found in registry
)

type Result struct {
    Decision   Decision
    Assessment *Assessment
    Matches    []Match
    Warnings   []Warning
    Command    string
}
```

Then `internal/packs/pack.go` imports `guard` for `Severity` and `Confidence`:

```go
package packs

import (
    "github.com/dcosson/destructive-command-guard-go/guard"
    "github.com/dcosson/destructive-command-guard-go/internal/parse"
)

type DestructivePattern struct {
    Name         string
    Match        CommandMatcher
    Severity     guard.Severity
    Confidence   guard.Confidence
    Reason       string
    Remediation  string
    EnvSensitive bool
}
```

**Import flow revised**:

```mermaid
graph TD
    EVAL["internal/eval"] --> GUARD_TYPES["guard (types.go)"]
    EVAL --> PARSE["internal/parse"]
    EVAL --> PACKS["internal/packs"]
    EVAL --> ENVDETECT["internal/envdetect"]
    PACKS --> GUARD_TYPES
    PACKS --> PARSE
    ENVDETECT --> GUARD_TYPES
```

`guard/guard.go` (plan 04) will import `internal/eval` — no cycle because
`guard/types.go` has no imports from `internal/`.

### 5.2 CommandMatcher Interface and Built-in Matchers (`internal/packs/matcher.go`)

```go
package packs

import "github.com/dcosson/destructive-command-guard-go/internal/parse"

// CommandMatcher tests whether an extracted command matches a pattern.
// Implementations must be safe for concurrent use (stateless or read-only).
type CommandMatcher interface {
    Match(cmd parse.ExtractedCommand) bool
}
```

#### 5.2.1 NameMatcher

Matches the normalized command name (exact string equality).

```go
// NameMatcher matches commands by normalized name.
// Example: NameMatcher{Name: "git"} matches any command where cmd.Name == "git".
type NameMatcher struct {
    Name string
}

func (m NameMatcher) Match(cmd parse.ExtractedCommand) bool {
    return cmd.Name == m.Name
}
```

#### 5.2.2 FlagMatcher

Checks presence or absence of specific flags. Supports both required and
forbidden flags.

```go
// FlagMatcher checks for the presence of specific flags.
// Required: all must be present for a match.
// Forbidden: if ANY is present, match fails.
//
// Flag names must be exact: "--force" does NOT match "--force-with-lease".
// Values are optionally checked: if RequiredValues[flag] is non-empty,
// the flag's value must match exactly.
type FlagMatcher struct {
    Required       []string          // All must be present
    Forbidden      []string          // None may be present
    RequiredValues map[string]string // Flag must have this exact value
}

func (m FlagMatcher) Match(cmd parse.ExtractedCommand) bool {
    for _, f := range m.Required {
        if _, ok := cmd.Flags[f]; !ok {
            return false
        }
    }
    for _, f := range m.Forbidden {
        if _, ok := cmd.Flags[f]; ok {
            return false
        }
    }
    for flag, wantVal := range m.RequiredValues {
        gotVal, ok := cmd.Flags[flag]
        if !ok || gotVal != wantVal {
            return false
        }
    }
    return true
}
```

#### 5.2.3 ArgMatcher

Checks positional arguments. Supports exact match, glob, and index-specific
matching.

```go
// ArgMatcher checks positional arguments.
//
// Modes:
//   - HasAny: true if cmd.Args contains at least one argument matching Pattern
//   - AtIndex: check only the argument at the specified index
//   - Pattern: string to match against. Supports:
//       - Exact: "origin" matches "origin"
//       - Glob: "*.sql" matches "dump.sql" (uses path.Match semantics)
//       - Prefix: "db:" matches "db:reset" (when PrefixMatch is true)
type ArgMatcher struct {
    Pattern     string // Pattern to match
    AtIndex     int    // -1 means "any position" (default)
    PrefixMatch bool   // Match prefix instead of exact/glob
    HasAny      bool   // Just check that args is non-empty (ignores Pattern)
}

func (m ArgMatcher) Match(cmd parse.ExtractedCommand) bool {
    if m.HasAny {
        return len(cmd.Args) > 0
    }
    if m.AtIndex >= 0 {
        if m.AtIndex >= len(cmd.Args) {
            return false
        }
        return m.matchArg(cmd.Args[m.AtIndex])
    }
    // Any position
    for _, arg := range cmd.Args {
        if m.matchArg(arg) {
            return true
        }
    }
    return false
}

func (m ArgMatcher) matchArg(arg string) bool {
    if m.PrefixMatch {
        return strings.HasPrefix(arg, m.Pattern)
    }
    // Use globMatch for consistent glob semantics across the codebase.
    // globMatch has no invalid patterns (only `*` is special), so no
    // fallback needed.
    return globMatch(m.Pattern, arg)
}
```

#### 5.2.4 ArgContentMatcher

Regex or substring match against argument values. Primarily used for SQL
patterns (e.g., `DROP TABLE` in `psql -c "DROP TABLE users"`).

```go
// ArgContentMatcher checks whether any argument contains a substring or
// matches a regex. Used for SQL detection in psql -c "DROP TABLE ...".
//
// If Regex is non-nil, it takes precedence over Substring.
// The regex is compiled once at pack registration time (not per-match).
type ArgContentMatcher struct {
    Substring string         // Simple substring match
    Regex     *regexp.Regexp // Compiled regex (takes precedence)
    AtIndex   int            // -1 means "any argument"
}

func (m ArgContentMatcher) Match(cmd parse.ExtractedCommand) bool {
    check := func(arg string) bool {
        if m.Regex != nil {
            return m.Regex.MatchString(arg)
        }
        return strings.Contains(arg, m.Substring)
    }
    if m.AtIndex >= 0 {
        if m.AtIndex >= len(cmd.Args) {
            return false
        }
        return check(cmd.Args[m.AtIndex])
    }
    for _, arg := range cmd.Args {
        if check(arg) {
            return true
        }
    }
    return false
}
```

#### 5.2.4a RawArgContentMatcher

Matches against `cmd.RawArgs` (pre-normalization, original token order).
Use this when semantics depend on raw argument tokens that may be transformed
or decomposed in normalized `Args`/`Flags` form.

```go
// RawArgContentMatcher checks raw argument tokens.
// This is required for commands that encode subcommands as dash-prefixed
// tokens (e.g., dscl . -delete), where normalization can lose the original
// token shape.
type RawArgContentMatcher struct {
    Substring string
    Regex     *regexp.Regexp // takes precedence when non-nil
    AtIndex   int            // -1 means any raw arg
}

func (m RawArgContentMatcher) Match(cmd parse.ExtractedCommand) bool {
    check := func(arg string) bool {
        if m.Regex != nil {
            return m.Regex.MatchString(arg)
        }
        return strings.Contains(arg, m.Substring)
    }
    if m.AtIndex >= 0 {
        if m.AtIndex >= len(cmd.RawArgs) {
            return false
        }
        return check(cmd.RawArgs[m.AtIndex])
    }
    for _, arg := range cmd.RawArgs {
        if check(arg) {
            return true
        }
    }
    return false
}
```

#### 5.2.5 EnvMatcher

Checks inline environment variables (from the AST) for specific names/values.

```go
// EnvMatcher checks inline env vars on the command.
// Example: EnvMatcher{Name: "RAILS_ENV", Value: "production"} matches
// commands prefixed with RAILS_ENV=production.
//
// If Value is empty, any value for that env var matches.
type EnvMatcher struct {
    Name  string // Env var name (required)
    Value string // Expected value (empty = any value)
}

func (m EnvMatcher) Match(cmd parse.ExtractedCommand) bool {
    val, ok := cmd.InlineEnv[m.Name]
    if !ok {
        return false
    }
    if m.Value == "" {
        return true
    }
    return val == m.Value
}
```

#### 5.2.6 CompositeMatcher

Composes multiple matchers with AND/OR logic. This is the primary composition
mechanism for building pack patterns.

```go
// CompositeMatcher combines multiple matchers with AND or OR logic.
//
// AND (Op == OpAnd): all children must match.
// OR  (Op == OpOr):  at least one child must match.
//
// Typical usage: AND(NameMatcher("git"), FlagMatcher(Required: ["--force"]))
type CompositeMatcher struct {
    Op       CompositeOp
    Children []CommandMatcher
}

type CompositeOp int

const (
    OpAnd CompositeOp = iota
    OpOr
)

func (m CompositeMatcher) Match(cmd parse.ExtractedCommand) bool {
    switch m.Op {
    case OpAnd:
        for _, child := range m.Children {
            if !child.Match(cmd) {
                return false
            }
        }
        return true
    case OpOr:
        for _, child := range m.Children {
            if child.Match(cmd) {
                return true
            }
        }
        return false
    default:
        return false
    }
}
```

#### 5.2.7 NegativeMatcher

Inverts a match. Used for exclusion patterns (e.g., "rm -rf BUT NOT /tmp").

```go
// NegativeMatcher inverts its inner matcher.
// Matches when the inner matcher does NOT match.
// Example: NegativeMatcher{Inner: ArgMatcher{Pattern: "/tmp/*"}}
// matches when no argument starts with /tmp/
type NegativeMatcher struct {
    Inner CommandMatcher
}

func (m NegativeMatcher) Match(cmd parse.ExtractedCommand) bool {
    return !m.Inner.Match(cmd)
}
```

#### 5.2.8 AnyNameMatcher

Matches any command regardless of name. Used for cross-cutting patterns that
detect dangerous behavior based on argument content rather than command identity
— for example, detecting access to personal file paths (`~/Desktop`, `~/Documents`)
regardless of which command is used (`cat`, `rm`, `cp`, `vim`, etc.).

```go
// AnyNameMatcher matches any command name unconditionally.
// Used for command-agnostic patterns where detection is based on
// argument content (e.g., personal file path detection, sensitive
// path detection) rather than a specific command.
type AnyNameMatcher struct{}

func (m AnyNameMatcher) Match(cmd parse.ExtractedCommand) bool {
    return true
}
```

Packs using `AnyNameMatcher` should use **argument content keywords** in their
`Keywords` field rather than command name keywords. For example, a personal files
pack would use keywords like `"Desktop"`, `"Documents"`, `"Downloads"` — the
Aho-Corasick pre-filter matches these as substrings in the raw command, so
`cat ~/Desktop/file.txt` triggers the pack because `Desktop` is found at a
word boundary. This ensures the pre-filter remains effective: most commands
don't contain personal path components, so they skip this pack entirely.

#### 5.2.9 Builder Functions

Convenience constructors to reduce verbosity when defining pack patterns:

```go
// Builder functions for concise pattern definitions.

// Name creates a NameMatcher.
func Name(name string) NameMatcher {
    return NameMatcher{Name: name}
}

// Flags creates a FlagMatcher requiring the given flags.
func Flags(flags ...string) FlagMatcher {
    return FlagMatcher{Required: flags}
}

// ForbidFlags creates a FlagMatcher forbidding the given flags.
func ForbidFlags(flags ...string) FlagMatcher {
    return FlagMatcher{Forbidden: flags}
}

// Arg creates an ArgMatcher matching any position.
func Arg(pattern string) ArgMatcher {
    return ArgMatcher{Pattern: pattern, AtIndex: -1}
}

// ArgAt creates an ArgMatcher at a specific index.
func ArgAt(index int, pattern string) ArgMatcher {
    return ArgMatcher{Pattern: pattern, AtIndex: index}
}

// ArgPrefix creates an ArgMatcher with prefix matching at any position.
func ArgPrefix(prefix string) ArgMatcher {
    return ArgMatcher{Pattern: prefix, PrefixMatch: true, AtIndex: -1}
}

// ArgContent creates an ArgContentMatcher with substring matching.
func ArgContent(substring string) ArgContentMatcher {
    return ArgContentMatcher{Substring: substring, AtIndex: -1}
}

// ArgContentRegex creates an ArgContentMatcher with regex matching.
// Panics if the regex is invalid (caught at init time, not runtime).
func ArgContentRegex(pattern string) ArgContentMatcher {
    return ArgContentMatcher{Regex: regexp.MustCompile(pattern), AtIndex: -1}
}

// RawArgContent creates a RawArgContentMatcher with substring matching.
func RawArgContent(substring string) RawArgContentMatcher {
    return RawArgContentMatcher{Substring: substring, AtIndex: -1}
}

// RawArgContentRegex creates a RawArgContentMatcher with regex matching.
func RawArgContentRegex(pattern string) RawArgContentMatcher {
    return RawArgContentMatcher{Regex: regexp.MustCompile(pattern), AtIndex: -1}
}

// Env creates an EnvMatcher.
func Env(name, value string) EnvMatcher {
    return EnvMatcher{Name: name, Value: value}
}

// And creates a CompositeMatcher with AND logic.
func And(matchers ...CommandMatcher) CompositeMatcher {
    return CompositeMatcher{Op: OpAnd, Children: matchers}
}

// Or creates a CompositeMatcher with OR logic.
func Or(matchers ...CommandMatcher) CompositeMatcher {
    return CompositeMatcher{Op: OpOr, Children: matchers}
}

// Not creates a NegativeMatcher.
func Not(matcher CommandMatcher) NegativeMatcher {
    return NegativeMatcher{Inner: matcher}
}

// AnyName creates an AnyNameMatcher (matches any command).
func AnyName() AnyNameMatcher {
    return AnyNameMatcher{}
}
```

**ArgContent anti-footgun rule**: `ArgContent()` is literal substring matching
only. Regex-like literals (for example `ArgContent("^:")`, `ArgContent("^0$")`)
MUST NOT be used. Use `ArgContentRegex()` (or exact `Arg`/`ArgAt`/`ArgPrefix`)
for anchored or regex semantics. This rule is mandatory for pack authoring and
is enforced by matcher regression tests in the test harness.

**Example pack pattern** (used in the test pack, §5.9):

```go
// git push --force (destructive)
DestructivePattern{
    Name: "git-push-force",
    Match: And(Name("git"), ArgAt(0, "push"), Or(
        Flags("--force"),
        Flags("-f"),
    )),
    Severity:     guard.High,
    Confidence:   guard.ConfidenceHigh,
    Reason:       "git push --force overwrites remote history",
    Remediation:  "Use --force-with-lease for safer force pushing",
    EnvSensitive: false,
}
```

**Example command-agnostic pattern** (personal file path detection):

```go
// Any command accessing personal directories (cross-cutting)
DestructivePattern{
    Name: "personal-files-access",
    Match: And(AnyName(), ArgContentRegex(
        `(?:^|/)(?:Desktop|Documents|Downloads|Pictures|Music|Videos)(?:/|$)`,
    )),
    Severity:     guard.Medium,
    Confidence:   guard.ConfidenceHigh,
    Reason:       "Command accesses a personal file directory",
    Remediation:  "Verify this file access is intentional and necessary",
    EnvSensitive: false,
}
```

This pattern matches `cat ~/Desktop/notes.txt`, `rm ~/Documents/file`, and any
other command targeting personal directories. The pack's `Keywords` field would
contain `["Desktop", "Documents", "Downloads", "Pictures", "Music", "Videos"]`
— path components rather than command names.

### 5.3 Pack Registry (`internal/packs/registry.go`)

The registry holds all registered packs, supports lookup by ID, and provides
the aggregated keyword list for the pre-filter.

```go
package packs

import (
    "fmt"
    "sort"
    "sync"
)

// Registry holds all registered pattern packs.
// Packs are registered at init time and are immutable after that.
// The registry is safe for concurrent read access after init.
type Registry struct {
    mu             sync.RWMutex
    packs          map[string]*Pack     // ID → Pack
    allPacks       []*Pack              // Ordered by registration
    keywords       []string             // Deduplicated aggregate of all pack keywords
    keywordIndex   map[string][]*Pack   // Reverse index: keyword → packs (built on freeze)
    frozen         bool                 // True after first read — no more registration
}

// DefaultRegistry is the global registry used by default.
var DefaultRegistry = NewRegistry()

func NewRegistry() *Registry {
    return &Registry{
        packs: make(map[string]*Pack),
    }
}

// Register adds a pack to the registry.
// Panics if a pack with the same ID is already registered (caught at init time).
// Panics if called after the registry is frozen (first read occurred).
func (r *Registry) Register(p Pack) {
    r.mu.Lock()
    defer r.mu.Unlock()
    if r.frozen {
        panic(fmt.Sprintf("packs: registry frozen, cannot register pack %q", p.ID))
    }
    if _, exists := r.packs[p.ID]; exists {
        panic(fmt.Sprintf("packs: duplicate pack ID %q", p.ID))
    }
    // Deep copy to prevent mutation of registered data via original slices
    cp := p
    cp.Keywords = append([]string{}, p.Keywords...)
    cp.Safe = append([]SafePattern{}, p.Safe...)
    cp.Destructive = append([]DestructivePattern{}, p.Destructive...)
    r.packs[p.ID] = &cp
    r.allPacks = append(r.allPacks, &cp)
    r.keywords = nil // Invalidate cached keywords
}

// Get returns a pack by ID, or nil if not found.
// Freezes the registry on first call.
func (r *Registry) Get(id string) *Pack {
    r.freeze()
    r.mu.RLock()
    defer r.mu.RUnlock()
    return r.packs[id]
}

// All returns all registered packs in registration order.
// Freezes the registry on first call.
func (r *Registry) All() []*Pack {
    r.freeze()
    r.mu.RLock()
    defer r.mu.RUnlock()
    result := make([]*Pack, len(r.allPacks))
    copy(result, r.allPacks)
    return result
}

// Keywords returns the deduplicated, sorted list of all pack keywords.
// Used by the pre-filter to build the Aho-Corasick automaton.
func (r *Registry) Keywords() []string {
    r.freeze()
    r.mu.RLock()
    if r.keywords != nil {
        defer r.mu.RUnlock()
        return r.keywords
    }
    r.mu.RUnlock()

    r.mu.Lock()
    defer r.mu.Unlock()
    if r.keywords != nil {
        return r.keywords // Double-check after upgrade
    }
    seen := make(map[string]bool)
    for _, p := range r.allPacks {
        for _, kw := range p.Keywords {
            seen[kw] = true
        }
    }
    r.keywords = make([]string, 0, len(seen))
    for kw := range seen {
        r.keywords = append(r.keywords, kw)
    }
    sort.Strings(r.keywords)
    return r.keywords
}

// PacksForKeyword returns all packs that have the given keyword.
// Uses a reverse index built during freeze() for O(1) lookup.
func (r *Registry) PacksForKeyword(keyword string) []*Pack {
    r.freeze()
    r.mu.RLock()
    defer r.mu.RUnlock()
    return r.keywordIndex[keyword]
}

// PacksByID returns packs matching the given IDs.
// Unknown IDs are silently ignored.
func (r *Registry) PacksByID(ids []string) []*Pack {
    r.freeze()
    r.mu.RLock()
    defer r.mu.RUnlock()
    var result []*Pack
    for _, id := range ids {
        if p, ok := r.packs[id]; ok {
            result = append(result, p)
        }
    }
    return result
}

// HasPack returns true if a pack with the given ID is registered.
func (r *Registry) HasPack(id string) bool {
    r.freeze()
    r.mu.RLock()
    defer r.mu.RUnlock()
    _, ok := r.packs[id]
    return ok
}

func (r *Registry) freeze() {
    r.mu.Lock()
    defer r.mu.Unlock()
    if r.frozen {
        return
    }
    r.frozen = true
    // Build reverse index: keyword → packs
    r.keywordIndex = make(map[string][]*Pack)
    for _, p := range r.allPacks {
        for _, kw := range p.Keywords {
            r.keywordIndex[kw] = append(r.keywordIndex[kw], p)
        }
    }
}
```

**Platform-conditional packs**: Packs that are OS-specific (e.g., macOS system
commands) use Go build tags for conditional registration:

```go
//go:build darwin

package macos

func init() {
    packs.DefaultRegistry.Register(macosPack)
}
```

This ensures the macOS pack is only compiled and registered on Darwin. The
registry itself has no platform awareness — conditional registration is handled
entirely by Go's build tag system at compile time.

**Registration pattern**: Each pack file in `internal/packs/core/`,
`internal/packs/database/`, etc. has an `init()` function that registers
its pack:

```go
// internal/packs/core/git.go
package core

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

func init() {
    packs.DefaultRegistry.Register(gitPack)
}

var gitPack = packs.Pack{
    ID:       "core.git",
    Name:     "Git",
    Keywords: []string{"git"},
    // ... patterns ...
}
```

### 5.4 Keyword Pre-Filter (`internal/eval/prefilter.go`)

The pre-filter uses an Aho-Corasick automaton for O(n) multi-pattern matching
against all pack keywords simultaneously.

```go
package eval

import (
    "sort"
    "strings"
    "sync"

    "github.com/dcosson/destructive-command-guard-go/internal/packs"
)

// KeywordPreFilter provides fast keyword-based rejection of commands that
// cannot match any pack pattern. Uses Aho-Corasick for O(n) multi-pattern
// matching against all pack keywords in a single pass.
//
// Design choice: A single automaton is built from ALL registered pack keywords.
// Pack selection (WithPacks/WithDisabledPacks) is applied later when selecting
// candidate packs, NOT by building per-subset automatons. This eliminates
// subset automaton cache proliferation and keeps the pre-filter stateless
// after init.
type KeywordPreFilter struct {
    registry  *packs.Registry
    automaton *ahoCorasick // Single automaton for all keywords
}

// NewKeywordPreFilter creates a pre-filter backed by the given registry.
// The automaton is built eagerly from all pack keywords.
func NewKeywordPreFilter(registry *packs.Registry) *KeywordPreFilter {
    return &KeywordPreFilter{
        registry:  registry,
        automaton: newAhoCorasick(registry.Keywords()),
    }
}

// MatchResult contains the keywords matched and the candidate packs.
type MatchResult struct {
    Matched  bool     // true if any keyword matched
    Keywords []string // which keywords matched (word-boundary filtered)
}

// Contains checks if the command string contains any pack keyword.
// Returns the matched keywords (used to select candidate packs).
//
// Always uses the full automaton. Pack selection is done downstream in
// selectCandidatePacks. This avoids building/caching per-subset automatons.
//
// Word-boundary post-filter: After Aho-Corasick finds a substring match,
// we verify that the match is at a word boundary — the character before
// the match must be a word separator (space, start-of-string, |, ;, (, etc.)
// and the character after must also be a word separator or end-of-string.
// This prevents "git" from matching inside "gitignore" or "github".
func (kf *KeywordPreFilter) Contains(command string) MatchResult {
    rawMatches := kf.automaton.FindAllWithPos(command)
    var filtered []string
    seen := make(map[string]bool)
    for _, m := range rawMatches {
        if isWordBoundary(command, m.start, m.end) && !seen[m.keyword] {
            seen[m.keyword] = true
            filtered = append(filtered, m.keyword)
        }
    }
    if len(filtered) == 0 {
        return MatchResult{}
    }
    return MatchResult{
        Matched:  true,
        Keywords: filtered,
    }
}

// isWordBoundary checks that the match at [start, end) is at a word boundary.
func isWordBoundary(text string, start, end int) bool {
    if start > 0 {
        c := text[start-1]
        if isWordChar(c) {
            return false
        }
    }
    if end < len(text) {
        c := text[end]
        if isWordChar(c) {
            return false
        }
    }
    return true
}

// isWordChar returns true for alphanumeric and underscore characters.
func isWordChar(c byte) bool {
    return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
        (c >= '0' && c <= '9') || c == '_'
}
```

#### 5.4.1 Aho-Corasick Implementation

We use a pure-Go Aho-Corasick implementation. Given the small number of
keywords (~50 across 21 packs), a textbook implementation is sufficient.

**Library selection**: Use `github.com/cloudflare/ahocorasick` or
`github.com/petar-dambovaliev/aho-corasick` — both are pure Go, no cgo.
If neither meets requirements, a custom implementation is straightforward
for ~50 patterns. The interface we need:

```go
// ahoCorasick wraps an Aho-Corasick automaton.
type ahoCorasick struct {
    // Internal automaton (library-specific)
    automaton interface{}
    patterns  []string
}

// acMatch is a single match with its position in the text.
type acMatch struct {
    keyword string
    start   int // Start byte offset (inclusive)
    end     int // End byte offset (exclusive)
}

// newAhoCorasick builds an automaton from the given patterns.
// Matching is case-sensitive — bash commands are case-sensitive and
// keywords match against raw command text.
func newAhoCorasick(patterns []string) *ahoCorasick {
    // Build automaton from patterns
    ...
}

// FindAllWithPos returns all matches with their byte positions.
// Used by the word-boundary post-filter to check context.
func (ac *ahoCorasick) FindAllWithPos(text string) []acMatch {
    // Run automaton on text, collect matches with positions
    ...
}
```

**Case sensitivity**: The pre-filter matches against the raw command string
(before parsing). Bash command names are case-sensitive (`Git` ≠ `git`), but
in practice LLMs generate lowercase command names. We match case-sensitively
to avoid false keyword matches on prose/comments in the command string.

**Keyword quality**: With word-boundary filtering, even short keywords like
`"rm"` won't match inside `"format"` or `"arm64"`. Keywords should be the
tool/command names as they appear in commands:
- Good: `"git"`, `"terraform"`, `"kubectl"`, `"rm"`, `"psql"`
- The word-boundary check prevents `"git"` from matching `"gitignore"` or
  `"github"` — only standalone `git` (preceded/followed by non-word chars)
- The pre-filter is a performance optimization, not a correctness gate.
  False positives at the pre-filter level mean unnecessary parsing, not
  false detections.

### 5.5 Evaluation Pipeline Orchestration (`internal/eval/pipeline.go`)

The pipeline implements the 12-step evaluation flow from architecture §4.

```go
package eval

import (
    "context"

    "github.com/dcosson/destructive-command-guard-go/guard"
    "github.com/dcosson/destructive-command-guard-go/internal/envdetect"
    "github.com/dcosson/destructive-command-guard-go/internal/packs"
    "github.com/dcosson/destructive-command-guard-go/internal/parse"
)

// Pipeline orchestrates the evaluation of a command string.
// It is the internal engine called by guard.Evaluate().
type Pipeline struct {
    parser    *parse.BashParser
    prefilter *KeywordPreFilter
    registry  *packs.Registry
    envDet    *envdetect.Detector
}

// NewPipeline creates a Pipeline with the given dependencies.
func NewPipeline(parser *parse.BashParser, registry *packs.Registry) *Pipeline {
    return &Pipeline{
        parser:    parser,
        prefilter: NewKeywordPreFilter(registry),
        registry:  registry,
        envDet:    envdetect.NewDetector(),
    }
}

// evalConfig holds per-evaluation configuration (from guard.Option funcs).
type evalConfig struct {
    policy        guard.Policy
    allowlist     []string   // Glob patterns
    blocklist     []string   // Glob patterns
    enabledPacks  []string   // nil = all packs
    disabledPacks []string   // Packs to exclude
    callerEnv     []string   // Process env vars (os.Environ() format)
}

// Run executes the full evaluation pipeline.
func (p *Pipeline) Run(ctx context.Context, command string, cfg *evalConfig) guard.Result {
    result := guard.Result{Command: command}

    // Config validation: default to InteractivePolicy if nil
    policy := cfg.policy
    if policy == nil {
        policy = guard.InteractivePolicy()
    }

    // Validate pack IDs and warn on unknown (catches typos)
    result.Warnings = append(result.Warnings, p.validatePackConfig(cfg)...)

    // Step 1: Input validation
    if isEmptyOrWhitespace(command) {
        result.Decision = guard.Allow
        return result
    }
    if len(command) > parse.MaxInputSize {
        result.Warnings = append(result.Warnings, guard.Warning{
            Code:    guard.WarnInputTruncated,
            Message: "input exceeds maximum size",
        })
        result.Assessment = &guard.Assessment{
            Severity:   guard.Indeterminate,
            Confidence: guard.ConfidenceHigh,
        }
        result.Decision = policy.Decide(*result.Assessment)
        return result
    }

    // Step 2: Blocklist/Allowlist
    if pattern, matched := matchesGlobFirst(command, cfg.blocklist); matched {
        result.Decision = guard.Deny
        result.Assessment = &guard.Assessment{
            Severity:   guard.Critical,
            Confidence: guard.ConfidenceHigh,
        }
        // Synthetic match for observability — callers can see WHY denied
        result.Matches = []guard.Match{{
            Pack:       "_blocklist",
            Rule:       pattern,
            Severity:   guard.Critical,
            Confidence: guard.ConfidenceHigh,
            Reason:     fmt.Sprintf("Command matched blocklist pattern: %s", pattern),
        }}
        return result
    }
    if matchesGlob(command, cfg.allowlist) {
        result.Decision = guard.Allow
        return result
    }

    // Short-circuit if no packs are enabled (nothing to match)
    enabledPacks := p.resolveEnabledPacks(cfg)
    if enabledPacks != nil && len(enabledPacks) == 0 {
        result.Decision = guard.Allow
        return result
    }

    // Step 3: Keyword pre-filter (always uses full automaton)
    filterResult := p.prefilter.Contains(command)
    if !filterResult.Matched {
        result.Decision = guard.Allow
        return result
    }

    // Steps 4-6: Parse + Extract + Normalize (handled by ParseAndExtract)
    parseResult := p.parser.ParseAndExtract(ctx, command, 0)
    // ParseResult.Warnings uses guard.Warning directly (parse imports guard/types.go)
    result.Warnings = append(result.Warnings, parseResult.Warnings...)

    // Handle parse failure
    if len(parseResult.Commands) == 0 && parseResult.HasError {
        result.Assessment = &guard.Assessment{
            Severity:   guard.Indeterminate,
            Confidence: guard.ConfidenceHigh,
        }
        result.Decision = policy.Decide(*result.Assessment)
        return result
    }

    // Step 9: Environment detection — per-command for inline env,
    // global for exported vars and process env.
    // Process env is parsed once for the entire evaluation.
    globalEnvResult := p.envDet.DetectGlobal(
        parseResult.ExportedVars,
        cfg.callerEnv,
    )

    // Step 10: Pattern matching
    candidatePacks := p.selectCandidatePacks(filterResult.Keywords, enabledPacks)
    var allMatches []guard.Match

    for _, cmd := range parseResult.Commands {
        // Per-command env detection: check this command's inline env
        cmdEnvResult := p.envDet.DetectInline(cmd.InlineEnv)
        isProduction := cmdEnvResult.IsProduction || globalEnvResult.IsProduction

        matches := p.matchCommand(cmd, candidatePacks, isProduction)
        allMatches = append(allMatches, matches...)
    }

    // Step 11: Assessment aggregation
    if len(allMatches) > 0 {
        result.Matches = allMatches
        result.Assessment = aggregateAssessments(allMatches)
    }

    // Handle partial parse with no matches → Indeterminate
    if parseResult.HasError && result.Assessment == nil {
        result.Assessment = &guard.Assessment{
            Severity:   guard.Indeterminate,
            Confidence: guard.ConfidenceHigh,
        }
    }

    // Handle expansion cap → Indeterminate floor.
    // If expansion was capped, unchecked expansions might contain more severe
    // matches. Use Indeterminate as a FLOOR: if existing assessment is less
    // severe, promote to Indeterminate.
    if hasWarning(result.Warnings, guard.WarnExpansionCapped) {
        if result.Assessment == nil {
            result.Assessment = &guard.Assessment{
                Severity:   guard.Indeterminate,
                Confidence: guard.ConfidenceHigh,
            }
        } else if result.Assessment.Severity < guard.Indeterminate {
            // Indeterminate is iota 0, so this check is: if severity is...
            // Actually Indeterminate=0, Low=1, so Indeterminate < Low.
            // We want: if current severity could be exceeded by unchecked
            // expansions, ensure at least Indeterminate. Since Indeterminate
            // is the lowest severity value and we can't know what the unchecked
            // expansions would produce, we add Indeterminate to warnings and
            // let the policy handle it. The existing assessment stands, but
            // we ensure the WarnExpansionCapped warning is visible.
        }
        // The WarnExpansionCapped warning is already present. The policy will
        // see the assessment (which may be from checked expansions) and the
        // caller can inspect warnings. If NO matches were found in checked
        // expansions, the nil-assessment case above sets Indeterminate.
    }

    // Step 12: Policy application
    if result.Assessment != nil {
        result.Decision = policy.Decide(*result.Assessment)
    } else {
        result.Decision = guard.Allow
    }

    return result
}

// validatePackConfig warns about unknown pack IDs in configuration.
func (p *Pipeline) validatePackConfig(cfg *evalConfig) []guard.Warning {
    var warnings []guard.Warning
    for _, id := range cfg.enabledPacks {
        if !p.registry.HasPack(id) {
            warnings = append(warnings, guard.Warning{
                Code:    guard.WarnUnknownPackID,
                Message: fmt.Sprintf("enabled pack %q not found in registry", id),
            })
        }
    }
    for _, id := range cfg.disabledPacks {
        if !p.registry.HasPack(id) {
            warnings = append(warnings, guard.Warning{
                Code:    guard.WarnUnknownPackID,
                Message: fmt.Sprintf("disabled pack %q not found in registry", id),
            })
        }
    }
    return warnings
}

// matchCommand matches a single extracted command against candidate packs.
// Safe-before-destructive: if a safe pattern matches in a pack, destructive
// patterns in that pack are skipped for this command.
//
// isProduction is the merged result of per-command inline env detection
// and global (exported + process) env detection.
func (p *Pipeline) matchCommand(
    cmd parse.ExtractedCommand,
    candidatePacks []*packs.Pack,
    isProduction bool,
) []guard.Match {
    var matches []guard.Match

    for _, pack := range candidatePacks {
        // Check safe patterns first
        safeMatched := false
        for _, sp := range pack.Safe {
            if matched, panicked := p.safeMatch(sp.Match, cmd); matched && !panicked {
                safeMatched = true
                break
            }
        }
        if safeMatched {
            continue // Skip destructive patterns for this command in this pack
        }

        // Check destructive patterns
        for _, dp := range pack.Destructive {
            if matched, _ := p.safeMatch(dp.Match, cmd); matched {
                sev := dp.Severity
                escalated := false
                // Environment escalation (per-command: only escalate if
                // production indicators are relevant to this command)
                if dp.EnvSensitive && isProduction {
                    sev = escalateSeverity(sev)
                    escalated = true
                }
                matches = append(matches, guard.Match{
                    Pack:         pack.ID,
                    Rule:         dp.Name,
                    Severity:     sev,
                    Confidence:   dp.Confidence,
                    Reason:       dp.Reason,
                    Remediation:  dp.Remediation,
                    EnvEscalated: escalated,
                })
            }
        }
    }

    return matches
}

// escalateSeverity bumps severity by one level, capped at Critical.
func escalateSeverity(s guard.Severity) guard.Severity {
    if s >= guard.Critical {
        return guard.Critical
    }
    return s + 1
}

// aggregateAssessments returns the highest-ranked assessment from matches.
// Primary sort: severity (descending). Secondary: confidence (descending).
func aggregateAssessments(matches []guard.Match) *guard.Assessment {
    if len(matches) == 0 {
        return nil
    }
    best := guard.Assessment{
        Severity:   matches[0].Severity,
        Confidence: matches[0].Confidence,
    }
    for _, m := range matches[1:] {
        if m.Severity > best.Severity ||
            (m.Severity == best.Severity && m.Confidence > best.Confidence) {
            best.Severity = m.Severity
            best.Confidence = m.Confidence
        }
    }
    return &best
}

// selectCandidatePacks returns packs whose keywords matched.
func (p *Pipeline) selectCandidatePacks(
    matchedKeywords []string,
    enabledPacks []string,
) []*packs.Pack {
    seen := make(map[string]bool)
    var result []*packs.Pack
    for _, kw := range matchedKeywords {
        for _, pack := range p.registry.PacksForKeyword(kw) {
            if !seen[pack.ID] {
                seen[pack.ID] = true
                if enabledPacks == nil || contains(enabledPacks, pack.ID) {
                    result = append(result, pack)
                }
            }
        }
    }
    return result
}

// resolveEnabledPacks computes the effective pack list from config.
// Returns nil for "all packs".
func (p *Pipeline) resolveEnabledPacks(cfg *evalConfig) []string {
    if cfg.enabledPacks != nil {
        // Explicit inclusion list — filter out disabled
        var result []string
        for _, id := range cfg.enabledPacks {
            if !contains(cfg.disabledPacks, id) {
                result = append(result, id)
            }
        }
        return result
    }
    if len(cfg.disabledPacks) > 0 {
        // All packs minus disabled
        var result []string
        for _, p := range p.registry.All() {
            if !contains(cfg.disabledPacks, p.ID) {
                result = append(result, p.ID)
            }
        }
        return result
    }
    return nil // All packs
}

// Helper functions

func isEmptyOrWhitespace(s string) bool {
    return strings.TrimSpace(s) == ""
}

func contains(slice []string, item string) bool {
    for _, s := range slice {
        if s == item {
            return true
        }
    }
    return false
}

func hasWarning(warnings []guard.Warning, code guard.WarningCode) bool {
    for _, w := range warnings {
        if w.Code == code {
            return true
        }
    }
    return false
}

// matchesGlobFirst returns the first matching pattern and true, or ("", false).
func matchesGlobFirst(command string, patterns []string) (string, bool) {
    for _, pattern := range patterns {
        if globMatch(pattern, command) {
            return pattern, true
        }
    }
    return "", false
}
```

**Warning type sharing**: `internal/parse` imports `guard/types.go` directly
for the shared `Warning` and `WarningCode` types. `guard/types.go` has zero
internal imports and is the leaf dependency. No `convertWarnings` function
needed — `ParseResult.Warnings` uses `guard.Warning` directly.

**Import flow** (definitive):

```mermaid
graph TD
    EVAL["internal/eval"] --> GUARD["guard (types.go)"]
    EVAL --> PARSE["internal/parse"]
    EVAL --> PACKS["internal/packs"]
    EVAL --> ENVDETECT["internal/envdetect"]
    PACKS --> GUARD
    PACKS --> PARSE
    ENVDETECT --> GUARD
    PARSE --> GUARD
```

`guard/types.go` is the leaf dependency. `parse.ParseResult` uses `guard.Warning`
directly:

```go
// In internal/parse/command.go (cross-plan requirement)
type ParseResult struct {
    Commands     []ExtractedCommand
    Warnings     []guard.Warning       // Shared warning type — no conversion needed
    HasError     bool
    ExportedVars map[string][]string   // From DataflowAnalyzer.ExportedVars()
}
```

**Cross-plan interface change**: Plan 01's `ParseResult` must add the
`ExportedVars map[string][]string` field, populated by the extractor from
`DataflowAnalyzer.ExportedVars()` after the AST walk completes. Plan 01's
`Warning`/`WarningCode` types should be replaced with imports from
`guard/types.go`. This is documented here as a required plan 01 update.

### 5.6 Policy Engine (`guard/policy.go`)

```go
package guard

// Policy converts an Assessment into a Decision.
// Implementations must be safe for concurrent use.
type Policy interface {
    Decide(Assessment) Decision
}

// StrictPolicy denies on Medium+ severity and any Indeterminate.
// Never produces Ask — it's designed for autonomous agents where
// there's no human to ask.
type strictPolicy struct{}

func StrictPolicy() Policy { return strictPolicy{} }

func (strictPolicy) Decide(a Assessment) Decision {
    switch {
    case a.Severity >= Medium:
        return Deny
    case a.Severity == Indeterminate:
        return Deny
    default:
        return Allow
    }
}

// InteractivePolicy asks on Medium severity and Indeterminate,
// denies on High+. Designed for human-in-the-loop workflows.
type interactivePolicy struct{}

func InteractivePolicy() Policy { return interactivePolicy{} }

func (interactivePolicy) Decide(a Assessment) Decision {
    switch {
    case a.Severity >= High:
        return Deny
    case a.Severity == Medium:
        return Ask
    case a.Severity == Indeterminate:
        return Ask
    default:
        return Allow
    }
}

// PermissivePolicy only denies Critical severity.
// Asks on High, allows Indeterminate. For risk-tolerant callers.
type permissivePolicy struct{}

func PermissivePolicy() Policy { return permissivePolicy{} }

func (permissivePolicy) Decide(a Assessment) Decision {
    switch {
    case a.Severity >= Critical:
        return Deny
    case a.Severity >= High:
        return Ask
    default:
        return Allow
    }
}
```

**Policy decision matrix**:

| Severity | Strict | Interactive | Permissive |
|----------|--------|-------------|------------|
| Critical | Deny | Deny | Deny |
| High | Deny | Deny | Ask |
| Medium | Deny | Ask | Allow |
| Low | Allow | Allow | Allow |
| Indeterminate | Deny | Ask | Allow |

**Note on Confidence (MF-P2.6)**: All three built-in policies decide based
solely on `Severity`; the `Confidence` field in `Assessment` is currently
unused. The 15-cell decision matrix above collapses to 5 rows (one per
severity). `Confidence` is carried through the pipeline to support custom
`Policy` implementations that may want to differentiate (e.g., a custom
policy that asks instead of denying for `ConfidenceLow` + `High` severity).
If no custom policies leverage it after real-world usage, consider removing
`Confidence` from `Assessment` to simplify the API.

### 5.7 Allowlist/Blocklist Matching (`internal/eval/allowlist.go`)

**Security constraint**: `*` must NOT match command separators (`;`, `&&`,
`||`, `|`, `\n`, `` ` ``, `$(`). Without this restriction, an allowlist
pattern `"git status *"` would match `"git status; rm -rf /"`, allowing
the destructive command to bypass all structural analysis. This is a P0
security issue — the glob's `*` matches arguments and flags but not
command boundaries.

```go
package eval

// matchesGlob checks if the command matches any of the given glob patterns.
func matchesGlob(command string, patterns []string) bool {
    for _, pattern := range patterns {
        if globMatch(pattern, command) {
            return true
        }
    }
    return false
}

// commandSeparators are characters that `*` must NOT match in glob patterns.
// These are shell command separators — matching across them would allow
// compound command injection through allowlist patterns.
var commandSeparators = [256]bool{
    ';':  true,
    '|':  true,
    '&':  true,
    '\n': true,
    '`':  true,
    '$':  true,
    '(':  true,
    ')':  true,
}

// globMatch implements glob matching where `*` matches any sequence of
// characters EXCEPT command separators (;, |, &, \n, `, $, (, )).
//
// This prevents compound command injection through allowlist patterns:
// "git status *" does NOT match "git status; rm -rf /"
//
// Only `*` is special. No `?`, `[...]`, or `**` support.
//
// Trailing ` *` semantics: "git push *" matches "git push --force" but
// NOT "git push" (the space before * is literal and must be present).
// Users wanting "with or without args" should use "git push*".
func globMatch(pattern, text string) bool {
    px, tx := 0, 0
    starPx, starTx := -1, -1

    for tx < len(text) {
        if px < len(pattern) && pattern[px] == '*' {
            // Star: record position, advance pattern
            starPx = px
            starTx = tx
            px++
        } else if px < len(pattern) && pattern[px] == text[tx] {
            // Exact match: advance both
            px++
            tx++
        } else if starPx >= 0 {
            // Mismatch but have a star: backtrack
            // But don't let * match across command separators
            starTx++
            if starTx < len(text) && commandSeparators[text[starTx]] {
                // Star cannot cross this separator — match fails
                return false
            }
            tx = starTx
            px = starPx + 1
        } else {
            return false
        }
    }

    // Consume trailing stars
    for px < len(pattern) && pattern[px] == '*' {
        px++
    }

    return px == len(pattern)
}
```

**Glob semantics summary**:
- `"git status"` — exact match
- `"git status *"` — matches `git status` with any args, but NOT across `;`/`&&`/`||`/`|`
- `"*/bin/git *"` — matches path-prefixed git with any args
- `"*"` — matches any single command (but not across separators)
- `"git push*"` — matches `"git push"` and `"git push --force"` (no space before `*`)

**Blocklist `"*"` pattern**: A blocklist entry of `"*"` blocks any single
command. Since blocklist patterns match before parsing, this effectively
blocks everything except empty commands. This is intentionally powerful
but should be validated at configuration time with a warning.

**`ArgMatcher` glob semantics**: `ArgMatcher.matchArg()` also uses glob
matching for argument values. It uses the same `globMatch` function as
allowlist/blocklist (with command-separator restrictions), which is more
permissive than `path.Match` for `/` characters. This ensures consistent
glob semantics across the codebase. Pack authors can use `"*.sql"` to match
`"backup.sql"` and `"/tmp/dump.sql"` alike.

### 5.8 Environment Detection (`internal/envdetect/detect.go`)

```go
package envdetect

import (
    neturl "net/url"
    "regexp"
    "strings"

    "github.com/dcosson/destructive-command-guard-go/guard"
)

// Detector checks for production environment indicators.
// Stateless and safe for concurrent use.
type Detector struct {
    rules []envRule
}

// Result contains the outcome of environment detection.
type Result struct {
    IsProduction bool               // True if any production indicator found
    Indicators   []ProductionIndicator // What was detected
}

// ProductionIndicator describes a detected production indicator.
type ProductionIndicator struct {
    Source string // "inline", "export", "process"
    Var    string // Env var name
    Value  string // Env var value
}

// envRule defines how to check one category of env vars.
type envRule struct {
    Names   []string        // Env var names to check
    Check   func(string) bool // Returns true if value indicates production
    RuleType string          // For diagnostics
}

// prodWordBoundary matches "prod" as a word boundary (not substring).
// "productivity.internal" → no match. "prod-db.acme.com" → match.
var prodWordBoundary = regexp.MustCompile(`(?i)\bprod\b`)

func NewDetector() *Detector {
    return &Detector{
        rules: []envRule{
            {
                // Exact-value env vars: checked for "production" or "prod"
                Names:    []string{"RAILS_ENV", "NODE_ENV", "FLASK_ENV", "APP_ENV", "MIX_ENV", "RACK_ENV"},
                Check:    isExactProd,
                RuleType: "exact",
            },
            {
                // URL-shaped env vars: hostname contains "prod" as word boundary
                Names:    []string{"DATABASE_URL", "REDIS_URL", "MONGO_URL", "ELASTICSEARCH_URL"},
                Check:    hasURLProd,
                RuleType: "url",
            },
            {
                // Profile env vars: value contains "prod" as word boundary
                Names:    []string{"AWS_PROFILE", "GOOGLE_CLOUD_PROJECT", "AZURE_SUBSCRIPTION"},
                Check:    hasProfileProd,
                RuleType: "profile",
            },
        },
    }
}

// DetectInline checks a single command's inline env vars for production
// indicators. Called once per extracted command in the pipeline.
func (d *Detector) DetectInline(inlineEnv map[string]string) Result {
    var indicators []ProductionIndicator
    for _, rule := range d.rules {
        for _, name := range rule.Names {
            if val, ok := inlineEnv[name]; ok && rule.Check(val) {
                indicators = append(indicators, ProductionIndicator{
                    Source: "inline",
                    Var:    name,
                    Value:  val,
                })
            }
        }
    }
    return Result{
        IsProduction: len(indicators) > 0,
        Indicators:   indicators,
    }
}

// DetectGlobal checks exported vars and process env for production
// indicators. Called once per pipeline run (these sources are global,
// not scoped to individual commands).
//
// exportedVars: variables exported via `export` (from dataflow analysis).
// processEnv: caller-provided env vars in os.Environ() format ("KEY=VALUE").
func (d *Detector) DetectGlobal(
    exportedVars map[string][]string,
    processEnv []string,
) Result {
    var indicators []ProductionIndicator

    // Check exported vars (from dataflow analysis)
    for _, rule := range d.rules {
        for _, name := range rule.Names {
            if vals, ok := exportedVars[name]; ok {
                for _, val := range vals {
                    if rule.Check(val) {
                        indicators = append(indicators, ProductionIndicator{
                            Source: "export",
                            Var:    name,
                            Value:  val,
                        })
                    }
                }
            }
        }
    }

    // Check process env vars
    processEnvMap := parseEnv(processEnv)
    for _, rule := range d.rules {
        for _, name := range rule.Names {
            if val, ok := processEnvMap[name]; ok && rule.Check(val) {
                indicators = append(indicators, ProductionIndicator{
                    Source: "process",
                    Var:    name,
                    Value:  val,
                })
            }
        }
    }

    return Result{
        IsProduction: len(indicators) > 0,
        Indicators:   indicators,
    }
}

// MergeResults combines two Results, preserving all indicators.
func MergeResults(a, b Result) Result {
    indicators := append(a.Indicators, b.Indicators...)
    return Result{
        IsProduction: a.IsProduction || b.IsProduction,
        Indicators:   indicators,
    }
}

// isExactProd checks for exact "production" or "prod" values.
func isExactProd(value string) bool {
    v := strings.ToLower(strings.TrimSpace(value))
    return v == "production" || v == "prod"
}

// hasURLProd checks if a URL contains "prod" as a word boundary in hostname.
func hasURLProd(value string) bool {
    // Extract hostname portion (between :// and next / or :port)
    host := extractHostname(value)
    return prodWordBoundary.MatchString(host)
}

// hasProfileProd checks if a profile/project name contains "prod" as word boundary.
func hasProfileProd(value string) bool {
    return prodWordBoundary.MatchString(value)
}

// extractHostname extracts the hostname from a URL-like string using
// net/url.Parse. Falls back to raw string extraction for non-URL values.
//
// Handles: "postgres://user:pass@prod-db.acme.com:5432/mydb" → "prod-db.acme.com"
// Handles: "user@host/db@name" correctly (no confusion from @ in path)
func extractHostname(rawURL string) string {
    // Ensure a scheme exists so net/url.Parse treats it as absolute.
    toParse := rawURL
    if !strings.Contains(toParse, "://") {
        toParse = "scheme://" + toParse
    }
    u, err := neturl.Parse(toParse)
    if err != nil || u.Hostname() == "" {
        // Fallback: strip scheme, userinfo, port, path manually
        s := rawURL
        if idx := strings.Index(s, "://"); idx >= 0 {
            s = s[idx+3:]
        }
        if idx := strings.Index(s, "@"); idx >= 0 {
            s = s[idx+1:]
        }
        if idx := strings.IndexAny(s, ":/"); idx >= 0 {
            s = s[:idx]
        }
        return s
    }
    return u.Hostname()
}

// parseEnv parses os.Environ()-format env vars into a map.
// Note (SE-02-P2.5): This allocates a new map per call. For typical usage
// (seconds between calls), this is negligible. If profiling shows this as
// a hotspot (e.g., in benchmarks), the pipeline can cache the parsed map
// in evalConfig since callerEnv is constant for the process lifetime.
func parseEnv(env []string) map[string]string {
    m := make(map[string]string, len(env))
    for _, e := range env {
        if idx := strings.IndexByte(e, '='); idx >= 0 {
            m[e[:idx]] = e[idx+1:]
        }
    }
    return m
}
```

**Detection split (per MF-P1.2/SE-02-P1.3 review feedback)**:

Environment detection is split into per-command and global phases to avoid
false escalation. The pipeline calls:
1. `DetectInline(cmd.InlineEnv)` — once per extracted command. Only inline
   env vars scoped to that specific command are checked.
2. `DetectGlobal(exportedVars, callerEnv)` — once per pipeline run. Exported
   vars and process env vars are global context.

Results are merged with `MergeResults()` before checking `isProduction` for
each command's env-sensitive patterns. This prevents
`RAILS_ENV=production echo hello && git clean -f` from escalating `git clean`
due to an inline env var on a different command.

**`extractHostname` implementation (per MF-P2.2 review feedback)**:

Uses `net/url.Parse()` for robust URL parsing. Falls back to manual string
extraction if `net/url.Parse()` fails (e.g., for non-URL values). This
correctly handles `@` in passwords and paths:

- `"postgres://user:p@ss@prod-db.acme.com:5432/mydb"` → `"prod-db.acme.com"` ✓
- `"user@host/db@name"` → `"host"` (net/url resolves correctly) ✓
- No scheme: `"prod-db.acme.com:5432"` → `"prod-db.acme.com"` (scheme prepended for parsing)
- IPv4: `"192.168.1.1"` → `"192.168.1.1"` (no "prod" match)
- IPv6: `"[::1]"` → `"::1"` (net/url.Hostname() strips brackets)
- Empty string: → `""` (no match)

### 5.9 Test Pack: `core.git` (`internal/packs/core/git.go`)

A minimal pack to validate the entire framework end-to-end. Contains 2
safe patterns and 5 destructive patterns.

```go
package core

import (
    "github.com/dcosson/destructive-command-guard-go/guard"
    "github.com/dcosson/destructive-command-guard-go/internal/packs"
)

func init() {
    packs.DefaultRegistry.Register(gitPack)
}

var gitPack = packs.Pack{
    ID:          "core.git",
    Name:        "Git",
    Description: "Git version control destructive operations",
    Keywords:    []string{"git"},

    Safe: []packs.SafePattern{
        {
            Name: "git-push-no-force",
            // Per MF-P1.3: also forbid --mirror and --delete, which are
            // destructive but don't use --force. Without this, "git push
            // --mirror" would match the safe pattern and skip destructive
            // pattern evaluation for this pack.
            Match: packs.And(
                packs.Name("git"),
                packs.ArgAt(0, "push"),
                packs.Not(packs.Or(
                    packs.Flags("--force"),
                    packs.Flags("-f"),
                    packs.Flags("--mirror"),
                    packs.Flags("--delete"),
                )),
            ),
        },
        {
            Name: "git-push-force-with-lease",
            Match: packs.And(
                packs.Name("git"),
                packs.ArgAt(0, "push"),
                packs.Flags("--force-with-lease"),
                packs.Not(packs.Flags("--force")),
            ),
        },
    },

    Destructive: []packs.DestructivePattern{
        {
            Name: "git-push-force",
            Match: packs.And(
                packs.Name("git"),
                packs.ArgAt(0, "push"),
                packs.Or(
                    packs.Flags("--force"),
                    packs.Flags("-f"),
                ),
            ),
            Severity:     guard.High,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "git push --force overwrites remote history, potentially losing commits",
            Remediation:  "Use git push --force-with-lease for safer force pushing",
            EnvSensitive: false,
        },
        {
            Name: "git-reset-hard",
            Match: packs.And(
                packs.Name("git"),
                packs.ArgAt(0, "reset"),
                packs.Flags("--hard"),
            ),
            Severity:     guard.High,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "git reset --hard discards uncommitted changes permanently",
            Remediation:  "Use git stash before git reset --hard, or use git reset --soft",
            EnvSensitive: false,
        },
        {
            Name: "git-clean-force",
            Match: packs.And(
                packs.Name("git"),
                packs.ArgAt(0, "clean"),
                packs.Or(
                    packs.Flags("-f"),
                    packs.Flags("--force"),
                ),
            ),
            Severity:     guard.Medium,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "git clean -f permanently deletes untracked files",
            Remediation:  "Use git clean -n (dry run) first to preview what will be deleted",
            EnvSensitive: false,
        },
        {
            // Per MF-P1.3: --mirror is destructive (overwrites all remote refs)
            Name: "git-push-mirror",
            Match: packs.And(
                packs.Name("git"),
                packs.ArgAt(0, "push"),
                packs.Flags("--mirror"),
            ),
            Severity:     guard.High,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "git push --mirror overwrites all remote refs to match local, potentially deleting remote branches",
            Remediation:  "Use explicit branch pushes instead of --mirror",
            EnvSensitive: false,
        },
        {
            // Per MF-P1.3: --delete is destructive (deletes remote branches)
            Name: "git-push-delete",
            Match: packs.And(
                packs.Name("git"),
                packs.ArgAt(0, "push"),
                packs.Flags("--delete"),
            ),
            Severity:     guard.Medium,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "git push --delete removes remote branches or tags",
            Remediation:  "Verify the branch/tag name before deleting from remote",
            EnvSensitive: false,
        },
    },
}
```

### 5.10 Golden File Infrastructure (`internal/eval/testdata/golden/`)

Golden files provide regression testing for the full evaluation pipeline.
Each entry is a command with its expected outcome.

**File format** (one entry per block, separated by `---`):

Each golden file starts with a `format: v1` header line. The parser
validates this header on load and rejects files without it, preventing
silent format drift (SE-02-P3.3).

```
format: v1
---
# Description of the test case
command: git push --force origin main
decision: Deny
severity: High
confidence: High
pack: core.git
rule: git-push-force
env_escalated: false
---
# Safe push should be allowed
command: git push origin main
decision: Allow
---
# Force push with lease is safe
command: git push --force-with-lease origin main
decision: Allow
---
```

**Golden file test runner**:

```go
package golden

import (
    "bufio"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

// GoldenEntry represents a single golden file test case.
type GoldenEntry struct {
    Description   string
    Command       string
    Decision      string
    Severity      string // Empty if Allow
    Confidence    string // Empty if Allow
    Pack          string // Empty if Allow
    Rule          string // Empty if Allow
    EnvEscalated  string // Empty if false or Allow
    Warnings      []string
    File          string // Source file for error reporting
    Line          int    // Line number for error reporting
}

// LoadCorpus loads all golden files from the corpus directory.
func LoadCorpus(t *testing.T, dir string) []GoldenEntry {
    t.Helper()
    var entries []GoldenEntry
    err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
        if err != nil {
            return err
        }
        if !strings.HasSuffix(path, ".txt") {
            return nil
        }
        fileEntries := parseGoldenFile(t, path)
        entries = append(entries, fileEntries...)
        return nil
    })
    if err != nil {
        t.Fatalf("walk corpus dir: %v", err)
    }
    return entries
}

func parseGoldenFile(t *testing.T, path string) []GoldenEntry {
    t.Helper()
    // Parse entries separated by "---", each with key: value pairs
    // Lines starting with # are descriptions
    //
    // Schema validation (MF-P3.1):
    // 1. First non-comment line must be "format: v1"
    // 2. Every entry must have "command" and "decision" fields
    // 3. "decision" must be one of: Allow, Deny, Ask
    // 4. "severity" must be one of: Critical, High, Medium, Low, Indeterminate (if present)
    // 5. Unknown keys emit t.Errorf (catches typos like "desision")
    ...
}

// RunCorpus runs all golden file entries against the pipeline.
func RunCorpus(t *testing.T, entries []GoldenEntry, pipeline *Pipeline, cfg *evalConfig) {
    for _, e := range entries {
        t.Run(e.Description, func(t *testing.T) {
            result := pipeline.Run(context.Background(), e.Command, cfg)
            assertDecision(t, e, result)
            if e.Severity != "" {
                assertSeverity(t, e, result)
            }
            if e.Pack != "" {
                assertPack(t, e, result)
            }
            // ... etc.
        })
    }
}
```

**Initial seed corpus**: Created as part of plan 02 implementation, covering:
- core.git pack: all 3 destructive patterns + 2 safe patterns (10–15 entries)
- Edge cases: path-prefixed `git`, quoted args, empty commands (5–10 entries)
- Env escalation: `RAILS_ENV=production git push --force` (2–3 entries)
- Allow cases: benign commands that should skip entirely (5–10 entries)

Total initial seed: ~30–40 golden entries. Batch 3 packs will add entries
as they are developed. Batch 5 targets 500+.

**Golden file properties verified automatically**:
1. Every destructive pattern in every registered pack has at least 1 golden
   entry that matches it
2. Every safe pattern has at least 1 golden entry that triggers it
3. No golden entry produces an unexpected warning (warnings field is exhaustive)

---

## 6. State Diagram: Pipeline Decision Flow

```mermaid
stateDiagram-v2
    [*] --> InputValidation
    InputValidation --> Allow: Empty/whitespace
    InputValidation --> Indeterminate: Too large
    InputValidation --> BlocklistCheck

    BlocklistCheck --> Deny: Blocklist match
    BlocklistCheck --> AllowlistCheck

    AllowlistCheck --> Allow: Allowlist match
    AllowlistCheck --> KeywordFilter

    KeywordFilter --> Allow: No keywords match
    KeywordFilter --> Parse: Keywords found

    Parse --> Indeterminate: Full parse failure
    Parse --> PatternMatch: Commands extracted

    PatternMatch --> EnvEscalation: Destructive match
    PatternMatch --> CheckPartialParse: No match

    EnvEscalation --> Aggregation
    Aggregation --> PolicyDecision

    CheckPartialParse --> Indeterminate: HasError=true
    CheckPartialParse --> Allow: Clean parse

    Indeterminate --> PolicyDecision
    PolicyDecision --> Allow
    PolicyDecision --> Deny
    PolicyDecision --> Ask

    Allow --> [*]
    Deny --> [*]
    Ask --> [*]
```

---

## 7. Error Handling

### Matcher Panics

Every `CommandMatcher.Match()` call is wrapped in a recovery boundary in
the pipeline's `matchCommand` function:

```go
func (p *Pipeline) safeMatch(matcher packs.CommandMatcher, cmd parse.ExtractedCommand) (matched bool, panicked bool) {
    defer func() {
        if r := recover(); r != nil {
            panicked = true
        }
    }()
    return matcher.Match(cmd), false
}
```

If a matcher panics, `WarnMatcherPanic` is added to the result and the
pipeline continues with the next pattern. This mirrors the panic recovery
approach in plan 01 for the parser and extractor.

### Invalid Glob Patterns

`globMatch` handles all patterns without error — `*` is the only special
character, and everything else is literal. There are no invalid patterns.

### Registry Panics (Init-time Only)

`Register()` panics on duplicate pack IDs or registration after freeze.
These are programming errors caught at init time (during `init()` functions),
not at runtime.

### Environment Detection Edge Cases

- Empty env var values → no match (empty string is never "production"/"prod")
- Malformed URLs → `extractHostname` handles gracefully (returns whatever is there)
- Very long env var values → no special handling (regex match is bounded)

---

## 8. Testing Strategy

### 8.1 Unit Tests

**matcher_test.go** — CommandMatcher implementations (table-driven):

```go
func TestNameMatcher(t *testing.T) {
    tests := []struct {
        name    string
        matcher NameMatcher
        cmd     parse.ExtractedCommand
        want    bool
    }{
        {"exact match", Name("git"), cmd("git"), true},
        {"no match", Name("git"), cmd("docker"), false},
        {"case sensitive", Name("git"), cmd("Git"), false},
        {"empty name", Name(""), cmd(""), true},
    }
    ...
}

func TestFlagMatcher(t *testing.T) {
    tests := []struct {
        name    string
        matcher FlagMatcher
        cmd     parse.ExtractedCommand
        want    bool
    }{
        {"required present", Flags("--force"), cmdWithFlags("--force"), true},
        {"required absent", Flags("--force"), cmdWithFlags("--verbose"), false},
        {"multiple required all present", Flags("--force", "--no-verify"),
            cmdWithFlags("--force", "--no-verify"), true},
        {"forbidden present", ForbidFlags("--force"), cmdWithFlags("--force"), false},
        {"forbidden absent", ForbidFlags("--force"), cmdWithFlags("--verbose"), true},
        {"force vs force-with-lease", Flags("--force"), cmdWithFlags("--force-with-lease"), false},
        {"required value match", FlagMatcher{RequiredValues: map[string]string{"--strategy": "ours"}},
            cmdWithFlagValues(map[string]string{"--strategy": "ours"}), true},
        {"required value mismatch", FlagMatcher{RequiredValues: map[string]string{"--strategy": "ours"}},
            cmdWithFlagValues(map[string]string{"--strategy": "theirs"}), false},
    }
    ...
}

func TestArgMatcher(t *testing.T) {
    // Test exact match, glob match, prefix match, index-specific, any position
    ...
}

func TestArgContentMatcher(t *testing.T) {
    // Test substring, regex, at-index, any position
    ...
}

func TestEnvMatcher(t *testing.T) {
    // Test name+value match, name-only match, missing var
    ...
}

func TestCompositeMatcher(t *testing.T) {
    // Test AND, OR with various child combinations
    // Test nested composition: And(Name("git"), Or(Flags("--force"), Flags("-f")))
    ...
}

func TestNegativeMatcher(t *testing.T) {
    // Test inversion of inner matcher
    ...
}

// Full pattern tests matching real pack patterns
func TestGitPushForcePattern(t *testing.T) {
    pattern := And(Name("git"), ArgAt(0, "push"), Or(Flags("--force"), Flags("-f")))
    tests := []struct {
        name string
        cmd  parse.ExtractedCommand
        want bool
    }{
        {"git push --force", cmdFull("git", []string{"push"}, map[string]string{"--force": ""}), true},
        {"git push -f", cmdFull("git", []string{"push"}, map[string]string{"-f": ""}), true},
        {"git push (no force)", cmdFull("git", []string{"push"}, nil), false},
        {"git push --force-with-lease", cmdFull("git", []string{"push"}, map[string]string{"--force-with-lease": ""}), false},
        {"docker push --force", cmdFull("docker", []string{"push"}, map[string]string{"--force": ""}), false},
        {"git pull --force", cmdFull("git", []string{"pull"}, map[string]string{"--force": ""}), false},
    }
    ...
}
```

**registry_test.go** — Pack registry:

```go
func TestRegistryRegister(t *testing.T) {
    // Register a pack, Get it back
    // Duplicate ID panics
    // Frozen registry panics on Register
}

func TestRegistryKeywords(t *testing.T) {
    // Keywords aggregated from all packs
    // Keywords are deduplicated and sorted
}

func TestRegistryPacksForKeyword(t *testing.T) {
    // Returns correct packs for keyword
    // Unknown keyword returns nil
}
```

**prefilter_test.go** — Keyword pre-filter:

```go
func TestPreFilterContains(t *testing.T) {
    tests := []struct {
        name    string
        command string
        want    bool
    }{
        {"git command", "git push --force", true},
        {"no keywords", "ls -la", false},
        {"keyword in string arg", `echo "git is great"`, true}, // Acceptable FP
        {"empty command", "", false},
        {"keyword substring rejected", "gitignore", false}, // Word-boundary filter rejects
    }
    ...
}

func TestPreFilterWordBoundary(t *testing.T) {
    // Verify word-boundary post-filter reduces false positives
    tests := []struct {
        name    string
        command string
        want    bool
    }{
        {"keyword as standalone word", "git push --force", true},
        {"keyword as substring", "gitignore", false},   // Word-boundary check rejects
        {"keyword in compound word", "gitbook setup", false},
        {"keyword after separator", "echo hi; git push", true},
        {"keyword after pipe", "ls | git status", true},
    }
    ...
}
```

**allowlist_test.go** — Glob matching:

```go
func TestGlobMatch(t *testing.T) {
    tests := []struct {
        pattern string
        text    string
        want    bool
    }{
        {"git status", "git status", true},
        {"git status", "git push", false},
        {"git status *", "git status --short", true},
        {"git status *", "git status", false}, // * requires at least one char
        {"*", "anything at all", true},
        {"*/bin/git *", "/usr/bin/git push", true},
        {"git *", "git push --force origin main", true},
        {"", "", true},
        {"*git*", "using git today", true},
    }
    ...
}
```

**policy_test.go** — Policy engine:

```go
func TestStrictPolicy(t *testing.T) {
    p := StrictPolicy()
    tests := []struct {
        assessment Assessment
        want       Decision
    }{
        {Assessment{Critical, ConfidenceHigh}, Deny},
        {Assessment{High, ConfidenceHigh}, Deny},
        {Assessment{Medium, ConfidenceHigh}, Deny},
        {Assessment{Low, ConfidenceHigh}, Allow},
        {Assessment{Indeterminate, ConfidenceHigh}, Deny},
    }
    ...
}

func TestInteractivePolicy(t *testing.T) { ... }
func TestPermissivePolicy(t *testing.T) { ... }
```

**detect_test.go** — Environment detection (split API):

```go
func TestDetectInline(t *testing.T) {
    d := NewDetector()
    tests := []struct {
        name      string
        inlineEnv map[string]string
        wantProd  bool
    }{
        {"RAILS_ENV=production", map[string]string{"RAILS_ENV": "production"}, true},
        {"RAILS_ENV=prod", map[string]string{"RAILS_ENV": "prod"}, true},
        {"RAILS_ENV=development", map[string]string{"RAILS_ENV": "development"}, false},
        {"NODE_ENV=production", map[string]string{"NODE_ENV": "production"}, true},
        {"no env vars", map[string]string{}, false},
        {"irrelevant env var", map[string]string{"FOO": "bar"}, false},
    }
    ...
}

func TestDetectGlobalURLProd(t *testing.T) {
    d := NewDetector()
    tests := []struct {
        name     string
        url      string
        wantProd bool
    }{
        {"prod in hostname", "postgres://user:pass@prod-db.acme.com:5432/mydb", true},
        {"production in hostname", "redis://production.redis.acme.com:6379", true},
        {"productivity false positive", "postgres://user@productivity.internal:5432/db", false},
        {"staging", "postgres://user@staging-db.acme.com:5432/mydb", false},
        {"localhost", "postgres://localhost:5432/mydb", false},
        // MF-P2.2: @-in-path edge case
        {"at-in-path", "postgres://user@host/db@name", false},  // host is "host", not "name"
        {"at-in-password", "postgres://user:p@ss@prod-db.acme.com:5432/db", true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            envMap := map[string][]string{"DATABASE_URL": {tt.url}}
            result := d.DetectGlobal(envMap, nil)
            assert.Equal(t, tt.wantProd, result.IsProduction)
        })
    }
}

func TestDetectGlobalProcessEnv(t *testing.T) {
    // Same checks but via processEnv (os.Environ format)
    ...
}

func TestMergeResults(t *testing.T) {
    a := Result{IsProduction: false, Indicators: nil}
    b := Result{IsProduction: true, Indicators: []ProductionIndicator{
        {Source: "inline", Var: "RAILS_ENV", Value: "production"},
    }}
    merged := MergeResults(a, b)
    assert.True(t, merged.IsProduction)
    assert.Len(t, merged.Indicators, 1)
}
```

### 8.2 Integration Tests

**pipeline_test.go** — Full pipeline tests:

```go
func TestPipelineEndToEnd(t *testing.T) {
    // Uses real BashParser, real Registry with test pack
    pipeline := setupTestPipeline(t)
    cfg := &evalConfig{policy: InteractivePolicy()}

    tests := []struct {
        name     string
        command  string
        decision Decision
    }{
        {"safe push", "git push origin main", Allow},
        {"force push", "git push --force origin main", Deny},
        {"force with lease", "git push --force-with-lease origin main", Allow},
        {"reset hard", "git reset --hard HEAD~3", Deny},
        {"clean force", "git clean -f", Ask}, // Medium → Ask with Interactive
        {"no keywords", "ls -la", Allow},
        {"empty command", "", Allow},
        {"unknown command", "foo bar baz", Allow},
        {"pipeline with force push", "echo ready && git push --force", Deny},
    }
    ...
}

func TestPipelineEnvEscalation(t *testing.T) {
    pipeline := setupTestPipeline(t)
    cfg := &evalConfig{
        policy:    InteractivePolicy(),
        callerEnv: []string{"RAILS_ENV=production"},
    }
    // Test that env-sensitive patterns get escalated
    ...
}

func TestPipelineAllowlist(t *testing.T) {
    pipeline := setupTestPipeline(t)
    cfg := &evalConfig{
        policy:    StrictPolicy(),
        allowlist: []string{"git push *"},
    }
    result := pipeline.Run(ctx, "git push --force origin main", cfg)
    assert.Equal(t, Allow, result.Decision) // Allowlisted
}

func TestPipelineBlocklist(t *testing.T) {
    pipeline := setupTestPipeline(t)
    cfg := &evalConfig{
        policy:    PermissivePolicy(),
        blocklist: []string{"rm -rf *"},
    }
    result := pipeline.Run(ctx, "rm -rf /tmp/test", cfg)
    assert.Equal(t, Deny, result.Decision) // Blocklisted
}

func TestPipelineBlocklistPrecedence(t *testing.T) {
    pipeline := setupTestPipeline(t)
    cfg := &evalConfig{
        policy:    PermissivePolicy(),
        allowlist: []string{"git *"},
        blocklist: []string{"git push --force *"},
    }
    result := pipeline.Run(ctx, "git push --force origin main", cfg)
    assert.Equal(t, Deny, result.Decision) // Blocklist wins
}

func TestPipelinePartialParse(t *testing.T) {
    pipeline := setupTestPipeline(t)
    cfg := &evalConfig{policy: InteractivePolicy()}
    result := pipeline.Run(ctx, "git push &&& echo done", cfg)
    // Should have WarnPartialParse warning
    // Decision depends on whether destructive patterns were found in parsed portion
    ...
}

// MF-P3.2: Disabled pack behavior
func TestPipelineDisabledPacks(t *testing.T) {
    pipeline := setupTestPipeline(t)

    tests := []struct {
        name          string
        command       string
        disabledPacks []string
        enabledPacks  []string
        wantDecision  Decision
    }{
        {"disabled pack skips matching",
            "git push --force", []string{"core.git"}, nil, Allow},
        {"enabled and disabled same pack — disabled wins",
            "git push --force", []string{"core.git"}, []string{"core.git"}, Allow},
        {"all packs disabled",
            "git push --force", []string{"core.git"}, nil, Allow},
        {"non-existent disabled pack — silently ignored",
            "git push --force", []string{"nonexistent"}, nil, Deny},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            cfg := &evalConfig{
                policy:        InteractivePolicy(),
                disabledPacks: tt.disabledPacks,
                enabledPacks:  tt.enabledPacks,
            }
            result := pipeline.Run(ctx, tt.command, cfg)
            assert.Equal(t, tt.wantDecision, result.Decision)
        })
    }
}

// SE-02-P3.2: ArgMatcher with invalid glob falls back to exact match
func TestArgMatcherInvalidGlob(t *testing.T) {
    matcher := packs.Arg("[invalid") // Invalid glob pattern (unclosed bracket)
    cmd := parse.ExtractedCommand{Args: []string{"[invalid"}}
    assert.True(t, matcher.Match(cmd), "invalid glob should fallback to exact match")

    cmd2 := parse.ExtractedCommand{Args: []string{"other"}}
    assert.False(t, matcher.Match(cmd2), "invalid glob fallback should not match other strings")
}

// MF-P2.5: Flag coexistence — --force and --force-with-lease are separate keys
func TestFlagCoexistenceSeparateKeys(t *testing.T) {
    // Verify that the extractor stores --force and --force-with-lease as separate
    // flag keys, not treating one as a variant of the other.
    // This is a cross-plan consistency test — plan 01's flag decomposition
    // must produce separate entries for each flag.
    cmd := parse.ExtractedCommand{
        Name: "git",
        Args: []string{"push"},
        Flags: map[string]string{
            "--force":           "",
            "--force-with-lease": "",
        },
    }
    forcePattern := packs.Flags("--force")
    leasePattern := packs.Flags("--force-with-lease")
    assert.True(t, forcePattern.Match(cmd), "--force should match independently")
    assert.True(t, leasePattern.Match(cmd), "--force-with-lease should match independently")
}
```

### 8.3 Benchmarks

```go
func BenchmarkPreFilter(b *testing.B) {
    kf := NewKeywordPreFilter(packs.DefaultRegistry)
    commands := []string{
        "ls -la",                      // No match (common case)
        "git push --force origin main", // Match
        "echo hello world",             // No match
        strings.Repeat("a ", 1000),     // Long no-match
    }
    for _, cmd := range commands {
        b.Run(cmd[:min(20, len(cmd))], func(b *testing.B) {
            for i := 0; i < b.N; i++ {
                kf.Contains(cmd, nil)
            }
        })
    }
}

func BenchmarkGlobMatch(b *testing.B) {
    // Benchmark various pattern/text combinations
    ...
}

func BenchmarkMatchCommand(b *testing.B) {
    // Benchmark pattern matching against extracted commands
    ...
}

func BenchmarkFullPipeline(b *testing.B) {
    pipeline := setupBenchPipeline(b)
    cfg := &evalConfig{policy: InteractivePolicy()}
    commands := []string{
        "git push --force origin main",
        "ls -la",
        "RAILS_ENV=production rails db:reset",
        "echo hello",
    }
    for _, cmd := range commands {
        b.Run(cmd[:min(30, len(cmd))], func(b *testing.B) {
            for i := 0; i < b.N; i++ {
                pipeline.Run(context.Background(), cmd, cfg)
            }
        })
    }
}
```

---

## 9. Alien Artifacts

### Aho-Corasick Automaton for Keyword Pre-Filter

The Aho-Corasick algorithm is a multi-pattern string matching algorithm
that constructs a finite automaton from a set of patterns and scans the
input in a single pass, O(n + m + z) where n is input length, m is total
pattern length, and z is number of matches. Originally published by
Alfred Aho and Margaret Corasick (1975).

**Why it's applicable**: We have ~50 keywords from 21 packs. Naive
substring search would require 50 separate passes. Aho-Corasick matches
all 50 simultaneously in one pass. For short inputs (~100 bytes), the
difference is negligible. But it scales linearly if inputs grow (up to
128KB) and provides a clean, well-understood algorithmic foundation.

**Implementation choice**: Pure-Go library (no cgo). A single automaton is
built once at init time from all registered pack keywords and reused for all
evaluations. The pre-filter returns matched keywords; the pipeline then
uses the registry's reverse index (`keywordIndex`) to map keywords to
candidate packs and filters by `enabledPacks`/`disabledPacks` downstream.
This eliminates subset automaton caching entirely (per MF-P2.3/SE-02-P1.5),
reducing memory footprint and complexity.

**Word-boundary post-filter**: After Aho-Corasick returns substring matches,
a word-boundary check filters out matches embedded in longer words (e.g.,
"git" in "gitignore"). This significantly reduces false pass-through while
maintaining O(1) per-match cost (per MF-P1.4/SE-02-P2.1).

---

## 10. URP (Unreasonably Robust Programming)

### Golden File Regression Testing

Every behavior change must be explicitly acknowledged. The golden file
corpus grows monotonically — entries are never removed, only added. If a
code change causes a golden file mismatch, the developer must update the
golden file entry AND document why in the commit message. This creates an
audit trail of behavioral changes.

**Measurement**: Golden file count, coverage of pack patterns (every
pattern must have ≥3 entries), CI pass rate.

### Matcher Panic Recovery

Every `CommandMatcher.Match()` invocation in the pipeline is wrapped in
`recover()`. A panicking matcher produces `WarnMatcherPanic` and the
pipeline continues with remaining patterns. This ensures a bug in a single
matcher never crashes the entire evaluation.

**Measurement**: Panic count in production (target: 0). Fuzz testing
should never trigger a panic.

### Pack Registry Freeze Invariant

The registry freezes on first read, preventing runtime modification of
the pack set. This eliminates a class of concurrency bugs (TOCTOU between
checking pack existence and using a pack). The panic on post-freeze
registration is caught at init time, not during live traffic.

### Pattern Completeness Verification

A test verifies that every registered pack has:
1. At least one keyword
2. At least one destructive pattern
3. Every destructive pattern has a non-empty Reason and Remediation
4. Every pattern Name is unique within its pack
5. Every golden file entry references a valid pack+rule

This runs in CI and catches incomplete pack registrations.

### Policy Decision Matrix Test

A single table-driven test verifies the complete decision matrix for all
three built-in policies across all severity levels. This makes it impossible
to accidentally change policy behavior without updating the test.

---

## 11. Extreme Optimization

Per architecture §10, extreme optimization is not applicable. The
evaluation pipeline processes short strings at LLM-response frequency.

**Applicable optimizations**:

1. **Aho-Corasick pre-filter**: O(n) multi-pattern match eliminates >90%
   of inputs before parsing. This is the highest-leverage optimization —
   avoiding work entirely.

2. **Lazy pack initialization**: Matchers that use compiled regexes
   (`ArgContentMatcher`) compile the regex once at pack registration time
   (in `init()`), not on each match. `regexp.MustCompile` panics at init
   if the regex is invalid, preventing runtime regex compilation errors.

3. **Single pre-filter automaton**: One automaton covers all keywords.
   Pack filtering is done downstream via the registry's reverse index,
   avoiding the cost of building and caching subset automatons.

4. **Short-circuit in safe pattern matching**: If a safe pattern matches,
   destructive patterns for that pack are skipped entirely. This reduces
   the number of matcher invocations for commands that are explicitly safe.

---

## 12. Implementation Order

1. **`guard/types.go`** — Shared types: `Severity`, `Confidence`, `Decision`,
   `Assessment`, `Match`, `Warning`, `WarningCode`, `Result`. These are
   the leaf dependency for all internal packages.

2. **`internal/packs/pack.go`** — Pack, SafePattern, DestructivePattern types.
   Depends on `guard/types.go` for Severity/Confidence.

3. **`internal/packs/matcher.go`** — `CommandMatcher` interface and all
   built-in matchers + builder functions. Depends on `internal/parse` for
   `ExtractedCommand`. Write matcher tests.

4. **`internal/packs/registry.go`** — Registry with Register, Get, All,
   Keywords, PacksForKeyword, freeze semantics. Write registry tests.

5. **`internal/packs/core/git.go`** — Test pack with 2 safe + 5 destructive
   patterns. Validates the pack → registry → matcher chain. Write pack tests.

6. **`guard/policy.go`** — Policy interface + three built-in policies.
   Write policy decision matrix test.

7. **`internal/envdetect/detect.go`** — Environment detection with all
   three env var categories. Write env detection tests.

8. **`internal/eval/allowlist.go`** — Glob matching for allowlist/blocklist.
   Write glob matching tests.

9. **`internal/eval/prefilter.go`** — Aho-Corasick pre-filter. Select and
   integrate pure-Go AC library. Write pre-filter tests.

10. **`internal/eval/pipeline.go`** — Pipeline orchestration connecting
    all components. This is the integration point. Write pipeline integration
    tests including env escalation, allowlist/blocklist precedence, partial
    parse handling.

11. **Golden file infrastructure** — Test runner, format definition, initial
    seed corpus. Write golden file tests that exercise the full pipeline.

Each step should have passing tests before proceeding. Steps 1–5 can
be implemented together as they form a tight unit. Steps 6–8 are
independent of each other and can be parallelized. Steps 9–11 depend
on all prior steps.

---

## 13. Open Questions

1. **Aho-Corasick library selection**: Which pure-Go library to use?
   Candidates: `github.com/cloudflare/ahocorasick`,
   `github.com/petar-dambovaliev/aho-corasick`. Decision should be made
   based on API fit, maintenance status, and benchmark performance.
   Fallback: implement textbook Aho-Corasick (~200 LOC for 50 patterns).

2. ~~**Warning code sharing mechanism**~~: **Resolved**. `parse` imports
   `guard/types.go` directly. `ParseResult.Warnings` uses `guard.Warning`
   type. `convertWarnings` is eliminated. See §5.5.

3. ~~**`globMatch` edge case — trailing `*`**~~: **Resolved** (MF-P1.1).
   `"git status *"` does NOT match `"git status"` (the space before `*`
   must match, so at least one character after the space is required).
   This is documented in E3 test cases. Users wanting to match with or
   without arguments should use `"git status*"` (no space before `*`).
   Additionally, `*` now does NOT match command separators (`;`, `|`, `&`,
   `\n`, `` ` ``, `$(`, `)`) per MF-P0.1 security fix.

4. **Pre-filter keyword overlap**: If a keyword like `"rm"` appears in
   multiple packs, the pre-filter correctly returns all packs containing
   that keyword. But short keywords increase false pass-through rate.
   Should we enforce a minimum keyword length? Recommendation: No — let
   benchmark data guide keyword quality improvements in Batch 5.

---

## Round 1 Review Disposition

| Finding | Reviewer | Severity | Summary | Disposition | Notes |
|---------|----------|----------|---------|-------------|-------|
| MF-P0.1 | security-correctness | P0 | Allowlist `*` matches command separators — compound injection bypass | Incorporated | §5.7: `*` restricted from matching `;`, `\|`, `&`, `\n`, `` ` ``, `$`, `(`, `)`. E3/SEC2 updated. |
| MF-P0.2 | security-correctness | P0 | Blocklist deny has Assessment but empty Matches | Incorporated | §5.5: Blocklist adds synthetic Match with pack `_blocklist`. |
| MF-P1.1 | security-correctness | P1 | `globMatch` trailing `*` semantics unclear | Incorporated | §13 OQ3 resolved. Documented in E3: `"git status *"` requires char after space. |
| MF-P1.2 | security-correctness | P1 | Env detection global, not per-command | Incorporated | §5.5/§5.8: Split into `DetectInline` (per-cmd) + `DetectGlobal` (once). |
| MF-P1.3 | security-correctness | P1 | Safe pattern `git-push-no-force` too broad | Incorporated | §5.9: Added `--mirror`/`--delete` to forbidden flags + 2 new destructive patterns. |
| MF-P1.4 | security-correctness | P1 | AC substring matching causes pass-through | Incorporated | §5.4: Word-boundary post-filter added. Prefilter tests updated. |
| MF-P2.1 | security-correctness | P2 | `WarnExpansionCapped` ignored with other matches | Incorporated | §5.5: Indeterminate used as floor when expansion capped. |
| MF-P2.2 | security-correctness | P2 | `extractHostname` fails with `@` in paths | Incorporated | §5.8: Replaced with `net/url.Parse()` + fallback. |
| MF-P2.3 | security-correctness | P2 | Subset automaton cache unbounded | Incorporated | §5.4: Subset cache removed entirely; single automaton + downstream pack filtering. |
| MF-P2.4 | security-correctness | P2 | `resolveEnabledPacks` empty slice vs nil | Incorporated | §5.5: Short-circuit to Allow when enabledPacks is empty. |
| MF-P2.5 | security-correctness | P2 | `--force`/`--force-with-lease` coexistence test | Incorporated | §8: Cross-plan flag coexistence test added. |
| MF-P2.6 | security-correctness | P2 | Confidence unused by built-in policies | Incorporated | §5.6: Documented as intentionally unused; available for custom policies. |
| MF-P3.1 | security-correctness | P3 | Golden file no schema validation | Incorporated | §5.10: Parser validates required fields, enum values, warns on unknown keys. |
| MF-P3.2 | security-correctness | P3 | No test for disabled pack behavior | Incorporated | §8: `TestPipelineDisabledPacks` added with 4 cases. |
| MF-P3.3 | security-correctness | P3 | `isEmptyOrWhitespace` misses unicode | Incorporated | §5.5: Uses `strings.TrimSpace` instead of manual char check. |
| MF-P3.4 | security-correctness | P3 | `ArgMatcher` `path.Match` vs `globMatch` inconsistency | Incorporated | §5.2.3: `ArgMatcher` now uses `globMatch` instead of `path.Match`. |
| SE-02-P0.1 | systems-engineer | P0 | Nil policy dereference in Pipeline.Run() | Incorporated | §5.5: Nil policy defaults to `InteractivePolicy()`. |
| SE-02-P0.2 | systems-engineer | P0 | ParseResult doesn't expose exported vars | Incorporated | §1: `ExportedVars map[string][]string` added to ParseResult. Cross-plan change documented. |
| SE-02-P1.1 | systems-engineer | P1 | Blocklist deny produces opaque decision | Incorporated | Merged with MF-P0.2. Synthetic Match entry added. |
| SE-02-P1.2 | systems-engineer | P1 | ExtractedCommand diverges between plan 01 and 02 | Incorporated | §1: Cross-plan interface note added. Plan 01 owns canonical definition. |
| SE-02-P1.3 | systems-engineer | P1 | Global env escalation — all matches escalated | Incorporated | Merged with MF-P1.2. Split into per-command + global detection. |
| SE-02-P1.4 | systems-engineer | P1 | `convertWarnings` contradicts import redesign | Incorporated | §5.5: `convertWarnings` removed. `ParseResult` uses `guard.Warning` directly. |
| SE-02-P1.5 | systems-engineer | P1 | `resolveEnabledPacks` builds unnecessary subset automatons | Incorporated | §5.4/§5.5: Subset cache removed. Single automaton + downstream filtering. |
| SE-02-P2.1 | systems-engineer | P2 | AC substring matching FPs | Incorporated | Merged with MF-P1.4. Word-boundary post-filter added. |
| SE-02-P2.2 | systems-engineer | P2 | `disabledPacks` typos silently ignored | Incorporated | §5.5: `validatePackConfig` emits `WarnUnknownPackID` for unknown IDs. |
| SE-02-P2.3 | systems-engineer | P2 | `globMatch` empty pattern / `"*"` blocklist edge cases | Incorporated | §5.7: Documented `"*"` blocklist behavior. E3 updated with test case. |
| SE-02-P2.4 | systems-engineer | P2 | Pack copy in Register is shallow | Incorporated | §5.3: Deep copy of Keywords, Safe, Destructive slices in Register(). |
| SE-02-P2.5 | systems-engineer | P2 | `processEnv` parsed every call | Not Incorporated | Documented in §5.8 as acceptable for expected call frequency. Caching deferred to implementation profiling. |
| SE-02-P2.6 | systems-engineer | P2 | P8 monotonicity test incomplete | Incorporated | Test harness P8: Full ordering check with explicit restrictiveness map. |
| SE-02-P3.1 | systems-engineer | P3 | `PacksForKeyword` O(packs × keywords) | Incorporated | §5.3: Reverse index `keywordIndex map[string][]*Pack` built on freeze(). O(1) lookup. |
| SE-02-P3.2 | systems-engineer | P3 | No test for ArgMatcher invalid glob | Incorporated | §8: `TestArgMatcherInvalidGlob` added. |
| SE-02-P3.3 | systems-engineer | P3 | Golden file no versioning | Incorporated | §5.10: `format: v1` header line added. Parser validates on load. |
| SE-02-P3.4 | systems-engineer | P3 | SEC2 documents limitation without resolution | Incorporated | Test harness SEC2: Concrete expectations added. Bypass vectors now blocked by MF-P0.1 fix. |
| SE-02-P3.5 | systems-engineer | P3 | `isEmptyOrWhitespace` unicode whitespace | Incorporated | Merged with MF-P3.3. Uses `strings.TrimSpace`. |

## Round 2 Review Disposition

| # | Reviewer | Severity | Summary | Disposition | Notes |
|---|----------|----------|---------|-------------|-------|
| 1 | dcg-coder-1 | P1 | Matcher set lacks RawArgs content matcher despite foundation need | Incorporated | Added `RawArgContentMatcher` plus `RawArgContent` / `RawArgContentRegex` builders to matcher DSL (§5.2.4a, §5.2.9). |
| 2 | dcg-coder-1 | P2 | ArgContent API leaves regex-literal footgun unaddressed | Incorporated | Added explicit anti-footgun rule: regex-like literals must use `ArgContentRegex`, with test-harness enforcement note. |

## Round 3 Review Disposition

No new findings.
