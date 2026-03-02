# Review: 03a-packs-core (dcg-coder-1)

- Source doc: `docs/plans/03a-packs-core.md`
- Reviewed commit: 5dbf12e
- Reviewer: dcg-coder-1

## Findings

### P1 - Regex-Like ArgContent Usage Creates False-Negative Pack Rules

**Problem**
Several destructive patterns use `ArgContent` with regex-style anchors (`"^:"`, `"^\\+"`, `"^0$"`) (`docs/plans/03a-packs-core.md:735`, `docs/plans/03a-packs-core.md:750`, `docs/plans/03a-packs-core.md:1182`). In plan 02, `ArgContent` is substring matching, not regex (`docs/plans/02-matching-framework.md:611`), so these patterns will not match intended commands reliably.

**Required fix**
Replace these with semantically correct matchers (`ArgContentRegex`, `ArgPrefix`, or exact `Arg` forms as appropriate) and update the pack authoring guidance to forbid regex literals in `ArgContent`.

---

## Summary

1 findings: 0 P0, 1 P1, 0 P2, 0 P3

**Verdict**: Approved with revisions
