# Review: 03g-packs-macos (dcg-reviewer)

- Source doc: `docs/plans/03g-packs-macos.md`
- Reviewed commit: 5dbf12eb757c00bf7f02c1e1d5bd3b555b79ad80
- Reviewer: dcg-reviewer

## Findings

### P2 - Known-broken dscl pattern is still counted as implemented coverage

**Problem**
The document explicitly marks D7 (`dscl-delete`) as non-functional and skipped in tests (`docs/plans/03g-packs-macos.md:802`, `docs/plans/03g-packs-macos.md:811`, `docs/plans/03g-packs-macos.md:1051`), but the summary table still counts `macos.system` as having 13 destructive patterns (`docs/plans/03g-packs-macos.md:46`). This overstates effective coverage for a critical command family and can mislead downstream planning and acceptance criteria.

**Required fix**
Either (a) make RawArgContent support a hard prerequisite and update this plan to use it (removing the known-broken state), or (b) explicitly remove D7 from claimed implemented counts/coverage and add a blocking dependency note with revised acceptance metrics until D7 is functional.

---

## Summary

1 findings: 0 P0, 0 P1, 1 P2, 0 P3

**Verdict**: Approved with revisions
