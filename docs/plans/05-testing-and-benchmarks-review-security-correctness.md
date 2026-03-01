# 05: Testing & Benchmarks — Security/Correctness Review

**Reviewer**: dcg-alt-reviewer (independent review)
**Plan**: [05-testing-and-benchmarks.md](./05-testing-and-benchmarks.md)
**Test Harness**: [05-testing-and-benchmarks-test-harness.md](./05-testing-and-benchmarks-test-harness.md)
**Focus**: Fuzz testing coverage of security-critical paths, mutation testing
soundness, comparison test methodology, grammar coverage completeness.

---

## Findings Summary

| ID | Severity | Title |
|----|----------|-------|
| TB-P0.1 | P0 | Fuzz testing never exercises allowlist/blocklist — bypass bugs undetectable |
| TB-P0.2 | P0 | Mutation testing only covers destructive patterns — safe pattern false negatives undetectable |
| TB-P1.1 | P1 | No mutation operator for Not clause removal |
| TB-P1.2 | P1 | No ArgAt position-shift mutation operator |
| TB-P1.3 | P1 | P3 invariant tightness test only covers INV-1 through INV-5, missing INV-6/7/8 |
| TB-P2.1 | P2 | Grammar coverage missing `until_statement` and `redirected_command` node types |
| TB-P2.2 | P2 | TestGrammarCoverageAllPacks only tests 5 of 17+ structural contexts |
| TB-P2.3 | P2 | Hook input fuzzer doesn't test post-unmarshal processing logic |
| TB-P2.4 | P2 | E2E tests don't vary policy — Indeterminate handling under different policies untested |
| TB-P2.5 | P2 | Config fuzzer bypasses os.Exit path, only tests yaml.Unmarshal directly |
| TB-P2.6 | P2 | Comparison divergence classification ("reasonable") is underspecified |
| TB-P3.1 | P3 | EmptyReason mutation operator inflates kill count without testing matching |
| TB-P3.2 | P3 | SEC2 golden file execution test is too weak |
| TB-P3.3 | P3 | D3 test admits testing.T can't be mocked — wrapper not shown |
| TB-P3.4 | P3 | Upstream comparison corpus should include upstream's own test suite |
| TB-P3.5 | P3 | INV-8 oversized threshold hardcoded in two places |

---

## P0 — Security-Critical

### TB-P0.1: Fuzz testing never exercises allowlist/blocklist — bypass bugs undetectable

**Location**: §5.1 (FuzzEvaluate), §5.2 (verifyInvariants)

**Issue**: The pipeline fuzzer always calls `guard.Evaluate(command,
guard.WithPolicy(guard.InteractivePolicy()))` — it never passes
`WithAllowlist()` or `WithBlocklist()` options. There are zero fuzz
invariants that verify allowlist/blocklist behavior.

This means:
- A bug where allowlist glob matching has an off-by-one or fails to match
  a pattern that should match → commands that should be allowed are denied.
  Not caught by fuzzing.
- A bug where blocklist glob matching is bypassed by certain input
  characters (e.g., null bytes, unicode, command separators inside the
  glob) → commands that should be denied are allowed. Not caught by fuzzing.
- The plan states that `*` in allowlist/blocklist globs "doesn't cross
  command separators." There is no fuzz invariant verifying this critical
  safety property. A fuzzed command containing `; rm -rf /` after an
  allowlisted prefix should NOT be allowed, but this is never tested.

**Recommendation**: Add a second fuzz function `FuzzEvaluateWithAllowlist`
that fuzzes BOTH the command AND the allowlist/blocklist patterns. Add
invariants:
- INV-A1: If command exactly matches an allowlist pattern with no
  separators, decision is Allow.
