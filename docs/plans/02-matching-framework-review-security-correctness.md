# 02: Matching Framework — Review: Security & Correctness

**Plan**: [02-matching-framework.md](./02-matching-framework.md)
**Test Harness**: [02-matching-framework-test-harness.md](./02-matching-framework-test-harness.md)
**Reviewer Focus**: Security, correctness — allowlist/blocklist bypass, glob injection, policy decisions, environment detection FP/FN, safe-before-destructive ordering, Indeterminate handling, assessment aggregation edge cases
**Date**: 2026-02-28

---

## Findings

### MF-P0.1: Allowlist Raw-Text Matching Bypasses Structural Analysis — Compound Command Injection

**Severity**: P0
**Component**: Pipeline (§5.5), AllowBlockChecker (§5.7)
**Category**: Security / Bypass

The allowlist matches against the **raw command string** and short-circuits **before parsing** (Step 2 in the pipeline). This is documented as intentional in the architecture, but the security implications are more severe than acknowledged.

Consider an allowlist pattern `"git status *"`. The following command matches:

```bash
git status; rm -rf /
```

The glob `"git status *"` matches because `*` matches `"; rm -rf /"`. The entire compound command is allowed without any structural analysis. The `rm -rf /` is never evaluated.

The test harness (SEC2: Allowlist Bypass Attempts) explicitly calls this out and documents it as "a known trade-off" — but the test expectations are left blank (`// Document this in test expectations`). The test doesn't actually verify any specific behavior. It acknowledges the bypass exists but doesn't mitigate it.

This is a P0 because it means any allowlist pattern ending with `*` (which is the primary use case per the architecture examples: `"git status *"`, `"*/bin/git *"`) creates a trivially exploitable bypass. An LLM could generate `git status; dangerous_command` and it would be allowed.

