# 03d: Containers & Kubernetes Packs — Security/Correctness Review

**Reviewer**: dcg-alt-reviewer (independent review)
**Plan**: [03d-packs-containers-k8s.md](./03d-packs-containers-k8s.md)
**Test Harness**: [03d-packs-containers-k8s-test-harness.md](./03d-packs-containers-k8s-test-harness.md)
**Date**: 2026-03-01
**Focus Areas**: Severity assignments for container/k8s operations, safe
pattern robustness, kubectl delete coverage, helm rollback severity
appropriateness

---

## Summary

16 findings: 2 P0, 4 P1, 6 P2, 4 P3.

Both P0s are safe-pattern-blocks-destructive bugs: helm's
`helm-install-upgrade-safe` (S2) matches ALL `helm upgrade` commands
including those with `--force` and `--reset-values`, making D3 and D4
unreachable. Similarly, kubectl's `kubectl-modify-safe` (S4) matches
`kubectl apply` without excluding the `--prune` flag, which deletes
resources not present in the applied manifests. Both would be caught by
the P2 property test (safe patterns never block destructive reachability
commands) but represent plan-level bugs.

Notable P1s: `kubectl delete --all-namespaces` not escalated, missing
`docker network prune` pattern (inconsistent with other prune patterns),
and concerns about `kubectl delete secret` severity.

Helm rollback at Medium is appropriate — it's reversible and the severity
correctly reflects that it can cause disruption but not data loss.

---

## P0: Critical Security/Correctness Issues

### CK-P0.1: Helm S2 `helm-install-upgrade-safe` blocks D3 and D4 — destructive patterns unreachable

**Location**: Plan §5.4, S2, D3 `helm-upgrade-force`, D4 `helm-upgrade-reset-values`

**Issue**: S2 (`helm-install-upgrade-safe`) matches any `helm upgrade` command:
```go
packs.And(
    packs.Name("helm"),
    packs.Or(
        packs.ArgAt(0, "install"),
        packs.ArgAt(0, "upgrade"),  // matches ALL helm upgrade commands
    ),
),
```

D3 matches `helm upgrade --force` (delete-and-recreate resources).
D4 matches `helm upgrade --reset-values` (discard custom values).

Because safe patterns are evaluated before destructive patterns within
a pack, S2 matches first and D3/D4 are never reached:

```
helm upgrade my-release my-chart --force
  → S2: Name("helm") ✓, ArgAt(0, "upgrade") ✓ → SAFE
  → D3 never evaluated → false negative

helm upgrade my-release my-chart --reset-values
  → S2: Name("helm") ✓, ArgAt(0, "upgrade") ✓ → SAFE
  → D4 never evaluated → false negative
```

**Note**: The P2 property test (`TestPropertyContainerK8sSafePatternsNeverBlockDestructive`)
WOULD catch this bug — S2 would match D3/D4 reachability commands, causing
assertion failure. But the plan itself has the pattern ordering wrong.

**Fix**: Add Not exclusions to S2:
```go
packs.And(
    packs.Name("helm"),
    packs.Or(
        packs.ArgAt(0, "install"),
        packs.ArgAt(0, "upgrade"),
    ),
    packs.Not(packs.Flags("--force")),
    packs.Not(packs.Flags("--reset-values")),
),
```

**Severity**: P0 — two destructive patterns (D3/D4) are unreachable.
`helm upgrade --force` causes momentary downtime; `helm upgrade --reset-values`
can break release configurations. Both classified as safe.

---

### CK-P0.2: kubectl S4 `kubectl-modify-safe` doesn't exclude `--prune` for `kubectl apply`

**Location**: Plan §5.3, S4 `kubectl-modify-safe`

**Issue**: S4 marks `kubectl apply` as safe:
```go
packs.Or(
    packs.ArgAt(0, "apply"),
    ...
),
```

But `kubectl apply --prune` deletes resources that are NOT present in the
applied manifests. `kubectl apply --prune --all` can delete ALL unmanaged
resources in the namespace. This is a destructive operation classified as
safe.

```
kubectl apply -f manifests/ --prune --all
  → S4: Name("kubectl") ✓, ArgAt(0, "apply") ✓ → SAFE
  → No destructive pattern exists for kubectl apply --prune
  → Command classified as safe — resources deleted silently
```

