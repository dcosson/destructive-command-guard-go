# Plan Review: 03d-packs-containers-k8s — Systems Engineer Perspective

**Reviewer**: dcg-reviewer (systems-engineer persona)
**Plan**: `docs/plans/03d-packs-containers-k8s.md`
**Test Harness**: `docs/plans/03d-packs-containers-k8s-test-harness.md`
**Date**: 2026-03-01
**Review Round**: R1

---

## Summary

The plan defines 4 packs (docker, compose, kubectl, helm) with 17 safe and 30
destructive patterns covering container lifecycle and Kubernetes operations. The
architecture handles Docker's dual-syntax (old-style vs management commands) and
Compose's dual-naming (standalone vs plugin) well. The kubectl resource-type
severity tiers are well-designed, and the split env-sensitivity (docker/compose
not sensitive, kubectl/helm sensitive) accurately reflects real-world risk.

However, there are two critical issues: the helm safe pattern S2 short-circuits
two destructive patterns making them completely unreachable, and kubectl delete
patterns only match singular resource type names, missing the plural forms that
kubectl also accepts. The test harness is thorough — notably P4 (dual-syntax
parity), P5 (dual-naming parity), and P8 (catch-all completeness) are excellent
property tests, though P2 (safe patterns never match destructive reachability
commands) would catch the P0-1 helm bug if implemented before the plan is
finalized.

**Findings**: 2 P0, 5 P1, 5 P2, 5 P3

---

## P0 — Critical

### P0-1: helm-install-upgrade-safe S2 short-circuits helm-upgrade-force (D3) and helm-upgrade-reset-values (D4)

**Location**: §5.4, lines 1112-1121 (S2) vs lines 1159-1186 (D3, D4)

S2 `helm-install-upgrade-safe` matches `ArgAt(0, "upgrade")` unconditionally.
D3 `helm-upgrade-force` matches `ArgAt(0, "upgrade") + Flags("--force")`.
D4 `helm-upgrade-reset-values` matches `ArgAt(0, "upgrade") + Flags("--reset-values")`.

Since safe patterns short-circuit destructive patterns, S2 matches ALL
`helm upgrade` commands, including those with `--force` or `--reset-values`.
D3 and D4 are **completely unreachable**.

Consequence for golden file entries (§6.4):
- `helm upgrade my-release my-chart --force → Ask/Medium` — WRONG, actual: Allow
- `helm upgrade my-release my-chart --reset-values → Ask/Medium` — WRONG, actual: Allow

The reachability test (test harness P1) would pass because D3/D4 DO match their
reachability commands in isolation. But the safe-vs-destructive interaction test
(test harness P2) WOULD catch this — the reachability commands for D3/D4 also
match S2.

**Impact**: `helm upgrade --force` (which deletes and recreates resources causing
downtime) and `helm upgrade --reset-values` (which discards all custom config)
are classified as Allow instead of Ask/Medium.

**Fix**: Add Not clauses to S2:
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

### P0-2: kubectl delete patterns only match singular resource types, missing plurals

**Location**: §5.3, lines 855-998 (D1-D5, D8)

kubectl accepts both singular and plural resource type names:
- `kubectl delete namespace prod` (singular) ✓ matched by D1
- `kubectl delete namespaces prod` (plural) ✗ falls through to D8 Medium

The patterns only match singular and abbreviated forms. All plural forms are
misclassified:

| Plural form | Expected | Actual |
|---|---|---|
| `kubectl delete namespaces prod` | D1 Critical | D8 Medium |
| `kubectl delete deployments my-app` | D2 High | D8 Medium |
| `kubectl delete statefulsets my-db` | D2 High | D8 Medium |
| `kubectl delete daemonsets my-ds` | D2 High | D8 Medium |
| `kubectl delete persistentvolumeclaims data` | D3 High | D8 Medium |
| `kubectl delete persistentvolumes vol` | D3 High | D8 Medium |
| `kubectl delete nodes worker-1` | D4 High | D8 Medium |
| `kubectl delete services my-svc` | D5 High | D8 Medium |

