# 02: Matching Framework — Test Harness

**Plan**: [02-matching-framework.md](./02-matching-framework.md)
**Architecture**: [00-architecture.md](./00-architecture.md)

---

## Overview

This document specifies the test harness for the matching framework (plan 02).
It covers property-based tests, fault injection, comparison oracles,
deterministic examples, benchmarks, stress tests, security tests, manual QA,
CI tier mapping, and exit criteria.

The test harness complements the unit and integration tests described in the
plan doc §8. Those tests verify correctness of individual components. This
harness verifies system-level properties, robustness under adversarial
conditions, and performance baselines.

---

## P: Property-Based Tests

### P1: Pipeline Never Panics

**Invariant**: For any byte sequence input, `Pipeline.Run()` returns a valid
`Result` without panicking.

```go
func TestPropertyPipelineNeverPanics(t *testing.T) {
    pipeline := setupTestPipeline(t)
    cfg := &evalConfig{policy: InteractivePolicy()}

    f := func(input []byte) bool {
        result := pipeline.Run(context.Background(), string(input), cfg)
        return result.Decision >= Allow && result.Decision <= Ask
    }
    if err := quick.Check(f, &quick.Config{MaxCount: 10000}); err != nil {
        t.Fatal(err)
    }
}
```

**Generator**: Random byte sequences of length 0–2048. Includes null bytes,
multi-byte UTF-8, and control characters.

### P2: Decision is Always Valid

**Invariant**: `Result.Decision` is always one of `{Allow, Deny, Ask}`.

```go
func TestPropertyDecisionValid(t *testing.T) {
    // For any random command string and any of the three policies:
    // Result.Decision ∈ {Allow, Deny, Ask}
}
```

### P3: Assessment Consistency

**Invariant**: If `Result.Assessment` is nil, `Result.Decision` is `Allow`.
If `Result.Matches` is non-empty, `Result.Assessment` is non-nil.

```go
func TestPropertyAssessmentConsistency(t *testing.T) {
    f := func(input string) bool {
        result := pipeline.Run(ctx, input, cfg)
        if result.Assessment == nil && result.Decision != Allow {
            return false // nil assessment must be Allow
        }
        if len(result.Matches) > 0 && result.Assessment == nil {
            return false // matches require assessment
        }
        return true
    }
    quick.Check(f, nil)
}
```

### P4: Blocklist Always Denies

**Invariant**: If a command matches a blocklist pattern, the decision is
always `Deny`, regardless of policy, allowlist, or pack configuration.

```go
func TestPropertyBlocklistAlwaysDenies(t *testing.T) {
    patterns := []string{"rm -rf *", "git push --force *"}
    // For each pattern, generate random commands matching the pattern
    // Verify Decision == Deny with all three policies
}
```

### P5: Allowlist Never Denied

**Invariant**: If a command matches an allowlist pattern and does NOT match
any blocklist pattern, the decision is always `Allow`, regardless of policy.

```go
func TestPropertyAllowlistNeverDenied(t *testing.T) {
    cfg := &evalConfig{
        policy:    StrictPolicy(),
        allowlist: []string{"git push *"},
        blocklist: []string{}, // No blocklist
    }
    // git push --force should be Allowed (allowlisted)
    result := pipeline.Run(ctx, "git push --force origin main", cfg)
    assert.Equal(t, Allow, result.Decision)
}
```

### P6: Blocklist Precedence Over Allowlist

**Invariant**: If a command matches both blocklist and allowlist, the decision
is `Deny` (blocklist wins).

```go
func TestPropertyBlocklistPrecedence(t *testing.T) {
    cfg := &evalConfig{
        policy:    PermissivePolicy(),
        allowlist: []string{"git *"},
        blocklist: []string{"git push --force *"},
    }
    result := pipeline.Run(ctx, "git push --force origin main", cfg)
    assert.Equal(t, Deny, result.Decision)
}
```

### P7: Empty Command Always Allows

**Invariant**: Empty or whitespace-only commands always produce `Allow`
with no warnings.

```go
func TestPropertyEmptyAlwaysAllow(t *testing.T) {
    empties := []string{"", " ", "\t", "\n", "  \t\n  "}
    for _, cmd := range empties {
        result := pipeline.Run(ctx, cmd, cfg)
        assert.Equal(t, Allow, result.Decision)
        assert.Nil(t, result.Assessment)
        assert.Empty(t, result.Warnings)
    }
}
```

### P8: Policy Monotonicity

**Invariant**: `StrictPolicy` is at least as restrictive as `InteractivePolicy`,
which is at least as restrictive as `PermissivePolicy`. For any assessment,
if `PermissivePolicy` denies, so do the other two. If `StrictPolicy` allows,
so do the other two.

```go
func TestPropertyPolicyMonotonicity(t *testing.T) {
    strict := StrictPolicy()
    interactive := InteractivePolicy()
    permissive := PermissivePolicy()

    // Restrictiveness ordering: Deny (most) > Ask > Allow (least)
    restrictiveness := map[Decision]int{Allow: 0, Ask: 1, Deny: 2}

    severities := []Severity{Indeterminate, Low, Medium, High, Critical}
    confidences := []Confidence{ConfidenceLow, ConfidenceMedium, ConfidenceHigh}

    for _, sev := range severities {
        for _, conf := range confidences {
            a := Assessment{sev, conf}
            sd := strict.Decide(a)
            id := interactive.Decide(a)
            pd := permissive.Decide(a)

            // Full monotonicity check (SE-02-P2.6):
            // Strict >= Interactive >= Permissive for all assessments
            assert.GreaterOrEqual(t, restrictiveness[sd], restrictiveness[id],
                "strict must be at least as restrictive as interactive for %v/%v", sev, conf)
            assert.GreaterOrEqual(t, restrictiveness[id], restrictiveness[pd],
                "interactive must be at least as restrictive as permissive for %v/%v", sev, conf)
        }
    }
}
```

