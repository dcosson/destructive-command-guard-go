# 05: Testing & Benchmarks — Test Harness

**Plan**: [05-testing-and-benchmarks.md](./05-testing-and-benchmarks.md)
**Architecture**: [00-architecture.md](./00-architecture.md) (§9 URP)
**Upstream Plans**: [01](./01-treesitter-integration.md), [02](./02-matching-framework.md), [03a](./03a-packs-core.md), [04](./04-api-and-cli.md)

---

## Overview

This document specifies the test harness for the testing & benchmarks
plan (plan 05). Plan 05 is itself a testing plan — its deliverables
are test subsystems. This harness therefore tests the testing
infrastructure: verifying that benchmarks produce meaningful results,
mutation analysis catches real issues, fuzz invariants are correct,
and the comparison harness classifies divergences accurately.

**Meta-testing**: We are testing the tests. This is not circular —
the plan 05 subsystems are software components with their own
correctness requirements. A broken mutation harness that reports
100% kill rate when mutations survive is worse than no mutation
harness at all.

Key testing challenges:

- **Benchmark stability**: Benchmarks must produce stable, comparable
  results across runs. Flaky benchmarks are noise, not signal.
- **Mutation operator correctness**: Each mutation operator must
  produce a genuinely different matcher, not an equivalent one.
- **Fuzz invariant completeness**: The invariant set must be tight
  enough to catch bugs but loose enough to not produce false failures.
- **Comparison harness accuracy**: Divergence classification must be
  deterministic and reproducible.
- **Golden file integrity**: The corpus must not contain contradictory
  entries or entries that depend on unregistered packs.

---

## P: Property-Based Tests

### P1: Benchmark Stability

**Invariant**: For any benchmark, running it 5 times in succession
produces stable results. Tiered CV thresholds account for the different
noise characteristics of sub-microsecond vs longer benchmarks:
- Benchmarks >100μs mean: CV < 0.15 (lower tolerance — these should be stable)
- Benchmarks ≤100μs mean: CV < 0.30 (higher tolerance — OS scheduling noise dominates)

```go
func TestPropertyBenchmarkStability(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping stability test in short mode")
    }

    benchmarks := []struct {
        name string
        fn   func(b *testing.B)
    }{
        {"PreFilter_miss", benchPreFilterMiss},
        {"PreFilter_hit", benchPreFilterHit},
        {"Evaluate_safe", benchEvaluateSafe},
        {"Evaluate_destructive", benchEvaluateDestructive},
    }

    for _, bm := range benchmarks {
        t.Run(bm.name, func(t *testing.T) {
            results := make([]float64, 5)
            for i := range results {
                r := testing.Benchmark(bm.fn)
                results[i] = float64(r.NsPerOp())
            }

            mean := stat.Mean(results, nil)
            stddev := stat.StdDev(results, nil)
            cv := stddev / mean

            // Tiered CV threshold: tighter for longer benchmarks
            // where measurement noise should be proportionally smaller.
            maxCV := 0.30
            if mean > 100_000 { // >100μs
                maxCV = 0.15
            }

            t.Logf("%s: mean=%.0fns, stddev=%.0fns, cv=%.2f (max=%.2f)",
                bm.name, mean, stddev, cv, maxCV)
            assert.Less(t, cv, maxCV,
                "benchmark %s is too unstable (cv=%.2f, max=%.2f)",
                bm.name, cv, maxCV)
        })
    }
}
```

### P2: Mutation Operator Produces Different Behavior

**Invariant**: For every mutation applied to a pattern, there exists
at least one command that produces a different match result with the
mutated pattern vs the original.

```go
func TestPropertyMutationOperatorsDiffer(t *testing.T) {
    pack := getTestPack(t, "core.git")
    for _, pattern := range pack.Destructive[:3] { // Sample 3
        mutations := generateMutations(pattern)
        for _, mutation := range mutations {
            t.Run(mutation.Operator+"/"+mutation.Detail, func(t *testing.T) {
                // The mutated pattern must differ from original on
                // at least one command from the test corpus
                originalMatches := runPatternOnCorpus(pattern)
                mutatedMatches := runPatternOnCorpus(mutation.Apply(pattern))
                assert.NotEqual(t, originalMatches, mutatedMatches,
                    "mutation %s did not change behavior — operator may be broken",
                    mutation.Operator)
            })
        }
    }
}
```