kubectl also supports additional abbreviations not currently matched:
- `kubectl delete no worker-1` (abbreviation for node)
- `kubectl delete po my-pod` (abbreviation for pod — but this is Medium anyway)

**Impact**: Plural resource type names get Medium instead of Critical/High. LLM
agents commonly generate both singular and plural forms. The severity
under-classification could lead to inadequate warnings for dangerous operations.

**Fix**: Add plural forms to D1-D5 and the D8 exclusion list:
- D1: add `packs.ArgAt(1, "namespaces")`
- D2: add `"deployments"`, `"statefulsets"`, `"daemonsets"`
- D3: add `"persistentvolumeclaims"`, `"persistentvolumes"`, `"pvcs"`
- D4: add `"nodes"`
- D5: add `"services"`
- D8 exclusion list: add all the same plural forms

---

## P1 — High

### P1-1: kubectl delete --all-namespaces at Medium severity

**Location**: §13 Q3

`kubectl delete pods --all-namespaces` deletes ALL pods across the ENTIRE
cluster. The catch-all D8 classifies this as Medium, same as deleting a single
pod. The plan documents this as a v2 deferral.

While the v2 deferral is acknowledged, `--all-namespaces` represents a
categorically different blast radius. A single `kubectl delete configmaps
--all-namespaces` could take down the entire cluster by removing every
configmap across all namespaces.

**Fix**: At minimum, add a specific pattern for `kubectl delete` with
`Flags("--all-namespaces")` or `Flags("-A")` at High severity, regardless
of resource type.

### P1-2: docker exec classified as safe — inner commands undetectable

**Location**: §5.1, lines 290-301 (S7) and §5.1.1

S7 classifies all `docker exec` as safe. The notes claim "the core.filesystem
pack handles inner commands if they are shell commands in the overall pipeline."
This is incorrect.

For `docker exec -it prod-db rm -rf /data`, tree-sitter parsing yields:
- Name: "docker"
- Args: ["exec", "prod-db", "rm", "/data"]
- Flags: {"-i": "", "-t": "", "-r": "", "-f": ""}

The command Name is "docker", not "rm". The core.filesystem pack matches
`Name("rm")` and will NOT trigger. The inner `rm -rf /data` is invisible to the
pack system because it's parsed as arguments to docker, not a separate command.

**Impact**: `docker exec prod-db rm -rf /data` → Allow. A clearly destructive
command against a production database container goes undetected.

**Fix**: (a) Correct the documentation to state that inner commands in docker
exec are NOT detected by the pack system. (b) Consider adding destructive
patterns to the docker pack for `docker exec` with known-dangerous arg content
(ArgContent("rm "), ArgContent("dd "), etc.), similar to ansible's command
module handling. (c) If (b) is deferred, add to the v2 list in §13 Q6.

### P1-3: kubectl delete --all flag not escalated

**Location**: §13 Q2

`kubectl delete pods --all` deletes ALL pods in the current namespace. Combined
with `--all-namespaces` (P1-1), this is catastrophic. Even within a single
namespace, `--all` dramatically increases blast radius.

Currently caught by D8 at Medium, same as single-resource deletion. The plan
documents this as a v2 deferral.

**Fix**: Add a pattern for `kubectl delete` with `Flags("--all")` at High
severity, or at minimum add `Flags("--all")` as a severity escalation condition
on D8.

### P1-4: docker system prune --volumes flag not addressed

**Location**: §5.1, lines 345-380 (D1, D2)

`docker system prune` with `--volumes` prunes ALL unused volumes in addition to
the default cleanup. This is a critical distinction since volume data loss is
permanent. The current patterns:
- D1: `-a` AND `-f` → Critical (all images + forced, but not necessarily volumes)
- D2: everything else → High

The `--volumes` flag is not mentioned. Important scenarios:
- `docker system prune -f --volumes` → D2 High (removes volumes without prompt)
- `docker system prune -a -f --volumes` → D1 Critical (removes everything)

