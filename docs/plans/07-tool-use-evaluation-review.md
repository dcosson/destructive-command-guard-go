# Review: 07-tool-use-evaluation

- Source doc: `docs/plans/07-tool-use-evaluation.md`
- Reviewed commit: `f25d2fdf017b97a4dcd7022733fa6e27bf3efb7c`
- Reviewer: `fast-dale`

## Findings

### P1 - Normalization failures are specified to fail open instead of producing an indeterminate decision

**Problem**
The plan says missing required fields for known tools should "produce a warning and return empty commands (allow)" ([docs/plans/07-tool-use-evaluation.md](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/07-tool-use-evaluation.md#L179), [docs/plans/07-tool-use-evaluation.md](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/07-tool-use-evaluation.md#L181)), and `EvaluateToolUse()` then returns `Allow` whenever normalization yields no commands ([docs/plans/07-tool-use-evaluation.md](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/07-tool-use-evaluation.md#L221), [docs/plans/07-tool-use-evaluation.md](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/07-tool-use-evaluation.md#L224)). That is materially weaker than the current Bash path: when `Pipeline.Run()` cannot reliably understand input, it produces an indeterminate assessment and lets policy decide rather than silently allowing it (`internal/eval/pipeline.go`).

For a hook-facing API this becomes a policy bypass. A known tool with an unexpected schema, a nil field, or a future Claude payload shape change would be auto-allowed even when the tool use is security-sensitive.

**Required fix**
Known tools with malformed or incomplete structured input must not collapse to unconditional allow. The plan should introduce an explicit normalization-error path that yields an indeterminate result (or equivalent warning-backed `Ask`/policy-driven outcome), and add tests covering missing required fields, wrong field types, and unexpected payload shapes.

---

### P1 - The `RawText` contract is inconsistent with current blocklist/allowlist and matcher semantics

**Problem**
The plan first says tool use bypasses the pre-filter because commands are synthesized ([docs/plans/07-tool-use-evaluation.md](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/07-tool-use-evaluation.md#L46), [docs/plans/07-tool-use-evaluation.md](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/07-tool-use-evaluation.md#L47)), but later says `RunCommands()` should scan `RawText` with the existing pre-filter and blocklist/allowlist logic ([docs/plans/07-tool-use-evaluation.md](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/07-tool-use-evaluation.md#L203), [docs/plans/07-tool-use-evaluation.md](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/07-tool-use-evaluation.md#L207)). The normalization section then under-specifies `RawText` as only needing to "include the path" ([docs/plans/07-tool-use-evaluation.md](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/07-tool-use-evaluation.md#L153), [docs/plans/07-tool-use-evaluation.md](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/07-tool-use-evaluation.md#L176)).

That is not enough to preserve current semantics. Today `Pipeline.Run()` applies blocklist/allowlist globs to the full command string and several rules/safe-rules inspect `RawText` for flags or command text (`internal/eval/pipeline.go`, `internal/packs/matcher.go`). If normalized tool-use `RawText` is only a path fragment, a user blocklist like `cat *` will not match `Read`, and any `RawTextContains`/`RawTextRegex`-based logic stops being equivalent to shell evaluation.

**Required fix**
The plan should make `RawText` a fully specified synthetic command string for every normalized tool, not just a path-bearing blob, and it should explicitly state that `RunCommands()` reuses the same blocklist/allowlist, pre-filter, and `RawText*` matcher behavior as `Run()`. Add regression tests that prove custom allowlists/blocklists behave identically for Bash-vs-tool equivalents.

---

### P1 - Removing `guard.Evaluate()` breaks the published API and invalidates existing docs/tests without a migration plan

**Problem**
The plan removes `guard.Evaluate()` outright ([docs/plans/07-tool-use-evaluation.md](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/07-tool-use-evaluation.md#L28), [docs/plans/07-tool-use-evaluation.md](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/07-tool-use-evaluation.md#L233)) and treats caller migration as limited to a small set of files ([docs/plans/07-tool-use-evaluation.md](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/07-tool-use-evaluation.md#L238), [docs/plans/07-tool-use-evaluation.md](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/07-tool-use-evaluation.md#L243)). In the current repo, `guard.Evaluate()` is the documented public API in [CLAUDE.md](/Users/dcosson/h2home/projects/destructive-command-guard-go/CLAUDE.md#L22), in [README.md](/Users/dcosson/h2home/projects/destructive-command-guard-go/README.md#L140), and throughout the existing public/integration test surface.

This is more than a local refactor: it breaks downstream callers and contradicts the already-approved API shape in earlier planning docs unless those docs and consumers are revised together.

**Required fix**
Either keep `guard.Evaluate()` as a compatibility wrapper around `EvaluateToolUse("Bash", ...)`, or expand the plan into an explicit breaking-change migration that inventories and updates every public doc, library example, test harness, and dependent repo. As written, the migration scope is too small for a public API removal.

---

## Summary

3 findings: 0 P0, 3 P1, 0 P2, 0 P3

**Verdict**: Approved with revisions