**Note (MF-P2.6)**: Since all built-in policies currently ignore `Confidence`,
the confidence dimension doesn't affect results. The test still iterates over
all confidence levels to catch regressions if a policy is later modified to
use confidence.

### P9: Glob Idempotence

**Invariant**: Glob matching is deterministic. `globMatch(p, t)` always
returns the same result for the same pattern and text.

```go
func TestPropertyGlobDeterministic(t *testing.T) {
    f := func(pattern, text string) bool {
        r1 := globMatch(pattern, text)
        r2 := globMatch(pattern, text)
        return r1 == r2
    }
    quick.Check(f, nil)
}
```

### P10: Safe Pattern Short-Circuit

**Invariant**: If a safe pattern matches a command in a pack, no destructive
pattern from that pack appears in `Result.Matches` for that command.

```go
func TestPropertySafeShortCircuit(t *testing.T) {
    // git push --force-with-lease matches "git-push-force-with-lease" safe pattern
    // Result.Matches must NOT contain any core.git destructive matches
    result := pipeline.Run(ctx, "git push --force-with-lease origin main", cfg)
    for _, m := range result.Matches {
        assert.NotEqual(t, "core.git", m.Pack, "safe pattern should short-circuit")
    }
}
```

---

## E: Deterministic Example Tests

### E1: Pre-Filter Keyword Matching (40+ cases)

```
# Commands that SHOULD pass the pre-filter (contain keywords)
git push --force origin main               → match: ["git"]
git status                                  → match: ["git"]
docker run -d nginx                         → match: ["docker"]
kubectl delete pod foo                      → match: ["kubectl"]
terraform destroy                           → match: ["terraform"]
rails db:reset                              → match: ["rails"]
RAILS_ENV=production rails db:reset         → match: ["rails"]
psql -c "DROP TABLE users"                  → match: ["psql"]
rm -rf /tmp/build                           → match: ["rm"]

# Commands that SHOULD NOT pass the pre-filter
ls -la
echo "hello world"
cd /tmp
cat file.txt
curl https://example.com
npm install
pip install flask
make build
go test ./...
```

### E2: Pattern Matching — core.git Test Pack (30+ cases)

```
# Destructive matches
git push --force origin main                → git-push-force (High)
git push -f origin main                     → git-push-force (High)
git push --force                            → git-push-force (High)
/usr/bin/git push --force origin main       → git-push-force (High)
git reset --hard HEAD~3                     → git-reset-hard (High)
git reset --hard                            → git-reset-hard (High)
git clean -f                                → git-clean-force (Medium)
git clean --force                           → git-clean-force (Medium)
git clean -fd                               → git-clean-force (Medium)
git clean -f -d                             → git-clean-force (Medium)

# Safe matches (should NOT trigger destructive)
git push origin main                        → Allow (safe: git-push-no-force)
git push                                    → Allow (safe: git-push-no-force)
git push --force-with-lease origin main     → Allow (safe: git-push-force-with-lease)
git push --force-with-lease                 → Allow (safe: git-push-force-with-lease)

# Non-matches (git keyword but no pattern match)
git status                                  → Allow
git log --oneline                           → Allow
git diff HEAD~1                             → Allow
git stash                                   → Allow
git pull --rebase                           → Allow
git branch -d feature                       → Allow (not in test pack)
git checkout -b feature                     → Allow

# Edge cases
git push --force-with-lease --force main    → git-push-force (both flags, --force wins)
echo "git push --force"                     → Allow (in string, not command)

### E8: AnyName Command-Agnostic Matcher Coverage

Verify command-agnostic matching behavior for packs that key off argument
content rather than command name:

```go
func TestAnyNameMatcherCoverage(t *testing.T) {
    m := packs.And(
        packs.AnyName(),
        packs.ArgContentRegex(`(?:~|\\$HOME)/Documents(?:/|$)`),
    )

    // Different command names, same protected-path argument content.
    cases := []parse.ExtractedCommand{
        {Name: "cat", Args: []string{"~/Documents/notes.txt"}},
        {Name: "rm", Args: []string{"$HOME/Documents/file.txt"}},
        {Name: "sqlite3", Args: []string{"~/Documents/db.sqlite"}},
    }
    for _, c := range cases {
        require.True(t, m.Match(c))
    }

    // Candidate-pack selection contract: command-agnostic packs still need
    // argument-content keywords to survive pre-filtering.
    pf := eval.NewKeywordFilter(registry)
    candidates := pf.Contains("cat ~/Documents/notes.txt", nil)
    require.Contains(t, candidates.PackIDs, "personal.files")
}
```

### E9: ArgContent Literal-vs-Regex Semantic Split

Lock down the contract that `ArgContent()` is literal substring matching while
`ArgContentRegex()` is regex matching:

```go
func TestArgContentLiteralVsRegex(t *testing.T) {
    cmd := parse.ExtractedCommand{Args: []string{":main", "+refs/heads/main", "0"}}

    // Literal semantics: "^:" and "^0$" are just plain substrings here.
    require.False(t, packs.ArgContent("^:").Match(cmd))
    require.False(t, packs.ArgContent("^0$").Match(cmd))

    // Regex semantics: anchors are interpreted and should match.
    require.True(t, packs.ArgContentRegex("^:").Match(cmd))
    require.True(t, packs.ArgContentRegex("^\\+").Match(cmd))
    require.True(t, packs.ArgContentRegex("^0$").Match(cmd))
}
```
git push --force origin main | cat          → git-push-force (pipeline)
git reset --hard && echo "done"             → git-reset-hard (&& chain)
```