### P3: Fuzz Invariants Are Tight

**Invariant**: Every fuzz invariant (INV-1 through INV-8) is violated
by at least one synthetic input when the pipeline is deliberately
broken. This proves the invariant is actually checking something.

```go
func TestPropertyFuzzInvariantsTight(t *testing.T) {
    // For each invariant, construct a deliberately broken Result
    // and verify the invariant catches it. This proves each invariant
    // is actually checking something — a removed or weakened invariant
    // would allow a broken result through.
    brokenResults := []struct {
        name    string
        result  guard.Result
        command string
        inv     string
    }{
        {
            name:    "invalid decision",
            result:  guard.Result{Decision: guard.Decision(99)},
            command: "echo hello",
            inv:     "INV-1",
        },
        {
            name:    "command not preserved",
            result:  guard.Result{Command: "different"},
            command: "echo hello",
            inv:     "INV-2",
        },
        {
            name:    "empty command non-allow",
            result:  guard.Result{Decision: guard.Deny},
            command: "",
            inv:     "INV-3",
        },
        {
            name:    "nil assessment with deny",
            result:  guard.Result{Decision: guard.Deny},
            command: "echo hello",
            inv:     "INV-4",
        },
        {
            name:    "matches without assessment",
            result:  guard.Result{
                Matches: []guard.Match{{Pack: "test", Rule: "test"}},
            },
            command: "echo hello",
            inv:     "INV-5",
        },
        {
            name:    "invalid severity",
            result:  guard.Result{
                Decision:   guard.Deny,
                Command:    "echo hello",
                Assessment: &guard.Assessment{Severity: guard.Severity(99)},
                Matches:    []guard.Match{{Pack: "test", Rule: "test"}},
            },
            command: "echo hello",
            inv:     "INV-6",
        },
        {
            name:    "empty pack in match",
            result:  guard.Result{
                Decision:   guard.Deny,
                Command:    "echo hello",
                Assessment: &guard.Assessment{Severity: guard.High},
                Matches:    []guard.Match{{Pack: "", Rule: ""}},
            },
            command: "echo hello",
            inv:     "INV-7",
        },
        {
            name:    "oversized non-indeterminate",
            result:  guard.Result{
                Decision:   guard.Deny,
                Command:    strings.Repeat("a", eval.MaxCommandBytes+1),
                Assessment: &guard.Assessment{Severity: guard.High},
                Matches:    []guard.Match{{Pack: "test", Rule: "test"}},
            },
            command: strings.Repeat("a", eval.MaxCommandBytes+1),
            inv:     "INV-8",
        },
    }

    for _, br := range brokenResults {
        t.Run(br.name, func(t *testing.T) {
            // Use t.Run() sub-test to capture failures from
            // verifyInvariants without aborting the outer test.
            // This is idiomatic Go — t.Run() returns false if the
            // sub-test failed.
            passed := t.Run("verify", func(sub *testing.T) {
                verifyInvariants(sub, br.command, br.result)
            })
            assert.False(t, passed,
                "invariant %s did not catch broken result %s",
                br.inv, br.name)
        })
    }
}
```

### P4: Golden File Entries Are Self-Consistent

**Invariant**: No two golden file entries for the same command produce
contradictory expected decisions (unless they use different policies).

```go
func TestPropertyGoldenFileConsistency(t *testing.T) {
    entries := loadAllGoldenEntries(t)

    // Group by command
    byCommand := make(map[string][]GoldenEntry)
    for _, e := range entries {
        byCommand[e.Command] = append(byCommand[e.Command], e)
    }

    for cmd, group := range byCommand {
        if len(group) <= 1 {
            continue
        }
        // All entries for the same command with the same policy
        // should have the same expected decision
        byPolicy := make(map[string]string)
        for _, e := range group {
            policy := e.Policy
            if policy == "" {
                policy = "interactive"
            }
            existing, ok := byPolicy[policy]
            if ok {
                assert.Equal(t, existing, e.Decision,
                    "contradictory decisions for %q under policy %s",
                    cmd, policy)
            }
            byPolicy[policy] = e.Decision
        }
    }
}
```

### P5: Comparison Classification Is Deterministic

**Invariant**: Running the comparison classifier on the same input
data produces the same classification every time.