**Fix**: Add `Not(Flags("--prune"))` to S4, and add a destructive pattern:
```go
{
    Name: "kubectl-apply-prune",
    Match: packs.And(
        packs.Name("kubectl"),
        packs.ArgAt(0, "apply"),
        packs.Flags("--prune"),
    ),
    Severity:     guard.High,
    Confidence:   guard.ConfidenceHigh,
    Reason:       "kubectl apply --prune deletes resources not in the applied manifests",
    EnvSensitive: true,
}
```

**Severity**: P0 — `kubectl apply --prune` can delete arbitrary resources
in a namespace but is classified as safe.

---

## P1: High-Priority Issues

### CK-P1.1: `kubectl delete --all-namespaces` should escalate severity

**Location**: Plan §5.3, §13 Open Questions Q3

**Issue**: `kubectl delete pods --all-namespaces` deletes pods across ALL
namespaces. Currently matches D8 catch-all at Medium. But cross-namespace
deletion is significantly more dangerous — it affects every namespace
including system namespaces (kube-system, monitoring, etc.).

The plan defers this to v2 (Open Question 3). However, `--all-namespaces`
turns any Medium `kubectl delete` into a cluster-wide operation. An LLM
agent generating `kubectl delete pods --all-namespaces` is almost certainly
a mistake.

**Fix**: Add a pattern that escalates severity for `--all-namespaces`:
```go
{
    Name: "kubectl-delete-all-namespaces",
    Match: packs.And(
        packs.Name("kubectl"),
        packs.ArgAt(0, "delete"),
        packs.Or(
            packs.Flags("--all-namespaces"),
            packs.Flags("-A"),
        ),
    ),
    Severity:     guard.Critical,
    Confidence:   guard.ConfidenceHigh,
    ...
    EnvSensitive: true,
}
```

This pattern should be placed BEFORE D1-D8 so it catches all
`kubectl delete --all-namespaces` commands regardless of resource type.

**Severity**: P1 — cluster-wide deletion classified at same level as
single-resource deletion.

---

### CK-P1.2: Missing `docker network prune` destructive pattern

**Location**: Plan §5.1, §5.1.1

**Issue**: Docker prune operations have inconsistent coverage:

| Command | Pattern | Severity |
|---------|---------|----------|
| `docker container prune` | D8 | Medium |
| `docker image prune` | D7 | Medium |
| `docker volume prune` | D4 | High |
| `docker system prune` | D2 | High |
| `docker network prune` | **none** | Allowed |

The plan acknowledges this gap (§5.1.1): "Network pruning removes unused
networks, which is generally safe since active networks can't be pruned."

However, network prune with custom networks that containers reference by
name can cause connectivity issues when containers restart. The
inconsistency with other prune patterns (all covered) may confuse users
who expect consistent behavior.

**Fix**: Add `docker-network-prune` at Medium:
```go
{
    Name: "docker-network-prune",
    Match: packs.And(
        packs.Name("docker"),
        packs.ArgAt(0, "network"),
        packs.ArgAt(1, "prune"),
    ),
    Severity:   guard.Medium,
    Confidence: guard.ConfidenceHigh,
    ...
}
```

S8 already excludes `ArgAt(1, "prune")` so there's no safe pattern
conflict.

**Severity**: P1 — inconsistent prune coverage, potential for network
disruption.

---

### CK-P1.3: `kubectl delete secret` at Medium — cascading auth/TLS failures

**Location**: Plan §4.4, §5.3 D8

**Issue**: `kubectl delete secret` matches D8 catch-all at Medium. But
secrets contain TLS certificates, API keys, database credentials, and
other auth material. Deleting a secret causes cascading failures:

1. All pods mounting the secret (via env vars or volumes) fail on restart
2. Ingress controllers lose TLS certificates → HTTPS breaks
3. Service accounts lose credentials → API calls fail

The blast radius of secret deletion is comparable to service deletion
(High) or even deployment deletion (High), not pod deletion (Medium).

**Fix**: Add `secret` and `secrets` to D5 (kubectl-delete-service) or
create a separate D pattern at High severity, and add `secret`/`secrets`
to D8's exclusion list.

**Severity**: P1 — severity under-classification for a resource type
with cascading failure potential.

---

### CK-P1.4: `kubectl scale --replicas=0` classified as safe — production service outage risk

**Location**: Plan §5.3 S4, §5.3.1

**Issue**: S4 includes `packs.ArgAt(0, "scale")` as safe. §5.3.1 says:
"Scaling to zero pods is operational, not destructive — the deployment
still exists and can be scaled back up."

