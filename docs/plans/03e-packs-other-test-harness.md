# 03e: Other Packs — Test Harness

**Plan**: [03e-packs-other.md](./03e-packs-other.md)
**Architecture**: [00-architecture.md](./00-architecture.md)
**Core Pack Test Harness**: [03a-packs-core-test-harness.md](./03a-packs-core-test-harness.md)

---

## Overview

This document specifies the test harness for the frameworks, rsync, vault,
and github packs (plan 03e). It covers property-based tests, deterministic
examples, fault injection, comparison oracles, benchmarks, stress tests,
security tests, manual QA, CI tier mapping, and exit criteria.

The test harness complements the unit tests described in the plan doc §9.
Unit tests verify individual pattern behavior. This harness verifies
system-level properties, cross-pattern interactions, and robustness.

These packs introduce testing challenges specific to their domains:
- **Framework multi-tool coverage**: 5 distinct CLI tools (rails, rake,
  manage.py, artisan, mix) in one pack. Tests must cover all tools and
  verify no cross-tool interference.
- **Dual invocation forms**: manage.py (direct vs `python manage.py`) and
  artisan (direct vs `php artisan`) must both match. Tests must verify
  both forms for every relevant pattern.
- **Environment sensitivity split**: frameworks and vault are env-sensitive,
  rsync and github are NOT. This split must be verified.
- **Colon/dot-delimited subcommands**: `rails db:reset`, `mix ecto.reset`
  use punctuation within argument tokens. Tests must verify exact matching
  (no partial matches on `db:` or `ecto.`).
- **Vault S2 Not clause correctness**: S2 has Not clauses for `auth disable`,
  `token revoke`, `policy delete`, and `audit disable` — test harness must
  verify these exclusions work correctly.

---

## P: Property-Based Tests

### P1: Every Destructive Pattern Has a Matching Command

**Invariant**: For each destructive pattern in each 03e pack, there exists
at least one `ExtractedCommand` that the pattern matches.

Same property as 03a P1, extended to 4 packs. Uses the reachability
command map from plan doc §7.

