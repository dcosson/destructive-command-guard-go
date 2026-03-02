# 03d: Containers & Kubernetes Packs — Test Harness

**Plan**: [03d-packs-containers-k8s.md](./03d-packs-containers-k8s.md)
**Architecture**: [00-architecture.md](./00-architecture.md)
**Core Pack Test Harness**: [03a-packs-core-test-harness.md](./03a-packs-core-test-harness.md)

---

## Overview

This document specifies the test harness for the container and Kubernetes
packs (plan 03d). It covers property-based tests, deterministic examples,
fault injection, comparison oracles, benchmarks, stress tests, security
tests, manual QA, CI tier mapping, and exit criteria.

The test harness complements the unit tests described in the plan doc §7.
Unit tests verify individual pattern behavior. This harness verifies
system-level properties, cross-pattern interactions, and robustness.

Container and Kubernetes packs introduce testing challenges not present
in other pack categories:
- **Docker dual-syntax matching**: Old-style (`docker rm`) and management
  command (`docker container rm`) must both match. Tests must cover both
  syntaxes.
- **Compose dual-naming**: `docker-compose` (standalone) and `docker
  compose` (plugin) must both match. Tests must verify both forms.
- **Split environment sensitivity**: Docker/Compose are NOT env-sensitive
  while kubectl/Helm ARE. This split must be verified.
- **kubectl resource type routing**: D1-D5 handle specific high-impact
  resources, D8 is a catch-all. The exclusion list in D8 must stay
  synchronized with D1-D5.
- **Keyword overlap**: Docker and Compose packs share the `"docker"`
  keyword. Cross-pack interference must be verified.

---

## P: Property-Based Tests

### P1: Every Destructive Pattern Has a Matching Command

**Invariant**: For each destructive pattern in each container/k8s pack,
there exists at least one `ExtractedCommand` that the pattern matches.

Same property as 03a P1, extended to 4 packs. Uses the reachability
command map from plan doc §7.2.

```go
func TestPropertyEveryContainerK8sDestructivePatternReachable(t *testing.T) {
    packs := []packs.Pack{dockerPack, composePack, kubectlPack, helmPack}
    for _, pack := range packs {
        for _, dp := range pack.Destructive {
            t.Run(pack.ID+"/"+dp.Name, func(t *testing.T) {
                cmd := getReachabilityCommand(pack.ID, dp.Name)
                assert.True(t, dp.Match.Match(cmd),
                    "pattern %s has no matching reachability command", dp.Name)
            })
        }
    }
}
```

### P2: Safe Patterns Never Match Destructive Reachability Commands

**Invariant**: For each destructive pattern's reachability command, no safe
pattern in the same pack matches it.

This is critical for:
- Docker S8: includes `"network"` and `"volume"` as safe subcommands
  but must exclude `"rm"`, `"remove"`, and `"prune"` via Not clauses.
- Helm S2: includes `"upgrade"` as safe but must exclude `--force` and
  `--reset-values` via Not clauses (so D3/D4 remain reachable).
- kubectl S4: includes `"apply"` as safe but must exclude `--prune`
  via Not clause (so kubectl-apply-prune remains reachable).

```go
func TestPropertyContainerK8sSafePatternsNeverBlockDestructive(t *testing.T) {
    packs := []packs.Pack{dockerPack, composePack, kubectlPack, helmPack}
    for _, pack := range packs {
        for _, dp := range pack.Destructive {
            cmd := getReachabilityCommand(pack.ID, dp.Name)
            for _, sp := range pack.Safe {
                assert.False(t, sp.Match.Match(cmd),
                    "safe pattern %s blocks destructive %s in pack %s",
                    sp.Name, dp.Name, pack.ID)
            }
        }
    }
}
```

### P3: Split Environment Sensitivity

**Invariant**: Every destructive pattern in kubectl and helm packs has
`EnvSensitive: true`. Every destructive pattern in docker and compose
packs has `EnvSensitive: false`.

```go
func TestPropertySplitEnvSensitivity(t *testing.T) {
    // Must be env-sensitive
    for _, pack := range []packs.Pack{kubectlPack, helmPack} {
        for _, dp := range pack.Destructive {
            assert.True(t, dp.EnvSensitive,
                "%s/%s must be env-sensitive", pack.ID, dp.Name)
        }
    }
    // Must NOT be env-sensitive
    for _, pack := range []packs.Pack{dockerPack, composePack} {
        for _, dp := range pack.Destructive {
            assert.False(t, dp.EnvSensitive,
                "%s/%s must NOT be env-sensitive", pack.ID, dp.Name)
        }
    }
}
```

### P4: Docker Dual-Syntax Parity

**Invariant**: For every docker destructive pattern that supports both
old-style and management command syntax, both syntaxes produce the same
match result.

