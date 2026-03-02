# Review: 01-treesitter-integration-test-harness (dcg-coder-1)

- Source doc: `docs/plans/01-treesitter-integration-test-harness.md`
- Reviewed commit: 5dbf12e
- Reviewer: dcg-coder-1

## Findings

### P2 - Harness Does Not Lock The ParseResult Boundary Contract

**Problem**
The harness covers dataflow value behavior (`docs/plans/01-treesitter-integration-test-harness.md:198`) but does not define explicit assertions for the parse boundary contract now required by plan 02 (`ParseResult.ExportedVars` + shared warning semantics). Without dedicated boundary tests, the foundation API drift can reappear silently.

**Required fix**
Add contract tests that assert `ParseResult` carries exported vars and warning payloads in the exact cross-plan form expected by the matching pipeline, including mixed warning scenarios (partial parse + extractor panic recovery paths).

---

## Summary

1 findings: 0 P0, 0 P1, 1 P2, 0 P3

**Verdict**: Approved with revisions
