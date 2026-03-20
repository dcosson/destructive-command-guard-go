# Seam Review: eval-pipeline (vertical) — tall-vale

- Mode: vertical
- Seam: eval-pipeline (command evaluation request end-to-end)
- Reviewed commit: 6b394aa
- Reviewer: tall-vale
- Plan docs reviewed:
  - `docs/plans/06-rule-categories.md` (the plan under review)
  - `docs/plans/00-architecture.md` (architecture — defines canonical types)
  - `docs/plans/02-matching-framework.md` (defines pipeline, Policy, Pack, Rule)
  - `docs/plans/04-api-and-cli.md` (defines guard package API, CLI modes)

## Seam Boundaries Analyzed

### Seam 1: `guard` package ↔ `internal/eval` (Config + Result types)

**Plan docs**: 06 §4.5/§4.7 (new Config), 04 §3 (guard API), 02 §8 (pipeline)

| Category | Status | Details |
|----------|--------|---------|
| Interface signatures | PASS | `Pipeline.Run(command, Config)` unchanged; Config fields change but call site is internal |
| Data formats/types | FAIL | See finding P1-1 below |
| Lifecycle ordering | PASS | No lifecycle changes |
| Configuration contracts | PASS | `evalConfig.toInternal()` bridges guard→eval; plan covers both sides |
| Error handling | PASS | No error path changes |

### Seam 2: `internal/eval` ↔ `internal/packs` (Pack/Rule iteration)

**Plan docs**: 06 §4.2 (Pack.Rules rename), 02 §4 (pipeline iterates pack.Destructive)

| Category | Status | Details |
|----------|--------|---------|
| Interface signatures | FAIL | See finding P1-2 below |
| Data formats/types | PASS | Rule struct change is additive (Category field) |
| Lifecycle ordering | PASS | N/A |
| Configuration contracts | PASS | N/A |
| Error handling | PASS | N/A |

### Seam 3: `internal/eval` ↔ `internal/evalcore` (Policy interface)

**Plan docs**: 06 §4.5 (PolicyConfig), 02 §8.6 (Policy interface), 00-architecture §3

| Category | Status | Details |
|----------|--------|---------|
| Interface signatures | PASS | `Policy.Decide(Assessment) Decision` is unchanged; PolicyConfig wraps it |
| Data formats/types | FAIL | See finding P2-1 below |
| Lifecycle ordering | PASS | N/A |
| Configuration contracts | PASS | N/A |
| Error handling | PASS | N/A |

### Seam 4: `cmd/dcg-go` ↔ `guard` (CLI → API)

**Plan docs**: 06 §4.7/§4.8/§4.10 (new options, config, list cmd), 04 §4-6 (CLI modes)

| Category | Status | Details |
|----------|--------|---------|
| Interface signatures | PASS | New options are additive |
| Data formats/types | FAIL | See finding P1-3 below |
| Lifecycle ordering | PASS | N/A |
| Configuration contracts | PASS | Config YAML changes documented in §4.8 |
| Error handling | PASS | N/A |

## Acceptance Criteria Cross-Reference

Plan 06 doesn't have formal acceptance criteria, but the integration test
scenarios in §7 serve as implicit acceptance criteria.

| Acceptance criterion (source doc) | Seams touched | Status | Details |
|-----------------------------------|---------------|--------|---------|
| `rm -rf /tmp/foo` → Allow (destructive-permissive) | CLI→guard→eval→packs→policy | PASS | Request path is fully specified through all layers |
| `cat ~/.ssh/id_rsa` → Deny (privacy-strict) | CLI→guard→eval→packs→policy | PASS | Privacy rule in personal.ssh pack, category propagates through pipeline |
| `osascript send...` → Deny (both, privacy-strict) | CLI→guard→eval→packs→policy | PASS | CategoryBoth enters both lanes; merge logic defined |
| Conflicting-lane explanation test | eval→hook output | FAIL | See finding P2-1 — explanation precedence depends on per-category assessment data that Result must carry |

## Seam Compatibility Matrix

| Seam | Signatures | Data | Lifecycle | Config | Errors | Overall |
|------|-----------|------|-----------|--------|--------|---------|
| guard ↔ eval | PASS | FAIL | PASS | PASS | PASS | FAIL |
| eval ↔ packs | FAIL | PASS | PASS | PASS | PASS | FAIL |
| eval ↔ evalcore | PASS | FAIL | PASS | PASS | PASS | FAIL |
| cmd/dcg-go ↔ guard | PASS | FAIL | PASS | PASS | PASS | FAIL |

## Findings

