# 04: Public API & CLI — Security & Correctness Review

**Reviewer**: dcg-alt-reviewer
**Date**: 2026-03-01
**Scope**: [04-api-and-cli.md](./04-api-and-cli.md), [04-api-and-cli-test-harness.md](./04-api-and-cli-test-harness.md)
**Focus**: Hook mode input validation (malformed JSON, missing fields), config file security (path traversal, glob injection), policy defaulting safety, exit code correctness, public API information exposure.

---

## Summary

The plan is well-structured with a clean separation between the public `guard` package and the CLI binary. The thin-layer design principle is sound — keeping evaluation logic in `internal/eval` and exposing only configuration and results through the public API. The three built-in policies are correctly implemented with proper Indeterminate handling. However, the hook mode has an unbounded stdin read (OOM vector), `WithPolicy(nil)` leads to a nil pointer dereference, and the config loader silently degrades to defaults on all errors including security-critical misconfigurations.

**Finding count**: 2 P0, 3 P1, 6 P2, 5 P3

---

## P0 — Critical (must fix before implementation)

### AC-P0.1: Hook mode `io.ReadAll(os.Stdin)` has no size limit — OOM vector

**Location**: Plan §5.2, `runHookMode()`

```go
input, err := io.ReadAll(os.Stdin)
```

`io.ReadAll` reads ALL of stdin into memory with no upper bound. While Claude Code hook events are typically small (<1KB), the hook binary is a standalone process that could receive input from:
- A misconfigured pipe or wrapper script sending large data
- A future Claude Code protocol change with large payloads
- Adversarial input in non-Claude Code deployment scenarios

A 1GB stdin payload would OOM the process, potentially affecting the host system.

**Fix**: Replace with bounded read:
```go
const maxHookInputSize = 1 << 20 // 1MB — generous for hook events
input, err := io.ReadAll(io.LimitReader(os.Stdin, maxHookInputSize))
```

If the limit is exceeded, return an error. 1MB provides ample headroom for any realistic hook event while preventing OOM.

### AC-P0.2: `WithPolicy(nil)` causes nil pointer dereference in pipeline

**Location**: Plan §4.3 (`WithPolicy`), §4.4 (`Evaluate`)

```go
func WithPolicy(p Policy) Option {
    return func(c *evalConfig) {
        c.policy = p // Sets policy to nil
    }
}
```

`Evaluate()` calls `defaultConfig()` (which sets InteractivePolicy), then applies options. If `WithPolicy(nil)` is called, `c.policy` becomes nil. `toInternal()` copies nil to `eval.Config.Policy`. The pipeline's `Run()` calls `policy.Decide(assessment)`, which panics on nil.

The test harness F3 explicitly expects `NotPanics` for `WithPolicy(nil)`, but the plan shows no nil-guard in the code path. This is a specification gap — the test expects behavior the plan doesn't implement.

**Fix**: Add nil-policy guard in `Evaluate()`:
```go
cfg := defaultConfig()
for _, opt := range opts {
    opt(&cfg)
}
if cfg.policy == nil {
    cfg.policy = InteractivePolicy() // Restore default on nil
}
```

---

## P1 — High (should fix)

### AC-P1.1: Config `os.ReadFile(path)` has no size limit

**Location**: Plan §5.5, `loadConfig()`

```go
data, err := os.ReadFile(path)
```

`os.ReadFile` reads the entire file into memory. While config files are typically small, the `DCG_CONFIG` env var allows pointing to any file path. A symlink to `/dev/zero` or a large file would cause unbounded memory allocation.

**Fix**: Use `os.Open` + `io.LimitReader` with a reasonable limit (e.g., 64KB for a config file). If exceeded, log a warning and fall back to defaults.

### AC-P1.2: Test mode exit codes undefined for Deny/Ask decisions

**Location**: Plan §5.3, §14 Open Question 5

Test mode currently exits 0 for all successful evaluations regardless of the decision. For scripting use cases (`dcg-go test "cmd" && deploy`), there's no way to programmatically check the decision via exit code.

The plan acknowledges this in Open Question 5 but defers to implementation. For a security tool, exit codes are a critical part of the interface — they enable CI/CD pipeline integration.

**Fix**: Define exit codes for test mode:
- 0: Allow
- 1: Error (parse failure, bad flags)
- 2: Deny
- 3: Ask

Document these in the usage string and test them.

### AC-P1.3: `loadConfig()` silently falls back to defaults on all errors

**Location**: Plan §5.5, §5.5.2 Note 1

