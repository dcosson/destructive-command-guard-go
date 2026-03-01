# 02: Matching Framework — Review (Systems Engineer)

**Reviewed doc**: [02-matching-framework.md](./02-matching-framework.md)
**Test harness doc**: [02-matching-framework-test-harness.md](./02-matching-framework-test-harness.md)
**Reviewer focus**: API design correctness, concurrency safety, performance, pipeline orchestration, test coverage
**Cross-referenced**: [00-architecture.md](./00-architecture.md), [01-treesitter-integration.md](./01-treesitter-integration.md)

---

## Findings

### P0 — Critical

#### SE-02-P0.1: Nil policy dereference — unrecovered panic in Pipeline.Run()

**Location**: Plan §5.5 `Pipeline.Run()`, lines 1191, 1227, 1271

`Pipeline.Run()` calls `cfg.policy.Decide()` at three points:
- Step 1: oversized input (line 1191)
- Partial parse fallback (line 1227)
- Step 12: final decision (line 1271)

If `cfg.policy` is nil, all three call sites panic with a nil pointer
dereference. The pipeline has panic recovery for `CommandMatcher.Match()` (§7,
`safeMatch`), but no recovery wrapping the top-level `Run()` method or the
policy calls.

**Impact**: A caller passing `nil` policy (missing `WithPolicy()` option)
crashes the entire evaluation. Since `guard.Evaluate()` (plan 04) constructs
`evalConfig` from user-provided options, a default policy must be enforced.

**Recommendation**: Either (a) validate `cfg.policy != nil` at the top of
`Run()` and default to `InteractivePolicy()`, or (b) wrap the entire `Run()`
body in a deferred recover that produces `WarnMatcherPanic` + Indeterminate
assessment. Option (a) is preferred — fail fast at config validation, not
at runtime.

---

#### SE-02-P0.2: `ParseResult` does not expose exported variables for env detection

**Location**: Plan §5.5 `Pipeline.Run()`, line 1234

The pipeline calls:
```go
collectExportedVars(parseResult)
```

But `ParseResult` (from plan 01) has no field for exported variables:

```go
type ParseResult struct {
    Commands []ExtractedCommand
    Warnings []Warning
    HasError bool
}
```

The exported variable data comes from plan 01's `DataflowAnalyzer.ExportedVars()`
method, but there's no mechanism to thread it through `ParseResult` to the
pipeline. `collectExportedVars` is called but never defined, and has no data
source.

**Impact**: The entire dataflow-based environment detection path
(`export RAILS_ENV=production && rails db:reset`) won't work. The env detector
will only see inline env vars (`RAILS_ENV=production rails db:reset`) and
process env vars, missing the exported-vars case entirely.

**Recommendation**: Add an `ExportedVars map[string][]string` field to
`ParseResult`, populated by the extractor from `DataflowAnalyzer.ExportedVars()`
after the walk completes. This is a plan 01 interface change that plan 02
depends on — document it as a cross-plan requirement.

---

### P1 — High

#### SE-02-P1.1: Blocklist Deny produces Assessment but no Match — opaque decision

**Location**: Plan §5.5 `Pipeline.Run()`, lines 1196-1203

When blocklist matches:
```go
result.Decision = guard.Deny
result.Assessment = &guard.Assessment{
    Severity:   guard.Critical,
    Confidence: guard.ConfidenceHigh,
}
return result
```

`Result.Matches` is empty. A caller receiving `Decision: Deny` with
`Matches: []` has no diagnostic information about WHY the command was denied.
They'd have to know that empty Matches + Deny means blocklist match — this is
implicit protocol knowledge.

Similarly, the `Assessment` has no `Reason` or `Remediation`. The caller gets
a raw `{Critical, High}` assessment with no explanation.

**Recommendation**: Add a synthetic `Match` entry for blocklist matches:
```go
Match{
    Pack: "_blocklist",
    Rule: pattern,  // the blocklist pattern that matched
    Severity: Critical,
    Confidence: ConfidenceHigh,
    Reason: "Command matched blocklist pattern: " + pattern,
}
```
This makes the decision self-documenting. Consider the same for allowlist
matches (adding a synthetic match for observability even though decision is
Allow).

---

#### SE-02-P1.2: `ExtractedCommand` definition diverges between plan 01 and plan 02

**Location**: Plan §1 `ExtractedCommand` definition

Plan 02's `ExtractedCommand` (§1) has two fields not present in plan 01:
- `RawArgs []string` — presumably the unresolved argument text
- `DataflowResolved bool` — indicates if args were resolved via dataflow

