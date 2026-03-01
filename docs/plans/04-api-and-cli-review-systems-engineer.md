# 04-api-and-cli — Systems Engineer Review

**Reviewer**: systems-engineer (dcg-reviewer)
**Date**: 2026-03-01
**Plan doc**: `docs/plans/04-api-and-cli.md`
**Test harness**: `docs/plans/04-api-and-cli-test-harness.md`
**Review scope**: Public API design, CLI argument parsing, Claude Code hook
protocol fidelity, config file format and validation, integration test
coverage, cross-plan consistency with 01/02 pipeline specs

**Cross-references used**: Plan 02 (pipeline interface, blocklist match,
aggregateAssessments, matchCommand), Claude Code hook protocol documentation

---

## Summary

The plan defines a clean, minimal public API (`guard.Evaluate()` with
functional options) and a thin CLI wrapper with three modes (hook, test,
packs). The overall design is solid — stateless API, `sync.Once` lazy init,
proper import layering to avoid cycles. The guard package correctly delegates
all logic to `internal/eval`.

Key concern: `buildReason` uses `result.Matches[0]` to construct the hook
output reason string, but `result.Matches` is in insertion order (not sorted
by severity). For compound commands with multiple matches, this shows the
reason from the first-evaluated match, which may not be the highest severity
match that drove the decision. Claude Code shows this reason to users and
uses it to guide corrective action.

**Finding count**: 1 P0, 4 P1, 5 P2, 2 P3 (12 total)

---

## Findings

### P0-1: `buildReason` uses `result.Matches[0]` — may not be highest severity

**Location**: §5.2 hook.go, line 761

**Issue**: `buildReason` extracts the reason string from `result.Matches[0]`,
assuming it is the primary (highest severity) match. However, per plan 02
§matchCommand, matches are appended in pack iteration order × command
extraction order. `aggregateAssessments` selects the highest severity match
for `result.Assessment`, but does NOT reorder `result.Matches`.

For compound commands like `git push --force && rm -rf /`:
- Command 1 (`git push --force`) is processed first → Match{Severity: High}
- Command 2 (`rm -rf /`) is processed second → Match{Severity: Critical}
- `result.Matches = [{High, git-push-force}, {Critical, rm-rf}]`
- `result.Assessment.Severity = Critical` (from aggregateAssessments)
- `buildReason` uses `Matches[0]` → shows "git push --force" reason
- **But the decision (Deny) was driven by "rm -rf /"**

Claude Code displays this reason to the user and may use it to guide
corrective action. Showing the wrong reason could cause the agent to modify
the command to avoid the stated reason (e.g., removing `--force`) while
leaving the actual destructive command (`rm -rf /`) unchanged.

**Impact**: Hook output reason string is potentially wrong for compound
commands with multiple matches at different severity levels.

**Recommendation**: Either (a) plan 02: sort `result.Matches` by severity
descending before returning (so `Matches[0]` is always the primary match —
good API ergonomics for all callers); or (b) plan 04: change `buildReason`
to scan for the highest severity match:
```go
func buildReason(result guard.Result) string {
    if len(result.Matches) == 0 {
        return ""
    }
    best := result.Matches[0]
    for _, m := range result.Matches[1:] {
        if m.Severity > best.Severity {
            best = m
        }
    }
    // use best.Reason, best.Remediation, best.EnvEscalated
}
```
Option (a) is preferred — it benefits all API consumers, not just the CLI.

---

### P1-1: Hook mode `io.ReadAll(os.Stdin)` has no size limit

**Location**: §5.2 hook.go, line 710

**Issue**: `io.ReadAll(os.Stdin)` reads all bytes from stdin with no upper
bound. In normal Claude Code usage, stdin contains a bounded JSON payload
(~200-500 bytes). But if the hook is invoked outside Claude Code (e.g.,
piped from a large file, or by a misconfigured tool), this could consume
unbounded memory.

**Recommendation**: Use `io.LimitReader(os.Stdin, maxHookInputSize)` with a
reasonable limit (e.g., 1MB). The Claude Code hook input JSON is typically
<1KB, so 1MB provides ample headroom:
```go
input, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
```

---

### P1-2: Nil Option causes panic in Evaluate()

**Location**: §4.4 guard.go, line 488-490

**Issue**: The option application loop does not nil-check options:
```go
for _, opt := range opts {
    opt(&cfg) // panics if opt is nil
}
```

If a caller passes `guard.Evaluate("ls", nil)`, the nil is converted to a
nil `Option` (nil function pointer), and calling it panics. Library
functions should not panic on caller mistakes.

**Recommendation**: Add nil check:
```go
for _, opt := range opts {
    if opt != nil {
        opt(&cfg)
    }
}
```