**Recommendation**: Either:
1. **Parse before allowlist**: Move allowlist/blocklist checking after parsing. Match each extracted command against allowlist patterns independently. This eliminates the bypass but loses the "fast escape hatch" property.
2. **Restrict glob semantics**: `*` should not match `;`, `&&`, `||`, `|`, `\n`, `` ` ``, `$()` or other command separators. This prevents compound command injection through allowlists while preserving the fast-path. The glob still matches arguments and flags but not command boundaries.
3. **Warn on dangerous allowlist patterns**: At init time, emit a warning if any allowlist pattern ends with `*` (which could match injected commands). Not sufficient alone but useful for awareness.

Option 2 is recommended — it preserves the fast-path behavior while preventing the most dangerous bypass vectors.

---

### MF-P0.2: Blocklist Match Produces Critical Assessment Without Match Details

**Severity**: P0
**Component**: Pipeline (§5.5)
**Category**: Correctness / Observable Behavior

When a command matches the blocklist, the pipeline returns:

```go
result.Decision = guard.Deny
result.Assessment = &guard.Assessment{
    Severity:   guard.Critical,
    Confidence: guard.ConfidenceHigh,
}
return result
```

But `result.Matches` is left empty (nil). This means:
- A caller checking `result.Matches` to understand *why* a command was denied gets no information
- The invariant "if Assessment is non-nil, there's information about why" is violated
- Callers cannot distinguish between "blocked by blocklist" and "matched a Critical pattern"
- The test harness property P3 ("if Matches is non-empty, Assessment is non-nil") passes, but the **converse** is not tested: "if Assessment is non-nil, can the caller understand why?"

**Recommendation**: When a blocklist match triggers, add a synthetic `Match` entry:

```go
result.Matches = []guard.Match{{
    Pack:   "blocklist",
    Rule:   matchedPattern,
    Severity: guard.Critical,
    Confidence: guard.ConfidenceHigh,
    Reason: fmt.Sprintf("Command matches blocklist pattern: %s", matchedPattern),
}}
```

This provides observability and lets callers explain the decision.

---

### MF-P1.1: `globMatch` Semantics for `*` vs Empty String Not Fully Resolved

**Severity**: P1
**Component**: AllowBlockChecker (§5.7)
**Category**: Correctness

The plan's Open Question 3 asks whether `"git status *"` should match `"git status"`. The `globMatch` implementation uses a two-pointer algorithm where `*` matches zero or more characters. Looking at the algorithm:

When `pattern = "git status *"` and `text = "git status"`:
- After matching `"git status "` (with trailing space), the pattern pointer is at `*` and the text pointer is past the end. The trailing-star loop at the end consumes `*`, returning `true`.

But if `text = "git status"` (no trailing space): the pattern is `"git status *"`, and after matching `"git status"` the pattern pointer is at `" "` (space before `*`) while text is exhausted. The space doesn't match because text is empty. The star backtrack doesn't help. So `globMatch("git status *", "git status")` returns `false`.

This means `"git status *"` matches `"git status "` (with trailing space, unlikely) but not `"git status"`. The test harness E3 says `"git status *"` vs `"git status"` → no match (* needs chars after space). This is correct per the algorithm but **surprising** — a user writing `"git status *"` probably expects it to match `"git status"` (zero arguments is still "with any arguments").

This creates a usability footgun: users writing allowlist patterns will be surprised that `"git push *"` doesn't match bare `"git push"`.

**Recommendation**: Either:
1. Document this clearly and recommend `"git push*"` (no space before `*`) for matching with or without arguments, or
2. Change the glob semantics so trailing ` *` matches zero or more characters after the space (i.e., the space-then-star pattern also matches the version without the trailing space)

---

### MF-P1.2: Environment Detection Applies Globally Instead of Per-Command

**Severity**: P1
**Component**: Pipeline (§5.5), EnvDetector (§5.8)
**Category**: False Positives

Environment detection is performed once for the entire pipeline run (Step 9), producing a global `envResult.IsProduction`. This result is then applied to **all** commands' env-sensitive patterns:

```go
for _, cmd := range parseResult.Commands {
    matches := p.matchCommand(cmd, candidatePacks, envResult)
    ...
}
```

But production env vars may be scoped to a specific command:

```bash
RAILS_ENV=production rails db:reset && git clean -f
```

Here, `RAILS_ENV=production` is an inline env var on `rails`, not on `git clean`. But the global `envResult.IsProduction` is true, so `git clean -f` (Medium severity, `EnvSensitive: true` if the full core.git pack were env-sensitive) would be escalated even though the production indicator is unrelated to that command.

For the test pack (`core.git`), none of the patterns are `EnvSensitive`, so this doesn't immediately bite. But when database and infrastructure packs are added (Batch 3), this will cause false positives.

**Recommendation**: Make environment detection per-command:
1. Check the command's own `InlineEnv` for production indicators
2. Check dataflow-exported vars that are in scope at the point of that command
3. Check process-level env vars (these are global and apply to all commands)

Per-command inline env detection should be the primary check. Global process env is acceptable as a secondary check.

---

### MF-P1.3: Safe Pattern Short-Circuit Is Per-Pack, Not Cross-Pack — Could Miss

**Severity**: P1
**Component**: Pipeline (§5.5)
**Category**: Correctness / Design

The safe-before-destructive short-circuit is scoped to individual packs: if a safe pattern in `core.git` matches, only `core.git`'s destructive patterns are skipped. Other packs still evaluate the command independently.

This is correct and documented, but there's a subtle issue: what if two packs cover the same command? Consider:

- `core.git` has safe pattern `git-push-no-force` (git push without --force)
- A hypothetical `platform.github` pack also has a destructive pattern for `git push` (e.g., pushing to a protected branch)

If `core.git`'s safe pattern matches, `platform.github`'s destructive pattern still fires. This is correct behavior — each pack independently assesses the command. But pack authors need to be aware of this cross-pack interaction.

The more concerning case: a pack might have a safe pattern that's **too broad**, preventing legitimate destructive matches within the same pack. For example, if `core.git`'s safe pattern `git-push-no-force` matched any `git push` that doesn't have `--force`, it would prevent detecting `git push --mirror` (which is destructive but doesn't use `--force`).

Looking at the test pack: `git-push-no-force` matches `And(Name("git"), ArgAt(0, "push"), Not(Or(Flags("--force"), Flags("-f"))))`. This would match `git push --mirror`, making it safe and preventing any destructive pattern for `--mirror` from firing.

**Recommendation**:
1. Document the cross-pack interaction clearly for pack authors
2. Add a verification test that checks: for every safe pattern, no destructive pattern within the same pack is made unreachable (i.e., every destructive pattern has at least one test case where the safe pattern doesn't match but the destructive one does)
3. For the `core.git` pack specifically: `git-push-no-force` should also forbid `--mirror` and `--delete` in addition to `--force`/`-f`

---

### MF-P1.4: Pre-Filter Substring Matching Creates Unintended Pass-Through

**Severity**: P1
**Component**: KeywordPreFilter (§5.4)
**Category**: Performance / Correctness

The Aho-Corasick pre-filter matches keywords as **substrings** of the raw command string. The plan acknowledges this: `"keyword substring", "gitignore", true // AC finds "git" in "gitignore"`.