### E3: Allowlist/Blocklist Glob Matching (30+ cases)

```
# Pattern: "git status"
"git status"                                → match
"git status --short"                        → no match (extra text)
"git push"                                  → no match

# Pattern: "git status *"
"git status --short"                        → match
"git status --short --branch"               → match
"git status"                                → no match (* needs chars after space)

# Pattern: "*/bin/git *"
"/usr/bin/git push --force"                 → match
"/usr/local/bin/git status"                 → match
"git push"                                  → no match (no path prefix)

# Pattern: "*"
""                                          → match
"anything"                                  → match
"git push --force origin main"              → match

# Pattern: "git push --force*"
"git push --force"                          → match
"git push --force origin main"              → match
"git push --force-with-lease"               → match (glob doesn't understand flags)

# Command-separator restriction on * (MF-P0.1)
# * does NOT match: ; | & \n ` $ ( )
"git status *" vs "git status; rm -rf /"    → no match (* stops at ;)
"git status *" vs "git status | rm -rf /"   → no match (* stops at |)
"git status *" vs "git status && rm -rf /"  → no match (* stops at &)
"git status *" vs "git status\nrm -rf /"    → no match (* stops at \n)
"git status *" vs "git status `rm -rf /`"   → no match (* stops at `)
"git status *" vs "git status $(rm -rf /)"  → no match (* stops at $)

# Blocklist precedence
blocklist: ["git push --force *"]
allowlist: ["git *"]
"git push --force origin main"              → Deny (blocklist wins)
"git push origin main"                      → Allow (allowlist, no blocklist match)
"git status"                                → Allow (allowlist match)

# Blocklist "*" blocks everything (SE-02-P2.3)
blocklist: ["*"]
"any command at all"                        → Deny
```

### E4: Environment Detection (30+ cases)

```
# Exact-value env vars
RAILS_ENV=production                        → production detected
RAILS_ENV=prod                              → production detected
RAILS_ENV=development                       → NOT production
RAILS_ENV=staging                           → NOT production
NODE_ENV=production                         → production detected
NODE_ENV=prod                               → production detected
NODE_ENV=test                               → NOT production
FLASK_ENV=production                        → production detected
APP_ENV=production                          → production detected
MIX_ENV=prod                                → production detected
RACK_ENV=production                         → production detected

# URL-shaped env vars
DATABASE_URL=postgres://user@prod-db:5432/mydb  → production detected
DATABASE_URL=postgres://user@staging-db:5432/db  → NOT production
DATABASE_URL=postgres://localhost:5432/dev        → NOT production
REDIS_URL=redis://production.redis.acme.com:6379 → production detected
REDIS_URL=redis://localhost:6379                  → NOT production
DATABASE_URL=postgres://user@productivity.internal:5432/db → NOT production

# Profile env vars
AWS_PROFILE=production                      → production detected
AWS_PROFILE=prod-us-east-1                  → production detected
AWS_PROFILE=staging                         → NOT production
GOOGLE_CLOUD_PROJECT=my-prod-project        → production detected
GOOGLE_CLOUD_PROJECT=my-dev-project         → NOT production
AZURE_SUBSCRIPTION=prod-subscription        → production detected

# Case insensitivity in values
RAILS_ENV=Production                        → production detected (case insensitive)
RAILS_ENV=PRODUCTION                        → production detected
DATABASE_URL=postgres://user@PROD-DB:5432/db → production detected

# Multiple sources
inline: RAILS_ENV=production                → detected (source: inline)
export: export NODE_ENV=production          → detected (source: export)
process: os.Environ() has RAILS_ENV=production → detected (source: process)
```

### E5: Policy Decision Matrix (15 cases — exhaustive)

```
# StrictPolicy
Indeterminate → Deny
Low           → Allow
Medium        → Deny
High          → Deny
Critical      → Deny

# InteractivePolicy
Indeterminate → Ask
Low           → Allow
Medium        → Ask
High          → Deny
Critical      → Deny

# PermissivePolicy
Indeterminate → Allow
Low           → Allow
Medium        → Allow
High          → Ask
Critical      → Deny
```

### E6: Assessment Aggregation (10+ cases)

```
# Single match
[{High, ConfidenceHigh}]                    → Assessment{High, ConfidenceHigh}

# Multiple matches — highest severity wins
[{Medium, ConfidenceHigh}, {High, ConfidenceMedium}] → Assessment{High, ConfidenceMedium}

# Same severity — highest confidence wins
[{High, ConfidenceLow}, {High, ConfidenceHigh}] → Assessment{High, ConfidenceHigh}

# Environment escalation
Base Medium + production env                → Assessment{High, ...} (escalated)
Base High + production env                  → Assessment{Critical, ...}
Base Critical + production env              → Assessment{Critical, ...} (capped)

# Compound commands with mixed matches
"git push --force && git status"            → Assessment{High, ConfidenceHigh} (force push)
"echo done; git clean -f"                   → Assessment{Medium, ConfidenceHigh} (clean)
```

### E7: Full Pipeline End-to-End (20+ cases)

```
# These test the complete path: input → decision

# Allow — no keywords
ls -la                                      → Allow (no warnings)
echo "hello world"                          → Allow (no warnings)

# Allow — keyword but no pattern match
git status                                  → Allow (keyword hit, parsed, no match)
git log --oneline                           → Allow

# Allow — safe pattern match
git push origin main                        → Allow (safe match)
git push --force-with-lease                 → Allow (safe match)

# Deny — destructive pattern
git push --force origin main                → Deny (InteractivePolicy: High → Deny)
git reset --hard HEAD~3                     → Deny

# Ask — medium severity with interactive
git clean -f                                → Ask (InteractivePolicy: Medium → Ask)

# Deny — env escalation
RAILS_ENV=production git clean -f           → Deny (Medium → High via escalation)

# Allow — empty/whitespace
""                                          → Allow
"   "                                       → Allow
"\n\t"                                      → Allow

# Indeterminate — oversized input
<128KB+ string with "git" keyword>          → Indeterminate → policy decides

# Pipeline/compound commands
git push --force && echo done               → Deny (force push in chain)
echo start; git reset --hard                → Deny (reset in sequence)

# Dataflow integration
DIR=/; rm -rf $DIR                          → (depends on dataflow, plan 01)
```

---

## F: Fault Injection / Chaos Engineering Tests

### F1: Matcher Panic Recovery

Inject a panicking `CommandMatcher` into a pack and verify the pipeline
continues, producing `WarnMatcherPanic`.

```go
func TestFaultMatcherPanic(t *testing.T) {
    panicMatcher := panicOnMatchMatcher{}
    pack := packs.Pack{
        ID: "test.panic",
        Keywords: []string{"test"},
        Destructive: []packs.DestructivePattern{{
            Name:  "panic-pattern",
            Match: panicMatcher,
            Severity: guard.High,
        }},
    }
    registry := packs.NewRegistry()
    registry.Register(pack)
    pipeline := NewPipeline(parser, registry)

    result := pipeline.Run(ctx, "test command", cfg)
    // Should NOT panic
    assert.Contains(t, warningCodes(result), guard.WarnMatcherPanic)
}

type panicOnMatchMatcher struct{}
func (panicOnMatchMatcher) Match(parse.ExtractedCommand) bool { panic("test panic") }
```

### F2: Registry Freeze Violation

Verify that attempting to register a pack after the registry is frozen
panics (caught at init time, not runtime).

```go
func TestFaultRegistryFreezeViolation(t *testing.T) {
    r := packs.NewRegistry()
    r.Register(somePack)
    _ = r.All() // Freezes registry

    assert.Panics(t, func() {
        r.Register(anotherPack)
    })
}
```

### F3: Malformed Aho-Corasick Input

Feed the pre-filter with degenerate keyword sets:
- Empty keyword list (should match nothing)
- Single-character keywords
- Keywords with special regex characters
- Duplicate keywords
- Keywords that are substrings of each other ("git", "github")

```go
func TestFaultPreFilterDegenerateKeywords(t *testing.T) {
    // Empty keywords
    ac := newAhoCorasick([]string{})
    assert.Empty(t, ac.FindAll("git push"))

    // Substring keywords
    ac = newAhoCorasick([]string{"git", "github"})
    matches := ac.FindAll("github actions")
    assert.Contains(t, matches, "git")    // "git" is in "github"
    assert.Contains(t, matches, "github")
}
```

### F4: Concurrent Pipeline Stress

Run 100+ goroutines calling `Pipeline.Run()` simultaneously with different
commands, policies, and pack configurations. Verify no races and no panics.

```go
func TestFaultConcurrentPipeline(t *testing.T) {
    pipeline := setupTestPipeline(t)
    var wg sync.WaitGroup
    for i := 0; i < 200; i++ {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            cmds := []string{"git push --force", "ls -la", "echo hello", ""}
            policies := []Policy{StrictPolicy(), InteractivePolicy(), PermissivePolicy()}
            cmd := cmds[i%len(cmds)]
            pol := policies[i%len(policies)]
            cfg := &evalConfig{policy: pol}
            result := pipeline.Run(context.Background(), cmd, cfg)
            assert.True(t, result.Decision >= Allow && result.Decision <= Ask)
        }(i)
    }
    wg.Wait()
}
```

Run with `-race` flag to detect data races.

### F5: Nil and Zero-Value Inputs

Test pipeline behavior with edge-case inputs:

```go
func TestFaultNilInputs(t *testing.T) {
    // Nil policy → should use a default or panic at config time
    // Empty allowlist/blocklist → no matches
    // Nil callerEnv → no process env detection
    // enabledPacks empty slice vs nil → different semantics
}
```

---

## O: Comparison Oracle Tests

### O1: Policy Decision Exhaustive Verification

The policy decision matrix (plan §5.6) serves as the oracle. A single test
enumerates all (severity × confidence × policy) combinations and verifies
each against the matrix.

```go
func TestOraclePolicyMatrix(t *testing.T) {
    type expectation struct {
        severity   Severity
        confidence Confidence
        strict     Decision
        interactive Decision
        permissive Decision
    }
    expectations := []expectation{
        {Critical, ConfidenceHigh, Deny, Deny, Deny},
        {Critical, ConfidenceMedium, Deny, Deny, Deny},
        {Critical, ConfidenceLow, Deny, Deny, Deny},
        {High, ConfidenceHigh, Deny, Deny, Ask},
        {High, ConfidenceMedium, Deny, Deny, Ask},
        {High, ConfidenceLow, Deny, Deny, Ask},
        {Medium, ConfidenceHigh, Deny, Ask, Allow},
        {Medium, ConfidenceMedium, Deny, Ask, Allow},
        {Medium, ConfidenceLow, Deny, Ask, Allow},
        {Low, ConfidenceHigh, Allow, Allow, Allow},
        {Low, ConfidenceMedium, Allow, Allow, Allow},
        {Low, ConfidenceLow, Allow, Allow, Allow},
        {Indeterminate, ConfidenceHigh, Deny, Ask, Allow},
        {Indeterminate, ConfidenceMedium, Deny, Ask, Allow},
        {Indeterminate, ConfidenceLow, Deny, Ask, Allow},
    }
    // Verify each expectation against actual policy decisions
    ...
}
```

### O2: Glob Match Reference Oracle

Implement a naive reference glob matcher (brute-force recursive) and
verify it produces the same results as the optimized `globMatch` for a
large corpus of pattern/text pairs.

```go
func TestOracleGlobMatch(t *testing.T) {
    // Naive reference implementation
    naiveGlob := func(pattern, text string) bool {
        // Recursive backtracking implementation
        ...
    }

    // Generate random patterns and texts
    f := func(pattern, text string) bool {
        return globMatch(pattern, text) == naiveGlob(pattern, text)
    }
    quick.Check(f, &quick.Config{MaxCount: 50000})
}
```

### O3: Environment Detection Against Upstream Rules

Extract the production indicator rules from the architecture doc (§4 step 9)
and build a separate verification table that the detector's rules exactly
match the spec.

```go
func TestOracleEnvDetectionRules(t *testing.T) {
    d := NewDetector()

    // Verify exact-value vars
    exactVars := []string{"RAILS_ENV", "NODE_ENV", "FLASK_ENV", "APP_ENV", "MIX_ENV", "RACK_ENV"}
    for _, v := range exactVars {
        r := d.Detect(map[string]string{v: "production"}, nil, nil)
        assert.True(t, r.IsProduction, "%s=production should detect", v)
        r = d.Detect(map[string]string{v: "prod"}, nil, nil)
        assert.True(t, r.IsProduction, "%s=prod should detect", v)
        r = d.Detect(map[string]string{v: "development"}, nil, nil)
        assert.False(t, r.IsProduction, "%s=development should not detect", v)
    }

    // Verify URL vars
    urlVars := []string{"DATABASE_URL", "REDIS_URL", "MONGO_URL", "ELASTICSEARCH_URL"}
    for _, v := range urlVars {
        r := d.Detect(map[string]string{v: "postgres://user@prod-db:5432/mydb"}, nil, nil)
        assert.True(t, r.IsProduction, "%s with prod hostname should detect", v)
        r = d.Detect(map[string]string{v: "postgres://localhost:5432/mydb"}, nil, nil)
        assert.False(t, r.IsProduction, "%s with localhost should not detect", v)
    }

    // Verify profile vars
    profileVars := []string{"AWS_PROFILE", "GOOGLE_CLOUD_PROJECT", "AZURE_SUBSCRIPTION"}
    for _, v := range profileVars {
        r := d.Detect(map[string]string{v: "prod-us-east-1"}, nil, nil)
        assert.True(t, r.IsProduction, "%s with prod should detect", v)
        r = d.Detect(map[string]string{v: "staging"}, nil, nil)
        assert.False(t, r.IsProduction, "%s with staging should not detect", v)
    }
}
```

---

## B: Benchmarks and Performance Tests

### B1: Pre-Filter Throughput

**What**: Measure Aho-Corasick matching throughput for commands of various
lengths.

**Baseline**: Record on first run (no hard target — the pre-filter processes
inputs at LLM-response frequency, so any sub-millisecond result is fine).

```go
func BenchmarkPreFilterNoMatch(b *testing.B)   { ... } // "ls -la"
func BenchmarkPreFilterMatch(b *testing.B)     { ... } // "git push --force"
func BenchmarkPreFilterLongInput(b *testing.B) { ... } // 1KB no-match
func BenchmarkPreFilterSubset(b *testing.B)    { ... } // With pack subset
```

### B2: Glob Matching Throughput

**What**: Measure glob matching for various pattern/text combinations.

```go
func BenchmarkGlobExact(b *testing.B)   { ... } // "git status" vs "git status"
func BenchmarkGlobStar(b *testing.B)    { ... } // "git *" vs "git push --force origin main"
func BenchmarkGlobNoMatch(b *testing.B) { ... } // "docker *" vs "git push"
func BenchmarkGlobLong(b *testing.B)    { ... } // Long text with trailing *
```

### B3: Matcher Throughput

**What**: Measure individual matcher `Match()` throughput.

```go
func BenchmarkNameMatcher(b *testing.B)        { ... }
func BenchmarkFlagMatcher(b *testing.B)        { ... }
func BenchmarkArgMatcher(b *testing.B)         { ... }
func BenchmarkArgContentRegex(b *testing.B)    { ... }
func BenchmarkCompositeMatcher(b *testing.B)   { ... } // And(Name, Flags, Not(Flags))
func BenchmarkNegativeMatcher(b *testing.B)    { ... }
```

### B4: Environment Detection Throughput

**What**: Measure env detection latency.

```go
func BenchmarkEnvDetectNone(b *testing.B)      { ... } // No env vars
func BenchmarkEnvDetectInline(b *testing.B)    { ... } // Inline env match
func BenchmarkEnvDetectProcess(b *testing.B)   { ... } // Process env (50 vars)
func BenchmarkEnvDetectURL(b *testing.B)       { ... } // URL hostname extraction
```

### B5: Full Pipeline Throughput

**What**: Measure end-to-end pipeline latency for representative commands.

**Baseline**: Record on first run. The pipeline includes parsing (plan 01),
so this benchmark depends on the parse layer being available.

```go
func BenchmarkPipelineAllow(b *testing.B)        { ... } // "ls -la" (pre-filter reject)
func BenchmarkPipelineSafeMatch(b *testing.B)    { ... } // "git push origin main" (safe)
func BenchmarkPipelineDestructive(b *testing.B)  { ... } // "git push --force" (match)
func BenchmarkPipelineCompound(b *testing.B)     { ... } // "git push --force && echo done"
func BenchmarkPipelineEnvEscalation(b *testing.B) { ... } // "RAILS_ENV=prod git clean -f"
```

### B6: Pre-Filter Rejection Rate

**What**: Measure what percentage of a representative command corpus is
rejected at the keyword pre-filter stage (never reaches parsing).

**Target**: >90% of benign commands skip parsing.

```go
func TestPreFilterRejectionRate(t *testing.T) {
    benignCommands := []string{
        "ls -la", "cd /tmp", "echo hello", "cat file.txt",
        "curl https://example.com", "npm install", "go build",
        "make", "grep pattern file", "find . -name '*.go'",
        // ... 50+ benign commands
    }
    rejected := 0
    for _, cmd := range benignCommands {
        if !kf.Contains(cmd, nil).Matched {
            rejected++
        }
    }
    rate := float64(rejected) / float64(len(benignCommands))
    t.Logf("Pre-filter rejection rate: %.1f%%", rate*100)
    assert.Greater(t, rate, 0.9, "rejection rate should be >90%%")
}
```

---

## S: Stress / Soak Tests

### S1: Concurrent Pipeline Stress

**What**: Run 200 goroutines calling `Pipeline.Run()` continuously for 30
seconds. Verify no panics, no races, and stable memory usage.

```go
func TestStressConcurrentPipeline(t *testing.T) {
    if testing.Short() {
        t.Skip("stress test")
    }
    pipeline := setupTestPipeline(t)
    commands := loadStressCorpus() // Mix of allow, deny, ask commands

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    var wg sync.WaitGroup
    var panicCount atomic.Int64
    var evalCount atomic.Int64

    for i := 0; i < 200; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            defer func() {
                if r := recover(); r != nil {
                    panicCount.Add(1)
                }
            }()
            for {
                select {
                case <-ctx.Done():
                    return
                default:
                    cmd := commands[id%len(commands)]
                    pol := []Policy{StrictPolicy(), InteractivePolicy(), PermissivePolicy()}[id%3]
                    cfg := &evalConfig{policy: pol}
                    result := pipeline.Run(context.Background(), cmd, cfg)
                    _ = result
                    evalCount.Add(1)
                }
            }
        }(i)
    }
    wg.Wait()

    assert.Equal(t, int64(0), panicCount.Load(), "no panics")
    t.Logf("Completed %d evaluations in 30s", evalCount.Load())
}
```

Run with `-race` flag.

### S2: Memory Soak

**What**: Run 100,000 evaluations in a single goroutine and verify memory
doesn't grow unboundedly. Checks for leaks in parser pooling, automaton
caching, and string retention.

```go
func TestSoakMemory(t *testing.T) {
    if testing.Short() {
        t.Skip("soak test")
    }
    pipeline := setupTestPipeline(t)
    cfg := &evalConfig{policy: InteractivePolicy()}

    runtime.GC()
    var before runtime.MemStats
    runtime.ReadMemStats(&before)

    for i := 0; i < 100_000; i++ {
        cmd := fmt.Sprintf("git push --force-%d origin main", i)
        _ = pipeline.Run(context.Background(), cmd, cfg)
    }

    runtime.GC()
    var after runtime.MemStats
    runtime.ReadMemStats(&after)

    growth := after.HeapInuse - before.HeapInuse
    t.Logf("Heap growth: %d KB", growth/1024)
    // Allow up to 10MB growth for 100K evaluations (most should be GC'd)
    assert.Less(t, growth, uint64(10*1024*1024),
        "heap growth should be bounded")
}
```

---

## SEC: Security Tests

### SEC1: Glob Pattern Injection

Verify that glob patterns in allowlist/blocklist cannot be exploited to
bypass security. Test pathological patterns that could cause excessive
backtracking.

```go
func TestSecurityGlobInjection(t *testing.T) {
    // Pathological patterns that could cause catastrophic backtracking
    // in naive implementations. Our `*`-only glob has no backtracking issues
    // beyond the star-matching loop.
    patterns := []string{
        strings.Repeat("*", 100),
        "a" + strings.Repeat("*a", 50),
    }
    for _, p := range patterns {
        start := time.Now()
        globMatch(p, strings.Repeat("a", 1000))
        assert.Less(t, time.Since(start), 100*time.Millisecond,
            "glob should not have exponential backtracking")
    }
}
```

### SEC2: Allowlist Bypass Attempts

Verify that known bypass techniques don't work:

```go
func TestSecurityAllowlistBypass(t *testing.T) {
    pipeline := setupTestPipeline(t)

    // Per MF-P0.1: glob * now does NOT match command separators (;, |, &, \n, `, $, (, )).
    // This prevents the most dangerous bypass vectors.
    bypasses := []struct {
        name         string
        command      string
        allowlist    []string
        wantDecision Decision
        reason       string
    }{
        // These bypasses are now BLOCKED by the command-separator restriction on *
        {"semicolon injection blocked", "git status; rm -rf /",
            []string{"git status *"}, Deny,
            "* does not match ; — glob fails, command proceeds to parsing"},
        {"newline injection blocked", "git status\ngit push --force",
            []string{"git status *"}, Deny,
            "* does not match \\n — glob fails, both commands parsed"},
        {"backtick injection blocked", "git status `git push --force`",
            []string{"git status *"}, Deny,
            "* does not match ` — glob fails, subshell parsed"},
        {"subshell injection blocked", "git status $(git push --force)",
            []string{"git status *"}, Deny,
            "* does not match $( — glob fails, subshell parsed"},
        {"pipe injection blocked", "git status | rm -rf /",
            []string{"git status *"}, Deny,
            "* does not match | — glob fails, both sides parsed"},
        {"and-chain injection blocked", "git status && rm -rf /",
            []string{"git status *"}, Deny,
            "* does not match & — glob fails, both commands parsed"},

        // Verify allowlist still works for legitimate use
        {"legitimate allowlist match", "git status --short --branch",
            []string{"git status *"}, Allow,
            "Normal args matched by * (no separators)"},

        // Verify baseline: without allowlist, force push IS detected
        {"baseline without allowlist", "git push --force origin main",
            nil, Deny,
            "Without allowlist, destructive command detected normally"},
    }
    for _, b := range bypasses {
        t.Run(b.name, func(t *testing.T) {
            cfg := &evalConfig{
                policy:    StrictPolicy(),
                allowlist: b.allowlist,
            }
            result := pipeline.Run(ctx, b.command, cfg)
            assert.Equal(t, b.wantDecision, result.Decision, b.reason)
        })
    }
}
```

**Security note**: Per the MF-P0.1 review finding, glob `*` no longer matches
command separators (`;`, `|`, `&`, `\n`, `` ` ``, `$`, `(`, `)`). This
prevents the most dangerous allowlist bypass vectors where compound commands
could be injected after a benign prefix. The allowlist remains a fast escape
hatch for single-command patterns with flag/arg variations.