The `-a` flag controls image scope (dangling vs all), not volume inclusion.
`--volumes` is the flag that controls volume pruning. A non-`-a` prune with
`--volumes` still destroys volume data.

**Fix**: Add `--volumes` handling: either escalate D2 to Critical when
`--volumes` is present, or add a separate D1.5 pattern for
`system prune --volumes -f` at Critical.

### P1-5: docker network prune excluded from safe but no destructive pattern

**Location**: §5.1, lines 527-530 and S8 Not clauses

S8 includes "network" in the safe list with `Not(ArgAt(1, "prune"))` exclusion.
So `docker network prune` is excluded from safe. But no destructive pattern
matches it either — it falls through as Indeterminate.

The plan documents this as "accepted as unmatched" since "active networks can't
be pruned." However, `docker network prune -f` force-removes all unused networks
without confirmation, and networks may be unused at the moment but needed by
containers about to start (e.g., in a compose-like multi-container setup).

**Fix**: Add a Medium destructive pattern for `docker network prune`, or at
minimum `docker network prune -f` (forced, no confirmation).

---

## P2 — Medium

### P2-1: kubectl scale --replicas=0 classified as safe

**Location**: §5.3, S4 lines 845-847 and §5.3.1

S4 classifies `kubectl scale` as safe. `kubectl scale --replicas=0
deployment/production-api` terminates ALL pods in a deployment, effectively
causing complete service downtime. While the deployment resource persists and can
be scaled back up, the operational impact is equivalent to `docker stop` (which
IS classified as destructive at Medium).

The plan notes "the deployment still exists and can be scaled back up." This is
true, but the immediate impact is full service outage.

**Fix**: Consider excluding `scale` from S4 when `--replicas=0` is detected, or
document this as accepted over-permissiveness. Alternatively, add a Low-severity
informational pattern for `kubectl scale` with `ArgContent("replicas=0")`.

### P2-2: docker compose down -v --rmi combined severity

**Location**: §5.2, D1 and D2

`docker compose down -v --rmi all` removes containers, networks, volumes, AND
images. D1 (compose-down-volumes) fires first at High. D2 (compose-down-rmi)
also matches but is never reached.

The combined operation is arguably more destructive than either flag alone —
it represents a complete teardown of all compose artifacts including persistent
data. Both D1 and D2 are High, so no severity under-classification occurs.
But the plan doesn't address the combination.

**Fix**: Document that `-v --rmi` combined is handled by D1 at High, or consider
adding a Critical-severity pattern for the combination if warranted.

### P2-3: compose S3 includes "restart" as safe

**Location**: §5.2, S3 lines 610-611, 627-628

S3 `compose-up-build-safe` includes "restart" subcommand. `docker compose
restart` stops and restarts all containers, causing brief service interruption.
While no data is lost and it's reversible, it's operationally disruptive —
similar to `compose stop` which IS classified as Medium (D5).

**Impact**: `docker compose restart` is classified as safe while the
functionally similar `docker compose stop` is Medium.

**Fix**: Remove "restart" from S3 safe list. Either add it as Medium or let it
be Indeterminate. Alternatively, document the intentional distinction (restart
is self-healing, stop requires manual intervention).

### P2-4: golden file entries for helm D3/D4 expect wrong outcome

**Location**: §6.4, lines 1353-1354

Due to P0-1, these golden file entries are incorrect:
```
helm upgrade my-release my-chart --force            → Ask/Medium (helm-upgrade-force)
helm upgrade my-release my-chart --reset-values     → Ask/Medium (helm-upgrade-reset-values)
```

Actual behavior with the current plan: Allow (helm-install-upgrade-safe matches
first). After P0-1 is fixed, verify these entries match the corrected behavior.

### P2-5: missing golden file entries for kubectl plural resource types

**Location**: §6.3

No golden file entries test plural resource type forms:
- `kubectl delete deployments my-app` (not tested)
- `kubectl delete services my-svc` (not tested)
- `kubectl delete namespaces prod` (not tested)

After P0-2 is fixed, add golden file entries for plural forms to prevent
regression.

---

## P3 — Low