```go
func TestPropertyDockerDualSyntaxParity(t *testing.T) {
    pairs := []struct {
        name     string
        oldStyle parse.ExtractedCommand
        mgmtCmd  parse.ExtractedCommand
    }{
        {"rm",
            cmd("docker", []string{"rm", "container1"}, nil),
            cmd("docker", []string{"container", "rm", "container1"}, nil)},
        {"rmi",
            cmd("docker", []string{"rmi", "image1"}, nil),
            cmd("docker", []string{"image", "rm", "image1"}, nil)},
        {"stop",
            cmd("docker", []string{"stop", "container1"}, nil),
            cmd("docker", []string{"container", "stop", "container1"}, nil)},
        {"kill",
            cmd("docker", []string{"kill", "container1"}, nil),
            cmd("docker", []string{"container", "kill", "container1"}, nil)},
    }

    for _, pair := range pairs {
        t.Run(pair.name, func(t *testing.T) {
            for _, dp := range dockerPack.Destructive {
                oldMatch := dp.Match.Match(pair.oldStyle)
                mgmtMatch := dp.Match.Match(pair.mgmtCmd)
                assert.Equal(t, oldMatch, mgmtMatch,
                    "docker %s: old-style=%v management=%v for pattern %s",
                    pair.name, oldMatch, mgmtMatch, dp.Name)
            }
        })
    }
}
```

### P5: Compose Dual-Naming Parity

**Invariant**: For every compose destructive pattern, both
`docker-compose` and `docker compose` invocations produce the same
match result.

```go
func TestPropertyComposeDualNamingParity(t *testing.T) {
    pairs := []struct {
        name       string
        standalone parse.ExtractedCommand
        plugin     parse.ExtractedCommand
    }{
        {"down",
            cmd("docker-compose", []string{"down"}, nil),
            cmd("docker", []string{"compose", "down"}, nil)},
        {"down -v",
            cmd("docker-compose", []string{"down"}, m("-v", "")),
            cmd("docker", []string{"compose", "down"}, m("-v", ""))},
        {"down --rmi",
            cmd("docker-compose", []string{"down"}, m("--rmi", "all")),
            cmd("docker", []string{"compose", "down"}, m("--rmi", "all"))},
        {"rm -f",
            cmd("docker-compose", []string{"rm"}, m("-f", "")),
            cmd("docker", []string{"compose", "rm"}, m("-f", ""))},
        {"stop",
            cmd("docker-compose", []string{"stop"}, nil),
            cmd("docker", []string{"compose", "stop"}, nil)},
    }

    for _, pair := range pairs {
        t.Run(pair.name, func(t *testing.T) {
            for _, dp := range composePack.Destructive {
                saMatch := dp.Match.Match(pair.standalone)
                plMatch := dp.Match.Match(pair.plugin)
                assert.Equal(t, saMatch, plMatch,
                    "compose %s: standalone=%v plugin=%v for pattern %s",
                    pair.name, saMatch, plMatch, dp.Name)
            }
        })
    }
}
```

### P6: No Destructive Pattern Matches Empty Command

**Invariant**: An empty `ExtractedCommand` matches no destructive pattern
in any container/k8s pack.

```go
func TestPropertyContainerK8sEmptyCommandMatchesNothing(t *testing.T) {
    empty := parse.ExtractedCommand{}
    packs := []packs.Pack{dockerPack, composePack, kubectlPack, helmPack}
    for _, pack := range packs {
        for _, dp := range pack.Destructive {
            assert.False(t, dp.Match.Match(empty),
                "pattern %s/%s matches empty command", pack.ID, dp.Name)
        }
    }
}
```

### P7: Cross-Pack Pattern Isolation

**Invariant**: Docker commands don't trigger Compose, kubectl, or Helm
patterns, and vice versa.