### SEC3: Regex Denial of Service

For `ArgContentMatcher` with regex, verify that pathological regexes
don't cause excessive matching time.

```go
func TestSecurityRegexDoS(t *testing.T) {
    // ArgContentMatcher regex is compiled at init time (MustCompile).
    // The regex itself could be pathological. Since pack authors control
    // regexes, this is a supply-chain concern, not a user-input concern.
    //
    // Test: compile a pack with a known-safe regex and verify it completes
    // quickly on adversarial input.
    matcher := ArgContentRegex(`(?i)\bDROP\s+TABLE\b`)
    cmd := parse.ExtractedCommand{
        Args: []string{strings.Repeat("DROP ", 10000)},
    }
    start := time.Now()
    matcher.Match(cmd)
    assert.Less(t, time.Since(start), time.Second,
        "regex match should complete quickly")
}
```

---

## MQ: Manual QA Plan

### MQ1: Realistic Command Evaluation

Manually test with real-world commands from Claude Code sessions:

1. Copy 20 recent `Bash` tool invocations from Claude Code logs
2. Run each through `dcg-go test "command"` (plan 04)
3. Verify decisions are sensible — no false positives on safe commands,
   no false negatives on obviously destructive commands

**Judgment criteria**: A human/agent reviewer evaluates whether each
decision is "correct" (would a careful developer agree with the decision?).

