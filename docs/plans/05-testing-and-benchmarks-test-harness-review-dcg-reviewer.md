# Review: 05-testing-and-benchmarks-test-harness (dcg-reviewer)

- Source doc: `docs/plans/05-testing-and-benchmarks-test-harness.md`
- Reviewed commit: 5dbf12eb757c00bf7f02c1e1d5bd3b555b79ad80
- Reviewer: dcg-reviewer

## Findings

### P1 - Self-comparison oracle uses helper that assumes upstream CLI shape

**Problem**
O1 builds the Go binary and then routes it through `runUpstream` (`docs/plans/05-testing-and-benchmarks-test-harness.md:498`, `docs/plans/05-testing-and-benchmarks-test-harness.md:499`, `docs/plans/05-testing-and-benchmarks-test-harness.md:506`). In plan 05, `runUpstream` is defined to invoke `binary check <command>` (`docs/plans/05-testing-and-benchmarks.md:569`, `docs/plans/05-testing-and-benchmarks.md:571`), but the Go CLI contract in plan 04 uses `test`, not `check`. This makes the self-comparison oracle structurally incompatible unless special adapter logic is added.

**Required fix**
Define a dedicated Go-self runner for O1 (calling `guard.Evaluate` directly or invoking `dcg-go test --json`) instead of reusing the upstream adapter path; keep `runUpstream` only for the pinned Rust binary interface.

---

## Summary

1 findings: 0 P0, 1 P1, 0 P2, 0 P3

**Verdict**: Approved with revisions