These were likely added to address the plan 01 review findings (SE-01-P0.2),
but the changes are not documented as cross-plan interface modifications. Plan
01's detailed design (§6.1, §6.3) would need corresponding updates to populate
these fields.

**Recommendation**: Document the `ExtractedCommand` struct definition in a
single canonical location (probably `internal/parse/command.go` as plan 01
specifies). Plan 02 should reference that definition, not redefine it. If
fields were added, note the revision and its motivation. The `RawArgs` and
`DataflowResolved` fields should be defined in plan 01's design, which is the
owner of the type.

---

#### SE-02-P1.3: Environment escalation is global — all matches escalated if any command has prod indicators

**Location**: Plan §5.5 `Pipeline.Run()`, lines 1231-1236, §5.5 `matchCommand()`

The pipeline detects production indicators once, globally:
```go
envResult := p.envDet.Detect(
    collectInlineEnv(parseResult.Commands),  // merged from ALL commands
    collectExportedVars(parseResult),
    cfg.callerEnv,
)
```

Then every env-sensitive match is escalated:
```go
if dp.EnvSensitive && envResult.IsProduction {
    sev = escalateSeverity(sev)
}
```

Consider: `RAILS_ENV=production echo hello && git clean -f`

The `echo` command has `InlineEnv: {RAILS_ENV: production}`. The `git clean`
command has no inline env. But because env detection is global,
`envResult.IsProduction == true`, and `git-clean-force` (env-sensitive) gets
escalated from Medium to High.

This is conservative (biased toward false positives), which is the right
direction for a safety tool. However, it may confuse users who see "env
escalated" on a command that has no production env vars on it.

**Recommendation**: This is acceptable for v1. Document it explicitly as
intentional over-approximation in the pipeline design. For v2, consider
per-command env detection: check inline env on each command individually, merge
exported vars for the full command string, and process env is always global.

---

#### SE-02-P1.4: `convertWarnings` fragile type cast contradicts proposed import redesign

**Location**: Plan §5.5, lines 1430-1439, and the revision discussion at lines 1442-1478

The plan shows both (a) a `convertWarnings` function that casts
`parse.WarningCode` to `guard.WarningCode` via `guard.WarningCode(w.Code)`,
AND (b) a proposal to have `parse` import `guard/types.go` directly, which
would eliminate the need for conversion.

These are mutually exclusive approaches, but both appear in the plan as if
they coexist. The pipeline code at line 1219 calls `convertWarnings` even
though the revised import flow (§5.5 mermaid diagram at line 1453) shows
`parse → guard` directly.

**Recommendation**: Choose one approach and remove the other. The
"parse imports guard/types.go" approach (option a) is cleaner — `ParseResult`
uses `guard.Warning` directly, `convertWarnings` is deleted. Document this as
the definitive design and remove the `convertWarnings` code.

---

#### SE-02-P1.5: `resolveEnabledPacks` returns full pack list when `disabledPacks` is set — pre-filter uses wrong automaton

**Location**: Plan §5.5 `resolveEnabledPacks()`, lines 1378-1400

When only `disabledPacks` is set (no `enabledPacks`), `resolveEnabledPacks`
returns a list of all pack IDs minus the disabled ones. This list is passed
to `prefilter.Contains(command, enabledPacks)`, which builds a subset
automaton from those pack IDs.

The issue: building a new automaton for "all packs minus 1" is nearly as
expensive as the full automaton, and the result is cached by the full sorted
pack-set key. Every unique `disabledPacks` configuration creates a unique
cache entry. With 21 packs, there are 2^21 - 1 possible subsets.

In practice, callers will use a small number of configurations. But the
`resolveEnabledPacks` path creates pack ID lists on every call (allocating
a `[]string`), even for the common case where `disabledPacks` is empty.

**Recommendation**: Return `nil` (meaning "all packs") when `disabledPacks`
is empty. The `resolveEnabledPacks` already does this at line 1399. But when
`disabledPacks` is non-empty, consider whether filtering at the
`selectCandidatePacks` step is sufficient instead of building a subset
automaton. The pre-filter's job is to quickly reject commands with no relevant
keywords — using the full automaton and then filtering candidate packs by
enabled set achieves the same correctness with no cache proliferation.

---

### P2 — Medium

#### SE-02-P2.1: Aho-Corasick substring matching — "git" matches "gitignore", "github"

**Location**: Plan §5.4.1, test harness E1

