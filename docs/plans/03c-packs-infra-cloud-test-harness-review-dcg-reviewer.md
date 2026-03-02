# Review: 03c-packs-infra-cloud-test-harness (dcg-reviewer)

- Source doc: `docs/plans/03c-packs-infra-cloud-test-harness.md`
- Reviewed commit: ce2f53a20fbd6d96afae9537baf0cc14f248086d
- Reviewer: dcg-reviewer

## Findings

### P2 - Missing regression test for ansible safe-pattern shadowing edge case

**Problem**
Current ansible-focused tests validate reachability and non-panics, but do not assert that safe module matching cannot preempt destructive detection when safe tokens appear inside `-a` payloads (see [docs/plans/03c-packs-infra-cloud-test-harness.md:320](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/03c-packs-infra-cloud-test-harness.md:320) through [docs/plans/03c-packs-infra-cloud-test-harness.md:347](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/03c-packs-infra-cloud-test-harness.md:347) and [docs/plans/03c-packs-infra-cloud-test-harness.md:464](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/03c-packs-infra-cloud-test-harness.md:464) through [docs/plans/03c-packs-infra-cloud-test-harness.md:488](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/03c-packs-infra-cloud-test-harness.md:488)). This leaves the P1 matcher bug class unguarded.

**Required fix**
Add deterministic tests that include lexical overlap cases such as `ansible all -m shell -a 'rm -rf /tmp/setup'` and assert the command is classified by the destructive ansible rule set (not by `ansible-gather-safe`). Include both pattern-level and full pack-evaluation coverage so safe short-circuit behavior is explicitly validated.

---

## Summary

1 findings: 0 P0, 0 P1, 1 P2, 0 P3

**Verdict**: Approved with revisions
