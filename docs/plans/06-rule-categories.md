# Rule Categories: Destructive vs Privacy — Design Doc

**Status**: Draft
**Author**: dcg-scheduler
**Date**: 2026-03-06

---

## 1. Problem Statement

Currently, all rules in dcg-go are treated as a single dimension of "destructive
risk." The pipeline produces one Assessment (severity + confidence), applies one
Policy, and returns one Decision. However, rules actually fall into two distinct
categories:

- **Destructive**: Causes data loss, service disruption, or irreversible changes
  (e.g., `rm -rf /`, `DROP TABLE`, `git push --force`, `terraform destroy`)
- **Privacy**: Exposes sensitive data, reads private information, or enables
  exfiltration (e.g., `cat ~/.ssh/id_rsa`, `security find-generic-password`,
  reading browser history, AppleScript sending messages)

Users want to configure these independently. A common desired mode is
**privacy-strict + destructive-permissive** — "I'm comfortable with destructive
commands (I know what I'm doing) but I want to be warned about anything touching
private data."

## 2. Data Analysis

An audit of all rules across all packs shows a clean separation. Counts are
derived from the per-pack breakdown below (the authoritative source); a CI
validation test (see §8) must enforce these counts against the live registry
to prevent drift.

| Category | Count | Examples |
|----------|-------|----------|
| Purely Destructive | 114 | rm -rf, DROP TABLE, git push --force, k8s delete, terraform destroy |
| Purely Privacy | 8 | SSH key access, keychain read, Messages DB access, personal file reads |
| Both | 18 | csrutil disable, osascript send-message, diskutil erase, nvram operations |

The "both" cases are concentrated in macOS system/communication packs where
commands can simultaneously weaken security protections AND expose/exfiltrate data.

### Per-Pack Breakdown

| Pack | Destructive | Privacy | Both |
|------|-------------|---------|------|
| core.git | 22 | 0 | 0 |
| core.filesystem | 13 | 0 | 0 |
| database.* (all 5) | 35 | 0 | 0 |
| infrastructure.* | 9 | 0 | 0 |
| cloud.* | 4 | 0 | 0 |
| kubernetes.* | 6 | 0 | 0 |
| containers.* | 2 | 0 | 0 |
| frameworks | 7 | 0 | 0 |
| remote.rsync | 1 | 0 | 0 |
| secrets.vault | 5 | 0 | 0 |
| platform.github | 3 | 0 | 0 |
| personal.files | 3 | 2 | 0 |
| personal.ssh | 1 | 1 | 0 |
| macos.privacy | 0 | 5 | 0 |
| macos.system | 2 | 0 | 10 |
| macos.communication | 1 | 0 | 8 |

The separation is extremely clean. Most packs are 100% one category.

### The "Both" Case

The bitmask model (`CategoryBoth = CategoryDestructive | CategoryPrivacy`)
handles rules that span both dimensions without information loss at the
category membership level — a "both" match enters both aggregation lanes
and is evaluated against both policies.

The remaining limitation is that each rule has a single severity/confidence
value, not per-dimension scoring. In practice, the ~18 "both" rules are nearly
all Critical severity (disabling SIP, erasing disks, sending messages via
AppleScript), so a single severity is sufficient — these commands are Critical
regardless of which dimension you evaluate them in.

## 3. High-Level Design

### 3.1 Architecture Overview

```mermaid
graph TD
    subgraph "Current Pipeline"
        A[Command] --> B[Parse]
        B --> C[Match Rules]
        C --> D[Aggregate Assessment]
        D --> E[Apply Policy]
        E --> F[Decision]
    end

    subgraph "New Pipeline"
        A2[Command] --> B2[Parse]
        B2 --> C2[Match Rules<br/>each match now carries Category]
        C2 --> D2[Partition by Category]
        D2 --> E2a[Aggregate<br/>Destructive Assessment]
        D2 --> E2b[Aggregate<br/>Privacy Assessment]
        E2a --> F2a[Apply Destructive Policy]
        E2b --> F2b[Apply Privacy Policy]
        F2a --> G2[Merge Decisions<br/>deny > ask > allow]
        F2b --> G2
        G2 --> H2[Final Decision]
    end
```

### 3.2 Decision Merge Semantics

When a command triggers matches in both categories, the decisions are merged:

1. **Deny** if either lane denies
2. **Ask** if either lane asks (and neither denies)
3. **Allow** only if both lanes allow

This is conservative — the strictest decision wins.

### 3.3 Explanation Precedence

When the final decision is driven by one lane but both lanes produced matches,
the reason/remediation text must come from the lane(s) that determined the
final decision:

1. If one lane **Deny** and the other **Allow** → report matches from the
   denying lane as the primary reason.
2. If both lanes produce the same decision → report the match with the highest
   severity across both lanes (tie-break: destructive first).
3. All matches from both lanes are always included in `Result.Matches` for
   full visibility — this precedence only affects the primary reason string
   in hook output (§4.9).

### 3.4 Result Reporting

The Result struct gains per-category assessments so callers can see why a
decision was made in each dimension. The single `Assessment` field is removed
— callers use `DestructiveAssessment` and `PrivacyAssessment` directly.

## 4. Detailed Design

### 4.1 New Types in `internal/evalcore`

```go
// RuleCategory identifies whether a rule guards against destructive operations,
// privacy violations, or both. Implemented as a bitmask.
type RuleCategory uint8

const (
    CategoryDestructive RuleCategory = 1 << iota  // 0b01
    CategoryPrivacy                                // 0b10
    CategoryBoth = CategoryDestructive | CategoryPrivacy  // 0b11
)

func (c RuleCategory) String() string {
    switch c {
    case CategoryDestructive:
        return "Destructive"
    case CategoryPrivacy:
        return "Privacy"
    case CategoryBoth:
        return "Both"
    default:
        return "Unknown"
    }
}

func (c RuleCategory) HasDestructive() bool { return c&CategoryDestructive != 0 }
func (c RuleCategory) HasPrivacy() bool     { return c&CategoryPrivacy != 0 }
```

### 4.2 Pack and Rule Struct Changes (`internal/packs`)

The `Pack.Destructive` field is renamed to `Pack.Rules` since it now holds
rules of all categories (destructive, privacy, and both). `Pack.Safe` stays
as-is — safe rules are exemption patterns that don't need categories.

```go
type Pack struct {
    ID              string
    Name            string
    Description     string
    Keywords        []string
    Safe            []Rule
    Rules           []Rule   // was Destructive
    HasEnvSensitive bool
}

type Rule struct {
    ID           string
    Category     evalcore.RuleCategory
    Severity     int
    Confidence   int
    Reason       string
    Remediation  string
    EnvSensitive bool
    Match        MatchFunc
}
```

All callers of `pack.Destructive` (pipeline.go, registry.go, guard.go, pack
definitions, tests) must be updated to `pack.Rules`. This supersedes plan
02's pipeline code which references `pack.Destructive` (see §5).

Rules with `Category == 0` (unset) are normalized to `CategoryDestructive` at
match construction time. See §4.11 for the mandatory normalization point that
prevents fail-open bugs.

### 4.3 Match Struct Changes (`internal/evalcore`)

```go
type Match struct {
    Pack         string
    Rule         string
    Category     RuleCategory
    Severity     Severity
    Confidence   Confidence
    Reason       string
    Remediation  string
    EnvEscalated bool
}
```

### 4.4 Result Struct Changes (`internal/evalcore`)

```go
type Result struct {
    Decision              Decision
    DestructiveAssessment *Assessment       // nil if no destructive matches
    PrivacyAssessment     *Assessment       // nil if no privacy matches
    Matches               []Match
    Warnings              []Warning
    Command               string
}
```

### 4.5 Policy Configuration Changes

#### `internal/evalcore` — PolicyConfig (NEW)

```go
// PolicyConfig holds separate policies for each rule category.
type PolicyConfig struct {
    DestructivePolicy Policy
    PrivacyPolicy     Policy
}

// Decide applies the appropriate policy for each category assessment,
// then merges decisions (deny > ask > allow).
func (pc PolicyConfig) Decide(destructive, privacy *Assessment) Decision {
    dDec := Allow
    pDec := Allow
    if destructive != nil {
        dDec = pc.DestructivePolicy.Decide(*destructive)
    }
    if privacy != nil {
        pDec = pc.PrivacyPolicy.Decide(*privacy)
    }
    // Merge: deny > ask > allow
    if dDec == Deny || pDec == Deny {
        return Deny
    }
    if dDec == Ask || pDec == Ask {
        return Ask
    }
    return Allow
}
```

#### `internal/eval` — Config Changes

```go
type Config struct {
    DestructivePolicy  Policy
    PrivacyPolicy      Policy
    Allowlist          []string
    Blocklist          []string
    EnabledPacks       []string
    DisabledPacks      []string
    CallerEnv          []string
}
```

Both `DestructivePolicy` and `PrivacyPolicy` must be non-nil at evaluation
time. The `guard` package `defaultConfig()` sets both to `InteractivePolicy()`
so bare `Evaluate()` calls work without explicit policy options.

### 4.6 Pipeline Changes (`internal/eval`)

The `aggregate` function becomes category-aware:

```go
func aggregateByCategory(matches []Match) (destructive, privacy *Assessment) {
    var dMatches, pMatches []Match
    for _, m := range matches {
        if m.Category.HasDestructive() {
            dMatches = append(dMatches, m)
        }
        if m.Category.HasPrivacy() {
            pMatches = append(pMatches, m)
        }
    }
    if len(dMatches) > 0 {
        a := aggregate(dMatches)
        destructive = &a
    }
    if len(pMatches) > 0 {
        a := aggregate(pMatches)
        privacy = &a
    }
    return
}
```

In `Pipeline.Run()`, after matching:

```go
dAgg, pAgg := aggregateByCategory(result.Matches)
result.DestructiveAssessment = dAgg
result.PrivacyAssessment = pAgg

pc := PolicyConfig{
    DestructivePolicy: cfg.DestructivePolicy,
    PrivacyPolicy:     cfg.PrivacyPolicy,
}
result.Decision = pc.Decide(dAgg, pAgg)
```

### 4.7 Public API Changes (`guard` package)

The old `WithPolicy` option is replaced by two separate options:

```go
func WithDestructivePolicy(p Policy) Option {
    return func(c *evalConfig) { c.destructivePolicy = p }
}

func WithPrivacyPolicy(p Policy) Option {
    return func(c *evalConfig) { c.privacyPolicy = p }
}
```

The old `WithPolicy` is removed. `defaultConfig()` sets both to
`InteractivePolicy()`, so callers that don't pass either option get the same
default behavior as today.

### 4.8 CLI / Config Changes

The YAML config uses per-category policy fields:

```yaml
destructive_policy: permissive
privacy_policy: strict
```

The old single `policy` field is removed. Both fields are required in the
config. The `parsePolicy` function already exists and handles string-to-Policy
conversion.

The `dcg-go test` command gains `--destructive-policy` and `--privacy-policy`
flags. A bare `--policy X` is kept as shorthand that sets both to X:

```
dcg-go test --policy strict "rm -rf /"
dcg-go test --destructive-policy permissive --privacy-policy strict "cat ~/.ssh/id_rsa"
```

Test mode output includes the category prefix on each match (same format as
hook output in §4.9).

### 4.9 Hook Output Changes

The `buildReason` function should include the rule category in the reason string
so the caller knows why a command was flagged. When both lanes produce matches,
the primary reason is selected according to the explanation precedence rules
in §3.3 (the lane that determined the final decision takes priority):

```
"[privacy] SSH private key access detected. Suggestion: use ssh-add instead"
"[destructive] Force push overwrites remote history. Suggestion: use --force-with-lease"
```

When a "both" rule triggers and both lanes contribute to the decision, the
category prefix should be `[destructive+privacy]`.

**Hook JSON output**: The Claude Code hook protocol only consumes
`permissionDecision` and `permissionDecisionReason` — there is no mechanism
to pass structured category metadata. The `[category]` prefix in the reason
string is sufficient for now. If the hook protocol is extended in the future,
per-category assessments could be added to the JSON output.

### 4.10 CLI: Replace `packs` Command with `list` Command

The existing `dcg-go packs` command is replaced by `dcg-go list` with two
subcommands:

#### `dcg-go list packs`

Lists all registered packs with per-category rule counts:

```
core.git              Git Operations
                      22 destructive, 0 privacy

macos.system          macOS System Configuration
                      2 destructive, 0 privacy, 10 both

macos.privacy         macOS Privacy
                      0 destructive, 5 privacy
```

Supports `--json` for machine-readable output.

#### `dcg-go list rules`

Lists all individual rules, grouped by category (destructive, privacy, both),
with pack name in parentheses:

```
Destructive:
  force-push (core.git)              Force push overwrites remote history
  rm-recursive-force (core.filesystem)  Recursive force delete
  drop-table (database.sql)          DROP TABLE removes data permanently
  ...

Privacy:
  ssh-private-key-access (personal.ssh)  SSH private key read
  keychain-read-password (macos.privacy) Keychain password extraction
  ...

Both:
  csrutil-disable (macos.system)     Disables System Integrity Protection
  osascript-send-message (macos.communication)  Send messages via AppleScript
  ...
```

Supports `--json` for machine-readable output.

#### Guard Package Changes

The `PackInfo` struct replaces `SafeCount`/`DestrCount` with per-category
counts:

```go
type PackInfo struct {
    ID               string
    Name             string
    Description      string
    Keywords         []string
    DestructiveCount int
    PrivacyCount     int
    BothCount        int
    HasEnvSensitive  bool
}
```

A new `guard.Rules()` function is added returning `[]RuleInfo`:

```go
type RuleInfo struct {
    ID          string
    PackID      string
    Category    RuleCategory
    Severity    Severity
    Reason      string
    Remediation string
}

func Rules() []RuleInfo
```

### 4.11 Match Propagation and Category Normalization

**Mandatory normalization**: When constructing Match objects from rule matches,
a zero `Category` **must** be normalized to `CategoryDestructive` before the
match enters the pipeline. This is the single normalization point — without it,
a zero-category match would pass through `HasDestructive()` and `HasPrivacy()`
returning false, enter neither aggregation lane, and resolve to `Allow` despite
a matched rule (fail-open).

```go
cat := evalcore.RuleCategory(rule.Category)
if cat == 0 {
    cat = evalcore.CategoryDestructive
}
result.Matches = append(result.Matches, Match{
    Pack:         pack.ID,
    Rule:         rule.ID,
    Category:     cat,
    Severity:     Severity(sev),
    Confidence:   Confidence(rule.Confidence),
    Reason:       rule.Reason,
    Remediation:  rule.Remediation,
    EnvEscalated: envEscalated,
})
```

A regression test must verify that a rule with `Category == 0` is treated as
`CategoryDestructive` and does not bypass both aggregation lanes (see §8).

**Note on blocklist/allowlist**: These short-circuit before category
aggregation — blocklist always returns Deny, allowlist always returns Allow.
Their synthetic Match objects get `CategoryDestructive` via normalization, but
this is cosmetic since the dual-policy pipeline is never reached.

## 5. Cross-Document Updates

This plan changes types and interfaces defined in earlier plan docs. These
docs are now stale for the affected types and should be updated during
implementation:

- **00-architecture.md**: `Result` struct (remove `Assessment`, add
  `DestructiveAssessment`/`PrivacyAssessment`), `Policy` usage note,
  component diagram (add category partition step), `WithPolicy` → dual
  options, `Match` struct (add `Category`)
- **02-matching-framework.md**: Pipeline steps 11-12 (`policy.Decide` call
  sites become `PolicyConfig.Decide`), `Result` type definition, `Config`
  struct, `pack.Destructive` → `pack.Rules` in pipeline code
- **04-api-and-cli.md**: `guard.Result` definition, `guard.WithPolicy` →
  dual options, test mode output (add category prefix), packs mode → list
  command, config YAML schema

The earlier docs remain correct for everything not listed here.

## 6. Implementation Plan

### 6.1 Rule Tagging

Every existing rule gets a `Category` field. Based on the audit:

1. **Default to `CategoryDestructive`** — this covers the majority of rules
   correctly with zero changes.
2. **Explicitly tag privacy rules** — rules across personal.files,
   personal.ssh, macos.privacy packs (see Appendix).
3. **Explicitly tag "both" rules** — rules in macos.system,
   macos.communication packs (see Appendix).

A validation test ensures every rule has a non-zero category.

### 6.2 Golden File Updates

Golden files include match data. The Match struct gains a Category field, so
golden files will need regeneration. This is mechanical — run the golden update
tool.

## 7. Package / Import Structure

No new packages needed. Changes are additive within existing packages:

```
internal/evalcore/    ← RuleCategory type, PolicyConfig, Match/Result with Category
internal/packs/       ← Pack.Rules rename, Rule.Category field
internal/eval/        ← aggregateByCategory, Pipeline.Run, Config with dual policies
guard/                ← WithDestructivePolicy, WithPrivacyPolicy options
cmd/dcg-go/           ← Config with dual policies, buildReason with category prefix
```

Import flow is unchanged — no new cycles.

## 8. Testing Strategy

### Unit Tests

- `internal/evalcore`: RuleCategory bitmask operations, PolicyConfig.Decide
  with all combinations (destructive-only, privacy-only, both, neither),
  decision merge semantics, explanation precedence (§3.3).
- `internal/eval`: `aggregateByCategory` with mixed-category matches. Pipeline
  tests with per-category policies producing different decisions. **Zero-category
  regression test**: a rule with `Category == 0` must be normalized to
  `CategoryDestructive` and must not bypass both aggregation lanes (P0 fix).
- `guard`: Option tests for `WithDestructivePolicy`, `WithPrivacyPolicy`.
- `cmd/dcg-go`: Config parsing with per-category policy fields. `buildReason`
  includes category prefix. Test conflicting-lane cases where the primary
  reason must come from the deciding lane.

### Integration Tests

- Full pipeline with `privacy-strict + destructive-permissive`:
  - `rm -rf /tmp/foo` → Allow (destructive-permissive allows medium)
  - `cat ~/.ssh/id_rsa` → Deny (privacy-strict denies medium+)
  - `osascript -e 'tell application "Messages" to send...'` → Deny (both
    categories, privacy-strict denies)
- Conflicting-lane explanation test: command triggers destructive-High (Ask)
  and privacy-Medium (Allow under permissive privacy). Verify the primary
  reason references the destructive lane.

### Validation Tests

- Registry validation: every rule has non-zero Category after normalization.
- **CI category-count validation**: a test that queries the live rule registry,
  counts rules per category, and asserts against an approved baseline. This
  prevents category drift when rules are added/removed. The baseline should be
  stored as a checked-in file (e.g., `testdata/category-baseline.json`) and
  updated explicitly when rule counts change intentionally.

### Golden File Updates

Regenerate all golden files after adding Category to Match. Verify diffs are
only the added field.

## 9. Sequence Diagram: Dual-Policy Evaluation

```mermaid
sequenceDiagram
    participant Caller
    participant Pipeline
    participant Matcher
    participant Aggregator
    participant PolicyEngine

    Caller->>Pipeline: Evaluate("cat ~/.ssh/id_rsa",<br/>destructive=permissive,<br/>privacy=strict)
    Pipeline->>Matcher: Match against all packs
    Matcher-->>Pipeline: Match{Rule: ssh-private-key-access,<br/>Category: Privacy, Severity: High}
    Pipeline->>Aggregator: aggregateByCategory(matches)
    Aggregator-->>Pipeline: destructive=nil, privacy={High, High}
    Pipeline->>PolicyEngine: Decide(nil, {High, High})
    Note over PolicyEngine: Destructive: no matches → Allow<br/>Privacy: strict → High → Deny
    PolicyEngine-->>Pipeline: Deny
    Pipeline-->>Caller: Result{Decision: Deny,<br/>PrivacyAssessment: {High, High}}
```

## 10. Future Directions

The bitmask approach naturally extends if we ever need categories beyond
destructive/privacy (e.g., "network", "system-config"). Not proposing this
now, but the design doesn't preclude it.

---

## Appendix: Rule Category Assignments

Rules requiring explicit non-default category tagging (all others default to
`CategoryDestructive`). **This list must be verified against the live registry
at implementation time** — the CI validation test (§8) is the authoritative
source of truth post-merge.

### CategoryPrivacy

| Pack | Rule ID |
|------|---------|
| personal.ssh | ssh-private-key-access |
| personal.files | personal-files-access |
| macos.privacy | keychain-read-password |
| macos.privacy | keychain-dump |
| macos.privacy | messages-db-access |
| macos.privacy | private-data-access |
| macos.privacy | spotlight-search |

### CategoryBoth

| Pack | Rule ID |
|------|---------|
| macos.system | csrutil-disable |
| macos.system | diskutil-erase |
| macos.system | launchctl-remove |
| macos.system | nvram-clear |
| macos.system | nvram-write |
| macos.system | nvram-delete |
| macos.system | spctl-disable |
| macos.system | dscl-delete |
| macos.system | fdesetup-disable |
| macos.system | systemsetup-modify |
| macos.communication | osascript-send-message |
| macos.communication | osascript-send-email |
| macos.communication | osascript-system-events |
| macos.communication | osascript-sensitive-app |
| macos.communication | shortcuts-run |
| macos.communication | automator-run |
| macos.communication | open-terminal |
| macos.communication | osascript-jxa-catchall |

## Round 1 Review Disposition

| # | Reviewer | Severity | Summary | Disposition | Notes |
|---|----------|----------|---------|-------------|-------|
| 1 | 06-rule-categories-review | P0 | Zero `Category` fail-open during migration | Incorporated | §4.2, §4.10 add mandatory normalization; §7 adds regression test |
| 2 | 06-rule-categories-review | P1 | Backward-compat claim overstated for struct literals / snapshots | Incorporated | No external consumers — removed all backward-compat language, legacy fields, and fallback logic entirely |
| 3 | 06-rule-categories-review | P1 | Inventory counts inconsistent with appendix | Incorporated | §2 counts fixed to match per-pack table; §7 adds CI baseline validation |
| 4 | 06-rule-categories-review | P2 | No winning-lane explanation semantics | Incorporated | §3.3 added explanation precedence rules; §4.9 updated |
| 5 | 06-rule-categories-review | P3 | "Both" info-loss narrative conflicts with bitmask model | Incorporated | §2 subsection rewritten to reflect bitmask model |

## Round 2 Review Disposition

| # | Reviewer | Severity | Summary | Disposition | Notes |
|---|----------|----------|---------|-------------|-------|
| 1 | tall-vale | P1 | `Pack.Destructive` naming misleading for privacy rules | Incorporated | §4.2 renames to `Pack.Rules`; `Pack.Safe` unchanged |
| 2 | tall-vale | P1 | Default policy behavior unspecified | Incorporated | §4.5 and §4.7 specify `InteractivePolicy()` defaults |
| 3 | tall-vale | P2 | Test mode `--policy` flag not addressed | Incorporated | §4.8 adds `--destructive-policy`/`--privacy-policy` flags and `--policy` shorthand |
| 4 | tall-vale | P2 | Open Questions §9 is stale | Incorporated | §9 resolved: Q1/Q2 answered by §4.8/§4.10, Q3 kept as future directions |
| 5 | tall-vale | P3 | Blocklist matches get implicit CategoryDestructive | Incorporated | §4.11 adds note that blocklist/allowlist bypass dual-policy pipeline |

## Seam Review Disposition

| # | Reviewer | Severity | Summary | Disposition | Notes |
|---|----------|----------|---------|-------------|-------|
| 1 | tall-vale | P1 | Architecture/plan 02/04 define `Result.Assessment` which plan 06 removes | Incorporated | §5 added cross-document update tracking list |
| 2 | tall-vale | P1 | Plan 02 pipeline code references `pack.Destructive` renamed to `pack.Rules` | Incorporated | §4.2 notes plan 02 is superseded; §5 tracks the update |
| 3 | tall-vale | P2 | Hook JSON output lacks structured category metadata | Incorporated | §4.9 documents hook protocol limitation and reason-string-only approach |
| 4 | tall-vale | P3 | `PackInfo` struct changes not fully specified | Incorporated | §4.10 now shows full `PackInfo` struct with per-category counts |
