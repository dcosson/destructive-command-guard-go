# Review: 01-treesitter-integration (dcg-coder-1)

- Source doc: `docs/plans/01-treesitter-integration.md`
- Reviewed commit: 5dbf12e
- Reviewer: dcg-coder-1

## Findings

### P1 - ParseResult Contract Still Diverges From Plan 02 Foundation API

**Problem**
Plan 01 still defines `ParseResult` with local `Warnings []Warning` and no `ExportedVars` field (`docs/plans/01-treesitter-integration.md:278`), while plan 02 specifies the required cross-plan contract as `Warnings []guard.Warning` plus `ExportedVars map[string][]string` (`docs/plans/02-matching-framework.md:1612`). This is a direct foundation-chain API mismatch.

**Required fix**
Update plan 01 type definitions to match the plan 02 contract: shared warning type from guard and `ExportedVars` on `ParseResult`, with explicit producer responsibilities in extractor/dataflow sections.

---

## Summary

1 findings: 0 P0, 1 P1, 0 P2, 0 P3

**Verdict**: Approved with revisions
