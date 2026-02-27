# Review: 00-architecture (security-architect)

- Source doc: `docs/plans/00-architecture.md`
- Also reviewed: `docs/plans/00-plan-index.md`, `docs/shaping/shaping.md`, `docs/shaping/frame.md`
- Reviewed commit: 25782a7d6d34544c06f53c1292477ae765d32daf
- Reviewer: security-architect

## Findings

### P0 - Fail-open on parse errors enables trivial bypass via malformed syntax

**Problem**

Architecture Section 6, D4 states: "If tree-sitter fails to parse a command (malformed bash, exotic syntax), return Allow." The threat model (frame.md) explicitly scopes out adversarial evasion, which is fine. But this fail-open behavior can also be triggered *accidentally* by LLM-generated commands that are destructive but use unusual syntax that tree-sitter's bash grammar does not handle.

Tree-sitter's bash grammar is known to not cover 100% of real-world bash. For example, process substitution edge cases, associative array syntax, certain brace expansion patterns, and complex nested quoting can produce parse errors. An LLM could generate something like:

```bash
rm -rf /{important_dir,other_dir}
```

If the brace expansion causes a partial parse failure (tree-sitter returns ERROR nodes rather than a complete parse failure), the behavior is ambiguous. The doc does not distinguish between "tree-sitter returned an error" and "tree-sitter returned a partial parse with ERROR nodes in the tree." Tree-sitter almost always returns *some* tree, even for malformed input -- it does error recovery. A full "parse failure" is extremely rare. This means D4 as stated may almost never trigger, but the *real* risk is ERROR nodes in otherwise-parsed trees causing commands to be silently missed by the extractor.

**Required fix**

1. Clarify the distinction between "parse failure" (tree-sitter returns no tree at all, which is very rare) and "partial parse with ERROR nodes" (which is common for unusual syntax).
2. Define explicit behavior for partial parses: the extractor should still extract all `simple_command` nodes it can find in the tree, even if some subtrees contain ERROR nodes. This is not fail-open; it is best-effort extraction.
3. Add a test category in the golden file corpus specifically for commands with ERROR nodes to verify the extractor does not silently skip destructive commands that appear alongside malformed syntax.

---

### P0 - Allowlist matching semantics undefined: glob, regex, exact, or prefix?

**Problem**

Architecture Section 3, Layer 2 (pipeline step 1) says: "If the command matches a caller-provided allowlist pattern, short-circuit to Allow." The `WithAllowlist(patterns ...string)` and `WithBlocklist(patterns ...string)` options accept `...string` but the matching semantics are completely unspecified. Is `"rm"` an exact match? A substring? A glob? A regex?

This is a security-relevant gap. Allowlist matching is the most dangerous feature in the system because it can suppress all analysis. If the allowlist matches substrings, then `WithAllowlist("ls")` would also allow `rm -rf / && ls`. If it matches command prefixes, `WithAllowlist("git")` would allow `git push --force`. If it uses regex, the caller could accidentally write a pattern that matches too broadly.

Similarly, blocklist short-circuits to Deny before any parsing. An overly broad blocklist pattern could block harmless commands.

**Required fix**

1. Define the exact matching semantics for allowlist and blocklist patterns. Recommend: match against the full raw command string, using glob patterns (not regex) where `*` matches any characters. Document this clearly.
2. Specify the evaluation order unambiguously: does blocklist take precedence over allowlist, or vice versa? What if a command matches both?
3. Consider whether allowlist/blocklist should match against raw command strings or against extracted command names. Matching against raw strings is simpler but prone to over-matching; matching against extracted command names is safer but requires parsing first (which conflicts with allowlist being step 1 before parsing).

---

### P1 - Dataflow analysis does not handle conditional paths, creating false confidence

**Problem**

Architecture Section 8 (Alien Artifacts) describes the intraprocedural dataflow analysis. It explicitly notes: "we don't handle control flow (if/then/else), loops, or function definitions. We handle the linear and `&&`/`||` cases."

The problem is with `||` specifically. Consider:

```bash
DIR=/tmp || DIR=/; rm -rf $DIR
```

The dataflow analysis performs a "forward pass" and would see `DIR=/tmp`, then `DIR=/` (via `||`), and resolve `$DIR` to `/`. But the semantics of `||` mean `DIR=/` only executes if `DIR=/tmp` fails -- which it won't, since it's a simple assignment. So the actual value of `$DIR` is `/tmp`, not `/`. The analysis would produce a false positive.

Conversely:

```bash
test -d /safe && DIR=/safe || DIR=/; rm -rf $DIR
```

Here the analysis might resolve `$DIR` to `/` (the last assignment seen), which may or may not be correct depending on whether `/safe` exists.

The doc claims this covers ">95% of real-world LLM-generated commands" but does not justify this claim. More importantly, it does not describe what happens when the analysis is wrong -- does it produce false positives (unnecessary denials) or false negatives (missed dangerous commands)?

**Required fix**

1. Document the error mode explicitly: for `||` chains, state that the analysis conservatively tracks all possible values (or the last value, or some other strategy). Specify whether the bias is toward false positives or false negatives.
2. Recommend a "may-alias" approach: when a variable has multiple possible values due to `||` branching, substitute all of them and evaluate each possibility. If any substitution produces a dangerous match, flag it. This biases toward false positives, which is the correct direction for a safety tool.
3. Add test cases for `||` dataflow in the golden file corpus.

---

### P1 - Inline script recursive evaluation lacks depth limit and cycle protection

**Problem**

Architecture Section 4, "Data Flow: Inline Script Detection" shows that inline scripts are "recursively evaluated through the main pipeline." For example, `python -c "import os; os.system('rm -rf /')"` would extract `rm -rf /` and evaluate it recursively.

There is no mention of:
1. A recursion depth limit. What happens with `bash -c "bash -c \"bash -c 'rm -rf /'\""`?
2. How extracted shell commands from non-shell languages are re-evaluated. The diagram shows the Python AST walker extracting `os.system("rm -rf /")` and feeding `rm -rf /` back through the pipeline. But what about `os.system("python -c 'import shutil; shutil.rmtree(\"/\")'")` -- does the re-evaluation detect the nested Python inline script?
3. Whether infinite recursion is possible and how it is guarded against.

While this is a mistake-preventer and adversarial crafting is out of scope, an LLM could plausibly generate deeply nested inline scripts, and the tool should not hang or crash.

**Required fix**

1. Add a maximum recursion depth (e.g., 3 levels) for inline script evaluation. Document this in the architecture.
2. Specify that the recursion applies the full pipeline including inline script detection, enabling nested detection up to the depth limit.
3. Add a fuzz testing invariant: "recursive inline script evaluation terminates within bounded time."

---

### P1 - Assessment aggregation strategy is underspecified for compound commands

**Problem**

Architecture Section 3, Layer 2, pipeline step 10 says: "If multiple commands in a pipeline match, take the highest severity." This is stated for pipelines but the aggregation strategy for other compound forms is not specified:

- `&&` chains: `safe_cmd && dangerous_cmd` -- presumably the highest severity wins, but not stated.
- `||` chains: `dangerous_cmd || fallback_cmd` -- same question.
- Subshells: `(dangerous_cmd)` -- is the subshell boundary transparent?
- Command substitution: `echo $(dangerous_cmd)` -- should the inner command's destructiveness propagate?
- Backgrounded commands: `dangerous_cmd &` -- same question.
- Semicolons: `safe_cmd; dangerous_cmd` -- same question.

Also, what about combining severity and confidence across multiple matches? If one command matches at `High/Low-confidence` and another at `Medium/High-confidence`, which Assessment is surfaced? "Highest severity" ignores confidence, which could lead to a High-severity, Low-confidence match driving a Deny decision when the only real concern is Medium.

**Required fix**

1. Specify the aggregation strategy for all compound command forms, not just pipelines.
2. Define how confidence interacts with aggregation. Recommend: aggregate by `(severity, confidence)` as a tuple, with severity as primary sort and confidence as secondary. This way High/High outranks High/Low.
3. Clarify whether command substitution and backgrounded commands are evaluated.

---

### P1 - Environment detection lacks specificity on matching rules and false positive mitigation

**Problem**

Architecture Section 3 (pipeline step 8) and Shaping A7 describe environment detection checking for "production indicators" but the matching rules are vague. The shaping doc gives examples like `RAILS_ENV=production`, `DATABASE_URL` containing "prod", `AWS_PROFILE=production`.

Questions not addressed:
1. What is the full list of env var names checked? Is it hardcoded or configurable?
2. What constitutes a "production indicator" value? Is it exact match against `"production"`? Substring `"prod"`? What about `"prod-staging"`, `"reproduce"`, `"productivity"` (all contain "prod")?
3. If the caller provides `WithEnv(os.Environ())`, the tool scans the entire process environment. On a developer machine, there might be a `DOCKER_HOST=prod-registry.internal` or `JIRA_PROJECT=PROD-123` that contains "prod" but is not a production indicator. This creates false escalation.
4. The escalation behavior is described as "escalate severity" but the exact mapping is not defined. Does Medium become High? High become Critical? Is it a +1 bump or a jump to Critical?