### P3-1: docker system prune -a -f --volumes vs without --volumes both Critical

**Location**: §5.1, D1

D1 matches `docker system prune -a -f` regardless of `--volumes` flag. With
`--volumes`, the prune also removes all unused volumes (permanent data loss).
Without `--volumes`, volumes are preserved. Both get Critical severity.

Not a bug (Critical is appropriate either way), but the remediation message
should mention the `--volumes` flag difference. Currently the message says
"removes ALL stopped containers, ALL unused images, and ALL build cache" but
doesn't mention volumes.

**Fix**: Update D1 remediation to note: "Add --volumes to also remove unused
volumes (permanent data loss). Run docker volume ls to see affected volumes."

### P3-2: docker-compose rm without -f is Indeterminate

**Location**: §5.2.1

`docker-compose rm` (without `-f`) prompts for confirmation and is intentionally
unmatched. This means it's Indeterminate rather than explicitly safe. The plan
documents this decision: "Without -f, it prompts for confirmation, which is a
sufficient safety net."

This is consistent with other tools where prompted operations aren't flagged
(e.g., pulumi destroy without --yes in the original 03c plan). No action needed,
just confirming the deliberate choice.

### P3-3: test harness P8 catches D8 exclusion list sync

**Location**: Test harness P8, lines 261-302

The test harness has P8 "kubectl Delete Catch-All Completeness" which verifies
that the D8 exclusion list matches D1-D5 resource types. This is an excellent
property test that prevents the exclusion list from drifting. After P0-2 is
fixed (adding plural forms), P8 must also be updated to include the plural forms
in its `specificResources` list.

### P3-4: compose keyword overlap cross-pack interference

**Location**: §13 Q1

Both docker and compose packs use "docker" as a keyword. Every `docker` command
triggers evaluation of both packs. The plan documents this as accepted. The test
harness F3 specifically tests for cross-pack interference. The compose pack's
safe/destructive patterns all require `ArgAt(0, "compose")` or
`Name("docker-compose")`, ensuring no false matches.

Minor performance overhead but functionally correct.

### P3-5: golden file total count verification

**Location**: §6

Plan states 99 entries (30 + 22 + 27 + 20). Spot-checked each section:
- Docker: 30 entries (2 Critical + 7 High + 11 Medium + 7 Safe + 3 management variants) — need to recount carefully
- After P0-2 fix, kubectl golden entries will need additional plural form entries
- After P0-1 fix, helm golden entries for D3/D4 need correction

**Fix**: Verify total count matches after incorporating P0 fixes.

---

## Cross-Reference Verification

### Shaping doc (§A8) pack scope alignment
All 4 packs match the shaping doc's Pack Scope table. ✓

### Plan 02 matcher DSL compatibility
- ArgAt(), Flags(), Name(), And(), Or(), Not() — all used correctly. ✓
- No ArgContent or ArgContentRegex needed (pure positional matching). ✓
- No use of matchers not defined in plan 02. ✓

### Plan 01 tree-sitter parsing dependency
- Short flags decomposed: `-af` → {"-a": "", "-f": ""}. Critical for D1. ✓
- Management command syntax: `docker container rm` → Args: ["container", "rm", ...]. ✓
- Plugin compose: `docker compose down` → Name: "docker", Args: ["compose", "down"]. ✓
- Standalone compose: `docker-compose down` → Name: "docker-compose", Args: ["down"]. ✓

### Plan 03a pack template alignment
- All packs follow template structure. ✓
- Keyword definitions correct. ✓
- Docker/Compose NOT env-sensitive, kubectl/Helm env-sensitive — intentional split. ✓

### Test harness coverage
- P1-P8 property tests — thorough, especially P4 (dual-syntax) and P8 (catch-all). ✓
- P2 would catch P0-1 helm bug. ✓
- F3 specifically tests docker/compose keyword overlap. ✓
- SEC1 tests syntax evasion. ✓
- Missing: no property test for plural resource type matching — would catch P0-2.
- Missing: no E-series test case for `docker compose restart` vs `docker compose stop`
  severity comparison.