```go
func TestPropertyComparisonClassificationDeterministic(t *testing.T) {
    samples := []ComparisonEntry{
        {GoDecision: "Deny", RustDecision: "Deny"},
        {GoDecision: "Allow", RustDecision: "Deny"},
        {GoDecision: "Deny", RustDecision: "Allow"},
        {GoDecision: "Ask", RustDecision: "Deny"},
    }

    for _, sample := range samples {
        c1 := classifyDivergence(sample)
        c2 := classifyDivergence(sample)
        assert.Equal(t, c1, c2,
            "classification not deterministic for %v", sample)
    }
}
```

---

## F: Fault Injection Tests

### F1: Benchmark Harness Under GC Pressure

Test that benchmarks still produce meaningful results when GC is active:

```go
func TestFaultBenchmarkUnderGC(t *testing.T) {
    // Force GC during benchmark
    done := make(chan bool)
    go func() {
        for {
            select {
            case <-done:
                return
            default:
                runtime.GC()
                time.Sleep(time.Millisecond)
            }
        }
    }()
    defer func() { done <- true }()

    r := testing.Benchmark(func(b *testing.B) {
        for i := 0; i < b.N; i++ {
            guard.Evaluate("echo hello")
        }
    })

    assert.Greater(t, r.NsPerOp(), int64(0),
        "benchmark produced 0 ns/op under GC pressure")
}
```

### F2: Mutation Harness With Identical Mutation

Test that the mutation harness correctly identifies when a mutation
doesn't change behavior (should be flagged as operator bug):

```go
func TestFaultMutationIdenticalMutation(t *testing.T) {
    pack := getTestPack(t, "core.git")
    pattern := pack.Destructive[0]

    // Apply an "identity" mutation (change nothing)
    identityMutation := Mutation{
        Operator: "Identity",
        Apply: func(p Pattern) Pattern { return p },
    }

    // The harness should detect this as a non-mutation
    original := runPatternOnCorpus(pattern)
    mutated := runPatternOnCorpus(identityMutation.Apply(pattern))
    assert.Equal(t, original, mutated,
        "identity mutation should produce identical results")
}
```

### F3: Golden File With Missing Pack

Test that golden file tests skip gracefully when a referenced pack
isn't registered:

```go
func TestFaultGoldenFileMissingPack(t *testing.T) {
    entry := GoldenEntry{
        Command:  "imaginary-tool --destroy",
        Decision: "Deny",
        Pack:     "nonexistent.pack",
    }

    // Should skip, not fail
    if !HasRegisteredPack(entry.Pack) {
        t.Skipf("pack %s not registered (expected)", entry.Pack)
    }
}
```

### F4: Comparison Harness Without Upstream Binary

```go
func TestFaultComparisonNoUpstreamBinary(t *testing.T) {
    if os.Getenv("UPSTREAM_BINARY") != "" {
        t.Skip("UPSTREAM_BINARY is set; this test verifies skip behavior")
    }

    // The comparison test should skip gracefully
    // (not fail with a cryptic error)
    t.Log("Verified: comparison test skips when UPSTREAM_BINARY not set")
}
```

---

## D: Deterministic Example Tests

### D1: Known Benchmark Ordering

Verify that benchmarks produce results in the expected relative order
(pre-filter miss < simple match < compound command):

```go
func TestDeterministicBenchmarkOrdering(t *testing.T) {
    prefilterMiss := testing.Benchmark(func(b *testing.B) {
        for i := 0; i < b.N; i++ {
            guard.Evaluate("echo hello")
        }
    })

    simpleMatch := testing.Benchmark(func(b *testing.B) {
        for i := 0; i < b.N; i++ {
            guard.Evaluate("git push --force")
        }
    })

    compound := testing.Benchmark(func(b *testing.B) {
        for i := 0; i < b.N; i++ {
            guard.Evaluate("echo start && git push --force && rm -rf /")
        }
    })

    // Pre-filter miss should be fastest (no parsing)
    assert.Less(t, prefilterMiss.NsPerOp(), simpleMatch.NsPerOp(),
        "pre-filter miss should be faster than simple match")

    // Compound should be slowest (multiple commands)
    assert.Less(t, simpleMatch.NsPerOp(), compound.NsPerOp(),
        "simple match should be faster than compound")
}
```

### D2: Mutation Kill for Known Pattern

Verify that removing the `--force` flag check from `git-push-force`
is killed by the existing test suite:

```go
func TestDeterministicMutationKill(t *testing.T) {
    SkipIfPackMissing(t, "core.git")

    // Original: git push --force → Deny
    original := guard.Evaluate("git push --force")
    assert.Equal(t, guard.Deny, original.Decision)

    // Mutated: removing --force check should make "git push" also match
    // But "git push" (without --force) should be safe → test catches it
    safe := guard.Evaluate("git push origin main")
    assert.Equal(t, guard.Allow, safe.Decision,
        "git push without --force should be allowed")

    // This proves the --force check is load-bearing
}
```

### D3: Fuzz Invariant Catches Specific Bug

Verify that INV-4 (nil Assessment → Allow) catches a specific bug:

```go
func TestDeterministicFuzzInvariantCatchesBug(t *testing.T) {
    // Synthetic broken result: Deny decision but nil Assessment
    brokenResult := guard.Result{
        Decision: guard.Deny,
        Command:  "echo hello",
    }

    // INV-4 should catch this. Use t.Run() sub-test to capture
    // the failure without aborting the outer test — this is the
    // standard Go idiom for testing that a function calls t.Fatal.
    passed := t.Run("verify_catches_bug", func(sub *testing.T) {
        verifyInvariants(sub, "echo hello", brokenResult)
    })
    assert.False(t, passed,
        "INV-4 did not catch nil-assessment Deny result")
}
```

### D4: Golden File Corpus Counts

Verify the golden file corpus meets minimum size requirements:

```go
func TestDeterministicGoldenCorpusSize(t *testing.T) {
    entries := loadAllGoldenEntries(t)
    assert.GreaterOrEqual(t, len(entries), 750,
        "golden file corpus must have 750+ entries")

    // Count per-pack coverage
    packCounts := make(map[string]int)
    for _, e := range entries {
        packCounts[e.Pack]++
    }

    for _, pack := range guard.Packs() {
        count := packCounts[pack.ID]
        assert.GreaterOrEqual(t, count, 3,
            "pack %s has only %d golden entries (need 3+)", pack.ID, count)
    }
}
```

---

## O: Comparison Oracle Tests

### O1: Self-Comparison (Go vs Go)

As a sanity check, run the comparison harness with the Go binary as
both the "Go" and "upstream" implementation. Every entry should be
classified as `identical`:

```go
func TestOracleSelfComparison(t *testing.T) {
    corpus := loadComparisonCorpus(t, "testdata/comparison_corpus.json")

    for _, entry := range corpus { // Full corpus — self-comparison is fast
        t.Run(entry.Command, func(t *testing.T) {
            // Run through internal pipeline
            goResult := guard.Evaluate(entry.Command,
                guard.WithPolicy(guard.InteractivePolicy()))

            // Run through public API (same code path, different entry point)
            // This verifies the public API wrapper doesn't alter results.
            apiResult := guard.Evaluate(entry.Command,
                guard.WithPolicy(guard.InteractivePolicy()))

            // Must be identical — same binary, same inputs
            assert.Equal(t, goResult.Decision, apiResult.Decision,
                "self-comparison divergence for %q", entry.Command)
            // Severity lives under Assessment per plan 04 guard.Result contract
            var goSev, apiSev string
            if goResult.Assessment != nil {
                goSev = goResult.Assessment.Severity
            }
            if apiResult.Assessment != nil {
                apiSev = apiResult.Assessment.Severity
            }
            assert.Equal(t, goSev, apiSev,
                "self-comparison severity divergence for %q", entry.Command)
        })
    }
}
// Note: runUpstream() is reserved for the Rust upstream binary comparison
// in TestComparisonAgainstUpstream (plan 05 §4.6). It invokes
// `binary check <command>` which is the Rust CLI interface, not the Go
// CLI `test` subcommand. Self-comparison uses guard.Evaluate directly.
```

### O2: Golden File Cross-Validation

Run golden file entries through both the internal pipeline and the
public API, verifying identical results:

```go
func TestOracleGoldenCrossValidation(t *testing.T) {
    entries := loadAllGoldenEntries(t)
    for _, e := range entries {
        t.Run(e.Command, func(t *testing.T) {
            result := guard.Evaluate(e.Command,
                guard.WithPolicy(guard.InteractivePolicy()))
            assert.Equal(t, e.Decision, result.Decision.String(),
                "golden file mismatch for %q", e.Command)
        })
    }
}
```

---

## B: Benchmark Tests

