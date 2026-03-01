# Plan 05: Testing & Benchmarks — Systems Engineer Review

**Reviewer**: dcg-reviewer (systems-engineer persona)
**Date**: 2026-03-01
**Documents reviewed**:
- `docs/plans/05-testing-and-benchmarks.md` (~1778 lines)
- `docs/plans/05-testing-and-benchmarks-test-harness.md` (~778 lines)

**Focus areas**: Benchmark methodology correctness, fuzz testing invariant
completeness, mutation testing coverage claims, golden file corpus design,
comparison test infrastructure, CI integration feasibility, and whether the
testing plan actually provides the assurance it claims.

**Cross-references consulted**: plan 02 (matching framework — severity,
truncation, Indeterminate), plan 03e (frameworks pack — rails db:reset
severity), plan 04 (API — InteractivePolicy mapping), plan 01
(tree-sitter — command_substitution handling).

---

## Summary Assessment

The testing plan is well-structured with 8 complementary subsystems
providing layered assurance: benchmarks, comparison tests, fuzz testing,
mutation testing, golden file expansion, grammar coverage, E2E tests, and
profiling. The CI tier system (T1–T4) is practical and the regression gates
are reasonable.

Main weaknesses: (1) mutation testing excludes safe patterns, creating a
blind spot for the most dangerous mutation class (false short-circuit),
(2) the comparison test infrastructure has an undefined classification
function, (3) one E2E test expectation conflicts with cross-plan severity
rules, and (4) the test harness uses a broken testing.T mock pattern.

**Finding count**: 0 P0, 4 P1, 4 P2, 4 P3

---

## P1 Findings (High — Incorrect or incomplete, will cause test failures)

### SE-P1.1: E2E "dev database reset" expects Ask, should be Deny

**Location**: 05-testing-and-benchmarks.md lines 1208–1211

The E2E test:
```go
{
    name:     "dev database reset",
    command:  "RAILS_ENV=development rails db:reset",
    wantDecision: guard.Ask,
    note:     "Development env, lower severity",
}
```

Cross-plan analysis:
- Plan 03e D2 (line 428): `rails db:reset` base severity = **High**
- Plan 03e env detection: escalation to Critical only when production env
  detected. `RAILS_ENV=development` is NOT production → no escalation →
  severity remains **High**
- Plan 04 InteractivePolicy: High → **Deny** (not Ask)
- Plans 02 and 03e define NO de-escalation mechanism for development
  environments

The test expects Ask but the correct decision is Deny. Either:
(a) Fix the test expectation to Deny, or
(b) Add a de-escalation mechanism to plan 02 for development environments
    (significant design change).

The note "Development env, lower severity" suggests the author assumed
de-escalation exists, but it doesn't — `EnvSensitive` only escalates,
never de-escalates.

### SE-P1.2: Mutation testing excludes safe patterns

**Location**: 05-testing-and-benchmarks.md line 844

`runMutationAnalysis` iterates only `pack.Destructive`:
```go
for _, pattern := range pack.Destructive {
```

Open question Q2 (line 1712–1716) recommends including safe patterns, but
this recommendation is not incorporated into the main body.

**Why this matters**: A mutation in a safe pattern is MORE dangerous than a
mutation in a destructive pattern. If a safe pattern mutates to match too
broadly, it could short-circuit evaluation and cause destructive commands to
be classified as Allow. The plan's claim of 100% kill rate across ~940
mutations only covers destructive patterns — the safe-pattern blind spot
undermines the confidence claim.

**Fix**: Incorporate Q2 recommendation into §6. Define mutation operators
for safe patterns (e.g., broaden match predicate, remove condition in And(),
swap ArgAt value). Add safe pattern mutation count to the ~940 total.

### SE-P1.3: `classifyDivergence` function referenced but never defined

**Location**: 05-testing-and-benchmarks.md line 415, test harness P5

The comparison test calls `classifyDivergence(result)` to categorize Go vs
Rust divergences into {identical, intentional_improvement,
intentional_divergence, bug}. This function is never specified.

Without defined classification logic, the comparison testing infrastructure
cannot distinguish between "expected new behavior" and "regression bug".
The plan needs to specify:
- What constitutes `intentional_improvement` (e.g., Go correctly detects a
  command the Rust version misses, and the command is in a pack that Go adds)
- What constitutes `intentional_divergence` (e.g., different severity for
  documented design reasons)
- What constitutes `bug` (default — any unclassified divergence)
- Whether classifications are hardcoded per-entry or rule-based

### SE-P1.4: `testing.T{}` mock pattern is broken in Go