---

### P1-3: Missing integration test for `WithEnv()` with process env

**Location**: §7.3 integration tests

**Issue**: `TestIntegrationEnvEscalation` tests env escalation using inline
env vars (`RAILS_ENV=production rails db:reset`). But the primary use case
for `WithEnv()` — passing process environment variables — is untested. In
hook mode, `os.Environ()` is always passed via `WithEnv()`. An env var like
`RAILS_ENV=production` set in the user's shell (not inline) should also
trigger escalation.

**Recommendation**: Add integration test:
```go
func TestIntegrationWithEnvProcessEnv(t *testing.T) {
    skipIfPackMissing(t, "frameworks")
    result := guard.Evaluate("rails db:reset",
        guard.WithEnv([]string{"RAILS_ENV=production"}))
    assert.Equal(t, guard.Deny, result.Decision)
    assert.Equal(t, guard.Critical, result.Assessment.Severity)
    if len(result.Matches) > 0 {
        assert.True(t, result.Matches[0].EnvEscalated)
    }
}
```

---

### P1-4: Missing integration test for `WithPacks()` include list

**Location**: §7.3 integration tests

**Issue**: `WithDisabledPacks` is tested at line 1489-1495, but `WithPacks`
(explicit include list) is never tested in integration tests. `WithPacks`
has important semantics: passing an empty list disables all packs (§4.3).
This should be verified.

**Recommendation**: Add integration tests:
```go
func TestIntegrationWithPacksIncludeList(t *testing.T) {
    skipIfPackMissing(t, "core.git")
    // Only core.git enabled — filesystem patterns should not match
    result := guard.Evaluate("rm -rf /",
        guard.WithPacks("core.git"))
    // rm -rf is in core.filesystem, not core.git
    assert.Equal(t, guard.Allow, result.Decision)
}

func TestIntegrationWithPacksEmptyList(t *testing.T) {
    result := guard.Evaluate("git push --force",
        guard.WithPacks())
    assert.Equal(t, guard.Allow, result.Decision,
        "empty packs list should disable all evaluation")
}
```

---

### P2-1: Config file has no read size limit

**Location**: §5.5 config.go, line 1093

**Issue**: `os.ReadFile(path)` reads the entire config file into memory
with no size limit. SEC3 in the test harness tests a 10MB config. For a
user-controlled file in `~/.config/`, this is low risk, but defense-in-depth
suggests a size check.

**Recommendation**: Add a post-read size check:
```go
data, err := os.ReadFile(path)
if err != nil {
    return Config{}
}
if len(data) > 1<<20 { // 1MB
    fmt.Fprintf(os.Stderr, "warning: config at %s too large (%d bytes)\n", path, len(data))
    return Config{}
}
```

---

### P2-2: Missing integration test for multi-match commands

**Location**: §7.3 integration tests

**Issue**: No test verifies behavior when a compound command produces
multiple matches from different packs. This is important to verify that
`aggregateAssessments` correctly selects the highest severity match across
multiple commands.

**Recommendation**: Add:
```go
func TestIntegrationMultiMatchCompound(t *testing.T) {
    skipIfPackMissing(t, "core.git")
    skipIfPackMissing(t, "core.filesystem")
    result := guard.Evaluate("git push --force && rm -rf /")
    assert.Equal(t, guard.Deny, result.Decision)
    assert.GreaterOrEqual(t, len(result.Matches), 2)
    assert.Equal(t, guard.Critical, result.Assessment.Severity)
}
```

---

### P2-3: No stdin read timeout in hook mode

**Location**: §5.2 hook.go, line 710

**Issue**: `io.ReadAll(os.Stdin)` blocks indefinitely if stdin is not closed.
In normal Claude Code usage, stdin is closed after the JSON payload. But if
the hook is invoked with a pipe that hangs, the process blocks forever.

**Recommendation**: Consider wrapping stdin read with a context deadline, or
document that the hook process relies on Claude Code to close stdin. Since
hook mode is always invoked as a Claude Code subprocess, this is low risk
in practice.

---

### P2-4: Stringer methods return "Unknown" for out-of-range values

**Location**: §4.1 types.go, lines 156, 179, 201, 261

**Issue**: All enum `String()` methods return `"Unknown"` for values outside
the defined range. This is defensive programming, but the behavior is not
documented. If plan 02's `escalateSeverity` or a future change introduces
a new severity level, the String() methods would silently return "Unknown"
instead of failing.

**Recommendation**: Add a comment documenting this behavior, and consider
adding a test that exercises the default case to lock it in.

---

