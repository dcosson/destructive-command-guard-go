# 03a: Core Packs — Test Harness

**Plan**: [03a-packs-core.md](./03a-packs-core.md)
**Architecture**: [00-architecture.md](./00-architecture.md)

---

## Overview

This document specifies the test harness for the core packs (plan 03a).
It covers property-based tests, deterministic examples, fault injection,
comparison oracles, benchmarks, stress tests, security tests, manual QA,
CI tier mapping, and exit criteria.

The test harness complements the unit tests described in the plan doc §7.
Unit tests verify individual pattern behavior. This harness verifies
system-level properties, cross-pattern interactions, and robustness.

---

## P: Property-Based Tests

### P1: Every Destructive Pattern Has a Matching Command

**Invariant**: For each destructive pattern in each pack, there exists at
least one `ExtractedCommand` that the pattern matches.

```go
func TestPropertyEveryDestructivePatternReachable(t *testing.T) {
    for _, pack := range packs.DefaultRegistry.All() {
        for _, dp := range pack.Destructive {
            t.Run(pack.ID+"/"+dp.Name, func(t *testing.T) {
                // The reachability commands from plan doc §7.1 must be
                // maintained for each pattern. This test verifies they exist
                // and actually match.
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

```go
func TestPropertySafePatternsNeverBlockDestructive(t *testing.T) {
    for _, pack := range packs.DefaultRegistry.All() {
        for _, dp := range pack.Destructive {
            cmd := getReachabilityCommand(pack.ID, dp.Name)
            for _, sp := range pack.Safe {
                assert.False(t, sp.Match.Match(cmd),
                    "safe pattern %s blocks destructive %s", sp.Name, dp.Name)
            }
        }
    }
}
```

### P3: Pattern Name Uniqueness

**Invariant**: All pattern names (safe and destructive combined) within a
pack are unique.

```go
func TestPropertyPatternNameUniqueness(t *testing.T) {
    for _, pack := range packs.DefaultRegistry.All() {
        names := make(map[string]bool)
        for _, sp := range pack.Safe {
            assert.False(t, names[sp.Name], "duplicate name: %s in %s", sp.Name, pack.ID)
            names[sp.Name] = true
        }
        for _, dp := range pack.Destructive {
            assert.False(t, names[dp.Name], "duplicate name: %s in %s", dp.Name, pack.ID)
            names[dp.Name] = true
        }
    }
}
```

### P4: Matchers Are Deterministic

**Invariant**: Given the same `ExtractedCommand`, each matcher always returns
the same result.

```go
func TestPropertyMatcherDeterminism(t *testing.T) {
    f := func(name string, args [3]string, flagKey string) bool {
        cmd := parse.ExtractedCommand{
            Name:  name,
            Args:  []string{args[0], args[1], args[2]},
            Flags: map[string]string{flagKey: ""},
        }
        for _, pack := range packs.DefaultRegistry.All() {
            for _, dp := range pack.Destructive {
                r1 := dp.Match.Match(cmd)
                r2 := dp.Match.Match(cmd)
                if r1 != r2 {
                    return false
                }
            }
        }
        return true
    }
    quick.Check(f, &quick.Config{MaxCount: 5000})
}
```

### P5: No Pack Has Zero Keywords

**Invariant**: Every registered pack has at least one keyword.

```go
func TestPropertyAllPacksHaveKeywords(t *testing.T) {
    for _, pack := range packs.DefaultRegistry.All() {
        assert.NotEmpty(t, pack.Keywords, "pack %s has no keywords", pack.ID)
    }
}
```

### P6: (Merged into P2)

P6 was originally a strengthened version of P2 ensuring the reachability
command set covers every destructive pattern. Since P1 already guarantees
every destructive pattern has a reachability command, P1 + P2 = P6. The
P2 test above is the authoritative safe/destructive mutual exclusion test.

### P7: No Destructive Pattern Matches Empty Command

**Invariant**: An empty `ExtractedCommand` (empty name, no args, no flags)
matches no destructive pattern.

```go
func TestPropertyEmptyCommandMatchesNothing(t *testing.T) {
    empty := parse.ExtractedCommand{}
    for _, pack := range packs.DefaultRegistry.All() {
        for _, dp := range pack.Destructive {
            assert.False(t, dp.Match.Match(empty),
                "pattern %s/%s matches empty command", pack.ID, dp.Name)
        }
    }
}
```

---

## E: Deterministic Examples

### E1: core.git Pattern Matrix (60+ cases)

Complete test matrix for all git patterns. Each row tests one specific
command variant against both safe and destructive patterns. Uses
InteractivePolicy for decision column.

```
# git push variants
git push origin main                     → Allow (git-push-safe)
git push --force origin main             → Deny/High (git-push-force)
git push -f origin main                  → Deny/High (git-push-force)
git push --force-with-lease origin main  → Allow (git-push-force-with-lease)
git push --force-with-lease --force      → Deny/High (git-push-force)
git push --mirror                        → Deny/High (git-push-mirror)
git push --delete origin feature         → Ask/Medium (git-push-delete)
git push -d origin feature               → Ask/Medium (git-push-delete)
git push origin :feature-branch          → Ask/Medium (git-push-refspec-delete)
git push origin +main:main               → Deny/High (git-push-force-refspec)
git push origin main:main                → Allow (git-push-safe, normal refspec)