### MQ2: Pack Coverage Audit

For each registered pack, manually verify:
1. At least 3 golden file entries per destructive pattern
2. At least 1 golden file entry per safe pattern
3. Reason and Remediation strings are helpful and accurate
4. Keywords are sufficient to trigger the pack for all its patterns

### MQ3: Policy Suitability Review

Review the three built-in policies against real use cases:
1. **StrictPolicy**: Run 100 commands through strict mode. Verify that every
   `Deny` is justified and no `Allow` lets through something dangerous.
2. **InteractivePolicy**: Review every `Ask` decision. Is it genuinely
   ambiguous? Would a human need to decide?
3. **PermissivePolicy**: Review every `Allow`. Are any of them genuinely
   dangerous commands that should at least `Ask`?

### MQ4: Environment Detection Accuracy

Test with real production environment configurations:
1. Set up a shell with `RAILS_ENV=production` and run 10 commands
2. Set up with `DATABASE_URL=postgres://prod-db:5432/mydb` and run 10 commands
3. Verify severity escalation is applied correctly
4. Verify no false escalation on development/staging env vars

---

## CI: CI Tier Mapping

### Tier 1: On Every Commit (< 30 seconds)

- All unit tests (`matcher_test.go`, `registry_test.go`, `policy_test.go`,
  `allowlist_test.go`, `prefilter_test.go`, `detect_test.go`)
