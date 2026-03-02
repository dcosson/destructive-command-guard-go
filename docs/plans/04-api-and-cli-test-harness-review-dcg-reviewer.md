# Review: 04-api-and-cli-test-harness (dcg-reviewer)

- Source doc: `docs/plans/04-api-and-cli-test-harness.md`
- Reviewed commit: 5dbf12eb757c00bf7f02c1e1d5bd3b555b79ad80
- Reviewer: dcg-reviewer

## Findings

### P1 - MQ4 expectation contradicts explicit config-failure contract

**Problem**
The manual QA section expects `DCG_CONFIG=/nonexistent` to "work normally with defaults" (`docs/plans/04-api-and-cli-test-harness.md:955`, `docs/plans/04-api-and-cli-test-harness.md:956`), but the same document states explicit missing config is fatal (`docs/plans/04-api-and-cli-test-harness.md:870`, `docs/plans/04-api-and-cli-test-harness.md:873`, `docs/plans/04-api-and-cli-test-harness.md:986`). This contradiction will produce false-negative QA outcomes and incorrect acceptance signaling.

**Required fix**
Update MQ4 to expect fatal failure (non-zero exit) for explicit missing `DCG_CONFIG`, and keep "defaults fallback" only for the implicit default-path-missing case.

---

## Summary

1 findings: 0 P0, 1 P1, 0 P2, 0 P3

**Verdict**: Approved with revisions