While technically true (the deployment object persists), `kubectl scale
deployment my-app --replicas=0` in production causes a complete service
outage. This is functionally identical to `docker compose stop` (classified
as Medium) and `kubectl cordon` (classified as Medium).

The challenge is distinguishing `--replicas=0` (destructive) from
`--replicas=5` (normal scaling). This would require flag value inspection,
which the matching framework may not support for numeric comparisons.

**Fix options**:
1. Move `scale` from S4 safe to a Medium destructive pattern with
   ConfidenceLow (similar to `ansible-playbook-run`). Over-classifies
   normal scaling but prevents blind zero-scaling.
2. Keep `scale` in S4 but add a specific pattern for `--replicas=0`
   if the framework supports flag value matching.

**Severity**: P1 — complete production outage from a safe-classified
command.

---

## P2: Medium-Priority Issues

### CK-P2.1: `docker exec` classified as safe — inner command visibility depends on parser

**Location**: Plan §5.1 S7, §5.1.1

**Issue**: S7 marks `docker exec` as safe. §5.1.1 explains that the
core.filesystem pack handles inner commands "if they are shell commands
in the overall pipeline."

If the tree-sitter parser does NOT extract the inner command from
`docker exec db rm -rf /data`, the destructive operation goes undetected.
The safe classification from the docker pack would be the only result.