- Property tests P1–P10
- Deterministic examples E1–E7
- Golden file corpus validation
- `go vet`, `staticcheck`

### Tier 2: On Every PR (< 2 minutes)

- All Tier 1 tests
- Fault injection tests F1–F5
- Comparison oracle tests O1–O3
- Security tests SEC1–SEC3
- Pipeline integration tests
- Race detection (`-race` flag)
- Benchmarks (record only, no regression check yet)

### Tier 3: Nightly (< 15 minutes)

- All Tier 2 tests
- Stress test S1 (30-second concurrent stress)
- Soak test S2 (100K evaluations)
- Benchmark regression check (compare against recorded baselines)
- Pre-filter rejection rate verification (B6)

### Tier 4: Pre-Release

- All Tier 3 tests
- Manual QA MQ1–MQ4
- Full benchmark suite with profiling
- Extended soak test (1M evaluations)

---

## Exit Criteria

The following must all pass before plan 02 implementation is considered
complete and ready for Batch 3 (pack implementation):

### Must Pass

1. **All unit tests pass** — 100% of matcher, registry, policy, allowlist,
   pre-filter, and env detection tests
2. **All property tests pass** — P1–P10
3. **All deterministic examples pass** — E1–E7
4. **Golden file corpus passes** — All initial seed entries (30–40)
5. **Pipeline integration tests pass** — End-to-end with test pack
6. **No race conditions** — All tests pass with `-race`
7. **Fault injection tests pass** — F1–F5
8. **Comparison oracle tests pass** — O1–O3
9. **Security tests pass** — SEC1–SEC3
10. **Pre-filter rejection rate ≥ 90%** — B6