The pre-filter uses Aho-Corasick substring matching. A keyword like `"git"`
matches any string containing `"git"` — including `"gitignore"`, `"github"`,
`"gitter"`. The plan acknowledges this in prefilter_test.go:
```go
{"keyword substring", "gitignore", true}, // Acceptable FP
```

While the plan correctly notes that false positives just cause unnecessary
parsing (not false detections), this impacts the >90% rejection rate target.
Commands like `echo "Check the github issue"` would pass the pre-filter and
incur parsing overhead for nothing.

**Recommendation**: Consider post-filtering Aho-Corasick matches with a
word-boundary check: verify the character before the match start is a
word-boundary (space, start-of-string, `|`, `;`, `(`, etc.) and the character
after the match end is a word-boundary. This is O(1) per match and would
significantly reduce false pass-through. If this is deemed too complex for v1,
at minimum add a benchmark that measures the false-positive rate on a
representative command corpus.

---

#### SE-02-P2.2: `disabledPacks` with typos silently ignored

**Location**: Plan §5.5 `resolveEnabledPacks()`, §5.3 `PacksByID()`

`PacksByID(ids)` says "Unknown IDs are silently ignored." This means
`WithDisabledPacks("core.gti")` (typo) does nothing — the pack remains
enabled. The caller has no indication their configuration was ignored.

**Recommendation**: Either (a) validate pack IDs at configuration time and
return an error or warning for unknown IDs, or (b) at minimum log a
`WarnUnknownPackID` warning in the result. Option (b) is simpler and doesn't
change the API.

---

#### SE-02-P2.3: `globMatch` edge case for empty pattern and empty text

**Location**: Plan §5.7 `globMatch()`, lines 1589-1622

The algorithm returns `true` for `globMatch("", "")` (empty pattern matches
empty text). This means an empty string in the allowlist would match every
empty command. Since empty commands are handled in step 1 (before
allowlist/blocklist check), this is benign. But an empty pattern in the
blocklist would be problematic: `globMatch("", "some command")` traces as:
tx=0, px=0, px >= len(pattern) (0), no star, return false. So empty pattern
only matches empty text. This is correct.

However, `globMatch("*", "")` returns true (star matches zero characters after
consuming trailing stars). This means `blocklist: ["*"]` would block everything.
This is intentionally powerful but dangerous. Document the behavior and consider
a validation warning for `"*"` in blocklist.

---

#### SE-02-P2.4: `Pack` copy in `Register()` is shallow — slices shared

**Location**: Plan §5.3 `Register()`, line 852

```go
cp := p // Copy to prevent mutation
r.packs[p.ID] = &cp
```

This copies the `Pack` struct, but `Keywords`, `Safe`, and `Destructive`
slices still share underlying arrays with the caller's original. If the caller
later appends to the original slice (unlikely in the `init()` pattern but
possible), the registered pack's slices could see the appended elements.

**Recommendation**: Deep-copy the slices:
```go
cp.Keywords = append([]string{}, p.Keywords...)
cp.Safe = append([]SafePattern{}, p.Safe...)
cp.Destructive = append([]DestructivePattern{}, p.Destructive...)
```
Or, since the freeze invariant makes mutation after registration impossible,
document that the shallow copy is intentional and safe given the init-time
registration pattern.

---

#### SE-02-P2.5: `processEnv` parsed to map on every evaluation call

**Location**: Plan §5.8 `Detect()`, line 1743

```go
processEnvMap := parseEnv(processEnv)
```

If `callerEnv` comes from `os.Environ()` (typically 50-200 entries), this
allocates a new map on every call. For the expected call frequency (seconds
between calls), this is negligible. But if batched or benchmarked, the
allocation dominates.

**Recommendation**: Consider caching the parsed process env map in the
pipeline or evalConfig (it's the same for the lifetime of the process in most
use cases). Or accept the allocation and note it in benchmark analysis.

---

#### SE-02-P2.6: Test harness P8 monotonicity check is incomplete

**Location**: Test harness §P8

The monotonicity test checks:
- If Permissive denies → others deny
- If Strict allows → others allow

But it doesn't check:
- If Interactive denies → Strict also denies
- If Interactive allows → Permissive also allows

The current checks are necessary but not sufficient for full monotonicity.
The full check should be: for every assessment,
`restrictiveness(Strict) >= restrictiveness(Interactive) >= restrictiveness(Permissive)`
where `Deny > Ask > Allow`.