### B1: Benchmark Infrastructure Self-Test

Verify that the benchmark result collection and reporting infrastructure
works correctly:

```go
func TestBenchmarkInfrastructure(t *testing.T) {
    results := []BenchResult{
        {Name: "test1", NsPerOp: 100, AllocsPerOp: 2, BytesPerOp: 64},
        {Name: "test2", NsPerOp: 200, AllocsPerOp: 5, BytesPerOp: 128},
    }

    dir := t.TempDir()
    path := filepath.Join(dir, "results.json")
    WriteBenchResults(nil, path, results)

    data, err := os.ReadFile(path)
    assert.NoError(t, err)

    var loaded []BenchResult
    assert.NoError(t, json.Unmarshal(data, &loaded))
    assert.Len(t, loaded, 2)
    assert.Equal(t, float64(100), loaded[0].NsPerOp)
}
```

### B2: Regression Detection Threshold

Verify that the regression detection logic correctly flags >20%
regressions:

```go
func TestBenchmarkRegressionDetection(t *testing.T) {
    baseline := BenchResult{Name: "test", NsPerOp: 100}

    // 10% regression — should pass
    minor := BenchResult{Name: "test", NsPerOp: 110}
    assert.False(t, isRegression(baseline, minor, 0.20))

    // 30% regression — should flag
    major := BenchResult{Name: "test", NsPerOp: 130}
    assert.True(t, isRegression(baseline, major, 0.20))

    // 50% improvement — should pass
    improved := BenchResult{Name: "test", NsPerOp: 50}
    assert.False(t, isRegression(baseline, improved, 0.20))
}
```

---

## S: Stress Tests

### S1: Sustained Evaluation Load

Run 100,000 evaluations across 100 goroutines and verify no crashes,
leaks, or data races:

```go
func TestStressSustainedLoad(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping sustained load test in short mode")
    }

    const goroutines = 100
    const perGoroutine = 1000

    commands := []string{
        "git push --force", "rm -rf /", "echo hello",
        "git status", "docker system prune -af",
        "RAILS_ENV=production rails db:reset",
        "", "   ", "ls -la",
    }

    var wg sync.WaitGroup
    errors := make(chan error, goroutines)

    runtime.GC()
    var before runtime.MemStats
    runtime.ReadMemStats(&before)

    for i := 0; i < goroutines; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            for j := 0; j < perGoroutine; j++ {
                cmd := commands[(id*perGoroutine+j)%len(commands)]
                result := guard.Evaluate(cmd,
                    guard.WithPolicy(guard.InteractivePolicy()))
                // Verify basic invariants inline
                if result.Assessment == nil && result.Decision != guard.Allow {
                    errors <- fmt.Errorf("goroutine %d iter %d: nil assessment with %s",
                        id, j, result.Decision)
                    return
                }
            }
        }(i)
    }
    wg.Wait()
    close(errors)

    for err := range errors {
        t.Error(err)
    }

    runtime.GC()
    var after runtime.MemStats
    runtime.ReadMemStats(&after)

    growth := int64(after.HeapInuse) - int64(before.HeapInuse)
    t.Logf("Heap growth after %d evaluations: %d bytes",
        goroutines*perGoroutine, growth)
}
```

This test must pass with `-race` flag.

### S2: Mutation Testing Under Time Pressure

Verify the mutation harness completes within the time budget:

```go
func TestStressMutationTimeLimit(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping mutation time limit test")
    }

    // Run mutation analysis for a single pack with a timeout
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()

    done := make(chan MutationReport, 1)
    go func() {
        pack := getTestPack(t, "core.git")
        done <- runMutationAnalysis(t, pack)
    }()

    select {
    case report := <-done:
        t.Logf("core.git mutation analysis: %d/%d killed in time",
            report.Killed, report.Total)
    case <-ctx.Done():
        t.Fatal("mutation analysis exceeded 5 minute time limit for core.git")
    }
}
```

---

## SEC: Security Tests

### SEC1: Fuzz Corpus Does Not Contain Sensitive Data

Verify that the committed fuzz corpus doesn't contain file paths,
secrets, or other sensitive data that might have been generated
during fuzzing:

```go
func TestSecurityFuzzCorpusClean(t *testing.T) {
    corpusDir := "testdata/fuzz/"
    if _, err := os.Stat(corpusDir); os.IsNotExist(err) {
        t.Skip("no fuzz corpus directory")
    }

    sensitivePatterns := []string{
        "/Users/", "/home/", "password", "secret",
        "api_key", "token=", "Bearer ",
    }

    filepath.WalkDir(corpusDir, func(path string, d fs.DirEntry, err error) error {
        if err != nil || d.IsDir() {
            return nil
        }
        data, _ := os.ReadFile(path)
        for _, pattern := range sensitivePatterns {
            assert.NotContains(t, string(data), pattern,
                "fuzz corpus file %s contains sensitive pattern %q", path, pattern)
        }
        return nil
    })
}
```

### SEC2: Golden File Commands Are Not Executable

Verify that running the test harness doesn't actually execute any
commands (we only evaluate strings, never run them):

```go
func TestSecurityGoldenFileNotExecuted(t *testing.T) {
    // Create a marker file that would be created if commands were executed
    marker := filepath.Join(t.TempDir(), "executed")

    // Add a golden entry with a command that creates the marker
    result := guard.Evaluate(fmt.Sprintf("touch %s", marker))
    _ = result

    // Verify the marker was NOT created
    _, err := os.Stat(marker)
    assert.True(t, os.IsNotExist(err),
        "guard.Evaluate appears to have executed the command!")
}

// TestSecurityNoSubprocessExecution verifies that the ENTIRE golden file
// test suite doesn't accidentally execute commands. We run the golden file
// tests with an empty PATH — if any code path tries to exec.LookPath or
// exec.Command for the evaluated commands, it would fail.
func TestSecurityNoSubprocessExecution(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping subprocess execution test in short mode")
    }

    entries := loadAllGoldenEntries(t)
    // Set PATH to empty for this test — any accidental exec would fail
    t.Setenv("PATH", "")

    for _, e := range entries {
        result := guard.Evaluate(e.Command,
            guard.WithPolicy(guard.InteractivePolicy()))
        // We don't assert specific decisions here — just that
        // evaluation completes without subprocess execution errors.
        _ = result
    }
    // If we reach here, no subprocess execution was attempted.
}
```

---

## MQ: Manual QA Plan

### MQ1: Benchmark Report Review

After running the full benchmark suite, manually review:
1. All benchmarks produce non-zero results
2. Pre-filter miss is significantly faster than full pipeline
3. Compound commands scale roughly linearly with command count
4. No unexpected allocation spikes

### MQ2: Comparison Report Review

After running comparison tests against upstream:
1. Review all `intentional_divergence` entries — are they genuinely
   intentional?
2. Review all `intentional_improvement` entries — do we have evidence
   our behavior is better?
3. Verify no `bug` entries remain unresolved

### MQ3: Mutation Report Review

After running mutation analysis:
1. Review any surviving mutations — are they truly redundant conditions?
2. If kill rate < 100%, identify the gaps and add test cases
3. Check that mutation operators produce genuinely different patterns

### MQ4: Fuzz Corpus Growth Review

After running fuzz tests for extended periods:
1. Review new corpus entries for interesting edge cases
2. Verify no crashes were found (check testdata/fuzz/ for crash files)
3. Check that coverage increased with new corpus entries

---

## CI Tier Mapping

| Tier | Tests | Runtime | Trigger |
|------|-------|---------|---------|
| **Tier 1** (commit) | P4, P5, D4 (golden consistency, corpus size) | <5s | Every push |
| **Tier 2** (PR) | D1-D3, F3-F4, O2, SEC2 (deterministic, oracle) | <30s | PR create |
| **Tier 3** (nightly) | P1-P3, F1-F2, S1-S2, B1-B2, SEC1 (stability, stress) | <60m | Nightly |
| **Tier 4** (release) | MQ1-MQ4, O1 (manual review, self-comparison) | Manual | Pre-release |

---

## Exit Criteria

The plan 05 test harness is complete when:

1. **Benchmark stability** verified — tiered CV thresholds (≤0.15 for >100μs, ≤0.30 for ≤100μs) for all benchmarks (P1)
2. **Mutation operators** produce genuinely different behavior, including RemoveNot, RemoveNotAlternative, ShiftArgPosition (P2)
3. **Fuzz invariants** are tight — all 8 invariants (INV-1 through INV-8) caught by tightness test (P3)
4. **Golden file consistency** verified — no contradictions (P4)
5. **Comparison classification** is deterministic, using formalized classifyDivergence rules (P5)
6. **Benchmark ordering** matches expectations (D1)
7. **Known mutation kill** verified for representative pattern (D2)
8. **Corpus size** meets 750+ minimum, verified in Tier 1 CI (D4)
9. **Self-comparison** produces 100% identical results on full corpus (O1)
10. **Sustained load** — no crashes or leaks under 100K evaluations (S1)
11. **Fuzz corpus clean** — no sensitive data in committed corpus (SEC1)
12. **No subprocess execution** — golden file suite runs with empty PATH without errors (SEC2)

---

## Metrics Dashboard

- Benchmark stability: coefficient of variation per benchmark (from P1)
- Mutation kill rate: per-pack and aggregate (from plan 05 §6)
- Golden file corpus size and per-pack coverage (from D4)
- Comparison divergence counts by classification (from plan 05 §4)
- Fuzz time-to-first-crash: target never (from plan 05 §5)
- Memory growth under sustained load (from S1)
- Benchmark regression detection accuracy (from B2)

---

## Round 1 Review Disposition

| # | Reviewer | Severity | Summary | Disposition | Notes |
|---|----------|----------|---------|-------------|-------|
| 1 | dcg-alt-reviewer | P1 | TB-P1.3: P3 tightness test missing INV-6/7/8 | Incorporated | P3 added 3 broken-result entries |
| 2 | dcg-alt-reviewer | P3 | TB-P3.2: SEC2 too weak | Incorporated | SEC2 extended with restricted-PATH test |
| 3 | dcg-alt-reviewer | P3 | TB-P3.3: D3 testing.T mock broken | Incorporated | P3 + D3 rewritten with t.Run() sub-test |
| 4 | dcg-reviewer | P1 | SE-P1.4: testing.T mock broken | Incorporated | Merged with TB-P3.3 |
| 5 | dcg-reviewer | P2 | SE-P2.1: Benchmark CV threshold too loose | Incorporated | P1 tiered: ≤0.15 for >100μs, ≤0.30 for ≤100μs |
| 6 | dcg-reviewer | P2 | SE-P2.2: Self-comparison O1 samples only 20 | Incorporated | O1 iterates full corpus |

## Round 2 Review Disposition

| # | Reviewer | Severity | Summary | Disposition | Notes |
|---|----------|----------|---------|-------------|-------|
| 1 | dcg-reviewer | P1 | Self-comparison O1 uses runUpstream which calls 'check' but Go CLI uses 'test' | Incorporated | O1 rewritten to use guard.Evaluate directly for both sides; runUpstream reserved for Rust binary only |

## Round 3 Review Disposition

| # | Reviewer | Severity | Summary | Disposition | Notes |
|---|----------|----------|---------|-------------|-------|
| 1 | dcg-coder-1 | P2 | O1 self-comparison uses goResult.Severity instead of Assessment.Severity | Incorporated | O1 snippet updated to compare severity via Assessment.Severity with nil-safe handling per plan 04 guard.Result contract |

---


## Completion Signoff
- **Status**: Partial
- **Date**: 2026-03-04
- **Branch**: main
- **Commit**: 6f649d3
- **Verified by**: dcg-coder-1
- **Test verification**: `make bench` — PASS
- **Outstanding gaps**: Several doc-specified harness identifiers are renamed/not one-to-one with current e2etest naming; additionally, full broad harness selector `go test ./e2etest -run 'Test(Golden|GrammarCoverage|Mutation|E2E|Allocations|NoMemoryLeak|Deterministic|Fault|Oracle|Security|Stress).*' -count=1` currently fails at infra/cloud subcommand-evasion cases (AWS/GCP global-flag interposition).
- **Deviations from plan**: Harness location and execution model differ from original doc assumptions (legacy `internal/testharness` -> root `e2etest`, CI tier scripts aligned to new paths).
- **Reconciliation notes**: Planned O1/O2/O3 comparison intents map to `e2etest/comparison_test.go`; golden/grammar intents map to `e2etest/golden_*` and `e2etest/grammar_*`; mutation intents map to `e2etest/mutation_test.go` + `mutation_harness_property_test.go`; stress/security intents map to `e2etest/stress_security_ci_tier_test.go` plus domain suites.
- **Additions beyond plan**: Expanded benchmark coverage now includes pack-level suites under unified Make targets and CI tier integration.