```go
func TestPropertyContainerK8sCrossPackIsolation(t *testing.T) {
    packCommands := map[string]parse.ExtractedCommand{
        "containers.docker":           cmd("docker", []string{"system", "prune"}, m("-a", "", "-f", "")),
        "containers.compose":          cmd("docker-compose", []string{"down"}, m("-v", "")),
        "containers.compose-plugin":   cmd("docker", []string{"compose", "down"}, m("-v", "")),
        "kubernetes.kubectl":          cmd("kubectl", []string{"delete", "namespace", "prod"}, nil),
        "kubernetes.helm":             cmd("helm", []string{"uninstall", "my-release"}, nil),
    }

    packs := map[string]packs.Pack{
        "containers.docker":  dockerPack,
        "containers.compose": composePack,
        "kubernetes.kubectl": kubectlPack,
        "kubernetes.helm":    helmPack,
    }

    for cmdPackID, testCmd := range packCommands {
        // Map compose-plugin to compose pack for "own pack" check
        ownPackID := cmdPackID
        if cmdPackID == "containers.compose-plugin" {
            ownPackID = "containers.compose"
        }
        for packID, pack := range packs {
            if packID == ownPackID {
                continue
            }
            // Special case: docker pack and compose pack share keyword
            // docker compose commands (both standalone and plugin) should
            // not match docker destructive patterns
            if (cmdPackID == "containers.compose" || cmdPackID == "containers.compose-plugin") &&
                packID == "containers.docker" {
                for _, dp := range pack.Destructive {
                    assert.False(t, dp.Match.Match(testCmd),
                        "%s command triggers docker/%s", cmdPackID, dp.Name)
                }
                continue
            }
            for _, dp := range pack.Destructive {
                assert.False(t, dp.Match.Match(testCmd),
                    "%s command triggers %s/%s",
                    cmdPackID, packID, dp.Name)
            }
        }
    }
}
```

### P8: kubectl Delete Catch-All Completeness

**Invariant**: The D8 exclusion list in kubectl-delete-resource matches
exactly the resource types handled by D1-D5.

```go
func TestPropertyKubectlDeleteCatchAllCompleteness(t *testing.T) {
    // D1-D5 handle these resource types specifically (singular + plural)
    specificResources := []string{
        "namespace", "namespaces", "ns",
        "deployment", "deployments", "deploy",
        "statefulset", "statefulsets", "sts",
        "daemonset", "daemonsets", "ds",
        "pvc", "persistentvolumeclaim", "persistentvolumeclaims",
        "pv", "persistentvolume", "persistentvolumes",
        "node", "nodes",
        "service", "services", "svc",
        "secret", "secrets",
    }

    for _, res := range specificResources {
        testCmd := cmd("kubectl", []string{"delete", res, "test-resource"}, nil)

        // Should NOT match D8 (catch-all)
        d8 := findDestructiveByName(kubectlPack, "kubectl-delete-resource")
        assert.False(t, d8.Match.Match(testCmd),
            "resource type %q should be excluded from D8 catch-all", res)

        // SHOULD match one of D1-D5
        matchedSpecific := false
        for _, dp := range kubectlPack.Destructive {
            if dp.Name == "kubectl-delete-resource" {
                continue
            }
            if dp.Match.Match(testCmd) {
                matchedSpecific = true
                break
            }
        }
        assert.True(t, matchedSpecific,
            "resource type %q excluded from D8 but not matched by D1-D5", res)
    }
}
```

---

## E: Deterministic Examples

### E1: containers.docker Pattern Matrix (30 cases)

See plan doc §6.1 for the full golden file entries.

```
# Critical
docker system prune -a -f                           → Deny/Critical
docker system prune -af                             → Deny/Critical

# High
docker system prune                                 → Deny/High
docker system prune -f                              → Deny/High
docker volume rm my-volume                          → Deny/High
docker volume prune                                 → Deny/High

# Medium
docker rm my-container                              → Ask/Medium
docker container rm my-container                    → Ask/Medium
docker rmi my-image                                 → Ask/Medium
docker image rm my-image                            → Ask/Medium
docker image prune                                  → Ask/Medium
docker container prune                              → Ask/Medium
docker network rm my-network                        → Ask/Medium
docker stop my-container                            → Ask/Medium
docker kill my-container                            → Ask/Medium

# Safe
docker ps                                           → Allow
docker images                                       → Allow
docker inspect my-container                         → Allow
docker logs my-container                            → Allow
docker build -t myimage .                           → Allow
docker run -it ubuntu bash                          → Allow
docker info                                         → Allow
docker version                                      → Allow
```

### E2: containers.compose Pattern Matrix (22 cases)

See plan doc §6.2 for the full golden file entries.

### E3: kubernetes.kubectl Pattern Matrix (27 cases)

See plan doc §6.3 for the full golden file entries.

### E4: kubernetes.helm Pattern Matrix (20 cases)

See plan doc §6.4 for the full golden file entries.

### E5: Cross-Pack Non-Interference (4 cases)

Verify that container/k8s packs don't interfere with each other:

```
docker system prune -a -f                           → Matches containers.docker only
docker-compose down -v                              → Matches containers.compose only
kubectl delete namespace production                 → Matches kubernetes.kubectl only
helm uninstall my-release                           → Matches kubernetes.helm only
```

### E6: Docker Dual-Syntax Coverage (10 cases)