**Recommendation**: Add the full ordering check:
```go
restrictiveness := func(d Decision) int { return int(d) } // Deny=1 > Ask=2?
```
Actually, the `Decision` iota order is `Allow=0, Deny=1, Ask=2`. So
numeric comparison doesn't directly give restrictiveness. Define an explicit
ordering and check `sd >= id >= pd` for all assessments.

---

### P3 — Low

#### SE-02-P3.1: `PacksForKeyword` is O(packs × keywords) per keyword lookup

**Location**: Plan §5.3 `PacksForKeyword()`, lines 911-925

Linear scan over all packs and their keywords for each keyword lookup. With 21
packs and ~2 keywords each, this is fast enough. But the `selectCandidatePacks`
method calls `PacksForKeyword` for each matched keyword, making the total cost
O(matched_keywords × packs × avg_keywords_per_pack).

**Recommendation**: Build a reverse index `map[string][]*Pack` during
`freeze()`. This makes `PacksForKeyword` O(1). Not urgent but cleaner.

---

#### SE-02-P3.2: No test for `ArgMatcher` with invalid glob pattern

**Location**: Plan §5.2.3, test harness

`ArgMatcher.matchArg` falls back to exact match if `path.Match` returns an
error (invalid pattern). This is a reasonable fallback, but no test exercises
it. An invalid glob pattern like `"["` would trigger the fallback silently.

**Recommendation**: Add a test case for `ArgMatcher` with an invalid glob
pattern verifying it falls back to exact match.

---

#### SE-02-P3.3: Golden file format has no versioning

**Location**: Plan §5.10

The golden file format (`command:`, `decision:`, etc.) has no version header.
If the format needs to change (e.g., adding fields), existing golden files
would need migration. Without a version marker, the parser can't distinguish
between old and new formats.

**Recommendation**: Add a `format: v1` header line to golden files. The parser
checks this on load and can report clear errors if the format changes.

---

#### SE-02-P3.4: Test harness SEC2 documents a limitation without resolution

**Location**: Test harness §SEC2

The allowlist bypass test correctly identifies that allowlist matching on raw
text short-circuits before parsing. A command like
`"git status\ngit push --force"` could match allowlist `"git status *"` and
bypass detection of the embedded force push. The test notes this as a "known
trade-off."

This is architecturally sound (documented in the architecture doc as
intentional), but the test should verify the specific behavior rather than just
documenting it. Specifically: verify that the force push IS detected when no
allowlist is configured, and IS NOT detected when the allowlist matches.

---

#### SE-02-P3.5: `isEmptyOrWhitespace` doesn't handle all Unicode whitespace

**Location**: Plan §5.5, lines 1403-1410

The function checks `' '`, `'\t'`, `'\n'`, `'\r'`. It doesn't handle
Unicode whitespace characters like `\u00A0` (non-breaking space), `\u2003`
(em space), etc. These are unlikely in real-world command inputs from LLMs,
but a string consisting entirely of Unicode whitespace would pass the empty
check and proceed to blocklist/allowlist matching. Since blocklist won't
match (no content), it would proceed to pre-filter (no keywords), and
return Allow. So the behavior is correct even without handling Unicode
whitespace — it just takes a longer path to the same result.

**Recommendation**: Either use `unicode.IsSpace()` for completeness, or
document that only ASCII whitespace is considered "empty."

---

## Summary

| Priority | Count | Key Themes |
|----------|-------|------------|
| P0 | 2 | Nil policy panic; missing exported vars in ParseResult |
| P1 | 5 | Blocklist match opaque; cross-plan type divergence; global env escalation; warning conversion inconsistency; subset automaton cost |
| P2 | 6 | Keyword substring matching; silent config errors; glob edge cases; shallow copy; per-call allocation; monotonicity test gap |
| P3 | 5 | Reverse index optimization; invalid glob test; golden file versioning; SEC2 test resolution; Unicode whitespace |

**Overall assessment**: The plan is very thorough and well-structured. The
pipeline orchestration logic is clearly specified with detailed code. The
matcher builder DSL is well-designed and will make pack authoring clean. The
golden file infrastructure is a strong foundation for regression testing.

The two P0 findings need immediate resolution: (1) nil policy panics are a
simple defensive check but important for API robustness, and (2) the missing
exported vars interface between plan 01 and plan 02 blocks a core feature
(dataflow-based env detection). The P1 findings are mostly about cross-plan
consistency and observability — important for correctness but not blocking.

The plan correctly inherits and extends the plan 01 review's findings
(`DataflowResolved`, `RawArgs` fields), but these changes need to be
reconciled with plan 01's canonical type definitions.