This creates unintended pass-through to the parser. Commands containing keywords as substrings will be parsed unnecessarily:
- `echo "productivity tips"` → matches `"prod"` (from env detection? No — pack keywords only. But if `rm` is a keyword, `format` contains no keyword)
- `cat gitignore` → matches `"git"` keyword → unnecessary parse
- `echo "gitbook setup"` → matches `"git"` → unnecessary parse

The plan says "False positives at the pre-filter level just mean unnecessary parsing, not false detections." This is correct for correctness, but for performance the >90% rejection target may be harder to hit if common substring matches cause pass-through.

**Recommendation**: Consider whole-word matching in the pre-filter. Instead of matching `"git"` as a raw substring, match `\bgit\b` (word boundary). This could be done by post-filtering Aho-Corasick matches: after finding `"git"` in `"gitignore"`, check that the character before and after the match are non-alphanumeric (or beginning/end of string). This is a lightweight post-check that dramatically reduces false pass-through.

---

### MF-P2.1: `WarnExpansionCapped` Indeterminate Check Only When No Other Match

**Severity**: P2
**Component**: Pipeline (§5.5)
**Category**: Correctness

The pipeline has:

```go
if hasWarning(result.Warnings, guard.WarnExpansionCapped) && result.Assessment == nil {
    result.Assessment = &guard.Assessment{
        Severity:   guard.Indeterminate,
        Confidence: guard.ConfidenceHigh,
    }
}
```

The `result.Assessment == nil` condition means: if expansion was capped AND some other pattern matched (producing a non-nil assessment), the expansion cap is silently ignored. But the unchecked expansions might contain a *more severe* match.

Example: `D=/ || D=/b || ... (17 values); rm -rf $D && git clean -f`. Suppose expansion is capped at 16, missing `D=/`. `git clean -f` matches as Medium. The assessment is Medium (not nil), so the expansion cap doesn't promote to Indeterminate. But `rm -rf /` was missed.

**Recommendation**: The expansion cap should always produce at least an Indeterminate assessment, regardless of other matches. If other matches are more severe, use those. But if other matches are less severe than what the unchecked expansions might produce, promote to Indeterminate. Simplest approach: if `WarnExpansionCapped` is present, set a floor of Indeterminate on the assessment.

---

### MF-P2.2: `extractHostname` Uses `LastIndex` for Userinfo — Edge Case with `@` in Passwords

**Severity**: P2
**Component**: EnvDetector (§5.8)
**Category**: Correctness

`extractHostname` strips userinfo using `strings.LastIndex(url, "@")`:

```go
if idx := strings.LastIndex(url, "@"); idx >= 0 {
    url = url[idx+1:]
}
```

This correctly handles `user:pass@host` but fails for passwords containing `@`:

```
postgres://user:p@ss@prod-db.acme.com:5432/mydb
```