```
# Old-style and management command equivalence
docker rm my-container                              → Ask/Medium (docker-rm)
docker container rm my-container                    → Ask/Medium (docker-rm)
docker rmi my-image                                 → Ask/Medium (docker-rmi)
docker image rm my-image                            → Ask/Medium (docker-rmi)
docker stop my-container                            → Ask/Medium (docker-stop-kill)
docker container stop my-container                  → Ask/Medium (docker-stop-kill)
docker kill my-container                            → Ask/Medium (docker-stop-kill)
docker container kill my-container                  → Ask/Medium (docker-stop-kill)
docker volume rm my-vol                             → Deny/High (docker-volume-rm)
docker volume remove my-vol                         → Deny/High (docker-volume-rm)
```

### E7: Compose Dual-Naming Coverage (10 cases)

```
# Standalone and plugin equivalence
docker-compose down -v                              → Deny/High (compose-down-volumes)
docker compose down -v                              → Deny/High (compose-down-volumes)
docker-compose down                                 → Ask/Medium (compose-down)
docker compose down                                 → Ask/Medium (compose-down)
docker-compose rm -f                                → Ask/Medium (compose-rm-force)
docker compose rm -f                                → Ask/Medium (compose-rm-force)
docker-compose stop                                 → Ask/Medium (compose-stop)
docker compose stop                                 → Ask/Medium (compose-stop)
docker-compose ps                                   → Allow (compose-readonly-safe)
docker compose ps                                   → Allow (compose-readonly-safe)
```

---

## F: Fault Injection

### F1: Nil/Empty Fields in ExtractedCommand

Test that all container/k8s pack matchers handle degenerate inputs gracefully.

```go
func TestFaultContainerK8sNilFields(t *testing.T) {
    packs := []packs.Pack{dockerPack, composePack, kubectlPack, helmPack}
    degenerateCmds := []parse.ExtractedCommand{
        {Name: "docker", Args: nil, Flags: nil},
        {Name: "docker-compose", Args: nil, Flags: nil},
        {Name: "kubectl", Args: nil, Flags: nil},
        {Name: "helm", Args: nil, Flags: nil},
        {Name: "docker", Args: []string{}, Flags: map[string]string{}},
        {Name: "", Args: nil, Flags: nil},
    }

    for _, pack := range packs {
        for i, c := range degenerateCmds {
            t.Run(fmt.Sprintf("%s/degenerate-%d", pack.ID, i), func(t *testing.T) {
                for _, dp := range pack.Destructive {
                    assert.NotPanics(t, func() { dp.Match.Match(c) })
                }
                for _, sp := range pack.Safe {
                    assert.NotPanics(t, func() { sp.Match.Match(c) })
                }
            })
        }
    }
}
```

### F2: ArgAt Out of Bounds

Test that `ArgAt()` matchers handle commands with fewer args than expected:

```go
func TestFaultContainerK8sArgAtOutOfBounds(t *testing.T) {
    shortCmds := []parse.ExtractedCommand{
        cmd("docker", nil, nil),                        // 0 args
        cmd("docker", []string{"container"}, nil),      // 1 arg — ArgAt(1) out of bounds
        cmd("kubectl", nil, nil),                       // 0 args
        cmd("kubectl", []string{"delete"}, nil),        // 1 arg — ArgAt(1) out of bounds
        cmd("helm", nil, nil),                          // 0 args
        cmd("docker", []string{"compose"}, nil),        // 1 arg — ArgAt(1) out of bounds
    }

    packs := []packs.Pack{dockerPack, composePack, kubectlPack, helmPack}
    for _, pack := range packs {
        for i, c := range shortCmds {
            t.Run(fmt.Sprintf("%s/short-%d", pack.ID, i), func(t *testing.T) {
                for _, dp := range pack.Destructive {
                    assert.NotPanics(t, func() { dp.Match.Match(c) })
                }
            })
        }
    }
}
```

### F3: Docker Compose Keyword Overlap Edge Cases

Test that docker commands don't accidentally match compose patterns
and vice versa:

```go
func TestFaultDockerComposeKeywordOverlap(t *testing.T) {
    // Docker commands should never match compose destructive patterns
    dockerCmds := []parse.ExtractedCommand{
        cmd("docker", []string{"rm", "my-container"}, nil),
        cmd("docker", []string{"system", "prune"}, m("-a", "", "-f", "")),
        cmd("docker", []string{"volume", "rm", "my-vol"}, nil),
    }

    for i, c := range dockerCmds {
        t.Run(fmt.Sprintf("docker-cmd-%d", i), func(t *testing.T) {
            for _, dp := range composePack.Destructive {
                assert.False(t, dp.Match.Match(c),
                    "docker command matches compose pattern %s", dp.Name)
            }
        })
    }

    // docker compose commands should not match docker destructive patterns
    composeCmds := []parse.ExtractedCommand{
        cmd("docker", []string{"compose", "down"}, m("-v", "")),
        cmd("docker", []string{"compose", "rm"}, m("-f", "")),
        cmd("docker", []string{"compose", "stop"}, nil),
    }

    for i, c := range composeCmds {
        t.Run(fmt.Sprintf("compose-plugin-%d", i), func(t *testing.T) {
            for _, dp := range dockerPack.Destructive {
                assert.False(t, dp.Match.Match(c),
                    "compose command matches docker pattern %s", dp.Name)
            }
        })
    }
}
```