### P2-5: Test mode `flag.ExitOnError` bypasses error formatting

**Location**: §5.3 test.go, line 838

**Issue**: `flag.NewFlagSet("test", flag.ExitOnError)` causes flag parsing
errors to call `os.Exit(2)` directly, bypassing main.go's error formatting
(`fmt.Fprintf(os.Stderr, "error: %v\n", err)`). This means invalid flags
produce a different error format than other errors.

Same applies to packs mode (line 1018).

**Recommendation**: Use `flag.ContinueOnError` and handle the error
explicitly for consistent error formatting:
```go
fs := flag.NewFlagSet("test", flag.ContinueOnError)
if err := fs.Parse(args); err != nil {
    return err  // handled by main()
}
```

---

### P3-1: `WithPacks()` empty list means "no packs" — surprising semantic

**Location**: §4.3 option.go, line 416-421

**Issue**: §4.3 documents that `WithPacks()` with an empty list means
"no packs are evaluated (everything allows)." This is surprising — most
APIs treat an empty include list as "no constraint" (i.e., all items
included). The nil/empty distinction creates a foot-gun.

The distinction is:
- `nil` (not called) = all packs
- `[]string{}` (called with no args) = no packs

**Recommendation**: Document this prominently in the godoc and add a test
case. Consider whether `WithPacks()` (no args) should be equivalent to
"not called" (all packs) for ergonomic consistency.

---

### P3-2: Open Question 5 exit codes should be resolved

**Location**: §14 Open Questions, Q5

**Issue**: Q5 asks whether the CLI should use specific exit codes for
different decisions. The recommendation says "defer to implementation" but
this affects the test harness (shell scripts checking exit codes) and the
hook protocol (Claude Code may check exit codes).

Hook mode should always exit 0 on success (decision in JSON output) and
non-zero on error (broken hook). Test mode would benefit from decision-based
exit codes for scripting (e.g., `if dcg-go test "cmd"; then ...`).

**Recommendation**: Resolve now:
- Hook mode: exit 0 on success, exit 1 on error
- Test mode: exit 0 for Allow, exit 1 for Deny, exit 2 for Ask

---

## Cross-Plan Consistency Verification

1. **Plan 02 Pipeline.Run signature**: Plan 04 §6.1 shows
   `Run(command string, cfg Config)`. Plan 02 shows
   `RunPipeline(command string, cfg *evalConfig)`. The type name difference
   (`Config` vs `evalConfig`) is expected — plan 04 defines the published
   internal config type. ✓

2. **Plan 02 blocklist synthetic match**: Plan 04 integration test at line
   1480 asserts `result.Matches[0].Pack == "_blocklist"`. Plan 02 §5.5
   (MF-P0.2) defines this: blocklist adds synthetic Match with
   `Pack: "_blocklist"`. ✓

3. **Claude Code hook protocol**: Verified via documentation — `"allow"`,
   `"deny"`, and `"ask"` are all valid `permissionDecision` values. The
   deprecated values `"approve"` and `"block"` map to `"allow"` and `"deny"`.
   Plan 04's mapping is correct. ✓

4. **Plan 02 aggregateAssessments**: Selects highest severity match. Returns
   Assessment (severity + confidence only), not a reordered Matches list.
   This confirms P0-1 — `Matches[0]` is NOT guaranteed to be the primary
   match. ✓

5. **Plan 01 command extraction**: Tree-sitter extracts commands in order.
   Compound commands produce multiple `ExtractedCommand` values, processed
   sequentially by the pipeline. This feeds into the Matches ordering issue
   (P0-1). ✓

---

## Positive Observations

1. **Clean API surface**: `guard.Evaluate()` with functional options is
   idiomatic Go. The zero-value Result being a valid "nothing found, allow"
   result is elegant and removes nil-check boilerplate.

2. **Correct import layering**: `guard/types.go` as a leaf dependency with
   no imports cleanly breaks the potential circular import. Well thought out.

3. **`sync.Once` lazy init**: Pipeline construction is deferred until first
   use, avoiding expensive init when the package is imported but not used
   (e.g., in tests). ✓

4. **Config best-effort loading**: Missing files → defaults, malformed YAML
   → warning + defaults. The binary never fails to start due to config
   issues. This is the right approach for a security tool — fail-open on
   config, fail-closed on commands.

5. **Test harness property tests**: P1-P8 property tests are well-designed,
   especially P5 (policy monotonicity) which formally verifies the strictness
   ordering across all severity levels.

6. **Hook protocol fidelity**: Input/output JSON structures match the Claude
   Code PreToolUse hook specification. The "ask" decision is correctly
   supported. Non-Bash tools are correctly allowed without evaluation.