`LastIndex("@")` finds the last `@`, giving hostname `prod-db.acme.com:5432/mydb` — this is correct! The `LastIndex` approach handles this case correctly because the last `@` separates userinfo from hostname per RFC 3986.

Wait — what about `@` in the path? `postgres://user@host/db@name` → `LastIndex` would return the `@` in `db@name`, stripping too much. But `db@name` is in the path after `host/`, and we strip path first... no, the code strips userinfo before path:

```go
// Strip userinfo
if idx := strings.LastIndex(url, "@"); idx >= 0 { url = url[idx+1:] }
// Strip port and path
if idx := strings.IndexAny(url, ":/"); idx >= 0 { url = url[:idx] }
```

For `user@host/db@name` (after scheme strip): `LastIndex("@")` finds the `@` in `db@name` and returns `name`. Then path/port strip returns `name`. The hostname `host` is lost.

**Recommendation**: Parse the URL properly using `net/url.Parse()`. The custom parsing has too many edge cases with `@` in paths and passwords.

---

### MF-P2.3: Aho-Corasick Subset Cache Unbounded — Potential Memory Issue

**Severity**: P2
**Component**: KeywordPreFilter (§5.4)
**Category**: Resource Management

The subset automaton cache in `KeywordPreFilter` is unbounded:

```go
cache map[string]*ahoCorasick // packSetKey → automaton for subset
```

The plan acknowledges this: "The cache of subset automatons is unbounded... If cache size becomes a concern, an LRU eviction policy can be added."

In practice, the pack set key is generated from the caller's `WithPacks`/`WithDisabledPacks` configuration. If callers use unique combinations (e.g., per-request configuration), this cache grows without bound. Each cached automaton allocates memory proportional to the total keyword length.

**Recommendation**: Add a bounded cache (e.g., `sync.Map` with a counter, or a simple LRU with max 32 entries). Even if this is unlikely to be hit in practice, it prevents a theoretical resource leak that could bite in long-running processes.

---

### MF-P2.4: `resolveEnabledPacks` Returns `nil` vs Empty Slice — Semantic Difference

**Severity**: P2
**Component**: Pipeline (§5.5)
**Category**: Correctness

`resolveEnabledPacks` returns `nil` for "all packs" and a slice for a subset. Downstream code treats `nil` and empty-slice differently:

```go
func (kf *KeywordPreFilter) Contains(command string, enabledPacks []string) MatchResult {
    ac := kf.getAutomaton(enabledPacks)
    ...
}

func (kf *KeywordPreFilter) getAutomaton(enabledPacks []string) *ahoCorasick {
    if enabledPacks == nil {
        return kf.defaultAC
    }
    // Build subset automaton
    ...
}
```

If `resolveEnabledPacks` returns an empty slice `[]string{}` (all packs disabled minus all packs), `getAutomaton` treats it as a subset request, builds an empty automaton (no keywords), and all commands pass the pre-filter. This means all packs disabled → commands pass the pre-filter → get parsed → find no matching packs → Allow. Functionally correct, but wasteful.

More importantly, `selectCandidatePacks` with an `enabledPacks` empty slice returns no packs, so matching correctly produces no matches. The issue is only performance (unnecessary parsing).

But: if `cfg.enabledPacks = []string{"nonexistent"}` and `cfg.disabledPacks = []string{"nonexistent"}`, `resolveEnabledPacks` returns an empty slice. The pre-filter gets an empty-keyword automaton, passes everything through, parses everything, and finds no matches. This is correct but inefficient.

**Recommendation**: When `resolveEnabledPacks` produces an empty list, short-circuit to Allow immediately. No packs enabled = nothing to match.

---

### MF-P2.5: `git push --force-with-lease --force` Test Case — Safe/Destructive Ordering Ambiguity

**Severity**: P2
**Component**: Test pack (§5.9), Test harness (E2)
**Category**: Correctness

The test harness E2 includes:

```
git push --force-with-lease --force main    → git-push-force (both flags, --force wins)
```