### Should Pass

11. **Benchmarks recorded** — All B1–B5 benchmarks have baseline values
12. **Stress test passes** — S1 completes without panics
13. **Soak test passes** — S2 shows bounded memory growth

### Tracked Metrics

- Test count by category
- Golden file entry count (target: 30–40 for plan 02, growing to 500+ by Batch 5)
- Pre-filter rejection rate on benign command corpus
- Pipeline throughput (evaluations/second) from benchmarks
- Pack pattern coverage (% of patterns with ≥3 golden entries)

---

## Round 1 Review Disposition

| Finding | Reviewer | Severity | Summary | Disposition | Notes |
|---------|----------|----------|---------|-------------|-------|
| SE-02-P2.6 | systems-engineer | P2 | P8 monotonicity test incomplete | Incorporated | P8 rewritten with full ordering check using restrictiveness map. |
| SE-02-P3.4 | systems-engineer | P3 | SEC2 documents limitation without resolution | Incorporated | SEC2 rewritten with concrete expectations. Bypass vectors now blocked by glob separator restriction. |
| MF-P1.1 | security-correctness | P1 | Glob semantics unclear | Incorporated | E3 updated with command-separator restriction examples and `"*"` blocklist case. |

## Round 2 Review Disposition

| # | Reviewer | Severity | Summary | Disposition | Notes |
|---|----------|----------|---------|-------------|-------|
| 1 | dcg-coder-1 | P2 | Harness coverage omits AnyNameMatcher-specific behavior | Incorporated | Added E8 AnyName command-agnostic coverage with argument-content keywords across multiple command names. |
| 2 | dcg-coder-1 | P2 | Harness lacks regression for ArgContent regex-literal misuse | Incorporated | Added E9 regression test locking literal `ArgContent` vs regex `ArgContentRegex` semantics. |