---

## O: Comparison Oracle Tests

### O1: Upstream Rust Version Comparison

Compare container/k8s pack results against the upstream Rust
`destructive-command-guard` for shared commands.

```go
func TestComparisonContainerK8sUpstreamRust(t *testing.T) {
    if testing.Short() {
        t.Skip("comparison tests require upstream binary")
    }
    corpus := loadComparisonCorpus(t, "testdata/comparison/container_k8s_commands.txt")
    for _, entry := range corpus {
        t.Run(entry.Command, func(t *testing.T) {
            goResult := pipeline.Run(ctx, entry.Command, cfg)
            rustResult := runUpstream(t, entry.Command)

            if goResult.Decision != rustResult.Decision {
                t.Logf("DIVERGENCE: %q go=%v rust=%v category=%s",
                    entry.Command, goResult.Decision, rustResult.Decision,
                    categorizeDivergence(goResult, rustResult))
            }
        })
    }
}
```

**Comparison corpus**: All 117 golden file commands from §6 of the plan doc.

### O2: Orchestrator Consistency

For equivalent operations across container/k8s tools, verify severity
assignments are consistent. Assert equality for same-abstraction-level
comparisons; document expected differences for cross-level comparisons
(kubectl/helm operate at different abstraction levels).

```go
func TestComparisonOrchestratorConsistency(t *testing.T) {
    equivalents := []struct {
        name           string
        commands       map[string]parse.ExtractedCommand
        expectEqual    bool // false for cross-abstraction-level comparisons
    }{
        {
            "Release/deployment deletion",
            map[string]parse.ExtractedCommand{
                "kubectl": cmd("kubectl", []string{"delete", "deployment", "my-app"}, nil),
                "helm":    cmd("helm", []string{"uninstall", "my-release"}, nil),
            },
            true, // both High
        },
        {
            "Resource removal",
            map[string]parse.ExtractedCommand{
                "docker":  cmd("docker", []string{"rm", "my-container"}, nil),
                "compose": cmd("docker-compose", []string{"rm"}, m("-f", "")),
            },
            true, // both Medium
        },
    }

    for _, eq := range equivalents {
        t.Run(eq.name, func(t *testing.T) {
            severities := map[string]guard.Severity{}
            for name, testCmd := range eq.commands {
                pack := containerK8sPackFor(name)
                for _, dp := range pack.Destructive {
                    if dp.Match.Match(testCmd) {
                        severities[name] = dp.Severity
                        t.Logf("%s: severity=%v pattern=%s", name, dp.Severity, dp.Name)
                        break
                    }
                }
            }
            if eq.expectEqual && len(severities) > 1 {
                var first guard.Severity
                for _, s := range severities {
                    if first == 0 {
                        first = s
                    } else {
                        assert.Equal(t, first, s,
                            "severity mismatch in %q", eq.name)
                    }
                }
            }
        })
    }
}
```

### O3: Policy Monotonicity

For each container/k8s golden file entry, verify that stricter policies
never allow what looser policies deny:

```go
func TestComparisonContainerK8sPolicyMonotonicity(t *testing.T) {
    entries := golden.LoadCorpus(t, "testdata/golden/container_k8s_*.txt")
    restrictiveness := map[guard.Decision]int{guard.Allow: 0, guard.Ask: 1, guard.Deny: 2}

    for _, entry := range entries {
        t.Run(entry.Description, func(t *testing.T) {
            strict := pipeline.Run(ctx, entry.Command, strictCfg)
            inter := pipeline.Run(ctx, entry.Command, interCfg)
            perm := pipeline.Run(ctx, entry.Command, permCfg)

            sr := restrictiveness[strict.Decision]
            ir := restrictiveness[inter.Decision]
            pr := restrictiveness[perm.Decision]

            assert.GreaterOrEqual(t, sr, ir)
            assert.GreaterOrEqual(t, ir, pr)
        })
    }
}
```

---

## B: Benchmarks

### B1: Per-Pack Matching Throughput