Let's trace through the test pack's patterns:

1. Safe pattern `git-push-no-force`: `And(Name("git"), ArgAt(0, "push"), Not(Or(Flags("--force"), Flags("-f"))))`
   - `--force` IS present → `Not(Or(...))` → false → safe pattern doesn't match

2. Safe pattern `git-push-force-with-lease`: `And(Name("git"), ArgAt(0, "push"), Flags("--force-with-lease"), Not(Flags("--force")))`
   - `--force-with-lease` IS present ✓
   - `Not(Flags("--force"))` → `--force` IS present → false → safe pattern doesn't match

3. Destructive pattern `git-push-force`: `And(Name("git"), ArgAt(0, "push"), Or(Flags("--force"), Flags("-f")))`
   - `--force` IS present → matches → **Deny**

This is correct: the presence of `--force` alongside `--force-with-lease` is destructive (since `--force` overrides `--force-with-lease`). The safe pattern correctly requires `Not(Flags("--force"))`.

However, this analysis depends on the fact that `--force` and `--force-with-lease` are stored as separate keys in the Flags map. If the extractor ever stores them differently (e.g., treating `--force-with-lease` as a variant of `--force`), this would break.

**Recommendation**: Add explicit test cases that verify the Flags map contains both `--force` and `--force-with-lease` as separate keys when both are present. This is already implied by plan 01's flag decomposition, but an explicit cross-plan consistency test is valuable.

---

### MF-P2.6: Policy Confidence Not Used in Decisions

**Severity**: P2
**Component**: Policy Engine (§5.6)
**Category**: Design / Future Risk

The `Policy.Decide(Assessment)` method receives both `Severity` and `Confidence`, but all three built-in policies ignore `Confidence` entirely:

```go
func (strictPolicy) Decide(a Assessment) Decision {
    switch {
    case a.Severity >= Medium: return Deny
    case a.Severity == Indeterminate: return Deny
    default: return Allow
    }
}
```

The `Confidence` field is carried through the pipeline but never affects decisions. This means a `ConfidenceLow` match at `High` severity produces the same decision as `ConfidenceHigh` at `High` severity.

This is acknowledged in the architecture ("The `Decide(Assessment)` signature is intentionally minimal"), but it means the test harness property P8 (Policy Monotonicity) tests across confidence levels that are functionally irrelevant. All 15 decision matrix entries collapse to 5 (one per severity level).

**Recommendation**: This is fine for v0, but document clearly that confidence is currently unused by built-in policies. It's available for custom `Policy` implementations. If confidence remains unused after real-world data is collected, consider removing it to simplify the API.

---

### MF-P3.1: Golden File Format Lacks Schema Validation

**Severity**: P3
**Component**: Golden File Infrastructure (§5.10)
**Category**: Test Robustness

The golden file format is custom key-value text:

```
command: git push --force origin main
decision: Deny
severity: High
...
```

There's no schema validation for the golden file entries. Invalid keys, misspelled field names, or wrong severity values would be silently ignored by the parser (which only matches known keys). An entry with `desision: Deny` (typo) would have no decision assertion — the test would pass vacuously.

**Recommendation**: The golden file parser should:
1. Validate that required fields are present (at minimum: `command`, `decision`)
2. Validate field values against known enums (`decision` ∈ {Allow, Deny, Ask})
3. Warn on unknown keys (catch typos)

---

### MF-P3.2: No Test for Disabled Pack Behavior

**Severity**: P3
**Component**: Pipeline (§5.5)
**Category**: Test Coverage

The plan describes `WithDisabledPacks` and `resolveEnabledPacks` but neither the unit tests nor the test harness include test cases for:
- Disabling a pack that has a matching keyword and pattern
- Disabling all packs
- Disabling a non-existent pack (silently ignored per `PacksByID`)
- Interaction between `WithPacks` and `WithDisabledPacks`

