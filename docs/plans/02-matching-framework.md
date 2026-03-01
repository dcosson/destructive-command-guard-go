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
10. **Test pack** — Minimal `core.git` with 2–3 patterns to validate
    framework end-to-end (`internal/packs/core/git.go`)

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
    Commands []ExtractedCommand
    Warnings []Warning
    HasError bool
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
    // Try glob first; if pattern is invalid glob, fall back to exact
    matched, err := path.Match(m.Pattern, arg)
    if err != nil {
        return arg == m.Pattern
    }
    return matched
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

#### 5.2.8 Builder Functions

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
```

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
    mu       sync.RWMutex
    packs    map[string]*Pack // ID → Pack
    allPacks []*Pack          // Ordered by registration
    keywords []string         // Deduplicated aggregate of all pack keywords
    frozen   bool             // True after first read — no more registration
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
    cp := p // Copy to prevent mutation
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
// This is used by the pre-filter to select candidate packs after
// keyword matching.
func (r *Registry) PacksForKeyword(keyword string) []*Pack {
    r.freeze()
    r.mu.RLock()
    defer r.mu.RUnlock()
    var result []*Pack
    for _, p := range r.allPacks {
        for _, kw := range p.Keywords {
            if kw == keyword {
                result = append(result, p)
                break
            }
        }
    }
    return result
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

func (r *Registry) freeze() {
    r.mu.Lock()
    r.frozen = true
    r.mu.Unlock()
}
```

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
type KeywordPreFilter struct {
    registry  *packs.Registry
    defaultAC *ahoCorasick         // Automaton for all packs
    cache     map[string]*ahoCorasick // packSetKey → automaton for subset
    mu        sync.RWMutex           // Protects cache
}

// NewKeywordPreFilter creates a pre-filter backed by the given registry.
// The default automaton (all packs) is built eagerly.
func NewKeywordPreFilter(registry *packs.Registry) *KeywordPreFilter {
    kf := &KeywordPreFilter{
        registry: registry,
        cache:    make(map[string]*ahoCorasick),
    }
    kf.defaultAC = newAhoCorasick(registry.Keywords())
    return kf
}

// MatchResult contains the keywords matched and the candidate packs.
type MatchResult struct {
    Matched  bool     // true if any keyword matched
    Keywords []string // which keywords matched
}

// Contains checks if the command string contains any pack keyword.
// Returns the matched keywords (used to select candidate packs).
//
// enabledPacks is nil for "all packs" (uses the default automaton).
// When non-nil, a subset automaton is used (cached by pack set).
func (kf *KeywordPreFilter) Contains(command string, enabledPacks []string) MatchResult {
    ac := kf.getAutomaton(enabledPacks)
    matches := ac.FindAll(command)
    if len(matches) == 0 {
        return MatchResult{}
    }
    return MatchResult{
        Matched:  true,
        Keywords: matches,
    }
}

// getAutomaton returns the automaton for the given pack set.
// nil enabledPacks means "all packs" (default automaton).
func (kf *KeywordPreFilter) getAutomaton(enabledPacks []string) *ahoCorasick {
    if enabledPacks == nil {
        return kf.defaultAC
    }

    key := packSetKey(enabledPacks)

    kf.mu.RLock()
    if ac, ok := kf.cache[key]; ok {
        kf.mu.RUnlock()
        return ac
    }
    kf.mu.RUnlock()

    // Build subset automaton
    selected := kf.registry.PacksByID(enabledPacks)
    var keywords []string
    seen := make(map[string]bool)
    for _, p := range selected {
        for _, kw := range p.Keywords {
            if !seen[kw] {
                seen[kw] = true
                keywords = append(keywords, kw)
            }
        }
    }
    ac := newAhoCorasick(keywords)

    kf.mu.Lock()
    kf.cache[key] = ac
    kf.mu.Unlock()

    return ac
}