```go
func BenchmarkDockerPackMatch(b *testing.B) {
    commands := map[string]parse.ExtractedCommand{
        "safe-ps":          cmd("docker", []string{"ps"}, nil),
        "system-prune-all": cmd("docker", []string{"system", "prune"}, m("-a", "", "-f", "")),
        "volume-rm":        cmd("docker", []string{"volume", "rm", "vol"}, nil),
        "rm":               cmd("docker", []string{"rm", "c1"}, nil),
        "container-rm":     cmd("docker", []string{"container", "rm", "c1"}, nil),
    }
    for name, c := range commands {
        b.Run(name, func(b *testing.B) {
            for i := 0; i < b.N; i++ {
                matchPack(dockerPack, c)
            }
        })
    }
}

func BenchmarkKubectlPackMatch(b *testing.B) {
    commands := map[string]parse.ExtractedCommand{
        "safe-get":        cmd("kubectl", []string{"get", "pods"}, nil),
        "delete-ns":       cmd("kubectl", []string{"delete", "namespace", "prod"}, nil),
        "delete-deploy":   cmd("kubectl", []string{"delete", "deployment", "app"}, nil),
        "delete-pod":      cmd("kubectl", []string{"delete", "pod", "p1"}, nil),
        "drain":           cmd("kubectl", []string{"drain", "node1"}, nil),
    }
    for name, c := range commands {
        b.Run(name, func(b *testing.B) {
            for i := 0; i < b.N; i++ {
                matchPack(kubectlPack, c)
            }
        })
    }
}
```

**Targets** (initial — adjust after baseline measurement):
- Safe pattern match (short-circuit): < 100ns per command
- Destructive pattern match (ArgAt): < 300ns per command
- Full pack evaluation (all patterns): < 500ns per command

### B2: Container/K8s Golden File Corpus Throughput

```go
func BenchmarkContainerK8sGoldenCorpus(b *testing.B) {
    entries := golden.LoadCorpus(b, "testdata/golden/container_k8s_*.txt")
    pipeline := setupBenchPipeline(b)
    cfg := &evalConfig{policy: InteractivePolicy()}

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        for _, e := range entries {
            pipeline.Run(context.Background(), e.Command, cfg)
        }
    }
}
```

**Target**: Full container/k8s corpus (117 entries) < 2ms total.

---

## S: Stress Tests

### S1: Concurrent Container/K8s Pack Matching

```go
func TestStressConcurrentContainerK8sMatching(t *testing.T) {
    var wg sync.WaitGroup
    commands := []parse.ExtractedCommand{
        cmd("docker", []string{"system", "prune"}, m("-a", "", "-f", "")),
        cmd("docker-compose", []string{"down"}, m("-v", "")),
        cmd("kubectl", []string{"delete", "namespace", "prod"}, nil),
        cmd("helm", []string{"uninstall", "release"}, nil),
    }

    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(idx int) {
            defer wg.Done()
            c := commands[idx%len(commands)]
            packs := []packs.Pack{dockerPack, composePack, kubectlPack, helmPack}
            for j := 0; j < 1000; j++ {
                for _, pack := range packs {
                    for _, dp := range pack.Destructive {
                        dp.Match.Match(c)
                    }
                    for _, sp := range pack.Safe {
                        sp.Match.Match(c)
                    }
                }
            }
        }(i)
    }
    wg.Wait()
}
```

---

## SEC: Security Tests

### SEC1: Docker Syntax Evasion Attempts

Test that docker destructive operations can't be evaded by using
alternative syntax:

```go
func TestSecurityDockerSyntaxEvasion(t *testing.T) {
    tests := []struct {
        name     string
        cmd      parse.ExtractedCommand
        packID   string
        wantDeny bool
        reason   string
    }{
        // Management command should be caught like old-style
        {"docker container rm (management syntax)",
            cmd("docker", []string{"container", "rm", "c1"}, nil),
            "containers.docker", true,
            "management command syntax must be caught"},
        // image rm should be caught like rmi
        {"docker image rm (management syntax)",
            cmd("docker", []string{"image", "rm", "img1"}, nil),
            "containers.docker", true,
            "docker image rm must be caught like docker rmi"},
        // container stop via management command
        {"docker container stop (management syntax)",
            cmd("docker", []string{"container", "stop", "c1"}, nil),
            "containers.docker", true,
            "docker container stop must be caught like docker stop"},
        // docker compose via plugin syntax
        {"docker compose down -v (plugin syntax)",
            cmd("docker", []string{"compose", "down"}, m("-v", "")),
            "containers.compose", true,
            "plugin syntax must be caught like standalone"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            pack := packByID(tt.packID)
            matched := false
            for _, dp := range pack.Destructive {
                if dp.Match.Match(tt.cmd) {
                    matched = true
                    break
                }
            }
            assert.Equal(t, tt.wantDeny, matched, tt.reason)
        })
    }
}
```

### SEC2: kubectl Delete Resource Type Escalation

Test that high-impact resource types get higher severity than generic
resources:

```go
func TestSecurityKubectlDeleteResourceEscalation(t *testing.T) {
    highImpact := []struct {
        resource    string
        minSeverity guard.Severity
    }{
        {"namespace", guard.Critical},
        {"ns", guard.Critical},
        {"deployment", guard.High},
        {"deploy", guard.High},
        {"statefulset", guard.High},
        {"pvc", guard.High},
        {"node", guard.High},
        {"service", guard.High},
        {"secret", guard.High},
        {"secrets", guard.High},
    }

    for _, tt := range highImpact {
        t.Run(tt.resource, func(t *testing.T) {
            testCmd := cmd("kubectl", []string{"delete", tt.resource, "test"}, nil)
            matched := false
            for _, dp := range kubectlPack.Destructive {
                if dp.Match.Match(testCmd) {
                    matched = true
                    assert.GreaterOrEqual(t, int(dp.Severity), int(tt.minSeverity),
                        "kubectl delete %s should be at least %v", tt.resource, tt.minSeverity)
                    break
                }
            }
            assert.True(t, matched, "kubectl delete %s should match at least one pattern", tt.resource)
        })
    }

    // Generic resources should be Medium
    genericResources := []string{"pod", "configmap", "ingress", "job", "cronjob"}
    for _, res := range genericResources {
        t.Run("generic-"+res, func(t *testing.T) {
            testCmd := cmd("kubectl", []string{"delete", res, "test"}, nil)
            matched := false
            for _, dp := range kubectlPack.Destructive {
                if dp.Match.Match(testCmd) {
                    matched = true
                    assert.Equal(t, guard.Medium, dp.Severity,
                        "kubectl delete %s should be Medium", res)
                    break
                }
            }
            assert.True(t, matched, "kubectl delete %s should match at least one pattern", res)
        })
    }
}
```

### SEC3: Environment Sensitivity Preconditions

Verify env-sensitive kubectl/helm patterns correctly escalate:

```go
func TestSecurityContainerK8sEnvSensitivityPreConditions(t *testing.T) {
    envSensitivePatterns := []struct {
        packID       string
        pattern      string
        baseSeverity guard.Severity
    }{
        {"kubernetes.kubectl", "kubectl-delete-namespace", guard.Critical},
        {"kubernetes.kubectl", "kubectl-delete-workload", guard.High},
        {"kubernetes.kubectl", "kubectl-delete-resource", guard.Medium},
        {"kubernetes.kubectl", "kubectl-drain", guard.High},
        {"kubernetes.helm", "helm-uninstall", guard.High},
        {"kubernetes.helm", "helm-rollback", guard.Medium},
    }

    for _, tt := range envSensitivePatterns {
        t.Run(tt.packID+"/"+tt.pattern, func(t *testing.T) {
            pack := packByID(tt.packID)
            dp := findDestructiveByName(pack, tt.pattern)
            assert.True(t, dp.EnvSensitive, "pattern should be env-sensitive")
            assert.Equal(t, tt.baseSeverity, dp.Severity, "base severity mismatch")
        })
    }
}
```

---

## MQ: Manual QA Plan

### MQ1: Real-World Container/K8s Command Evaluation

Test with commands from actual LLM coding sessions:

1. Collect 15 container/k8s-related Bash tool invocations from agent logs:
   - Docker build/run/ps cycles
   - Docker Compose up/down for local dev
   - kubectl get/describe for debugging
   - kubectl apply for deployment
   - Helm install/upgrade/status for release management
2. Run each through the pipeline with `InteractivePolicy`
3. Verify:
   - No false positives on read-only operations (ps, images, get, describe)
   - All destructive commands are caught
   - Docker and Compose commands are NOT env-sensitive
   - kubectl and Helm commands ARE env-sensitive

### MQ2: Docker Dual-Syntax Completeness

Manually verify both docker syntaxes work for every destructive operation:
1. `docker rm` vs `docker container rm` → both Ask/Medium
2. `docker rmi` vs `docker image rm` → both Ask/Medium
3. `docker stop` vs `docker container stop` → both Ask/Medium
4. `docker kill` vs `docker container kill` → both Ask/Medium
5. `docker volume rm` vs `docker volume remove` → both Deny/High

### MQ3: kubectl Resource Type Coverage

Manually verify kubectl delete with all significant resource types:
1. `kubectl delete namespace` → Deny/Critical
2. `kubectl delete deployment` → Deny/High
3. `kubectl delete statefulset` → Deny/High
4. `kubectl delete pvc` → Deny/High
5. `kubectl delete node` → Deny/High
6. `kubectl delete service` → Deny/High
7. `kubectl delete pod` → Ask/Medium (catch-all)
8. `kubectl delete configmap` → Ask/Medium (catch-all)
9. `kubectl delete secret` → Deny/High (specific resource)
10. `kubectl delete -f manifest.yaml` → Ask/Medium (catch-all)