```go
if err := yaml.Unmarshal(data, &cfg); err != nil {
    fmt.Fprintf(os.Stderr, "warning: invalid config at %s: %v\n", path, err)
    return Config{}
}
```

On ANY config error (malformed YAML, wrong types, broken file), the binary falls back to default config with only a stderr warning. This means:
- A typo in `policy: strct` silently uses InteractivePolicy instead of StrictPolicy
- A misconfigured blocklist `blocklist: "rm -rf /"` (string instead of list) silently has no blocklist
- File permission errors silently skip the config

For a security tool, silent fallback to weaker defaults is dangerous. A user who configured StrictPolicy in their config expects strict behavior — if the config fails to load, they get interactive (less strict) behavior without knowing.

**Fix**: In hook mode, if `DCG_CONFIG` is explicitly set and the file can't be loaded or parsed, exit with an error instead of falling back. For the default config path (`~/.config/dcg-go/config.yaml`), silent fallback on missing file is acceptable, but malformed YAML should be a hard error.

---

## P2 — Medium (should consider)

### AC-P2.1: Indeterminate severity iota ordering may confuse custom Policy implementers

**Location**: Plan §4.1, Severity type

```go
const (
    Indeterminate Severity = iota // 0
    Low                           // 1
    Medium                        // 2
    High                          // 3
    Critical                      // 4
)
```

`Indeterminate` has value 0, so `Indeterminate < Low < Medium < High < Critical`. The built-in policies correctly handle Indeterminate with explicit checks:
```go
// StrictPolicy
if a.Severity >= Medium || a.Severity == Indeterminate { return Deny }
```

But custom `Policy` implementations might write:
```go
if a.Severity >= Medium { return Deny }
return Allow
```
This would incorrectly Allow Indeterminate commands because `0 >= 2` is false. The `Policy` interface doc should warn about this gotcha.

**Recommendation**: Add a doc comment to the `Policy` interface: "NOTE: Indeterminate severity has iota value 0. Implementations must handle it explicitly — a simple `sev >= Medium` check will not catch Indeterminate."

### AC-P2.2: Hook mode exit(1) behavior from Claude Code's perspective undocumented

**Location**: Plan §5.2.1 Note 5

The plan says "a broken hook should fail loudly, not silently allow everything" and exits with code 1. But it doesn't document how Claude Code responds to a hook process that exits non-zero. If Claude Code treats hook failure as "allow" (fail-open), then a broken hook (bad config, OOM, crash) silently allows all commands — the opposite of the plan's intent.

**Fix**: Document Claude Code's behavior on hook exit(1). If Claude Code is fail-open on hook errors, consider outputting a deny JSON response before exiting, so the failure mode is fail-closed.

### AC-P2.3: Test harness F1 only checks NotPanics — should verify correct behavior

**Location**: Test harness F1 `TestFaultMalformedHookInput`

F1 tests malformed hook inputs but only asserts `NotPanics`. It doesn't verify the correct behavior:
- Empty input → error (exit 1)
- Invalid JSON → error (exit 1)
- Missing `tool_input` → allow (not Bash)
- Null command → allow (empty command)
- Extra fields → allow (ignored, evaluates normally)

Each case has different expected behavior. Testing only for "no panic" misses correctness bugs.

**Fix**: Replace `NotPanics` assertions with specific expected outcomes for each malformed input case. Add assertions for the expected decision or error.

### AC-P2.4: PackInfo missing EnvSensitive field

**Location**: Plan §4.4, `PackInfo` struct

```go
type PackInfo struct {
    ID, Name, Description string
    Keywords              []string
    SafeCount, DestrCount int
}
```

`PackInfo` doesn't expose whether the pack is env-sensitive. Library callers using `guard.Packs()` for discoverability can't determine which packs respond to environment variables. This information is useful for UIs that display pack capabilities or for config tools that help users decide which env vars to set.

**Fix**: Add `EnvSensitive bool` to `PackInfo`.

### AC-P2.5: Allowlist/blocklist glob `*` separator semantics need user-facing documentation

**Location**: Plan §4.3, §5.5.1

The plan specifies that `*` in glob patterns matches "any character except command separators (; | & etc.)." This means `allowlist: ["git *"]` does NOT match `"git push --force; rm -rf /"`. This is correct security-wise but non-obvious to users who expect `*` to match everything.

The config file format documentation (§5.5.1) shows example patterns but doesn't explain the separator restriction. Users may write overly narrow patterns (thinking `*` matches everything) or be surprised that compound commands aren't matched.