// packSetKey produces a canonical string key for a set of pack IDs.
func packSetKey(packs []string) string {
    sorted := make([]string, len(packs))
    copy(sorted, packs)
    sort.Strings(sorted)
    return strings.Join(sorted, ",")
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

// newAhoCorasick builds an automaton from the given patterns.
// Patterns are matched case-insensitively (keywords like "git" should
// match "GIT" in env var values — but command names are already
// normalized to lowercase by the extractor). Actually, keep matching
// case-sensitive since keywords match raw command text which preserves
// case. Bash commands are case-sensitive.
func newAhoCorasick(patterns []string) *ahoCorasick {
    // Build automaton from patterns
    ...
}

// FindAll returns all patterns found in the text.
// Returns deduplicated pattern strings.
func (ac *ahoCorasick) FindAll(text string) []string {
    // Run automaton on text, collect matched patterns
    ...
}
```

**Case sensitivity**: The pre-filter matches against the raw command string
(before parsing). Bash command names are case-sensitive (`Git` ≠ `git`), but
in practice LLMs generate lowercase command names. We match case-sensitively
to avoid false keyword matches on prose/comments in the command string.

**Keyword quality**: Keywords should be specific enough to avoid excessive
pass-through to parsing but general enough to catch all variants:
- Good: `"git"`, `"terraform"`, `"kubectl"` — specific tool names
- Bad: `"rm"` alone — too many false positives (`rm` appears in comments, strings)
- Acceptable: `"rm"` — it IS the command name; even if it appears in strings,
  tree-sitter parsing handles the disambiguation. The pre-filter is a
  performance optimization, not a correctness gate. False positives at the
  pre-filter level just mean unnecessary parsing, not false detections.

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
        result.Decision = cfg.policy.Decide(*result.Assessment)
        return result
    }

    // Step 2: Blocklist/Allowlist
    if matchesGlob(command, cfg.blocklist) {
        result.Decision = guard.Deny
        result.Assessment = &guard.Assessment{
            Severity:   guard.Critical,
            Confidence: guard.ConfidenceHigh,
        }
        return result
    }
    if matchesGlob(command, cfg.allowlist) {
        result.Decision = guard.Allow
        return result
    }

    // Step 3: Keyword pre-filter
    enabledPacks := p.resolveEnabledPacks(cfg)
    filterResult := p.prefilter.Contains(command, enabledPacks)
    if !filterResult.Matched {
        result.Decision = guard.Allow
        return result
    }

    // Steps 4-6: Parse + Extract + Normalize (handled by ParseAndExtract)
    parseResult := p.parser.ParseAndExtract(ctx, command, 0)
    result.Warnings = append(result.Warnings, convertWarnings(parseResult.Warnings)...)

    // Handle parse failure
    if len(parseResult.Commands) == 0 && parseResult.HasError {
        result.Assessment = &guard.Assessment{
            Severity:   guard.Indeterminate,
            Confidence: guard.ConfidenceHigh,
        }
        result.Decision = cfg.policy.Decide(*result.Assessment)
        return result
    }

    // Step 9: Environment detection
    envResult := p.envDet.Detect(
        collectInlineEnv(parseResult.Commands),
        collectExportedVars(parseResult),
        cfg.callerEnv,
    )

    // Step 10: Pattern matching
    candidatePacks := p.selectCandidatePacks(filterResult.Keywords, enabledPacks)
    var allMatches []guard.Match

    for _, cmd := range parseResult.Commands {
        matches := p.matchCommand(cmd, candidatePacks, envResult)
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

    // Handle expansion cap → Indeterminate (if not already more severe)
    if hasWarning(result.Warnings, guard.WarnExpansionCapped) && result.Assessment == nil {
        result.Assessment = &guard.Assessment{
            Severity:   guard.Indeterminate,
            Confidence: guard.ConfidenceHigh,
        }
    }

    // Step 12: Policy application
    if result.Assessment != nil {
        result.Decision = cfg.policy.Decide(*result.Assessment)
    } else {
        result.Decision = guard.Allow
    }

    return result
}

// matchCommand matches a single extracted command against candidate packs.
// Safe-before-destructive: if a safe pattern matches in a pack, destructive
// patterns in that pack are skipped for this command.
func (p *Pipeline) matchCommand(
    cmd parse.ExtractedCommand,
    candidatePacks []*packs.Pack,
    envResult envdetect.Result,
) []guard.Match {
    var matches []guard.Match

    for _, pack := range candidatePacks {
        // Check safe patterns first
        safeMatched := false
        for _, sp := range pack.Safe {
            if sp.Match.Match(cmd) {
                safeMatched = true
                break
            }
        }
        if safeMatched {
            continue // Skip destructive patterns for this command in this pack
        }

        // Check destructive patterns
        for _, dp := range pack.Destructive {
            if dp.Match.Match(cmd) {
                sev := dp.Severity
                escalated := false
                // Environment escalation
                if dp.EnvSensitive && envResult.IsProduction {
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
    for _, c := range s {
        if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
            return false
        }
    }
    return true
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

func convertWarnings(parseWarnings []parse.Warning) []guard.Warning {
    result := make([]guard.Warning, len(parseWarnings))
    for i, w := range parseWarnings {
        result[i] = guard.Warning{
            Code:    guard.WarningCode(w.Code),
            Message: w.Message,
        }
    }
    return result
}
```

**Warning code alignment**: `parse.WarningCode` and `guard.WarningCode` must
have the same underlying values. During plan 02 implementation, we should
either: (a) have `parse` import `guard` for the warning codes (adds a
dependency edge), or (b) maintain the same iota ordering in both and convert
via type cast. Option (b) is simpler but fragile. Option (a) is preferred
— `parse` importing `guard` for types only (no `internal/eval` → no cycle).

**Revised import flow with parse → guard for types**:

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

This means `guard/types.go` has zero internal imports and is the leaf
dependency for shared types. This is clean. `parse.Warning` should be
replaced with `guard.Warning` directly, and `parse.WarningCode` with
`guard.WarningCode`. `parse.ParseResult` then uses `guard.Warning`:

```go
// In internal/parse/command.go
type ParseResult struct {
    Commands []ExtractedCommand
    Warnings []guard.Warning  // Shared warning type
    HasError bool
}
```

This eliminates the warning conversion entirely — `convertWarnings` becomes
unnecessary.

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

### 5.7 Allowlist/Blocklist Matching (`internal/eval/allowlist.go`)

```go
package eval

import "path"

// matchesGlob checks if the command matches any of the given glob patterns.
// Uses path.Match semantics — `*` matches any sequence of non-separator chars.
// Since we're matching command strings (not file paths), we treat the
// entire command as a flat string. `*` matches everything including spaces.
//
// However, path.Match's `*` does NOT match `/`. For command matching,
// we need `*` to match everything. We use a custom matcher that treats
// the pattern like a filepath match but normalizes separators.
//
// Actually: path.Match's `*` matches any non-`/` character. For our
// use case, we need different semantics. We implement simple glob matching
// where `*` matches any sequence of characters (including `/` and spaces).
func matchesGlob(command string, patterns []string) bool {
    for _, pattern := range patterns {
        if globMatch(pattern, command) {
            return true
        }
    }
    return false
}

// globMatch implements simple glob matching where `*` matches any
// sequence of characters (including `/` and space).
// Only `*` is special. No `?`, `[...]`, or `**` support.
// This is intentionally simple — complex patterns should use pack
// matchers instead.
func globMatch(pattern, text string) bool {
    // Standard two-pointer glob algorithm.
    // px = pattern index, tx = text index
    // starPx, starTx track the last `*` position for backtracking.
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
            starTx++
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

// Note: path.Match is NOT used because its `*` doesn't match `/`.
// Our glob semantics are simpler and match the architecture spec:
//   "git status"      — exact match
//   "git status *"    — matches git status with any args
//   "*/bin/git *"     — matches path-prefixed git with any args
var _ = path.Match // suppress unused import if path is imported elsewhere
```

### 5.8 Environment Detection (`internal/envdetect/detect.go`)

```go
package envdetect

import (
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

// Detect checks inline env vars, exported vars, and process env for
// production indicators.
//
// inlineEnv: merged InlineEnv maps from all extracted commands.
// exportedVars: variables exported via `export` (from dataflow analysis).
// processEnv: caller-provided env vars in os.Environ() format ("KEY=VALUE").
func (d *Detector) Detect(
    inlineEnv map[string]string,
    exportedVars map[string][]string,
    processEnv []string,
) Result {
    var indicators []ProductionIndicator

    // Check inline env vars
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

// extractHostname extracts the hostname from a URL-like string.
// Handles: "postgres://user:pass@prod-db.acme.com:5432/mydb"
// Returns: "prod-db.acme.com"
func extractHostname(url string) string {
    // Strip scheme
    if idx := strings.Index(url, "://"); idx >= 0 {
        url = url[idx+3:]
    }
    // Strip userinfo
    if idx := strings.LastIndex(url, "@"); idx >= 0 {
        url = url[idx+1:]
    }
    // Strip port and path
    if idx := strings.IndexAny(url, ":/"); idx >= 0 {
        url = url[:idx]
    }
    return url
}

// parseEnv parses os.Environ()-format env vars into a map.
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

**Detection priority**: All three sources (inline, export, process) are
checked. The first match is sufficient to set `IsProduction = true`. We
collect all indicators for diagnostic purposes (the Match's `EnvEscalated`
flag tells the caller why severity was bumped).

**`extractHostname` edge cases**:
- No scheme: `"prod-db.acme.com:5432"` → `"prod-db.acme.com"` (still works)
- IPv4: `"192.168.1.1"` → `"192.168.1.1"` (no "prod" match)
- IPv6: `"[::1]"` → handled by `IndexAny(":/")`
- Empty string: → `""` (no match)

### 5.9 Test Pack: `core.git` (`internal/packs/core/git.go`)

A minimal pack to validate the entire framework end-to-end. Contains 2
safe patterns and 3 destructive patterns.

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
            Match: packs.And(
                packs.Name("git"),
                packs.ArgAt(0, "push"),
                packs.Not(packs.Or(
                    packs.Flags("--force"),
                    packs.Flags("-f"),
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
    },
}
```

### 5.10 Golden File Infrastructure (`internal/eval/testdata/golden/`)

Golden files provide regression testing for the full evaluation pipeline.
Each entry is a command with its expected outcome.

**File format** (one entry per block, separated by `---`):

```
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
        {"keyword substring", "gitignore", true}, // AC finds "git" in "gitignore"
    }
    ...
}

func TestPreFilterSubsetAutomaton(t *testing.T) {
    // Test with enabled pack subset
    // Verify caching works
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

**detect_test.go** — Environment detection:

```go
func TestDetectInlineEnv(t *testing.T) {
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

func TestDetectURLProd(t *testing.T) {
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
    }
    ...
}

func TestDetectProcessEnv(t *testing.T) {
    // Same checks but via processEnv (os.Environ format)
    ...
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

**Implementation choice**: Pure-Go library (no cgo). The automaton is
built once at init time and reused. For subset pack configurations, subset
automatons are cached by the set of enabled packs.

**Dynamic cache bounding**: The cache of subset automatons is unbounded
in this plan. In practice, callers use a small number of pack configurations
(typically 1–3). If cache size becomes a concern, an LRU eviction policy
can be added, but this is not expected to be necessary.

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

3. **Pre-filter automaton caching**: Subset automatons are cached by pack
   set key. Repeated calls with the same `WithPacks` configuration reuse
   the same automaton.

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

5. **`internal/packs/core/git.go`** — Test pack with 2 safe + 3 destructive
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

2. **Warning code sharing mechanism**: Should `internal/parse` import
   `guard` for warning types, or should warning codes be duplicated?
   Recommendation: `parse` imports `guard/types.go` — it has no internal
   dependencies and breaks no cycles. See §5.5 discussion.

3. **`globMatch` edge case — trailing `*`**: Should `"git status *"` match
   `"git status"` (zero characters after space)? Current implementation: No,
   `*` requires at least zero characters (matches empty). Actually the
   algorithm as written matches zero characters for `*`. Need to verify
   and decide if this is the desired behavior. The architecture spec
   example `"git status *"` implies "git status with any arguments" which
   suggests matching `"git status "` (trailing space) but not `"git status"`.
   The glob semantics should match the spec.

4. **Pre-filter keyword overlap**: If a keyword like `"rm"` appears in
   multiple packs, the pre-filter correctly returns all packs containing
   that keyword. But short keywords increase false pass-through rate.
   Should we enforce a minimum keyword length? Recommendation: No — let
   benchmark data guide keyword quality improvements in Batch 5.