**Location**: test harness P3 (line 171–173), acknowledged at D3 (line
418–419)

P3 uses:
```go
innerT := &testing.T{}
verifyInvariants(innerT, br.command, br.result)
if innerT.Failed() {
```

You cannot create a bare `testing.T{}` and call `Failed()` — the struct
requires internal initialization from the test runner. The plan's D3 section
acknowledges this ("In practice, testing.T can't be mocked this way. The
actual test uses a wrapper that captures t.Fatal calls.") but never specifies
what this wrapper looks like.

**Fix**: Define the approach explicitly. Options:
(a) Use `t.Run()` sub-test and check `t.Failed()` after it returns
(b) Create a `fatalCapture` struct implementing a `TB` interface subset
(c) Use `recover()` to catch `runtime.Goexit()` from `t.Fatal()`

Option (a) is simplest and most idiomatic. Update both P3 and D3 code
snippets for consistency.

---

## P2 Findings (Medium — Weakens assurance or has correctness risk)

### SE-P2.1: Benchmark CV < 0.30 threshold may be too loose

**Location**: test harness P1 (lines 44–45), exit criteria (line 755)

A 30% coefficient of variation is very generous. For sub-microsecond
benchmarks (e.g., keyword pre-filter lookups, individual matcher
evaluations), measurement noise from OS scheduling and cache effects can
easily exceed 30% CV even with stable code. For longer benchmarks
(>100μs), 30% CV would indicate severe environmental interference.

A single threshold cannot serve both. Consider:
- Tiered: CV ≤ 0.15 for benchmarks >100μs, CV ≤ 0.30 for <100μs
- Or: rely on `benchstat`'s statistical significance testing (already
  planned in §3.4) instead of raw CV for the CI gate, and use CV only
  as a stability diagnostic

### SE-P2.2: Self-comparison O1 samples only 20 entries

**Location**: test harness O1 (line 462)

```go
for _, entry := range corpus[:20] { // Sample 20
```

The self-comparison oracle (Go vs Go) is a sanity check, not a performance
test. There's no reason to sample — running the full corpus takes negligible
time since it's just two invocations of the same binary per entry. A bug
that only manifests on the 21st+ entry would be missed.

**Fix**: Iterate over all entries: `for _, entry := range corpus {`

### SE-P2.3: Golden file entry count 501 may not reconcile

**Location**: 05-testing-and-benchmarks.md §7.1

The plan states 501 existing golden entries. Summing plan golden entry
counts: 03a + 03b + 03c + 03d + 03e = approximately 488–501 (depending on
counting method). After review incorporation — which adds entries (e.g.,
03e review P1-3 added env-escalated golden entries for D3, D5, D7, D9) —
the actual count will change. The target of 750+ should be revalidated
against actual post-incorporation counts.

**Fix**: Add a CI check (Tier 1) that counts golden entries and compares
against the documented target. This already appears partially planned in
D4 but without the post-incorporation reconciliation.

### SE-P2.4: Mutation operator `EmptyReason` kill mechanism unclear

**Location**: 05-testing-and-benchmarks.md §6.3

The `EmptyReason` operator empties the reason string of a destructive
pattern match. For this mutation to be "killed", some test must assert that
the reason is non-empty. The plan doesn't specify which tests do this.

If no test asserts on non-empty reasons, this operator would produce
unkilled mutations, reducing the kill rate below 100%. The golden file
format includes a `reason_contains` field — verify that all golden entries
that match destructive patterns include a `reason_contains` assertion.

---

## P3 Findings (Low — Polish, minor gaps)

### SE-P3.1: Fuzz seed corpus lacks structural diversity

**Location**: 05-testing-and-benchmarks.md lines 525–541

The 17 fuzz seeds cover basic cases well but miss several structural
categories that would help the fuzzer explore interesting paths faster:
- No nested quoting: `bash -c "git push --force"` (present but no double-
  nesting)
- No heredoc: `cat <<EOF\nDROP TABLE users;\nEOF` (present — good)
- No process substitution: `diff <(cmd1) <(cmd2)`
- No array expansion: `${arr[@]}`
- No brace expansion: `rm -rf /tmp/{a,b,c}`

Adding 3–5 structurally diverse seeds would improve early fuzz coverage.

### SE-P3.2: Grammar coverage omits process_substitution node type

**Location**: 05-testing-and-benchmarks.md §8

The grammar coverage test enumerates 17 command-bearing AST node types but
does not include `process_substitution` (e.g., `diff <(git log) <(svn log)`).
Process substitutions can contain arbitrary commands. Plan 01 may or may not
walk into these nodes — the grammar test should verify.