# git reset variants
git reset --hard HEAD~3                  → Deny/High (git-reset-hard)
git reset --hard HEAD                    → Deny/High (git-reset-hard)
git reset --soft HEAD~1                  → Allow (git-reset-safe)
git reset HEAD~1                         → Allow (git-reset-safe)
git reset --mixed HEAD                   → Allow (git-reset-safe)

# git checkout variants
git checkout -- .                        → Deny/High (git-checkout-discard-all)
git checkout .                           → Deny/High (git-checkout-dot)
git checkout -- src/main.go              → Allow/Low (git-checkout-discard-file)
git checkout main                        → Allow (no pattern match — checkout to branch)

# git restore variants
git restore .                            → Deny/High (git-restore-worktree-all)
git restore --worktree .                 → Deny/High (git-restore-worktree-all)
git restore --source HEAD~3 file.go      → Deny/High (git-restore-source)
git restore --staged file.go             → Allow (git-restore-staged-safe)
git restore file.go                      → Allow/Low (git-restore-file)

# git clean variants
git clean -f                             → Ask/Medium (git-clean-force)
git clean -fd                            → Ask/Medium (git-clean-force-dirs)
git clean --force -d                     → Ask/Medium (git-clean-force-dirs)
git clean -n                             → Allow (no match, dry-run)
git clean                                → Allow (no match, refuses without -f)

# git branch variants
git branch                               → Allow (git-branch-safe, list mode)
git branch -d merged-branch              → Allow (git-branch-safe, safe delete)
git branch -D feature                    → Ask/Medium (git-branch-force-delete)
git branch --delete --force old          → Ask/Medium (git-branch-force-delete)

# git rebase
git rebase main                          → Deny/High (git-rebase, ConfidenceMedium)
git rebase -i HEAD~5                     → Deny/High (git-rebase, ConfidenceMedium)
git rebase --abort                       → Allow (git-rebase-recovery)
git rebase --continue                    → Allow (git-rebase-recovery)
git rebase --skip                        → Allow (git-rebase-recovery)

# git stash
git stash                                → Allow (no match)
git stash pop                            → Allow (no match)
git stash drop stash@{0}                 → Ask/Medium (git-stash-drop)
git stash clear                          → Ask/Medium (git-stash-drop)

# git reflog / gc / filter
git reflog expire --expire=now --all     → Deny/High (git-reflog-expire)
git reflog                               → Allow (no match)
git gc --prune=now                       → Ask/Medium (git-gc-prune)
git gc                                   → Allow (no match)
git filter-branch --tree-filter 'rm f'   → Deny/High (git-filter-branch)
git filter-repo --path secret --invert   → Deny/High (git-filter-branch)