- INV-A2: If allowlist pattern matches but command contains `&&`, `||`,
  `;`, `|` beyond the matched portion, the compound parts are still
  evaluated (allowlist doesn't blanket-allow everything after a separator).
- INV-B1: If command matches a blocklist pattern, decision is Deny
  regardless of safe patterns.

### TB-P0.2: Mutation testing only covers destructive patterns — safe pattern false negatives undetectable

**Location**: §6.4, lines 844-861 (`runMutationAnalysis`)

**Issue**: The mutation analysis iterates only over `pack.Destructive`
patterns:

```go
for _, pattern := range pack.Destructive {
    mutations := generateMutations(pattern)
    ...
}
```

Safe patterns are NOT mutated. Open question Q2 (§15) asks about this
and recommends "yes, include safe patterns" but the implementation code
in §6.4 does not include them.

This is security-critical because safe patterns use safe-before-destructive
ordering. If a safe pattern is accidentally broadened (e.g., a Not clause
removed), it will match commands that should trigger destructive patterns,
causing false negatives (dangerous commands classified as safe).

Concrete examples from reviewed packs:
- rsync S1 has 6 Not clauses for `--delete*` flags. If any Not clause
  is removed, `rsync --delete /src /dst` would be classified as safe
  instead of destructive.
- Vault S2 has `Or(ArgAt(0, "auth"), ArgAt(0, "token"), ...)` as safe
  subcommands. If this broadens, destructive operations like
  `vault auth disable` pass as safe.
- kubectl S4 has Not clauses for `--all`, `--all-namespaces`. If removed,
  `kubectl delete --all pods` is classified as safe.

A safe-pattern mutation that STILL matches all its original test cases
means the Not clause (or other restriction) is dead code and could be
silently removed — the exact bug class mutation testing is designed to
catch.

**Recommendation**: Change the mutation loop to include BOTH destructive
AND safe patterns:

```go
allPatterns := append(pack.Destructive, pack.Safe...)
for _, pattern := range allPatterns {
    mutations := generateMutations(pattern)
    ...
}
```

For safe patterns, the kill criterion is inverted: the test suite should
detect that the mutated safe pattern now incorrectly matches a command
that should be destructive. Update the mutation count table in §6.5
accordingly (~940 will roughly double).

---

## P1 — Correctness Risks

### TB-P1.1: No mutation operator for Not clause removal

**Location**: §6.2 (Mutation Operators table)

**Issue**: The mutation operator set is:
RemoveCondition, NegateCondition, SwapCommandName, RemoveFlag,
SwapSeverity, RemoveEnvTrigger, EmptyReason.

There is no operator specifically targeting Not() clauses. While
`RemoveCondition` could theoretically remove a Not clause if it's
treated as a standalone condition, in practice Not clauses are nested
inside And/Or compositions, so `RemoveCondition` applied to the top-level
condition list may not reach them.

Not clauses are the primary mechanism for safe pattern precision across
all packs:
- rsync S1: 6 Not clauses
- kubectl S4: Not(Or(HasFlag("--all"), ...))
- helm S2: Not(Or(ArgAt(1, "upgrade"), ...))

If the mutation harness can't specifically mutate these Not clauses
(removing the Not, or removing one alternative from the Or inside a Not),
the most security-critical conditions in the codebase are untested by
mutation analysis.

**Recommendation**: Add two mutation operators:
- `RemoveNot`: Unwrap a Not clause, making its inner condition the direct
  condition (e.g., `Not(HasFlag("--delete"))` → `HasFlag("--delete")`)
- `RemoveNotAlternative`: Remove one alternative from an Or inside a Not
  (e.g., `Not(Or(A, B, C))` → `Not(Or(A, C))`)

### TB-P1.2: No ArgAt position-shift mutation operator

**Location**: §6.2 (Mutation Operators table)

**Issue**: Patterns use `ArgAt(position, value)` for positional argument
matching. There is no mutation operator that shifts the position index.
This matters for multi-level subcommand matching:

- Vault patterns: `ArgAt(0, "kv")`, `ArgAt(1, "delete")` — if position
  0 and 1 are swapped, `vault delete kv` would match instead of
  `vault kv delete`. Without a position-shift mutation, this class of
  bug is invisible.
- The upcoming 3-deep ArgAt matching (vault kv metadata delete) at
  ArgAt(2) amplifies this risk.

**Recommendation**: Add `ShiftArgPosition` mutation operator: increment
or decrement the ArgAt position by 1. Target kill rate: 100% — every
ArgAt position must be tested for correctness.

### TB-P1.3: P3 invariant tightness test only covers INV-1 through INV-5

**Location**: Test harness P3 (lines 117-182)

**Issue**: The property test `TestPropertyFuzzInvariantsTight` constructs
broken results for INV-1 ("invalid decision"), INV-2 ("command not
preserved"), INV-3 ("empty command non-allow"), INV-4 ("nil assessment
with deny"), and INV-5 ("matches without assessment").

INV-6 (severity validation), INV-7 (match fields populated), and INV-8
(oversized input → Indeterminate) have no tightness verification. This
means:
- If INV-6 is accidentally written as `default: // ok` (accepting invalid
  severities), no meta-test catches it.
- If INV-7's empty-Pack check is removed, nothing detects it.
- If INV-8's threshold check is wrong, nothing detects it.

**Recommendation**: Add 3 more broken-result test cases to P3:
- INV-6: `Assessment.Severity = Severity(99)` → must be caught
- INV-7: `Match{Pack: "", Rule: ""}` → must be caught
- INV-8: 200KB command with non-Indeterminate severity → must be caught

---

## P2 — Coverage Gaps

### TB-P2.1: Grammar coverage missing `until_statement` and `redirected_command` node types

**Location**: §8.1 (CommandBearingNodeTypes, lines 1026-1044)

**Issue**: The enumerated list of command-bearing node types is missing:

1. `until_statement` — `until cmd; do cmd; done` is syntactically distinct
   from `while_statement` in tree-sitter's bash grammar. It has its own
   node type. Commands in until loops are not verified by grammar coverage.

2. `redirected_command` — Commands with redirections like
   `git push --force > /dev/null 2>&1` are wrapped in a `redirected_command`
   node in tree-sitter. If the command extractor doesn't unwrap this node,
   commands with redirections could be missed.

3. `coproc` — Bash 4+ `coproc cmd` may have its own AST node type.

Also, `heredoc_body` (line 1042) is listed as a command-bearing node type,
but heredoc bodies are typically literal text, not commands. Including it
could cause false positives in grammar coverage analysis or misleading
synthetic test generation.

**Recommendation**: Verify against the actual tree-sitter-bash grammar
definition (node-types.json) during implementation. Add `until_statement`
and `redirected_command`. Evaluate `coproc`. Consider removing or
annotating `heredoc_body` with a note that it's literal text unless the
heredoc contains a command substitution.

### TB-P2.2: TestGrammarCoverageAllPacks only tests 5 of 17+ structural contexts

**Location**: §8.3 (lines 1124-1130)

**Issue**: The per-pack grammar coverage test only verifies 5 structural
contexts: simple, list_and, subshell, pipeline, if_body. But §8.1
enumerates 17+ command-bearing node types and §8.2 tests 19 contexts.

This means per-pack coverage only covers 26% of structural contexts.
If a pack's keyword pre-filter or pattern matching fails specifically
in a for_body, while_body, case_body, function_body, command_substitution,
or negated context, it won't be caught by this test.

**Recommendation**: Use the same full context set from §8.2 (19 templates)
in the per-pack test. The total test count increases from ~50 to ~190
(10 packs × 19 contexts), which is still fast enough for Tier 2.

### TB-P2.3: Hook input fuzzer doesn't test post-unmarshal processing logic

**Location**: §5.5 (FuzzHookInput, lines 709-722)

**Issue**: The hook input fuzzer only tests `json.Unmarshal(data,
&hookInput)` — it verifies that the JSON parser doesn't panic. It does
NOT test the actual hook processing logic that runs after unmarshaling:
extracting the command from `tool_input.command`, evaluating it through
the pipeline, and producing the output JSON.

A valid-JSON input with unexpected field values (e.g., deeply nested
tool_input, extremely long command string, tool_name with control
characters) would pass the unmarshal step but could cause issues in
downstream processing. This is the more interesting attack surface.

**Recommendation**: Extend FuzzHookInput to test the full hook processing
path (not just unmarshal). Either call the full hook handler function, or
create a separate `FuzzHookProcess` fuzzer that generates valid JSON
with random field values and runs the complete hook flow.

### TB-P2.4: E2E tests don't vary policy — Indeterminate handling under different policies untested

**Location**: §9.1 (TestE2ERealWorldScenarios)

**Issue**: All E2E scenarios use `guard.WithPolicy(guard.InteractivePolicy())`
and `guard.WithEnv([]string{})`. The test suite never exercises
StrictPolicy or PermissivePolicy in E2E scenarios.

This means:
- Indeterminate severity commands under StrictPolicy (→ Deny) are never
  tested end-to-end.
- PermissivePolicy's more lenient thresholds (Critical → Deny, High → Ask,
  Medium → Allow) are never verified in realistic scenarios.
- Policy-specific decision boundary behavior (e.g., a Medium-severity
  command that's Ask under Interactive but Allow under Permissive) is
  never tested.

**Recommendation**: Add policy-parameterized E2E scenarios that run the
same commands under all 3 policies and verify the expected
policy-specific decisions. At minimum, add scenarios for:
- Indeterminate command × StrictPolicy → Deny
- Indeterminate command × InteractivePolicy → Ask
- Indeterminate command × PermissivePolicy → Allow
- Medium command × PermissivePolicy → Allow (vs Ask under Interactive)

### TB-P2.5: Config fuzzer bypasses os.Exit path, only tests yaml.Unmarshal directly

**Location**: §5.4 (FuzzConfigParse, lines 678-701)

**Issue**: The config fuzzer's comment says "Note: loadConfig may os.Exit
on malformed input. For fuzz testing, we test the YAML parse path
directly." So it calls `yaml.Unmarshal(data, &cfg)` and `cfg.toOptions()`
instead of the actual `loadConfig()` function.

This means:
- Any panic-inducing code path in loadConfig that occurs before or after
  the YAML parse (file permissions check, path resolution, size validation)
  is not fuzzed.
- The `toOptions()` method is tested, but if loadConfig has additional
  validation or transformation logic that's not in toOptions, it's missed.

**Recommendation**: Refactor loadConfig to separate the os.Exit call
from the parsing logic. Create a `parseConfig(data []byte) (*Config,
error)` function that returns errors instead of exiting. Fuzz
`parseConfig` instead of raw `yaml.Unmarshal`. This tests the real
code path without the os.Exit problem.

### TB-P2.6: Comparison divergence classification is underspecified

**Location**: §4.4 (Divergence Classification table)

**Issue**: The classification says "Minor severity differences... are
classified as `intentional_divergence` if the assessment is reasonable."
"Reasonable" is subjective and not formalized. Additionally, the
`classifyDivergence` function is used in P5 determinism tests but its
implementation is never shown.

Without a clear specification:
- Different runs could classify the same divergence differently (violating
  P5 if classification involves human judgment).
- Genuine bugs where Go produces Allow and Rust produces Deny could be
  incorrectly classified as "intentional" by a permissive classifier.

**Recommendation**: Formalize classification rules:
- Identical decision = `identical`
- Go more restrictive (Deny when Rust allows) = `intentional_divergence`
  (safer default)
- Go less restrictive (Allow when Rust denies) = `bug` until manually
  reclassified
- Severity difference ≤1 level with same decision = `intentional_divergence`
- Severity difference ≥2 levels = requires manual review

Store known divergences in `comparison_divergences.json` as a lookup
table. `classifyDivergence` checks this table first; any entry NOT in
the table defaults to `bug`.

---

## P3 — Minor / Improvements

### TB-P3.1: EmptyReason mutation operator inflates kill count without testing matching

**Location**: §6.2 (Mutation Operators table)

**Issue**: The `EmptyReason` operator clears the reason string. This
doesn't change matching behavior — it only affects human-readable output.
Every EmptyReason mutation will be "killed" by any test that checks
the reason field, inflating the kill rate without actually testing
pattern correctness.

If a pack has 20 destructive patterns, EmptyReason adds 20 mutations
that are trivially killed, making a 90% real kill rate look like 95%.

**Recommendation**: Either (a) exclude EmptyReason from the kill rate
calculation (track separately as "metadata mutations"), or (b) replace
it with a more meaningful operator like `SwapConfidence` (High → Medium)
which affects the Assessment output.

### TB-P3.2: SEC2 golden file execution test is too weak

**Location**: Test harness SEC2 (lines 688-700)

**Issue**: SEC2 creates one `touch` command and verifies guard.Evaluate
doesn't execute it. This proves guard.Evaluate is string-only, but it's
a trivially obvious property. A more useful security test would verify
that the ENTIRE test harness (golden file tests, comparison tests, E2E
tests) doesn't accidentally execute commands — e.g., by running with
`PATH=""` and verifying no `exec.LookPath` failures occur during the
golden file test suite.

**Recommendation**: Extend SEC2 to run the full golden file test suite
with a restricted PATH and verify no subprocess execution attempts.

### TB-P3.3: D3 test admits testing.T can't be mocked — wrapper not shown

**Location**: Test harness D3 (lines 407-420)

**Issue**: The test code says "Note: In practice, testing.T can't be
mocked this way. The actual test uses a wrapper that captures t.Fatal
calls." But this wrapper is not shown in the plan. If it doesn't exist
in the implementation, D3 is a broken test.

**Recommendation**: Show the t.Fatal capture wrapper in the plan, or use
Go's `testing.TB` interface with a custom implementation that records
failures instead of aborting.

### TB-P3.4: Upstream comparison corpus should include upstream's own test suite

**Location**: §4.2 (Corpus Generation)

**Issue**: The comparison corpus is built from our golden files, our
structural variations, and our edge cases. It does not include the
upstream Rust implementation's own test suite as a seed source.

The upstream likely has test cases targeting its specific regex patterns
and edge cases. Using their test suite as comparison seeds would catch
cases where:
- They have patterns we don't (coverage gap)
- Our structural analysis produces different results on cases they
  specifically tested

**Recommendation**: Add a 4th corpus source: "Upstream test suite commands"
— extract test case commands from the upstream Rust repository and include
them in the comparison corpus.

### TB-P3.5: INV-8 oversized threshold hardcoded in two places

**Location**: §5.1 seed corpus, §5.2 INV-8

**Issue**: The oversized threshold `128*1024` appears as a hardcoded
value in both the fuzz seed (`strings.Repeat("a", 128*1024+1)`) and the
INV-8 invariant check (`if len(command) > 128*1024`). The actual
pipeline's size limit is defined elsewhere (plan 01/02).

If the pipeline's limit changes (e.g., to 256KB), both fuzz locations
need manual updating, and a mismatch would make INV-8 silently incorrect.

**Recommendation**: Define the limit as a constant exported from the
pipeline package (e.g., `eval.MaxCommandBytes`) and reference it in both
the fuzz seed and INV-8. This ensures the fuzz test stays in sync with
the actual limit.

---

## Cross-Cutting Observations

1. **Fuzz testing is the weakest link in security coverage**: The fuzzer
   tests the core Evaluate path well but completely ignores the option
   space (allowlists, blocklists, policies, env, disabled packs). Since
   these options control security-critical behavior, this is the most
   impactful gap to close.

2. **Mutation testing scope directly determines safety guarantees**: The
   100% kill rate target is excellent, but it only provides guarantees
   for the pattern types that are mutated. Excluding safe patterns from
   mutation analysis means the safe-before-destructive short-circuiting
   mechanism (arguably the most security-critical behavior in the system)
   has no mutation coverage.

3. **Grammar coverage is solid in design but incomplete in implementation**:
   §8.1/8.2 are well-designed, but the per-pack test (§8.3) undermines
   the value by only testing 5 contexts. The effort difference between
   5 and 19 contexts is minimal — it's the same test with a longer list.

4. **Comparison testing depends on unresolved infrastructure**: The
   upstream binary availability is listed as Open Question Q1 and doesn't
   have a resolution. The comparison test harness design is sound, but
   it's blocked on answering Q1. Consider adding a "mock upstream" mode
   for development that uses a fixed result set.