---

## CI Tier Mapping

| Tier | Tests | Trigger |
|------|-------|---------|
| T1 (Fast, every commit) | P1-P8, E1-E7, F1-F3, SEC1-SEC3 | Every commit |
| T2 (Standard, every PR) | T1 + B1-B2, S1 | PR open/update |
| T3 (Extended, nightly) | T1 + T2 + O1-O3 | Nightly schedule |
| T4 (Manual, pre-release) | MQ1-MQ3 | Before each release |

**T1 time budget**: < 10 seconds
**T2 time budget**: < 30 seconds
**T3 time budget**: < 5 minutes

---

## Exit Criteria

### Must Pass

1. **All property tests pass** — P1-P8
2. **All deterministic examples pass** — E1-E7
3. **All fault injection tests pass** — F1-F3
4. **All security tests pass** — SEC1-SEC3
5. **Golden file corpus passes** — All 117 container/k8s entries
6. **Pattern reachability 100%** — Every destructive pattern reachable across
   all 4 packs (36 patterns total)
7. **Cross-pack isolation verified** — P7 passes
8. **Split env sensitivity verified** — P3 passes for all 4 packs
9. **Docker dual-syntax parity** — P4 passes
10. **Compose dual-naming parity** — P5 passes
11. **kubectl catch-all completeness** — P8 passes
12. **No data races** — S1 passes with -race flag
13. **Zero panics in any test** — Including F1-F3 fault injection

### Should Pass

14. **Benchmarks recorded** — B1-B2 have baseline values
15. **Stress tests pass** — S1 completes without issues
16. **Comparison oracle baseline** — O1 has initial divergence report
17. **Orchestrator consistency** — O2 has no unexpected severity differences

### Tracked Metrics

- Pattern count by pack (safe + destructive) — target: 17 safe + 36 destructive
  across 4 packs
- Test count by category (unit, reachability, golden, property, security)
- Golden file entry count — target: 117 entries across 4 packs
- ArgAt matching latency per pattern (from B1)
- Environment sensitivity coverage: kubectl + helm = env-sensitive,
  docker + compose = not env-sensitive
- Upstream comparison divergence count and categorization
- Docker dual-syntax coverage: 100% parity verified
- Compose dual-naming coverage: 100% parity verified

## Round 1 Review Disposition

| # | Reviewer | Severity | Summary | Disposition | Notes |
|---|----------|----------|---------|-------------|-------|
| 1 | dcg-reviewer | P0 | P2 safe-pattern Not clauses incomplete (helm S2, kubectl S4) | Incorporated | P2 comment updated to mention helm S2 and kubectl S4 Not clause requirements |
| 2 | dcg-reviewer | P1 | P8 missing plural resource forms | Incorporated | specificResources list expanded with all plural forms and secret/secrets |
| 3 | dcg-reviewer | P2 | P7 missing compose-plugin form | Incorporated | Added plugin form entry with ownPackID mapping |
| 4 | dcg-reviewer | P2 | O1 corpus count outdated (99→116) | Incorporated | Updated to 116 throughout |
| 5 | dcg-reviewer | P3 | B2 corpus count outdated | Incorporated | Updated to 116 |
| 6 | dcg-alt-reviewer | P0 | P2 safe-pattern Not clauses incomplete | Incorporated | Duplicate of #1; same fix |
| 7 | dcg-alt-reviewer | P1 | SEC2 secret still at Medium instead of High | Incorporated | Moved secret/secrets from genericResources to highImpact |
| 8 | dcg-alt-reviewer | P2 | O2 logs but doesn't assert severity equality | Incorporated | Changed to assert with expectEqual field and documented expected differences |
| 9 | dcg-alt-reviewer | P2 | P7 only uses standalone compose form | Incorporated | Duplicate of #3; same fix |
| 10 | dcg-alt-reviewer | P2 | Exit criteria counts outdated | Incorporated | Updated to 35 patterns, 116 golden entries |

## Round 2 Review Disposition

| # | Reviewer | Severity | Summary | Disposition | Notes |
|---|----------|----------|---------|-------------|-------|
| 1 | domain-packs-r2 | P2 | SEC2 could silently pass when no kubectl delete pattern matched | Incorporated | Added explicit `matched` assertions for both high-impact and generic loops |
| 2 | domain-packs-r2 | P2 | MQ3 still listed `kubectl delete secret` as Ask/Medium | Incorporated | Updated MQ3 expected outcome to Deny/High |
| 3 | domain-packs-r2 | P3 | SEC3 naming implied escalation behavior while testing preconditions only | Incorporated | Renamed SEC3 heading and function to preconditions terminology |