**Fix**: Add `process_substitution` to the node type enumeration with a
test template like `diff <(rm -rf /) <(echo safe)`.

### SE-P3.3: E2E hook mode tests don't verify stderr isolation

**Location**: 05-testing-and-benchmarks.md §9.2

The hook mode E2E tests verify JSON stdout output and exit codes, but don't
assert that warnings appear on stderr (not stdout). For Claude Code hook
protocol compatibility, stdout must contain ONLY valid JSON. If a warning
is accidentally written to stdout, the hook would fail with a parse error.

**Fix**: Capture both stdout and stderr from the subprocess. Assert
`json.Valid(stdout)` and that warnings (if any) appear in stderr.

### SE-P3.4: Comparison test doesn't specify upstream binary version pinning

**Location**: 05-testing-and-benchmarks.md §4

The comparison tests run Go vs upstream Rust, but the plan doesn't specify
how the Rust binary version is pinned. If the upstream updates between CI
runs, comparison results could flip classification without any Go code
change. Open question Q1 discusses build vs download but not version
pinning.

**Fix**: Pin the upstream binary to a specific version/commit SHA in a
config file. Bumping the pin should be an explicit, reviewed change.

---

## Cross-Plan Consistency Checks

| Check | Result |
|-------|--------|
| INV-8 (oversized → Indeterminate) vs plan 02 truncation | **Consistent** — plan 02 returns Indeterminate immediately for oversized input (line 1203–1213), does NOT truncate and continue. INV-8 correctly expects Indeterminate. |
| Grammar coverage command_substitution vs plan 01 | **Consistent** — plan 01 walks into command_substitution nodes (line 552) and extracts commands. Grammar test expectation that `$(git push --force)` is detected is correct. |
| Fuzz seed `RAILS_ENV=production rails db:reset` vs plan 03e | **Consistent** — env detection escalates to Critical, InteractivePolicy → Deny. |
| Benchmark per-stage breakdown vs plan 02 pipeline stages | **Consistent** — parse, extract, pre-filter, match stages match plan 02 pipeline. |
| E2E exit codes vs plan 04 | **Consistent** — test mode exit codes 0/1/2/3 match plan 04 (after review incorporation SE-P2.5). |

---

## Overall Assurance Assessment

**Does the testing plan provide the assurance it claims?**

Largely yes, with caveats:

1. **Benchmarks** (§3): Sound methodology. `benchstat` integration provides
   statistical rigor. Per-stage breakdown enables targeted optimization.
   The 20% regression gate is reasonable. Weakness: CV threshold needs
   tiering (SE-P2.1).

2. **Comparison tests** (§4): Good concept but underspecified. The undefined
   `classifyDivergence` (SE-P1.3) and lack of version pinning (SE-P3.4) mean
   this subsystem can't be implemented as written.

3. **Fuzz testing** (§5): Strong. 8 invariants cover the critical properties.
   INV-8 correctly aligns with plan 02. Seed corpus is reasonable (SE-P3.1
   is minor). The 3-fuzzer approach (pipeline, config, hook) provides good
   entry-point diversity.

4. **Mutation testing** (§6): The 7 operators and 100% kill rate target are
   ambitious and appropriate. However, excluding safe patterns (SE-P1.2)
   creates the most dangerous blind spot — a broadened safe pattern is worse
   than a narrowed destructive pattern. Fixing this is the highest-leverage
   improvement.

5. **Golden file expansion** (§7): Solid. The 501→750+ target with
   structural variants, false-positive traps, and cross-pack entries provides
   strong regression coverage. The format is well-designed.

6. **Grammar coverage** (§8): Novel and valuable. Testing all 17
   command-bearing AST node types ensures the parser doesn't silently drop
   commands from new syntactic contexts. Minor gap: process_substitution
   (SE-P3.2).

7. **E2E tests** (§9): Good real-world scenario coverage. The dev database
   reset bug (SE-P1.1) is concerning because it suggests the author assumed
   a de-escalation mechanism that doesn't exist — worth checking if other
   E2E expectations have the same assumption.

8. **CI integration** (tiers): Practical and well-structured. T1 (<5s)
   through T4 (manual) provides appropriate feedback speed at each stage.

**Net assessment**: The plan provides strong assurance for its 8 subsystems.
Fixing the 4 P1 findings would make it comprehensive. The most impactful
fix is SE-P1.2 (mutation testing safe patterns) — without it, the 100%
mutation kill rate claim overstates coverage.