## Round 3 Review Disposition

No new findings.

---

## Completion Signoff

- **Status**: Partial
- **Date**: 2026-03-03
- **Branch**: main
- **Verified by**: dcg-coder-1
- **Completed items**:
  - Broad automated test coverage exists across `internal/eval` and `guard` (property-like, deterministic, fault, security, oracle/comparison, benchmark, and stress-style tests).
  - Golden-corpus testing exists and passes (`internal/eval/golden_test.go` with database-focused corpora).
  - Baseline verification passed: `make test`; targeted race checks for representative `internal/eval` and `internal/parse` paths.
- **Outstanding gaps**:
  - This harness is written for the plan-02 matcher architecture (parse-driven matcher DSL and envdetect package), but the implemented architecture is materially different (raw-string rule predicates in `internal/eval`/`internal/packs`). Severity: P1 (harness-to-implementation mismatch).
  - Several named harness expectations are not directly realizable against current code boundaries (for example matcher-type-specific coverage like `AnyName`/`ArgContentRegex` in a matcher DSL that is not implemented as documented). Severity: P2 (spec mismatch).
  - Exit criterion "all tests pass with `-race`" for the full relevant suite is not yet evidenced in this signoff pass; only targeted race subsets were validated due a stalled long-running full race invocation in this environment. Severity: P2 (verification completeness gap).