**Fix**: Add a comment in the config file example explaining the `*` separator behavior. Add a note in the library's `WithAllowlist` doc comment.

### AC-P2.6: Hook mode doesn't validate `hook_event_name` field

**Location**: Plan §5.2, `runHookMode()`

The hook code ignores `hookInput.HookEventName`. If Claude Code sends a `PostToolUse` event to the same binary (e.g., misconfigured settings.json), the hook evaluates the command and outputs a `PreToolUse` permission decision. This is at best ignored by Claude Code and at worst causes unexpected behavior.

**Fix**: Validate that `hookInput.HookEventName == "PreToolUse"`. For other event types, output allow or return an error. This is defensive — it prevents the hook from producing nonsensical output for events it doesn't understand.

---

## P3 — Low (nice to have)

### AC-P3.1: No hook protocol version field in output

**Location**: Plan §14 Open Question 1

Reasonable deferral for v1. The Claude Code hook protocol itself isn't versioned. When the protocol evolves, both the hook and Claude Code will need updates. A version field in the output would help with forward compatibility but isn't critical now.

### AC-P3.2: `enabled_packs` + `disabled_packs` interaction needs dedicated test

**Location**: Plan §5.5.2 Note 3, Test harness

Note 3 says both can be specified together: "enabled_packs acts as the include list and disabled_packs filters from that list." The test harness has P8 (all packs disabled) but no test for the combined case. A dedicated test verifying the interaction (e.g., enabled=[A, B, C], disabled=[B] → only A, C evaluated) would prevent regression.

### AC-P3.3: Test mode `--env` not passed by default — inline env still works

**Location**: Plan §5.3.1 Note 1

Test mode doesn't pass `os.Environ()` by default. This is intentional, but it means:
- `dcg-go test "RAILS_ENV=production rails db:reset"` — inline env IS detected (parsed from command) → escalated correctly
- `RAILS_ENV=production dcg-go test "rails db:reset"` — process env NOT detected unless `--env` is used → NOT escalated

This is documented behavior but may surprise users. Consider adding a note in the help text: "Note: process environment variables are not used for detection unless --env is specified. Inline env vars (e.g., RAILS_ENV=production cmd) are always detected."

### AC-P3.4: `buildReason` only includes first match reason in hook output

**Location**: Plan §5.2, `buildReason()`

For compound commands (e.g., `"git push --force && rm -rf /"`), the evaluation may produce multiple matches. `buildReason` only uses `result.Matches[0]` (highest severity). The user only sees one reason, not all risks.

This is acceptable for the hook output (concise) since test mode with `--explain` shows all matches. But for the hook, consider appending a note like "(+N more matches)" when multiple patterns match.

### AC-P3.5: Config warnings go to stderr only — not captured in Result.Warnings

**Location**: Plan §5.5 `loadConfig()`

When config is malformed, a warning is printed to stderr:
```go
fmt.Fprintf(os.Stderr, "warning: invalid config at %s: %v\n", path, err)
```

For the CLI binary, this is fine. But for library callers who might use `loadConfig()` patterns, there's no way to capture config issues programmatically. The `Result.Warnings` field has `WarnUnknownPackID` for bad pack IDs in options, but no equivalent for config issues.

This is a v2 consideration — for v1, config is CLI-only and stderr is appropriate.

---

## Cross-Cutting Observations

1. **The plan correctly implements the thin-layer principle** — the guard package adds minimal overhead over the internal pipeline. The `sync.Once` initialization and value-return `Result` are good design choices.

2. **The three-policy model is clean** — Strict/Interactive/Permissive provides a clear spectrum. The decision matrices are fully specified and tested (D2 in test harness). The Indeterminate handling is correct in all three built-in policies.

3. **Fail-open vs fail-closed tension** — The plan generally favors fail-open (missing config → defaults, missing fields → allow, non-Bash tools → allow). This is the right choice for a developer tool that shouldn't block work, but the config fallback case (AC-P1.3) deserves special attention because users who configure stricter security expect it to be enforced.

4. **Hook protocol is well-matched to Claude Code** — The input/output JSON format, the PreToolUse event handling, and the decision mapping (deny/ask/allow) align with Claude Code's documented hook protocol. The `buildReason` output provides actionable information to the user.

5. **Test harness is comprehensive** — P1-P8 properties cover the key invariants. The concurrent stress test (S1) with `-race` is essential. The golden file integration test (O1) provides confidence in the public-to-internal bridge. The main gap is F1 testing only panics, not correct behavior.