# Read-only commands
git status                               → Allow (git-status safe)
git log --oneline -20                    → Allow (git-log safe)
git diff HEAD~1                          → Allow (git-diff safe)
git fetch origin                         → Allow (git-fetch safe)
git show HEAD                            → Allow (no pattern match)
```

### E2: core.filesystem Pattern Matrix (40+ cases)

Uses InteractivePolicy for decision column.

```
# rm variants
rm file.txt                              → Allow (rm-single-safe)
rm -i file.txt                           → Allow (rm-single-safe)
rm -f file.txt                           → Allow (rm-single-safe, -f allowed)
rm -rf /                                 → Deny/Critical (rm-rf-root)
rm -rf /*                                → Deny/Critical (rm-rf-root)
rm -rf /tmp/build                        → Deny/High (rm-recursive-force)
rm -fr ./dist                            → Deny/High (rm-recursive-force)
rm -r -f /tmp/build                      → Deny/High (rm-recursive-force)
rm --recursive --force ./build           → Deny/High (rm-recursive-force)
rm -r ./build                            → Ask/Medium (rm-recursive)
rm -R ./build                            → Ask/Medium (rm-recursive)
rm --recursive ./build                   → Ask/Medium (rm-recursive)

# mkfs variants
mkfs.ext4 /dev/sda1                      → Deny/Critical (mkfs-any)
mkfs.xfs /dev/nvme0n1p1                  → Deny/Critical (mkfs-any)
mkfs /dev/sda                            → Deny/Critical (mkfs-any)
mkfs.btrfs /dev/sdb1                     → Deny/Critical (mkfs-any)
mkfs.ntfs /dev/sda2                      → Deny/Critical (mkfs-any)

# dd variants
dd if=/dev/zero of=/dev/sda bs=4M        → Deny/High (dd-write)
dd if=/dev/urandom of=/tmp/disk.img      → Deny/High (dd-write)
dd if=/dev/zero bs=1M count=10           → Allow (no of=)

# shred
shred /dev/sda                           → Deny/High (shred-any)
shred -vfz file.txt                      → Deny/High (shred-any)
shred file.txt                           → Deny/High (shred-any)

# chmod variants
chmod 644 file.txt                       → Allow (chmod-single-safe)
chmod +x script.sh                       → Allow (chmod-single-safe)
chmod -R 755 ./app                       → Ask/Medium (chmod-recursive)
chmod --recursive 644 ./app              → Ask/Medium (chmod-recursive)
chmod 777 script.sh                      → Ask/Medium (chmod-777)
chmod -R 777 ./app                       → Ask/Medium (both chmod-recursive and chmod-777 match; chmod-recursive listed as primary match due to evaluation order)
chmod 000 important.conf                 → Ask/Medium (chmod-000)

# chown variants
chown user:group file.txt                → Allow (chown-single-safe)
chown -R root:root /var                  → Ask/Medium (chown-recursive)
chown --recursive www:www /var/www       → Ask/Medium (chown-recursive)

# mv variants
mv file.txt backup/                      → Allow (mv-safe)
mv important.db /dev/null                → Ask/Medium (mv-to-devnull)

# truncate variants
truncate -s 0 /var/log/app.log           → Ask/Medium (truncate-zero)
truncate --size 0 data.db                → Ask/Medium (truncate-zero)
```

### E3: Cross-Pack Non-Interference (10+ cases)

Verify that one pack's safe pattern doesn't affect another pack's evaluation:

```
# git clean followed by rm — both packs evaluate independently
git clean -f && rm -rf /tmp              → Deny/High (both packs match independently)

# Safe in one pack, destructive in another
git push origin main && rm -rf /tmp      → Deny/High (git safe, filesystem destructive)

# Multiple matches across packs
git reset --hard && shred /dev/sda       → Deny/High (both packs match, worst wins)
```

---

## F: Fault Injection

### F1: Matcher Panic Recovery

Inject panics into matchers and verify pipeline recovery:

```go
func TestFaultMatcherPanic(t *testing.T) {
    // Create a pack with a matcher that panics
    panicPack := packs.Pack{
        ID:       "test.panic",
        Keywords: []string{"panic"},
        Destructive: []packs.DestructivePattern{{
            Name: "panicker",
            Match: panicMatcher{}, // implements Match() { panic("boom") }
            Severity: guard.High,
            Confidence: guard.ConfidenceHigh,
            Reason: "test",
            Remediation: "test",
        }},
    }
    // Pipeline should recover, add WarnMatcherPanic, and continue
    result := pipeline.Run(ctx, "panic test", cfg)
    assert.NotEqual(t, guard.Deny, result.Decision) // Panic prevented match
    assert.True(t, hasWarning(result.Warnings, guard.WarnMatcherPanic))
}
```

### F2: Nil Map Fields in ExtractedCommand

Test that matchers handle nil maps gracefully:

```go
func TestFaultNilFlags(t *testing.T) {
    cmd := parse.ExtractedCommand{
        Name:  "git",
        Args:  []string{"push"},
        Flags: nil, // nil, not empty map
    }
    // All patterns should handle this without panic
    for _, dp := range gitPack.Destructive {
        assert.NotPanics(t, func() { dp.Match.Match(cmd) })
    }
}

func TestFaultNilArgs(t *testing.T) {
    cmd := parse.ExtractedCommand{
        Name:  "rm",
        Args:  nil,
        Flags: map[string]string{"-rf": ""},
    }
    for _, dp := range fsPack.Destructive {
        assert.NotPanics(t, func() { dp.Match.Match(cmd) })
    }
}
```

### F3: Empty String Inputs

Test patterns with empty strings in various fields:

```go
func TestFaultEmptyStrings(t *testing.T) {
    cmds := []parse.ExtractedCommand{
        {Name: "", Args: nil, Flags: nil},
        {Name: "git", Args: []string{""}, Flags: nil},
        {Name: "git", Args: []string{"push"}, Flags: map[string]string{"": ""}},
        {Name: "", Args: []string{""}, Flags: map[string]string{"": ""}},
    }
    for _, c := range cmds {
        for _, pack := range packs.DefaultRegistry.All() {
            for _, dp := range pack.Destructive {
                assert.NotPanics(t, func() { dp.Match.Match(c) })
            }
            for _, sp := range pack.Safe {
                assert.NotPanics(t, func() { sp.Match.Match(c) })
            }
        }
    }
}
```

---

## O: Comparison Oracle Tests

### O1: Upstream Rust Version Comparison

Compare core pack results against the upstream Rust `destructive-command-guard`
for a shared command corpus. Commands are run through both versions and
differences are categorized.

```go
func TestComparisonUpstreamRust(t *testing.T) {
    if testing.Short() {
        t.Skip("comparison tests require upstream binary")
    }
    corpus := loadComparisonCorpus(t, "testdata/comparison/core_commands.txt")
    for _, entry := range corpus {
        t.Run(entry.Command, func(t *testing.T) {
            goResult := pipeline.Run(ctx, entry.Command, cfg)
            rustResult := runUpstream(t, entry.Command)

            if goResult.Decision != rustResult.Decision {
                // Log the difference, categorize as:
                // - Intentional improvement (Go catches more)
                // - Intentional divergence (different design)
                // - Bug (Go misses something)
                t.Logf("DIVERGENCE: %q go=%v rust=%v", entry.Command,
                    goResult.Decision, rustResult.Decision)
            }
        })
    }
}
```

**Comparison corpus** (shared with Batch 5, seeded here):
- All commands from E1 and E2
- 20 additional edge cases: unusual flag ordering, quoted arguments,
  path-prefixed binaries, compound commands

### O2: Decision Consistency Across Policies

For each command in the golden file corpus, verify that policy monotonicity
holds: if a stricter policy allows, looser policies also allow.

```go
func TestComparisonPolicyConsistency(t *testing.T) {
    entries := golden.LoadCorpus(t, "testdata/golden/")
    restrictiveness := map[guard.Decision]int{guard.Allow: 0, guard.Ask: 1, guard.Deny: 2}

    for _, entry := range entries {
        t.Run(entry.Description, func(t *testing.T) {
            strictResult := pipeline.Run(ctx, entry.Command, strictCfg)
            interResult := pipeline.Run(ctx, entry.Command, interCfg)
            permResult := pipeline.Run(ctx, entry.Command, permCfg)

            sr := restrictiveness[strictResult.Decision]
            ir := restrictiveness[interResult.Decision]
            pr := restrictiveness[permResult.Decision]

            assert.GreaterOrEqual(t, sr, ir, "strict >= interactive for %q", entry.Command)
            assert.GreaterOrEqual(t, ir, pr, "interactive >= permissive for %q", entry.Command)
        })
    }
}
```

---

## B: Benchmarks

### B1: Per-Pack Pattern Matching Throughput

```go
func BenchmarkGitPackMatch(b *testing.B) {
    commands := map[string]parse.ExtractedCommand{
        "safe-push":   cmd("git", []string{"push", "origin", "main"}, nil),
        "force-push":  cmd("git", []string{"push"}, m("--force", "")),
        "status":      cmd("git", []string{"status"}, nil),
        "reset-hard":  cmd("git", []string{"reset"}, m("--hard", "")),
        "rebase":      cmd("git", []string{"rebase", "main"}, nil),
    }
    for name, c := range commands {
        b.Run(name, func(b *testing.B) {
            for i := 0; i < b.N; i++ {
                matchPack(gitPack, c)
            }
        })
    }
}

func BenchmarkFilesystemPackMatch(b *testing.B) {
    commands := map[string]parse.ExtractedCommand{
        "rm-safe":    cmd("rm", []string{"file.txt"}, nil),
        "rm-rf":      cmd("rm", []string{"/tmp"}, m("-rf", "")),
        "dd-write":   cmd("dd", []string{"of=/dev/sda"}, nil),
        "chmod-safe":  cmd("chmod", []string{"644", "file"}, nil),
    }
    for name, c := range commands {
        b.Run(name, func(b *testing.B) {
            for i := 0; i < b.N; i++ {
                matchPack(fsPack, c)
            }
        })
    }
}
```

**Targets** (initial — adjust after baseline measurement):
- Safe pattern match (short-circuit): < 100ns per command
- Destructive pattern match: < 200ns per command
- Full pack evaluation (all patterns): < 500ns per command

The first implementation should establish actual baselines before freezing
targets. These numbers are aspirational and may need adjustment based on
real map lookup and string comparison costs.

### B2: Golden File Corpus Throughput

```go
func BenchmarkGoldenCorpus(b *testing.B) {
    entries := golden.LoadCorpus(b, "testdata/golden/")
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

**Target**: Full corpus (60+ entries) < 1ms total.

---

## S: Stress Tests

### S1: Concurrent Pack Matching

Verify packs are safe for concurrent use (all matchers are stateless):

```go
func TestStressConcurrentMatching(t *testing.T) {
    var wg sync.WaitGroup
    commands := []parse.ExtractedCommand{
        cmd("git", []string{"push"}, m("--force", "")),
        cmd("rm", []string{"/tmp"}, m("-rf", "")),
        cmd("git", []string{"status"}, nil),
        cmd("chmod", []string{"755", "file"}, nil),
    }

    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(idx int) {
            defer wg.Done()
            c := commands[idx%len(commands)]
            for j := 0; j < 1000; j++ {
                for _, pack := range packs.DefaultRegistry.All() {
                    for _, dp := range pack.Destructive {
                        dp.Match.Match(c) // Must not panic or race
                    }
                }
            }
        }(i)
    }
    wg.Wait()
}
```

Run with `-race` flag to detect data races.

### S2: Registry Freeze Under Concurrent Access

Verify that the registry's freeze-on-first-read behavior is safe under
concurrent access:

```go
func TestStressRegistryConcurrentFreeze(t *testing.T) {
    // Create a fresh registry (not the default)
    reg := packs.NewRegistry()
    reg.Register(gitPack)
    reg.Register(fsPack)

    var wg sync.WaitGroup
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            // All() triggers freeze
            packs := reg.All()
            assert.Len(t, packs, 2)
        }()
    }
    wg.Wait()
}
```

---

## SEC: Security Tests

### SEC1: Pattern Evasion Attempts

Test that known evasion techniques don't bypass pattern detection:

```go
func TestSecurityPatternEvasion(t *testing.T) {
    tests := []struct {
        name    string
        command string
        wantDeny bool
        reason  string
    }{
        // Path prefixes (handled by normalizer in plan 01)
        {"/usr/bin/git push --force", "git push --force origin main", true,
            "path-prefixed git should be normalized"},

        // Extra whitespace (handled by parser)
        {"git  push   --force", "git  push   --force  origin  main", true,
            "extra whitespace should not affect matching"},

        // Flag after arguments
        {"flags after args", "git push origin --force main", true,
            "flag position should not matter"},

        // Long flag = value form
        {"--force=true", "git push --force=true origin main", true,
            "flag with =value form should match"},

        // Compound with safe command
        {"safe ; destructive", "git status && git push --force", true,
            "destructive in compound should be caught"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := pipeline.Run(ctx, tt.command, strictCfg)
            if tt.wantDeny {
                assert.Equal(t, guard.Deny, result.Decision, tt.reason)
            }
        })
    }
}
```

### SEC2: Safe Pattern Completeness

Verify that safe patterns don't accidentally match commands that should be
destructive. This is the inverse of reachability — for each destructive
pattern, confirm the corresponding safe pattern does NOT match:

```go
func TestSecuritySafePatternCompleteness(t *testing.T) {
    // For each destructive reachability command, verify NO safe pattern matches
    // This is the same as P2 but framed as a security invariant
    for _, pack := range packs.DefaultRegistry.All() {
        for _, dp := range pack.Destructive {
            cmd := getReachabilityCommand(pack.ID, dp.Name)
            for _, sp := range pack.Safe {
                if sp.Match.Match(cmd) {
                    t.Errorf("SECURITY: safe %s blocks destructive %s in pack %s",
                        sp.Name, dp.Name, pack.ID)
                }
            }
        }
    }
}
```

---

## MQ: Manual QA Plan

### MQ1: Real-World Command Evaluation

Test with actual commands from LLM coding sessions:

1. Collect 30 recent `Bash` tool invocations from Claude Code logs
2. Run each through the pipeline with InteractivePolicy
3. Verify:
   - No false positives on safe commands (git add, git commit, ls, cat, etc.)
   - All destructive commands are caught (git push --force, rm -rf, etc.)
   - Decision severity matches human judgment
   - Remediation messages are helpful and accurate

### MQ2: Pack Documentation Review

Review each pack's patterns manually:
1. For each destructive pattern, verify the Reason accurately describes the risk
2. For each Remediation, verify the suggestion is actionable and correct
3. For each severity assignment, verify it matches the severity guidelines
4. Verify no important destructive variants are missing

### MQ3: Edge Case Walkthrough

Manually test edge cases that are hard to automate:
1. Very long git commands (many arguments, long branch names)
2. Unicode in file paths (`rm -rf /tmp/tëst`)
3. Commands with environment variable references (`rm -rf $DIR`)
4. Commands from different shells (zsh-specific syntax, fish)

---

## CI Tier Mapping

| Tier | Tests | Trigger |
|------|-------|---------|
| T1 (Fast, every commit) | P1-P7, E1-E3, F1-F3, SEC1-SEC2 | Every commit |
| T2 (Standard, every PR) | T1 + B1-B2, S1-S2 | PR open/update |
| T3 (Extended, nightly) | T1 + T2 + O1-O2 | Nightly schedule |
| T4 (Manual, pre-release) | MQ1-MQ3 | Before each release |

**T1 time budget**: < 10 seconds
**T2 time budget**: < 30 seconds
**T3 time budget**: < 5 minutes (includes upstream comparison)

---

## Exit Criteria

### Must Pass

1. **All property tests pass** — P1-P7
2. **All deterministic examples pass** — E1-E3
3. **All fault injection tests pass** — F1-F3
4. **All security tests pass** — SEC1-SEC2
5. **Golden file corpus passes** — All 84 entries
6. **Pattern reachability 100%** — Every destructive pattern reachable
7. **Pack completeness check passes** — All packs have required fields
8. **No data races** — S1 passes with -race flag
9. **Zero panics in any test** — Including fault injection

### Should Pass

10. **Benchmarks recorded** — B1-B2 have baseline values
11. **Stress tests pass** — S1-S2 complete without issues
12. **Comparison oracle baseline** — O1 has initial divergence report

### Tracked Metrics

- Pattern count by pack (safe + destructive)
- Test count by category (unit, reachability, golden, property)
- Golden file entry count (target: 84 for core packs)
- Pattern condition coverage (every branch in every matcher exercised)
- Benchmark latency per pack per command type
- Upstream comparison divergence count and categorization

---

## Review Disposition

| # | Reviewer | Severity | Summary | Disposition | Notes |
|---|----------|----------|---------|-------------|-------|
| 1 | dcg-alt-reviewer | P2 | SEC1 uses raw strings not ExtractedCommands (CP-P2.5) | Not Incorporated | SEC1 as integration test is by design |
| 2 | dcg-alt-reviewer | P2 | Golden file decisions depend on policy (CP-P2.6) | Incorporated | E1/E2 specify InteractivePolicy |
| 3 | dcg-alt-reviewer | P3 | E2 "higher priority" misleading (CP-P3.2) | Incorporated | Comment reworded to explain evaluation order |
| 4 | dcg-alt-reviewer | P3 | Oracle lacks divergence schema (CP-P3.4) | Not Incorporated | Deferred to implementation phase |
| 5 | dcg-reviewer | P3 | Benchmark targets aggressive (P3-3) | Incorporated | B1 targets marked as initial |
| 6 | dcg-reviewer | P3 | P6 redundant with P2 (P3-4) | Incorporated | P6 merged into P2 with note |
| 7 | dcg-reviewer | P3 | mv keyword false triggers (P3-5) | Not Incorporated | Performance negligible |