**Recommendation**: Add test cases to pipeline_test.go:
- `git push --force` with `disabledPacks: ["core.git"]` → Allow (pack disabled)
- `git push --force` with `enabledPacks: ["core.git"], disabledPacks: ["core.git"]` → Allow (disabled wins)
- Empty enabled packs → Allow (nothing to match)

---

### MF-P3.3: `isEmptyOrWhitespace` Doesn't Handle All Unicode Whitespace

**Severity**: P3
**Component**: Pipeline (§5.5)
**Category**: Edge Case

```go
func isEmptyOrWhitespace(s string) bool {
    for _, c := range s {
        if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
            return false
        }
    }
    return true
}
```

This only handles ASCII whitespace. Unicode whitespace characters (e.g., `\u00A0` non-breaking space, `\u2003` em space) are treated as non-empty, non-whitespace commands. These would then be passed to the pre-filter and parser.

This is unlikely to be exploited (LLMs don't generate unicode whitespace commands), but for completeness, consider using `unicode.IsSpace`.

**Recommendation**: Use `strings.TrimSpace` or `unicode.IsSpace` for a more robust check. Low priority since the impact is just unnecessary parsing, not incorrect results.

---

### MF-P3.4: `ArgMatcher` Uses `path.Match` — Inconsistent with `globMatch`

**Severity**: P3
**Component**: Matchers (§5.2.3)
**Category**: Design Inconsistency

The `ArgMatcher.matchArg` function uses `path.Match` for glob matching against argument values:

```go
matched, err := path.Match(m.Pattern, arg)
```

But the allowlist/blocklist uses the custom `globMatch` where `*` matches everything (including `/` and spaces). `path.Match`'s `*` does NOT match `/`.

So if a pack author writes `ArgMatcher{Pattern: "/tmp/*"}`, it would NOT match `/tmp/sub/dir/file` (because `*` doesn't cross `/` in `path.Match`). But if a user writes an allowlist pattern `"/tmp/*"`, it WOULD match `/tmp/sub/dir/file` (because `globMatch`'s `*` matches everything).

This inconsistency could confuse pack authors and users.

**Recommendation**: Document the difference clearly. Consider using `globMatch` in `ArgMatcher` as well, or provide separate `GlobArg` and `PathArg` matchers if both semantics are needed.

---

## Summary

| ID | Severity | Component | Summary |
|----|----------|-----------|---------|
| MF-P0.1 | P0 | AllowBlockChecker | Allowlist `*` matches command separators — compound command injection bypass |
| MF-P0.2 | P0 | Pipeline | Blocklist deny has Assessment but empty Matches — observability gap |
| MF-P1.1 | P1 | AllowBlockChecker | `globMatch` trailing `*` semantics unclear and surprising |
| MF-P1.2 | P1 | Pipeline | Environment detection is global, not per-command — false escalation |
| MF-P1.3 | P1 | Test Pack | Safe pattern `git-push-no-force` too broad — blocks `--mirror` detection |
| MF-P1.4 | P1 | KeywordPreFilter | Aho-Corasick substring matching causes unnecessary parse pass-through |
| MF-P2.1 | P2 | Pipeline | `WarnExpansionCapped` Indeterminate ignored when other matches exist |
| MF-P2.2 | P2 | EnvDetector | `extractHostname` fails with `@` in URL paths |
| MF-P2.3 | P2 | KeywordPreFilter | Subset automaton cache unbounded |
| MF-P2.4 | P2 | Pipeline | `resolveEnabledPacks` empty slice vs nil semantics |
| MF-P2.5 | P2 | Test Pack | `--force` and `--force-with-lease` coexistence needs cross-plan test |
| MF-P2.6 | P2 | Policy | Confidence field unused by all built-in policies |
| MF-P3.1 | P3 | Golden Files | No schema validation for golden file entries |
| MF-P3.2 | P3 | Pipeline | No tests for disabled pack behavior |
| MF-P3.3 | P3 | Pipeline | `isEmptyOrWhitespace` misses unicode whitespace |
| MF-P3.4 | P3 | ArgMatcher | `path.Match` vs `globMatch` inconsistency |
