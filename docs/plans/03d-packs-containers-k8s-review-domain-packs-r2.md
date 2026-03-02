# Review: 03d-packs-containers-k8s (domain-packs-r2)

- Source doc: `docs/plans/03d-packs-containers-k8s.md`
- Reviewed commit: 5514fa1
- Reviewer: domain-packs-r2
- Round: 2

## Findings

### P2 - kubectl apply --prune --all-namespaces not escalated to Critical

**Problem**
D7b `kubectl-apply-prune` (line ~1038) matches `kubectl apply --prune` at High severity. But `kubectl apply --prune --all-namespaces -f manifests/` prunes (deletes) resources across ALL namespaces that don't appear in the applied manifests — this could remove resources in system namespaces (`kube-system`, `kube-public`) and is comparable in impact to D7c `kubectl-delete-all-namespaces` at Critical.

D7c only matches `ArgAt(0, "delete")`, so it doesn't catch `kubectl apply --prune --all-namespaces`. The command falls through to D7b at High, which under-classifies a potentially cluster-destroying operation.

For consistency with the existing escalation pattern (delete + --all-namespaces = Critical), apply + --prune + --all-namespaces should also be Critical.

**Required fix**
Add a Critical-severity pattern for `kubectl apply --prune --all-namespaces`:
```go
{
    Name: "kubectl-apply-prune-all-namespaces",
    Match: packs.And(
        packs.Name("kubectl"),
        packs.ArgAt(0, "apply"),
        packs.Flags("--prune"),
        packs.Or(
            packs.Flags("--all-namespaces"),
            packs.Flags("-A"),
        ),
    ),
    Severity: guard.Critical,
    ...
}
```
Place it before D7b so it takes precedence, and add `Not(--all-namespaces)` / `Not(-A)` to D7b.

---

### P3 - kubectl scale --replicas=0 known gap documented but no golden file entry for tracking

**Problem**
§5.3.1 (line ~1188) documents that `kubectl scale --replicas=0` is classified as safe (S4) but effectively causes a service outage. The plan correctly identifies this as a known limitation (framework can't inspect flag values) and defers to v2.

However, there's no golden file entry to track this known false negative. Adding `kubectl scale --replicas=0 deployment/my-app → Allow (KNOWN FALSE NEGATIVE)` to §6.3 would document the expected behavior for regression testing and ensure v2 improvements don't accidentally break other behavior.

**Required fix**
Add to §6.3:
```
kubectl scale --replicas=0 deployment/my-app  → Allow (KNOWN FALSE NEGATIVE — S4 can't inspect --replicas value)
```

---

## Summary

2 findings: 0 P0, 0 P1, 1 P2, 1 P3

**Verdict**: Approved with revisions

The plan is substantially correct after R1 incorporation (31 findings, well-addressed). The Docker dual-syntax matching, Compose dual-naming, and kubectl resource-type severity tiers are all well-designed. The split env-sensitivity policy (Docker/Compose NOT env-sensitive, kubectl/Helm ARE) is well-reasoned. The P2 finding about `kubectl apply --prune --all-namespaces` is the only functional gap — a potentially cluster-destroying command classified at High instead of Critical.