**Required fix**

1. Define the exact list of env var names checked (or state that it will be defined in the 02 sub-plan).
2. Specify the matching logic: recommend checking specific env var names (e.g., `RAILS_ENV`, `NODE_ENV`, `FLASK_ENV`, `APP_ENV`) against specific values (`production`, `prod`), and checking URL-shaped values in `DATABASE_URL`, `REDIS_URL`, etc. for hostnames containing `prod`. Do not do substring matching on arbitrary env vars.
3. Define the severity escalation mapping explicitly (e.g., "env escalation bumps severity by one level, capped at Critical").
4. Consider making the env detection rules configurable or at least extensible.

---

### P2 - CommandMatcher interface is too minimal for safe pattern expressiveness

**Problem**

Architecture Section 3, Layer 3 defines `CommandMatcher` as:

```go
type CommandMatcher interface {
    Match(cmd ExtractedCommand) bool
}
```

The doc mentions structural matching like "rm -rf but NOT if target is under /tmp" but the interface only returns a bool. Safe patterns also use `CommandMatcher`. The narrative suggests safe patterns short-circuit a command to Allow, but the interplay between safe and destructive matchers for the same command is underspecified.

Consider `git push --force-with-lease`. The core.git pack should have a safe pattern for `--force-with-lease` and a destructive pattern for `--force`. If both `CommandMatcher`s return true (because `--force-with-lease` contains `--force` as a prefix), what happens? The doc says "Safe patterns are checked first (short-circuit to Allow for that command)" -- so the safe pattern wins. But this depends entirely on the matcher implementation correctly distinguishing `--force` from `--force-with-lease`, which is an implementation detail not constrained by the architecture.

**Required fix**

1. Clarify that `CommandMatcher` implementations must do exact flag matching (not prefix matching) when matching against `ExtractedCommand.Flags`. The `Flags` map should use the full flag name as the key.
2. Specify how `ExtractedCommand.Flags` represents combined short flags (e.g., `rm -rf` where `-r` and `-f` are separate flags combined). Are they split into separate map entries? Or kept as `-rf`?
3. Document the safe-before-destructive evaluation order and clarify that if a safe pattern matches, destructive patterns for that *specific extracted command* are skipped, but other extracted commands in the same compound statement are still evaluated.

---

### P2 - No timeout or resource limit on tree-sitter parsing

**Problem**

Architecture Section 7 (Performance) says: "No hard budget." Section 6, D4 says fail-open on parse errors. But there is no mention of what happens if tree-sitter parsing takes an unexpectedly long time on adversarial or pathological input (e.g., deeply nested command substitutions, very long strings).

The frame doc initially mentioned a 100ms budget, then walked it back to "just benchmark." While the threat model is non-adversarial, an LLM could generate a very long command (e.g., a large heredoc or a long pipeline), and the caller should not block indefinitely.

**Required fix**

1. Add a maximum input length check before parsing (e.g., 64KB). Commands longer than this are rare and can be allowed without analysis.
2. Consider a context.Context parameter or timeout on `Evaluate()` to allow callers to enforce their own budgets if needed. At minimum, document that callers who need timeout behavior should wrap the call with their own deadline.

---

### P2 - Mutation testing and golden file corpus deferred to Batch 5 creates a testing gap during pack development

**Problem**

The plan index places all URP testing (mutation testing, golden file corpus, fuzz testing, comparison testing) in Batch 5. But Batch 3 implements all 21 packs. This means packs are developed and tested only with per-pack unit tests, without the mutation testing harness that would catch undertested matcher conditions, and without the golden file corpus that would catch cross-pack interactions.

The risk: if patterns are written in Batch 3 without the discipline of the mutation testing framework, they may have redundant conditions or gaps that are expensive to fix later. The golden file corpus is also a regression safety net that would be most valuable during active pack development.

**Required fix**

1. Move the golden file corpus infrastructure (not the full 500+ commands, but the framework and initial seed) to Batch 2. Packs in Batch 3 should contribute to the corpus as they are developed.
2. Consider moving mutation testing framework to Batch 2 or early Batch 3, so the core packs (03a) are developed with mutation testing from the start and establish the pattern for subsequent packs.

---

### P2 - ExtractedCommand.Flags as map[string]string loses flag ordering and duplicate flags

**Problem**

Architecture Section 3 defines:

```go
Flags map[string]string // Flag name -> value (or "" for boolean flags)
```

A `map[string]string` cannot represent:
1. Duplicate flags: `curl -H "Content-Type: application/json" -H "Authorization: Bearer token"` has two `-H` flags.
2. Flag ordering: Some commands have order-dependent flags (e.g., `iptables` rules).
3. Flags that appear multiple times with different values.

For the use case of detecting destructive patterns, this may not matter much (destructive patterns typically check for presence of specific flags, not their multiplicity or order). But it should be acknowledged as a known limitation.

**Required fix**

1. Acknowledge in the architecture that `map[string]string` loses duplicate and ordering information, and state whether this is acceptable or whether a `[]Flag` slice should be used instead.
2. If staying with the map, document that matchers should not depend on flag multiplicity or ordering.

---

### P2 - tree-sitter-go grammar export is a hard external dependency with no fallback plan

**Problem**

Architecture Section 6, D6 says grammars will be exported from tree-sitter-go by moving them from `internal/testgrammars/` to `grammars/`. Plan index Open Question 1 notes this needs to happen before Batch 1.

This is a change in a separate repository. If that change is delayed, blocked, or rejected, the entire project is blocked at Batch 1. There is no fallback plan described.

**Required fix**

1. Document a fallback: if the tree-sitter-go grammar export is delayed, DCG can vendor the grammar data directly (even if temporarily). This unblocks development.
2. Clarify who owns the tree-sitter-go change and whether it has been agreed to.

---

### P3 - No versioning strategy for the guard package public API

**Problem**

The `guard` package is described as a public API for library consumers (primarily h2). There is no mention of API stability guarantees, versioning strategy, or backward compatibility policy. Since this is Go, the module path and major version are tightly coupled (Go modules semver).

**Required fix**

1. State whether the initial release is v0 (no stability guarantees) or v1 (stable API). Recommend v0 for the initial release with a note about when to promote to v1.

---

### P3 - No consideration of command alias/function resolution

**Problem**

Architecture Section 3, Layer 2 (pipeline step 6) describes normalization as "strip path prefixes." But shell environments also have aliases (`alias rm='rm -i'`) and shell functions that could shadow command names. The tool has no way to know about these.

This is acknowledged implicitly by the threat model (we only analyze the command string, not the execution environment), but it could be confusing when a command like `ll` (common alias for `ls -la`) is not recognized.

**Required fix**

1. Add a brief note in the architecture acknowledging that alias/function resolution is out of scope and that the tool operates on the literal command string, not the resolved execution path.

---

### P3 - Shaping doc shows R9 (config file) as "Nice-to-have" but plan index includes it in Batch 4

**Problem**

Shaping doc marks R9 (config file loading) as "Nice-to-have" and the fit check shows it as failed (cross-mark). But Plan index Batch 4 includes "Config file loading (YAML)" as part of 04-api-and-cli. This is a minor inconsistency that should be reconciled.

**Required fix**

1. Either promote R9 to "Must-have" in the shaping doc, or mark config file loading as optional/deferred in the plan index.

---

### P3 - `Negated` field in ExtractedCommand is unused in the architecture narrative

**Problem**

`ExtractedCommand` includes `Negated bool // Preceded by !` but no pattern matching logic or pipeline step references how negation affects evaluation. If `! rm -rf /` is negated, does that change the assessment? (It shouldn't, since the command still executes -- `!` only inverts the exit code.)

**Required fix**

1. Clarify that `Negated` does not affect severity assessment (the command still runs). State why the field is preserved (useful for callers who want full command context, or for future use).

---

## Summary

14 findings: 2 P0, 4 P1, 4 P2, 4 P3

**Verdict**: Approved with revisions

The architecture is fundamentally sound. The tree-sitter-first approach is well-motivated and the layered design with assessment/policy separation is clean. The Alien Artifacts (dataflow analysis) and URP sections (mutation testing, golden file corpus, fuzz testing) are substantive and genuinely applicable -- these are not overclaimed.

The two P0 findings (fail-open behavior with partial parses, and undefined allowlist matching semantics) are concrete risks that should be resolved before implementation begins. The P1 findings around dataflow analysis edge cases, inline recursion limits, and assessment aggregation are design gaps that could lead to surprising behavior if not specified. None of these are architecturally blocking -- they can all be addressed with clarifications and additions to the existing design without restructuring.

The Extreme Optimization section is refreshingly honest about not applying SIMD/assembly to this workload. The focus on parser pooling and pre-filter effectiveness is correct.
