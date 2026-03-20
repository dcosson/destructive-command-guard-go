# Review: docs/plans/06-rule-categories.md (Round 2)

- Source doc: `docs/plans/06-rule-categories.md`
- Reviewed commit: de275b6
- Reviewer: tall-vale

## Findings

### P1 — `Pack.Destructive` field name is misleading for privacy-categorized rules

**Problem**
The current `Pack` struct (internal/packs/registry.go:21-29) has two rule
slices: `Safe` (exemption patterns) and `Destructive` (flagged patterns). The
plan adds `Category` to `Rule` but never addresses the fact that
privacy-categorized rules would be stored in a field called `Destructive`.
The pipeline (internal/eval/pipeline.go:146) iterates `pack.Destructive` —
so a rule like "SSH private key access" would live in `pack.Destructive`,
which is semantically wrong after this change.

**Required fix**
Rename `Pack.Destructive` to `Pack.Rules` (or `Pack.Flagged`). The field
holds all rules that trigger matches, regardless of category. `Pack.Safe`
stays as-is (exemptions don't need categories). Update all callers
(pipeline.go, registry.go, guard.go, packs.go, all pack definitions).

---

### P1 — Default policy behavior unspecified

**Problem**
§4.5 and §4.7 state both `DestructivePolicy` and `PrivacyPolicy` are
"required" and the old `WithPolicy` is removed. But the current code has a
sensible default: `guard.defaultConfig()` returns `InteractivePolicy()`, and
`guard.Evaluate()` falls back to it. The plan doesn't specify what
`defaultConfig()` should return when there are two policy fields. Without
defaults, bare calls like `guard.Evaluate("rm -rf /")` would panic (nil
policy dereference in `PolicyConfig.Decide`).

**Required fix**
Specify that both policies default to `InteractivePolicy()` in
`defaultConfig()`. "Required" should mean "must be non-nil at evaluation
time" (enforced by defaults), not "callers must explicitly pass both".

---

### P2 — Test mode `--policy` flag not addressed

**Problem**
The `dcg-go test` command (cmd/dcg-go/test.go) currently accepts
`--policy strict` to set a single policy. §4.8 covers the YAML config
changes but doesn't address the test mode CLI flags. After removing the
single `policy` concept, users need a way to specify per-category policies
from the command line for ad-hoc testing.

**Required fix**
Add `--destructive-policy` and `--privacy-policy` flags to test mode (or
a shorthand like `--dp` and `--pp`). A bare `--policy X` could be kept as
sugar that sets both to X for convenience.

---

### P2 — Open Questions §9 is stale

**Problem**
§9 contains three open questions. Question 2 ("Should `dcg-go packs` show
category distribution?") is now fully answered by §4.10 which replaces
`packs` with `list packs`/`list rules`. Question 1 ("Should test mode show
categories?") is implicitly answered by the category prefix in hook output
(§4.9) but not explicitly resolved for test mode.

**Required fix**
Remove or resolve all three questions. Q1: yes, test mode should show
category (add to §4.10 or note alongside test mode flag changes). Q2:
resolved by §4.10, delete. Q3: keep as a "future directions" note if desired,
or delete.

---

### P3 — Blocklist matches get implicit CategoryDestructive via normalization

**Problem**
The blocklist path in `Pipeline.Run()` (internal/eval/pipeline.go:52-70)
creates a synthetic Match with hardcoded fields and no Category. After this
change, normalization in §4.11 would set it to `CategoryDestructive`. This
is probably correct behavior (blocklist = "definitely block this") but it's
worth being explicit about since blocklist bypasses category-based policy
entirely (it always returns Deny).

**Required fix**
Add a brief note to §4.11 that blocklist/allowlist bypass the dual-policy
pipeline entirely (they short-circuit before category aggregation), so their
Match category is cosmetic. This avoids confusion during implementation.

---

## Summary

5 findings: 0 P0, 2 P1, 2 P2, 1 P3

**Verdict**: Approved with revisions — the P1s are straightforward naming and default-value issues that should be resolved before implementation.