```go
func TestPropertyEveryOtherPackDestructivePatternReachable(t *testing.T) {
    allPacks := []packs.Pack{frameworksPack, rsyncPack, vaultPack, githubPack}
    for _, pack := range allPacks {
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
- frameworks S1: includes `rails db:migrate` as safe but must not match
  destructive `db:drop`, `db:reset`, etc. (no Not clause needed — different
  ArgAt values).
- frameworks S4b: includes `manage.py migrate` as safe — must not shadow
  D7 `manage.py migrate --run-syncdb` (S4b's `Not(--run-syncdb)` prevents).
- rsync S1: includes rsync-without-delete as safe — must exclude all
  `--delete*` flag variants and `--remove-source-files` via Not clauses.
- vault S2: includes `vault token`, `vault auth`, `vault policy`,
  `vault audit` as safe — S2's Not clauses exclude `auth disable`,
  `token revoke`, `policy delete`, `audit disable` (D7-D10).
- vault S3: includes `vault secrets enable` — must not shadow D1
  `vault secrets disable` (different ArgAt(1) values, no conflict).

```go
func TestPropertyOtherPackSafePatternsNeverBlockDestructive(t *testing.T) {
    allPacks := []packs.Pack{frameworksPack, rsyncPack, vaultPack, githubPack}
    for _, pack := range allPacks {
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

### P3: Environment Sensitivity Split

**Invariant**: Every destructive pattern in frameworks and vault packs has
`EnvSensitive: true`. Every destructive pattern in rsync and github packs
has `EnvSensitive: false`.

```go
func TestPropertyEnvSensitivitySplit(t *testing.T) {
    // Must be env-sensitive
    for _, pack := range []packs.Pack{frameworksPack, vaultPack} {
        for _, dp := range pack.Destructive {
            t.Run("env-sensitive/"+pack.ID+"/"+dp.Name, func(t *testing.T) {
                assert.True(t, dp.EnvSensitive,
                    "%s/%s should be env-sensitive", pack.ID, dp.Name)
            })
        }
    }

    // Must NOT be env-sensitive
    for _, pack := range []packs.Pack{rsyncPack, githubPack} {
        for _, dp := range pack.Destructive {
            t.Run("not-env-sensitive/"+pack.ID+"/"+dp.Name, func(t *testing.T) {
                assert.False(t, dp.EnvSensitive,
                    "%s/%s should NOT be env-sensitive", pack.ID, dp.Name)
            })
        }
    }
}
```

### P4: Confidence Levels Are Valid

**Invariant**: Every destructive pattern has a confidence level of
`ConfidenceHigh`, `ConfidenceMedium`, or `ConfidenceLow` (never zero value).

```go
func TestPropertyConfidenceLevelsValid(t *testing.T) {
    allPacks := []packs.Pack{frameworksPack, rsyncPack, vaultPack, githubPack}
    for _, pack := range allPacks {
        for _, dp := range pack.Destructive {
            t.Run(pack.ID+"/"+dp.Name, func(t *testing.T) {
                assert.True(t,
                    dp.Confidence == guard.ConfidenceHigh ||
                    dp.Confidence == guard.ConfidenceMedium ||
                    dp.Confidence == guard.ConfidenceLow,
                    "%s has invalid confidence %v", dp.Name, dp.Confidence)
            })
        }
    }
}
```

### P5: Dual Invocation Parity

**Invariant**: For manage.py and artisan commands, both direct and
interpreter-prefixed invocations must match the same destructive pattern.

```go
func TestPropertyDualInvocationParity(t *testing.T) {
    // manage.py: direct vs python
    dualManagepy := []struct {
        direct parse.ExtractedCommand
        python parse.ExtractedCommand
    }{
        {
            cmd("manage.py", []string{"flush"}, nil),
            cmd("python", []string{"manage.py", "flush"}, nil),
        },
        {
            cmd("manage.py", []string{"migrate"}, m("--run-syncdb", "")),
            cmd("python", []string{"manage.py", "migrate"}, m("--run-syncdb", "")),
        },
    }

    for _, pair := range dualManagepy {
        for _, dp := range frameworksPack.Destructive {
            directMatch := dp.Match.Match(pair.direct)
            pythonMatch := dp.Match.Match(pair.python)
            assert.Equal(t, directMatch, pythonMatch,
                "pattern %s matches direct=%v but python=%v for manage.py command",
                dp.Name, directMatch, pythonMatch)
        }
    }

    // artisan: direct vs php
    dualArtisan := []struct {
        direct parse.ExtractedCommand
        php    parse.ExtractedCommand
    }{
        {
            cmd("artisan", []string{"migrate:fresh"}, nil),
            cmd("php", []string{"artisan", "migrate:fresh"}, nil),
        },
        {
            cmd("artisan", []string{"migrate:reset"}, nil),
            cmd("php", []string{"artisan", "migrate:reset"}, nil),
        },
    }

    for _, pair := range dualArtisan {
        for _, dp := range frameworksPack.Destructive {
            directMatch := dp.Match.Match(pair.direct)
            phpMatch := dp.Match.Match(pair.php)
            assert.Equal(t, directMatch, phpMatch,
                "pattern %s matches direct=%v but php=%v for artisan command",
                dp.Name, directMatch, phpMatch)
        }
    }
}
```

### P6: Colon-Delimited Exact Matching

**Invariant**: Framework subcommand matching is exact — `db:dro` must not
match `db:drop`, `db:` must not match `db:reset`, etc.

```go
func TestPropertyColonDelimitedExactMatching(t *testing.T) {
    // Partial subcommands that must NOT match any destructive pattern
    partials := []parse.ExtractedCommand{
        cmd("rails", []string{"db:"}, nil),
        cmd("rails", []string{"db:dro"}, nil),
        cmd("rails", []string{"db:rese"}, nil),
        cmd("rails", []string{"db:schema"}, nil),
        cmd("rails", []string{"db:drop:al"}, nil),
        cmd("rake", []string{"db:"}, nil),
        cmd("rake", []string{"db:drop:"}, nil),
        cmd("mix", []string{"ecto."}, nil),
        cmd("mix", []string{"ecto.rese"}, nil),
        cmd("artisan", []string{"migrate:"}, nil),
        cmd("artisan", []string{"migrate:fres"}, nil),
    }

    for _, partial := range partials {
        for _, dp := range frameworksPack.Destructive {
            assert.False(t, dp.Match.Match(partial),
                "pattern %s matched partial subcommand %v", dp.Name, partial.Args)
        }
    }
}
```

### P7: Keyword Coverage

**Invariant**: For every destructive pattern in each pack, the pack's
keywords appear in at least one synthetic command string that matches
the pattern. This ensures the pre-filter would trigger for real commands.

```go
func TestPropertyKeywordCoverage(t *testing.T) {
    allPacks := []packs.Pack{frameworksPack, rsyncPack, vaultPack, githubPack}
    for _, pack := range allPacks {
        for _, dp := range pack.Destructive {
            t.Run(pack.ID+"/"+dp.Name, func(t *testing.T) {
                reachCmd := getReachabilityCommand(pack.ID, dp.Name)
                // Reconstruct command string
                cmdStr := reachCmd.Name + " " + strings.Join(reachCmd.Args, " ")
                keywordFound := false
                for _, kw := range pack.Keywords {
                    if strings.Contains(cmdStr, kw) {
                        keywordFound = true
                        break
                    }
                }
                assert.True(t, keywordFound,
                    "pattern %s reachability command '%s' contains no pack keywords %v",
                    dp.Name, cmdStr, pack.Keywords)
            })
        }
    }
}
```

### P8: Frameworks Tool Isolation

**Invariant**: Rails patterns match only `rails` commands, rake patterns
match only `rake` commands, etc. No cross-tool matching.

```go
func TestPropertyFrameworkToolIsolation(t *testing.T) {
    // Map each destructive pattern to its expected tool
    toolPatterns := map[string][]string{
        "rails":     {"rails-db-drop", "rails-db-reset", "rails-db-schema-load"},
        "rake":      {"rake-db-drop-all", "rake-db-destructive"},
        "manage.py": {"managepy-flush", "managepy-migrate-syncdb"},
        "artisan":   {"artisan-migrate-fresh", "artisan-migrate-reset"},
        "mix":       {"mix-ecto-reset", "mix-ecto-drop"},
    }

    // Commands from wrong tools should never match
    wrongToolCmds := map[string][]parse.ExtractedCommand{
        "rails": {
            cmd("rake", []string{"db:drop"}, nil),
            cmd("manage.py", []string{"flush"}, nil),
            cmd("artisan", []string{"migrate:fresh"}, nil),
            cmd("mix", []string{"ecto.reset"}, nil),
        },
        "rake": {
            cmd("rails", []string{"db:drop"}, nil),
            cmd("manage.py", []string{"flush"}, nil),
        },
        "manage.py": {
            cmd("rails", []string{"db:reset"}, nil),
            cmd("rake", []string{"db:drop"}, nil),
        },
        "artisan": {
            cmd("rails", []string{"db:reset"}, nil),
            cmd("mix", []string{"ecto.reset"}, nil),
        },
        "mix": {
            cmd("rails", []string{"db:reset"}, nil),
            cmd("artisan", []string{"migrate:fresh"}, nil),
        },
    }

    for tool, patternNames := range toolPatterns {
        for _, pn := range patternNames {
            dp := findPattern(frameworksPack, pn)
            for otherTool, wrongCmds := range wrongToolCmds {
                if otherTool == tool {
                    continue
                }
                for _, wc := range wrongCmds {
                    assert.False(t, dp.Match.Match(wc),
                        "pattern %s (tool=%s) matched command from %s", pn, tool, otherTool)
                }
            }
        }
    }
}
```

---

## F: Fault Injection Tests

### F1: Malformed Extracted Commands

Test that patterns handle gracefully:
- Empty Name field
- Nil Args slice
- Nil Flags map
- Args with empty strings
- Extremely long argument values (>10K characters)

```go
func TestFaultMalformedCommands(t *testing.T) {
    allPacks := []packs.Pack{frameworksPack, rsyncPack, vaultPack, githubPack}
    malformed := []parse.ExtractedCommand{
        {Name: "", Args: nil, Flags: nil},
        {Name: "rails", Args: nil, Flags: nil},
        {Name: "rails", Args: []string{}, Flags: nil},
        {Name: "rails", Args: []string{""}, Flags: nil},
        {Name: "rails", Args: []string{strings.Repeat("a", 10000)}, Flags: nil},
        {Name: "vault", Args: []string{""}, Flags: map[string]string{}},
        {Name: "gh", Args: []string{""}, Flags: nil},
    }

    for _, pack := range allPacks {
        for _, dp := range pack.Destructive {
            for i, mc := range malformed {
                t.Run(fmt.Sprintf("%s/%s/malformed-%d", pack.ID, dp.Name, i), func(t *testing.T) {
                    // Must not panic
                    assert.NotPanics(t, func() {
                        dp.Match.Match(mc)
                    })
                })
            }
        }
    }
}
```

### F2: Unicode and Special Characters in Arguments

Test that patterns handle commands with unicode and special characters
in argument positions:

```go
func TestFaultUnicodeArguments(t *testing.T) {
    unicodeCmds := []parse.ExtractedCommand{
        cmd("rails", []string{"db:drôp"}, nil),
        cmd("vault", []string{"deleté", "secret/中文"}, nil),
        cmd("gh", []string{"repo", "delete", "org/répo-名前"}, nil),
        cmd("rsync", []string{"/src/", "/dest/"}, m("--deléte", "")),
    }

    allPacks := []packs.Pack{frameworksPack, rsyncPack, vaultPack, githubPack}
    for _, pack := range allPacks {
        for _, dp := range pack.Destructive {
            for _, uc := range unicodeCmds {
                assert.NotPanics(t, func() {
                    dp.Match.Match(uc)
                })
                // Unicode variants should NOT match (exact string matching)
                if uc.Name == pack.Keywords[0] || containsKeyword(pack.Keywords, uc.Name) {
                    // Only check if the command name is relevant to this pack
                    assert.False(t, dp.Match.Match(uc),
                        "pattern %s matched unicode variant", dp.Name)
                }
            }
        }
    }
}
```

---

## D: Deterministic Example Tests

### D1: Frameworks Escalation Examples

Test specific framework commands with and without environment escalation:

```go
func TestDeterministicFrameworksEscalation(t *testing.T) {
    tests := []struct {
        name       string
        cmd        parse.ExtractedCommand
        envVars    map[string]string
        wantMatch  string // expected destructive pattern name
        baseSev    guard.Severity
        escalSev   guard.Severity // expected severity with production env
    }{
        {
            "rails db:reset base",
            cmd("rails", []string{"db:reset"}, nil),
            nil,
            "rails-db-reset", guard.High, guard.High,
        },
        {
            "rails db:reset production",
            cmd("rails", []string{"db:reset"}, nil),
            map[string]string{"RAILS_ENV": "production"},
            "rails-db-reset", guard.High, guard.Critical,
        },
        {
            "artisan migrate:fresh production",
            cmd("php", []string{"artisan", "migrate:fresh"}, nil),
            map[string]string{"APP_ENV": "production"},
            "artisan-migrate-fresh", guard.High, guard.Critical,
        },
        {
            "mix ecto.reset prod",
            cmd("mix", []string{"ecto.reset"}, nil),
            map[string]string{"MIX_ENV": "prod"},
            "mix-ecto-reset", guard.High, guard.Critical,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            dp := findPattern(frameworksPack, tt.wantMatch)
            assert.True(t, dp.Match.Match(tt.cmd))
            assert.Equal(t, tt.baseSev, dp.Severity)
            assert.True(t, dp.EnvSensitive)
            // Escalation logic verified via eval pipeline test
        })
    }
}
```

### D2: Vault Severity Ordering

Verify that vault patterns have correct severity ordering:

```go
func TestDeterministicVaultSeverityOrdering(t *testing.T) {
    expected := []struct {
        name     string
        severity guard.Severity
    }{
        {"vault-secrets-disable", guard.Critical},
        {"vault-kv-destroy", guard.Critical},
        {"vault-lease-revoke", guard.High},
        {"vault-lease-revoke-prefix", guard.Critical},
        {"vault-delete", guard.High},
        {"vault-kv-delete", guard.High},
        {"vault-auth-disable", guard.Critical},
        {"vault-token-revoke", guard.High},
        {"vault-policy-delete", guard.High},
        {"vault-audit-disable", guard.Medium},
        {"vault-kv-metadata-delete", guard.Critical},
    }

    for _, tt := range expected {
        t.Run(tt.name, func(t *testing.T) {
            dp := findPattern(vaultPack, tt.name)
            assert.Equal(t, tt.severity, dp.Severity,
                "%s expected %v, got %v", tt.name, tt.severity, dp.Severity)
        })
    }
}
```

### D3: GitHub Severity Tiers

Verify github patterns follow expected severity tiers:

```go
func TestDeterministicGitHubSeverityTiers(t *testing.T) {
    assert.Equal(t, guard.Critical, findPattern(githubPack, "gh-repo-delete").Severity)
    assert.Equal(t, guard.High, findPattern(githubPack, "gh-release-delete").Severity)
    assert.Equal(t, guard.Low, findPattern(githubPack, "gh-issue-pr-close").Severity)
}
```

### D4: rsync Flag Combinations

Exhaustive test of rsync `--delete*` flag combinations:

```go
func TestDeterministicRsyncFlagCombinations(t *testing.T) {
    deleteFlags := []string{
        "--delete", "--delete-before", "--delete-after",
        "--delete-during", "--delete-excluded", "--delete-delay",
        "--remove-source-files",
    }

    for mask := 0; mask < (1 << len(deleteFlags)); mask++ {
        flags := make(map[string]string)
        var active []string
        for i, f := range deleteFlags {
            if mask&(1<<i) != 0 {
                flags[f] = ""
                active = append(active, f)
            }
        }

        testCmd := cmd("rsync", []string{"/src/", "/dest/"}, flags)
        t.Run(fmt.Sprintf("mask-%d", mask), func(t *testing.T) {
            if mask == 0 {
                // No delete flags — should match NO destructive pattern
                for _, dp := range rsyncPack.Destructive {
                    assert.False(t, dp.Match.Match(testCmd),
                        "no-delete command matched %s", dp.Name)
                }
                // Should match safe pattern
                assert.True(t, rsyncPack.Safe[0].Match.Match(testCmd),
                    "no-delete command should match safe pattern")
            } else {
                // At least one delete flag — should match SOME destructive pattern
                matched := false
                for _, dp := range rsyncPack.Destructive {
                    if dp.Match.Match(testCmd) {
                        matched = true
                        break
                    }
                }
                assert.True(t, matched,
                    "command with flags %v matched no destructive pattern", active)
                // Should NOT match safe pattern
                assert.False(t, rsyncPack.Safe[0].Match.Match(testCmd),
                    "delete command should not match safe pattern")
            }
        })
    }
}
```

---

## O: Comparison Oracle Tests

### O1: Golden File Corpus

Run the full golden file corpus (95 entries across 4 packs) through the
evaluation pipeline and verify decisions match.

```go
func TestOracleGoldenFileCorpus(t *testing.T) {
    entries := loadGoldenEntries(t, "03e-packs-other")
    assert.Equal(t, 95, len(entries), "expected 95 golden entries")

    for _, entry := range entries {
        t.Run(entry.Command, func(t *testing.T) {
            result := guard.Evaluate(entry.Command, guard.WithPolicy(guard.InteractivePolicy()))
            assertGoldenMatch(t, entry, result)
        })
    }
}
```

### O2: Cross-Plan Severity Comparison

For tools that appear in multiple plan docs or the upstream Rust version,
compare severity assignments:

```go
func TestOracleCrossPackSeverityConsistency(t *testing.T) {
    // Frameworks pack: all database-destroying operations should be
    // at same severity tier as database pack equivalents
    //
    // e.g., rails db:drop (High) vs psql -c "DROP DATABASE" (High in 03b)
    // These should be equal — both destroy a database.
    //
    // Document any expected differences:
    // - rake db:drop:all is Critical (cross-environment) vs individual db:drop at High
    // - manage.py migrate --run-syncdb is Medium (recoverable) vs flush at High (data loss)

    frameworkSeverities := map[string]guard.Severity{
        "rails-db-drop":          guard.High,
        "rails-db-reset":         guard.High,
        "rails-db-schema-load":   guard.High,
        "rake-db-drop-all":       guard.Critical,
        "rake-db-destructive":    guard.High,
        "managepy-flush":         guard.High,
        "managepy-migrate-syncdb": guard.Medium,
        "artisan-migrate-fresh":  guard.High,
        "artisan-migrate-reset":  guard.High,
        "mix-ecto-reset":         guard.High,
        "mix-ecto-drop":          guard.High,
    }

    for name, expected := range frameworkSeverities {
        dp := findPattern(frameworksPack, name)
        assert.Equal(t, expected, dp.Severity,
            "severity mismatch for %s", name)
    }
}
```

---

## B: Benchmark Tests

### B1: Pattern Matching Latency

Benchmark matching latency for each pack, measured per-command evaluation.

```go
func BenchmarkFrameworksMatching(b *testing.B) {
    testCmds := []parse.ExtractedCommand{
        cmd("rails", []string{"db:reset"}, nil),
        cmd("rails", []string{"server"}, nil),
        cmd("python", []string{"manage.py", "flush"}, nil),
        cmd("php", []string{"artisan", "migrate:fresh"}, nil),
        cmd("mix", []string{"ecto.reset"}, nil),
    }

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        c := testCmds[i%len(testCmds)]
        for _, dp := range frameworksPack.Destructive {
            dp.Match.Match(c)
        }
    }
}

func BenchmarkRsyncMatching(b *testing.B) {
    testCmds := []parse.ExtractedCommand{
        cmd("rsync", []string{"/src/", "/dest/"}, m("--delete", "")),
        cmd("rsync", []string{"/src/", "/dest/"}, nil),
    }

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        c := testCmds[i%len(testCmds)]
        for _, dp := range rsyncPack.Destructive {
            dp.Match.Match(c)
        }
    }
}

func BenchmarkVaultMatching(b *testing.B) {
    testCmds := []parse.ExtractedCommand{
        cmd("vault", []string{"secrets", "disable", "secret/"}, nil),
        cmd("vault", []string{"kv", "get", "secret/myapp"}, nil),
        cmd("vault", []string{"lease", "revoke", "abc123"}, nil),
    }

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        c := testCmds[i%len(testCmds)]
        for _, dp := range vaultPack.Destructive {
            dp.Match.Match(c)
        }
    }
}

func BenchmarkGitHubMatching(b *testing.B) {
    testCmds := []parse.ExtractedCommand{
        cmd("gh", []string{"repo", "delete", "my-repo"}, nil),
        cmd("gh", []string{"pr", "list"}, nil),
        cmd("gh", []string{"issue", "close", "42"}, nil),
    }

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        c := testCmds[i%len(testCmds)]
        for _, dp := range githubPack.Destructive {
            dp.Match.Match(c)
        }
    }
}
```

**Targets**: Each pack should evaluate all patterns in <50μs per command
(same target as 03a). These packs have simple ArgAt matching with no
regex or content analysis, so they should easily beat this target.

### B2: Full Pipeline Benchmark

Benchmark the full `guard.Evaluate()` pipeline with the golden file
corpus:

```go
func BenchmarkFullPipelineOtherPacks(b *testing.B) {
    goldenCmds := loadGoldenCommandStrings("03e-packs-other")
    assert.Equal(b, 95, len(goldenCmds))

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        cmd := goldenCmds[i%len(goldenCmds)]
        guard.Evaluate(cmd, guard.WithPolicy(guard.InteractivePolicy()))
    }
}
```

---

## S: Stress Tests

### S1: High-Volume Command Stream

Process 100K commands through the evaluation pipeline (mix of matching
and non-matching) and verify:
- No memory leaks (RSS stays within 2x baseline)
- No panics
- All results are deterministic (same input → same output)

```go
func TestStressHighVolumeOtherPacks(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping stress test in short mode")
    }

    commands := generateOtherPackCommandStream(100_000)
    results := make([]guard.Result, len(commands))

    for i, cmd := range commands {
        results[i] = guard.Evaluate(cmd, guard.WithPolicy(guard.InteractivePolicy()))
    }

    // Determinism: re-run a sample and verify identical results
    for i := 0; i < 1000; i++ {
        idx := i * 100
        r2 := guard.Evaluate(commands[idx], guard.WithPolicy(guard.InteractivePolicy()))
        assert.Equal(t, results[idx].Decision, r2.Decision,
            "non-deterministic result for command %d", idx)
    }
}
```

### S2: Concurrent Evaluation

Verify thread safety by evaluating commands from all 4 packs concurrently:

```go
func TestStressConcurrentOtherPacks(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping stress test in short mode")
    }

    commands := generateOtherPackCommandStream(10_000)
    var wg sync.WaitGroup

    for i := 0; i < 8; i++ {
        wg.Add(1)
        go func(workerID int) {
            defer wg.Done()
            for j := workerID; j < len(commands); j += 8 {
                result := guard.Evaluate(commands[j], guard.WithPolicy(guard.InteractivePolicy()))
                _ = result // Just verify no panics
            }
        }(i)
    }

    wg.Wait()
}
```

---

## SEC: Security Tests

### SEC1: No Secret Leakage in Match Results

Verify that Vault secret paths in command arguments don't appear in
Reason or Remediation strings (which might be logged):

```go
func TestSecurityNoSecretLeakage(t *testing.T) {
    sensitiveCommands := []string{
        "vault delete secret/production/api-keys/stripe",
        "vault kv destroy secret/prod/database/credentials",
        "vault lease revoke aws/creds/production-admin/abc123",
    }

    for _, cmdStr := range sensitiveCommands {
        result := guard.Evaluate(cmdStr, guard.WithPolicy(guard.InteractivePolicy()))
        for _, match := range result.Matches {
            // Reason and Remediation should be generic templates,
            // NOT contain the specific secret path
            assert.NotContains(t, match.Reason, "stripe")
            assert.NotContains(t, match.Reason, "credentials")
            assert.NotContains(t, match.Reason, "production-admin")
            assert.NotContains(t, match.Remediation, "stripe")
            assert.NotContains(t, match.Remediation, "credentials")
        }
    }
}
```

### SEC2: Vault S2 Not Clause Correctness

Verify that S2's Not clauses correctly exclude all formerly-shadowed
destructive operations, allowing D7-D10 to match:

```go
func TestSecurityVaultS2NotClauses(t *testing.T) {
    s2 := vaultPack.Safe[1].Match // S2 vault-inspect-safe

    // Destructive operations that S2 must NOT match (Not clauses)
    destructiveOps := []struct {
        name string
        cmd  parse.ExtractedCommand
    }{
        {"vault auth disable", cmd("vault", []string{"auth", "disable", "userpass/"}, nil)},
        {"vault token revoke", cmd("vault", []string{"token", "revoke", "s.abc123"}, nil)},
        {"vault policy delete", cmd("vault", []string{"policy", "delete", "my-policy"}, nil)},
        {"vault audit disable", cmd("vault", []string{"audit", "disable", "file/"}, nil)},
    }
    for _, op := range destructiveOps {
        t.Run("not-safe/"+op.name, func(t *testing.T) {
            assert.False(t, s2.Match(op.cmd),
                "%s must NOT match S2 safe pattern", op.name)
        })
    }

    // Safe operations that S2 must still match
    safeOps := []struct {
        name string
        cmd  parse.ExtractedCommand
    }{
        {"vault auth list", cmd("vault", []string{"auth", "list"}, nil)},
        {"vault auth enable", cmd("vault", []string{"auth", "enable", "userpass"}, nil)},
        {"vault token lookup", cmd("vault", []string{"token", "lookup"}, nil)},
        {"vault token create", cmd("vault", []string{"token", "create"}, nil)},
        {"vault policy read", cmd("vault", []string{"policy", "read", "my-policy"}, nil)},
        {"vault policy list", cmd("vault", []string{"policy", "list"}, nil)},
        {"vault audit list", cmd("vault", []string{"audit", "list"}, nil)},
    }
    for _, op := range safeOps {
        t.Run("safe/"+op.name, func(t *testing.T) {
            assert.True(t, s2.Match(op.cmd),
                "%s should match S2 safe pattern", op.name)
        })
    }
}
```

### SEC3: Framework Environment Escalation

Verify that the canonical environment-sensitive scenario works:

```go
func TestSecurityFrameworkEnvEscalation(t *testing.T) {
    // This is the canonical use case from shaping doc A8
    result := guard.Evaluate(
        "RAILS_ENV=production rails db:reset",
        guard.WithPolicy(guard.InteractivePolicy()),
    )

    assert.Equal(t, guard.Deny, result.Decision)
    assert.NotNil(t, result.Assessment)
    assert.Equal(t, guard.Critical, result.Assessment.Severity)
    assert.Len(t, result.Matches, 1)
    assert.Equal(t, "rails-db-reset", result.Matches[0].Rule)
    assert.True(t, result.Matches[0].EnvEscalated)
}
```

---

## MQ: Manual QA Plan

### MQ1: Framework Scenario Walkthrough

Run each framework tool's most dangerous command through the full CLI
and verify:

```bash
# Rails
dcg-go test "rails db:drop"
dcg-go test "RAILS_ENV=production rails db:reset"

# Rake
dcg-go test "rake db:drop:all"

# Django
dcg-go test "python manage.py flush"
dcg-go test "manage.py migrate --run-syncdb"

# Laravel
dcg-go test "php artisan migrate:fresh"
dcg-go test "APP_ENV=production php artisan migrate:fresh"

# Elixir
dcg-go test "mix ecto.reset"
dcg-go test "mix ecto.drop"
dcg-go test "MIX_ENV=prod mix ecto.reset"
dcg-go test "MIX_ENV=prod mix ecto.drop"
```

Verify: Correct pack, rule, severity, confidence, env_escalated flag.

### MQ2: Vault Scenario Walkthrough

```bash
dcg-go test "vault secrets disable kv-v2/"
dcg-go test "vault kv destroy -versions=1,2,3 secret/myapp"
dcg-go test "vault lease revoke -prefix aws/creds/my-role/"
dcg-go test "vault delete secret/myapp"
dcg-go test "vault auth disable userpass/"
dcg-go test "vault token revoke s.abc123"
dcg-go test "vault policy delete my-policy"
dcg-go test "vault audit disable file/"
dcg-go test "vault kv metadata delete secret/myapp"

# Safe commands
dcg-go test "vault read secret/myapp"
dcg-go test "vault kv put secret/myapp key=value"
dcg-go test "vault status"
dcg-go test "vault auth list"
dcg-go test "vault token lookup"
dcg-go test "vault policy read my-policy"
```

### MQ3: GitHub Scenario Walkthrough

```bash
dcg-go test "gh repo delete my-org/my-repo --yes"
dcg-go test "gh release delete v1.0.0 --cleanup-tag"
dcg-go test "gh issue close 42"

# Safe commands
dcg-go test "gh repo clone my-repo"
dcg-go test "gh pr create --title 'Fix bug'"
dcg-go test "gh issue list"
```

### MQ4: rsync Scenario Walkthrough

```bash
dcg-go test "rsync --delete -avz /src/ user@host:/dest/"
dcg-go test "rsync --delete-excluded -avz /src/ /dest/"
dcg-go test "rsync --delete-before -avz /src/ /dest/"
dcg-go test "rsync --remove-source-files -avz /src/ /dest/"

# Safe commands
dcg-go test "rsync -avz /src/ /dest/"
dcg-go test "rsync file.txt /backup/"
```

---

## CI Tier Mapping

| Tier | Tests | Runtime Target | Trigger |
|------|-------|---------------|---------|
| **Tier 1** (every commit) | P1-P8, F1-F2, D1-D4, SEC1-SEC3 | <10s | Every push |
| **Tier 2** (PR gate) | O1-O2 (golden corpus, comparison) | <30s | PR create/update |
| **Tier 3** (nightly) | B1-B2 (benchmarks), S1-S2 (stress) | <5m | Nightly schedule |
| **Tier 4** (release) | MQ1-MQ4 (manual QA) | Manual | Pre-release |

---

## Exit Criteria

Implementation of the 03e packs is complete when:

1. **All 29 destructive patterns** pass their reachability tests (P1)
2. **All 12 safe patterns** do not shadow any destructive pattern (P2)
3. **Environment sensitivity split** verified: frameworks + vault = true,
   rsync + github = false (P3)
4. **Dual invocation parity** verified for manage.py and artisan (P5)
5. **95 golden file entries** pass through the evaluation pipeline (O1)
6. **Per-pattern unit tests** pass (~80 match/near-miss tests from §9)
7. **Benchmarks** show <50μs per-command matching for all 4 packs (B1)
8. **No panics** on malformed input (F1, F2)
9. **rsync flag exhaustion** — all 128 flag combinations tested (D4)
10. **Vault S2 Not clauses** verified for all 4 excluded operations (SEC2)
11. **Framework env escalation** canonical scenario passes (SEC3)
12. **Zero secret leakage** in match results (SEC1)

---

## Metrics Dashboard

- Pattern count: 12 safe + 29 destructive = 41 patterns across 4 packs
- Test count by category (unit, reachability, golden, property, security)
- Golden file entry count — target: 95 entries across 4 packs (44+12+23+16)
- ArgAt matching latency per pattern (from B1)
- Environment sensitivity coverage: frameworks + vault = env-sensitive,
  rsync + github = not env-sensitive
- Dual invocation parity: manage.py (2 forms), artisan (2 forms)
- Framework tool isolation: 5 tools, zero cross-tool matches
- Vault S2 Not clause coverage: 4 exclusions (auth/disable, token/revoke,
  policy/delete, audit/disable)

---

## Review Disposition

| # | Reviewer | Severity | Summary | Disposition | Notes |
|---|----------|----------|---------|-------------|-------|
| 1 | systems-engineer | P1 | Missing golden entry for rsync --delete-delay | Incorporated | Golden entry added in plan doc §6.2 |
| 2 | systems-engineer | P2 | mix ecto.drop needs negative golden test | Incorporated | Resolved by D11 pattern; golden entry shows match |
| 3 | systems-engineer | P1 | Missing env-escalated golden entries for D3/D5/D7/D9 | Incorporated | 4 env-escalated golden entries added in plan doc §6.1 |
| 4 | security-correctness | P2 | SEC2 tests need S2 bypass verification | Incorporated | SEC2 rewritten to verify S2 Not clauses |
| 5 | security-correctness | P0 | Vault S2 shadows vault auth disable | Incorporated | P2 description updated; SEC2 rewritten |
| 6 | security-correctness | P1 | rsync --remove-source-files undetected | Incorporated | D4 flag combos updated to 7 flags / 128 combos |
| 7 | systems-engineer | P0 | Vault S2 shadows vault auth disable | Incorporated | All test harness counts and references updated |
| 8 | N/A | N/A | Pattern/golden/test counts | Incorporated | Updated throughout: 29 destructive, 95 golden, ~80 unit tests, 128 rsync combos |
