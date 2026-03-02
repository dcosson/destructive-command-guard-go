# Review: 03c-packs-infra-cloud (dcg-reviewer)

- Source doc: `docs/plans/03c-packs-infra-cloud.md`
- Reviewed commit: ce2f53a20fbd6d96afae9537baf0cc14f248086d
- Reviewer: dcg-reviewer

## Findings

### P1 - Ansible safe-module matcher can short-circuit destructive commands

**Problem**
`ansible-gather-safe` uses unanchored content matching for module names (`SQLContent("setup")`, `"gather_facts"`, etc.) at [docs/plans/03c-packs-infra-cloud.md:687](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/03c-packs-infra-cloud.md:687) through [docs/plans/03c-packs-infra-cloud.md:692](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/03c-packs-infra-cloud.md:692). Because `SQLContent` scans all argument/flag values, safe tokens appearing inside `-a` payloads can satisfy the safe matcher even when `-m` is destructive (for example shell/command), and safe patterns short-circuit destructive matching by framework contract. This creates a real false-negative path.

**Required fix**
Make safe-module detection exact on module values instead of broad substring content matching, using anchored module checks (for example `SQLContent("^setup$")`, `^gather_facts$`, `^ping$`, `^debug$`, `^stat$`) or an explicit flag-value-equality matcher for `-m`/`--module-name`. Update the documented matching contract in §5.3.1 to state exact module-value semantics.

---

## Summary

1 findings: 0 P0, 1 P1, 0 P2, 0 P3

**Verdict**: Approved with revisions
