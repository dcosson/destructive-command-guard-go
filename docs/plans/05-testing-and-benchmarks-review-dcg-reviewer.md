# Review: 05-testing-and-benchmarks (dcg-reviewer)

- Source doc: `docs/plans/05-testing-and-benchmarks.md`
- Reviewed commit: 5dbf12eb757c00bf7f02c1e1d5bd3b555b79ad80
- Reviewer: dcg-reviewer

## Findings

### P1 - Comparison CI runner does not actually provide UPSTREAM_BINARY to tests

**Problem**
The comparison test reads `UPSTREAM_BINARY` from environment (`docs/plans/05-testing-and-benchmarks.md:394`, `docs/plans/05-testing-and-benchmarks.md:397`), but the runner script only sets a shell variable and passes an undefined test flag (`docs/plans/05-testing-and-benchmarks.md:553`, `docs/plans/05-testing-and-benchmarks.md:557`). As written, `go test` will not receive the env var and the comparison test can skip unexpectedly, silently weakening the Tier 3 gate.

**Required fix**
Change the runner to export/prefix the env var for `go test` (for example: `UPSTREAM_BINARY="$UPSTREAM_BINARY" go test ...`) and remove the unsupported `-upstream-binary` argument unless a real test flag is implemented.

---

## Summary

1 findings: 0 P0, 1 P1, 0 P2, 0 P3

**Verdict**: Approved with revisions