### P1 — [IG] Architecture doc and plan 02/04 define `Result.Assessment` which plan 06 removes

**Seam**: guard ↔ eval, and cmd/dcg-go ↔ guard
**Category**: Data formats/types

**Problem**
The architecture doc (00-architecture.md:170-176) defines `Result` with a
single `Assessment *Assessment` field. Plan 02 (§8 pipeline, lines 1456,
1389, 1338) calls `policy.Decide(*result.Assessment)` in three places.
Plan 04 consumes `Result.Assessment` in test mode output.

Plan 06 §4.4 removes `Assessment` and replaces it with
`DestructiveAssessment` and `PrivacyAssessment`. This is correct for the new
design, but the plan doesn't call out that plans 02 and 04 (and the
architecture doc) must be updated. Anyone implementing from those docs would
use the old single-Assessment pipeline.

**Required fix**
Add a section to plan 06 listing the cross-document updates needed:
- 00-architecture.md: Update Result struct, Policy note, component diagram
- 02-matching-framework.md: Update pipeline steps 11-12, Result type, policy
  application code
- 04-api-and-cli.md: Update guard.Result definition, test mode output, hook
  mode buildReason

These don't need to be updated now, but the plan should explicitly track them
so implementors know the earlier docs are stale for these types.

---

### P1 — Plan 02 pipeline code references `pack.Destructive` which plan 06 renames to `pack.Rules`

**Seam**: eval ↔ packs
**Category**: Interface signatures

**Problem**
Plan 02 (§8.4, line ~1410) and the current pipeline code
(internal/eval/pipeline.go:146) iterate `pack.Destructive`. Plan 06 §4.2
renames this to `pack.Rules`. The rename is mentioned in §4.2 ("All callers
of pack.Destructive must be updated to pack.Rules") but plan 02's pipeline
code snippets still show the old name. Since plan 02 is the canonical
pipeline specification, implementors could be confused about whether to
follow plan 02 or plan 06.

**Required fix**
Note in plan 06 §4.2 or §5 that plan 02's pipeline code is superseded by
plan 06 for the `pack.Destructive` → `pack.Rules` rename. A one-line note is
sufficient — the implementor will update the actual code, not the plan doc.

---

### P2 — Hook output JSON lacks category information for downstream consumers

**Seam**: cmd/dcg-go ↔ external (Claude Code)
**Category**: Data formats/types

**Problem**
The hook mode output (cmd/dcg-go/hook.go) produces a JSON response with
`permissionDecision` and `permissionDecisionReason` fields. Plan 06 §4.9
adds a `[category]` prefix to the reason string, but the structured JSON
output doesn't include the category or per-category assessments. External
consumers (like Claude Code) that parse the JSON can only see the final
merged decision, not which dimension caused it.

This may be fine for the current Claude Code hook protocol (which only uses
`permissionDecision` and `permissionDecisionReason`), but it means the
dual-policy information is lost in the JSON wire format.

**Required fix**
Decide whether the hook JSON output should include category metadata (e.g.,
`"category": "privacy"` or `"assessments": {"destructive": null, "privacy":
{"severity": "High"}}`) or if the string prefix is sufficient. Document the
decision either way. If the Claude Code protocol doesn't support extra
fields, note that as the reason for not including them.

---

### P3 — `guard.Packs()` PackInfo struct changes not fully specified

**Seam**: guard ↔ cmd/dcg-go (list packs command)
**Category**: Data formats/types

**Problem**
Plan 06 §4.10 says `PackInfo` is "updated to include per-category counts
(replacing SafeCount/DestrCount with DestructiveCount/PrivacyCount/BothCount)"
but doesn't show the updated struct definition. The current `PackInfo` (guard/guard.go:60-68) has `SafeCount` and `DestrCount`. The new struct needs to
account for:
- `SafeCount` (exemption rules — kept? removed?)
- `DestructiveCount`, `PrivacyCount`, `BothCount` (new fields)
- `HasEnvSensitive` (kept? The rename from Destructive→Rules affects the
  helper that computes this)

**Required fix**
Show the updated `PackInfo` struct definition in §4.10.

---

## Summary

4 seam boundaries analyzed, 4 findings: 0 P0, 2 P1, 1 P2, 1 P3

Seam compatibility: 0/4 seams fully compatible (all have minor data format
mismatches due to cross-doc type changes, but no blocking interface
incompatibilities)

**Verdict**: Seams compatible with revisions — the P1s are documentation
tracking issues (earlier plan docs reference types that plan 06 changes),
not actual interface incompatibilities in the design.