This is a cross-plan concern (depends on plan 01's parser capabilities),
but the docker pack's safe classification adds risk — it explicitly marks
exec as safe rather than leaving it unmatched.

**Severity**: P2 — depends on parser behavior, but safe classification
is overly broad.

---

### CK-P2.2: `docker system prune -a` (no -f) same severity as `docker system prune` (no flags)

**Location**: Plan §5.1 D1, D2

**Issue**: D1 matches `docker system prune -a -f` (Critical). D2 matches
everything else (High). But `docker system prune -a` (without -f) removes
ALL unused images, while `docker system prune` (without -a) only removes
dangling images. The blast radius is very different:

| Command | What's removed | Severity |
|---------|---------------|----------|
| `docker system prune` | Dangling images, stopped containers | High |
| `docker system prune -a` | ALL unused images, stopped containers | High |
| `docker system prune -a -f` | Same as -a, no prompt | Critical |

`-a` without `-f` still prompts, but the amount of data removed is
dramatically larger. Consider making `docker system prune -a` (without -f)
a separate pattern at High or creating a Critical/High split for `-a`.

**Severity**: P2 — blast radius difference not reflected in severity.

---

### CK-P2.3: Golden file entries for helm-upgrade-force/reset-values inconsistent with CK-P0.1

**Location**: Plan §6.4

**Issue**: The golden file shows:
```
helm upgrade my-release my-chart --force       → Ask/Medium (helm-upgrade-force)
helm upgrade my-release my-chart --reset-values → Ask/Medium (helm-upgrade-reset-values)
```

But due to CK-P0.1, these commands would actually match S2 (safe) and
produce Allow, not Ask/Medium. The golden file entries are aspirational
but don't match the actual pattern behavior.

**Fix**: Fix CK-P0.1 so the golden file entries are correct.

**Severity**: P2 — golden file tests would fail, surfacing CK-P0.1.

---

### CK-P2.4: O2 orchestrator consistency test logs but doesn't assert severity equality

**Location**: Test harness §O2

**Issue**: O2 tests equivalent operations across docker/compose and
kubectl/helm for severity consistency. But the test only LOGS severity
differences — it doesn't assert equality:

```go
t.Logf("%s: severity=%v pattern=%s", name, dp.Severity, dp.Name)
```

Compare with the infra-cloud O2 test which asserts:
```go
assert.Equal(t, severities[0], severities[i], ...)
```

Without assertions, orchestrator severity inconsistencies would not
cause test failures.

**Fix**: Add severity equality assertions to O2, or at minimum document
which severity differences are expected and acceptable.

**Severity**: P2 — test doesn't enforce the property it claims to verify.

---

### CK-P2.5: `kubectl rollout restart` in S4 safe — could cause disruption

**Location**: Plan §5.3 S4, §5.3.1

**Issue**: S4 includes `rollout` as safe. `kubectl rollout restart
deployment/my-app` triggers a rolling restart of all pods. If the
deployment has maxUnavailable > 0 and minReadySeconds = 0, all pods may
restart simultaneously, causing service disruption.

§5.3.1 says "Even kubectl rollout restart just triggers a rolling restart."
This is true for well-configured deployments but optimistic for
misconfigured ones.

This is a lower priority because rolling restart is a standard operational
practice, and the rolling update strategy is the deployment's responsibility.

**Severity**: P2 — safe classification assumes well-configured deployments.

---

### CK-P2.6: P7 cross-pack isolation test only uses standalone compose form

**Location**: Test harness §P7

**Issue**: P7 tests cross-pack isolation using:
```go
"containers.compose": cmd("docker-compose", []string{"down"}, m("-v", "")),
```

This only tests standalone form (`docker-compose`). The plugin form
(`docker compose`, Name="docker") shares the keyword with the docker
pack and is the more interesting case for cross-pack interference.

F3 does test this explicitly, so the gap is covered, but P7's isolation
property should also verify the plugin form.

**Fix**: Add a second compose entry to P7 for plugin form:
```go
"containers.compose-plugin": cmd("docker", []string{"compose", "down"}, m("-v", "")),
```

**Severity**: P2 — test coverage gap (mitigated by F3).

---

## P3: Low-Priority Issues

### CK-P3.1: `docker-compose down -v --rmi all` matches both D1 and D2

**Location**: Plan §5.2, D1 `compose-down-volumes`, D2 `compose-down-rmi`

**Issue**: When both `-v` and `--rmi` are used together, the command
matches both D1 (via -v flag) and D2 (via --rmi flag). D1 matches first
due to ordering. The pattern name (`compose-down-volumes`) and reason
focus on volume deletion, which may not communicate the full impact
(images also being removed).

Both are High severity, so there's no functional impact. Only the pattern
name/reason may be slightly misleading.

**Severity**: P3 — cosmetic, no severity impact.

---

### CK-P3.2: `docker compose kill` not covered

**Location**: Plan §5.2, §13 Open Questions Q6

**Issue**: `docker compose kill` sends SIGKILL to running containers.
This is more aggressive than `docker compose stop` (which sends SIGTERM
with a grace period). Currently not covered, acknowledged as v2.

`docker compose kill` is rarely used by LLM agents (stop is the standard
approach), so the v2 deferral is reasonable.

**Severity**: P3 — acknowledged v2 gap, low LLM agent frequency.

---

### CK-P3.3: Helm rollback severity is appropriate at Medium

**Location**: Plan §5.4 D2, §5.4.1

**Assessment**: The scheduler asked about helm rollback severity
appropriateness. Medium is correct because:
1. Rollback is reversible (can roll back again to a different revision)
2. No data is destroyed — only Kubernetes resource configurations change
3. Service disruption is possible but temporary
4. The operator explicitly chose to rollback — intent is clear

The ConfidenceHigh rating is also appropriate since `helm rollback` has
unambiguous destructive potential (config reversal).

**Severity**: P3 — no change needed, confirming appropriateness.

---

### CK-P3.4: `docker builder prune` not covered

**Location**: Plan §5.1.1

**Issue**: `docker builder prune` removes build cache, which can be
significant (tens of GB). Not directly destructive to running services
but can significantly slow down subsequent builds.

The plan accepts this gap. Build cache is expendable and regenerated
automatically. Low priority for v2.

**Severity**: P3 — acknowledged gap, minimal production impact.

---

## Cross-Cutting Observations

### Split Environment Sensitivity
The split between Docker/Compose (not env-sensitive) and kubectl/Helm
(env-sensitive) is well-designed and reflects real-world usage patterns.
Docker is primarily local development; kubectl/Helm operate on potentially
production clusters. The P3 property test verifies this split invariant.

### kubectl Delete Resource Type Tiers
The three-tier approach (Critical/High/Medium by resource type) with
a catch-all D8 is sound. The D8 exclusion list stays synchronized with
D1-D5 via the P8 property test. The main gap is `secret` severity
(CK-P1.3) and `--all-namespaces` escalation (CK-P1.1).

### Docker Dual-Syntax Coverage
The Or() composition for old-style and management command syntax is
thorough. P4 property test verifies parity. All destructive patterns
handle both forms.

### Compose Dual-Naming Coverage
Both standalone (`docker-compose`) and plugin (`docker compose`) forms
are handled correctly with separate Or branches. P5 property test
verifies parity. The keyword overlap with the docker pack is handled
correctly — F3 explicitly tests this.

### Test Harness Quality
The test harness is strong. P8 (catch-all completeness) is a
particularly valuable property test that prevents D8 exclusion list
drift. The dual-syntax/dual-naming parity tests (P4, P5) are creative
and catch a real class of bugs. O2 should assert rather than log
(CK-P2.4).
