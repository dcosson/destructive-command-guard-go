# 01: Tree-sitter Integration — Review: Security & Correctness

**Plan**: [01-treesitter-integration.md](./01-treesitter-integration.md)
**Test Harness**: [01-treesitter-integration-test-harness.md](./01-treesitter-integration-test-harness.md)
**Reviewer Focus**: Security, correctness, threat model adherence, fail-safe behavior, edge cases in parsing, potential for false negatives, dataflow analysis soundness
**Date**: 2026-02-28

---

## Findings

### SC-P0.1: Dataflow `&&` Chains Unsound — False Negatives Possible

**Severity**: P0
**Component**: DataflowAnalyzer (§6.5)
**Category**: Correctness / False Negatives

The plan states that `&&` chains carry left-side definitions to the right, described as "correct for the success path and conservative for the failure path." However, the implementation of `Define()` uses **strong update** semantics (`da.defs[name] = []string{value}` — overwrites previous values).

Consider this command:

```bash
DIR=/important && DIR=/tmp && rm -rf $DIR
```

The analyzer would resolve `$DIR` to `["/tmp"]` only. This is correct. But consider:

```bash
DIR=/tmp && something_that_fails && DIR=/ && rm -rf $DIR
```

With `&&` semantics, if `something_that_fails` fails, `DIR=/` never executes, so `$DIR` is actually `/tmp`. But the analyzer walks linearly and performs strong updates, so it resolves `$DIR` to `["/"]`. This could produce a **false positive** (flagging `/` when the actual value would be `/tmp`), which is the safe direction.

However, the real P0 concern is the **reverse case**:

```bash
DIR=/ && something && DIR=/tmp && rm -rf $DIR
```

Here the analyzer resolves `$DIR` to `["/tmp"]` (safe), but if `something` fails, `DIR` stays as `/` (dangerous). The analyzer performs strong update and **loses the dangerous value**. This is a **false negative** — a dangerous command is allowed because the analyzer assumes the chain completed.

**Recommendation**: For `&&` chains, use may-alias semantics (like `||` chains) — track *all* values the variable could hold across all possible execution paths. Strong update should only apply within sequential (`;`) execution. This biases toward false positives, which is correct for a safety tool.

---

### SC-P0.2: Inline Script Detection Relies on Flag Ordering Assumptions

**Severity**: P0
**Component**: InlineDetector (§6.6)
**Category**: False Negatives

The plan states: "The argument immediately following the trigger flag is the script body." However, the `classifyArg` function in §6.3 decomposes combined short flags and treats short flags as always boolean.

This creates a problem. Consider:

```bash
python -c 'import os; os.system("rm -rf /")'
```

After `classifyArg` processing, `-c` is stored as a boolean flag (`{"-c": ""}`) and `'import os; os.system("rm -rf /")'` is a positional argument. The `InlineDetector.detectFlagScripts()` then needs to correlate the `-c` flag with the *next positional argument* as the script body.

But the plan's `classifyArg` doesn't preserve argument ordering relative to flags. The `Flags` map and `Args` slice are separate. If someone writes:

```bash
python -v -c 'rm -rf /' script.py
```

Or even:

```bash
python 'rm -rf /' -c
```

The detector needs to find the correct positional argument that follows `-c`. The plan does not specify how `detectFlagScripts` correlates flags with their values for short flags like `-c`. The `ExtractedCommand` struct loses the positional relationship between `-c` and its argument.

**Recommendation**: The `InlineDetector` needs access to the raw AST node (or at minimum, the ordered argument list preserving flag positions) to correctly identify which positional argument follows the trigger flag. Either:
1. Pass the AST node to the detector, not just the `ExtractedCommand`, or
2. Add an `OrderedArgs []string` field that preserves the full argument sequence including flags, or
3. Handle the flag-value association for known inline rules specially during extraction (before the generic `classifyArg`).

---

### SC-P1.1: `ResolveString` Expansion Cap Creates Silent False Negatives

**Severity**: P1
**Component**: DataflowAnalyzer (§6.5)
**Category**: False Negatives

The expansion limit of 16 in `ResolveString` is necessary to prevent combinatorial explosion, but the plan says: "Beyond that, the unresolved `$VAR` reference is left as-is."

Leaving `$VAR` as a literal string means the matcher will see the literal text `$VAR` as an argument. This creates a false negative: if one of the 17+ possible values was dangerous (e.g., `/`), it won't be checked because we stopped expanding.

Consider:

```bash
D=/a || D=/b || D=/c || D=/d || D=/e || D=/f || D=/g || D=/h || D=/i || D=/j || D=/k || D=/l || D=/m || D=/n || D=/o || D=/p || D=/ ; rm -rf $D
```

The variable `D` has 17 possible values. The 17th is `/` (dangerous). ResolveString hits the cap at 16 and leaves `$D` as-is. The `rm -rf $D` command has a literal `$D` as argument, which no pattern would flag.

**Recommendation**: When the expansion cap is hit, the result should include a warning (`WarnExpansionCapped` or similar) and the command's assessment should be promoted to Indeterminate, letting the policy decide. This preserves the fail-safe behavior — uncertainty should not silently produce Allow.

---

### SC-P1.2: `eval` Command Not Handled in Inline Detection

**Severity**: P1
**Component**: InlineDetector (§6.6)
**Category**: False Negatives

The `InlineScript.Source` field includes `"eval"` as a possible value, but the `inlineRules` list in §6.6 does not include any rules for the `eval` built-in:

```bash
eval "rm -rf /"
```

The `eval` command takes its arguments as a shell string and executes them. It's a primary attack vector for obfuscation. Without an inline rule for `eval`, the matcher only sees `eval` as the command name with `"rm -rf /"` as a positional argument — it would not detect the `rm -rf /` inside.

Similarly, `xargs` and `env` are common command wrappers that introduce indirection:

```bash
echo "rm -rf /" | xargs bash -c
env TERM=dumb bash -c "rm -rf /"
```

**Recommendation**: Add inline rules for:
- `eval` → Language: "bash", treat all args concatenated as the script body
- Consider `xargs` as a known limitation documented in the plan
- `env` command wrapper (everything after the env vars and flags is the actual command to evaluate)

---

### SC-P1.3: Heredoc Body Extraction Depends on Unspecified AST Walking

**Severity**: P1
**Component**: InlineDetector (§6.6)
**Category**: Under-specification

The heredoc detection function `detectHeredocs` has placeholder implementation (`...`). The plan says heredocs are "detected structurally from the bash AST" but doesn't specify:

1. How the detector determines which command the heredoc feeds into (the heredoc_redirect is a sibling, not a child of the command node in tree-sitter's bash grammar)
2. How `cat <<'EOF' | bash` is detected (the heredoc belongs to `cat`, but the pipe connects to `bash` — this requires cross-command analysis within a pipeline)
3. How quoted vs unquoted heredoc delimiters are handled (quoted `<<'EOF'` suppresses variable expansion, unquoted `<<EOF` allows it — affects whether dataflow analysis should resolve variables in the body)

Pattern 2 (`cat ... | bash`) is particularly important because it's a common shell idiom and the heredoc body should be re-parsed as bash. The plan mentions it in Open Question 3 but doesn't resolve it.

**Recommendation**: Fully specify the heredoc detection algorithm, including:
- AST node traversal from `redirected_statement` to `heredoc_redirect` to `heredoc_body`
- Pipeline-aware detection (command's stdout piped to `bash`/`sh`)
- Quoted vs unquoted delimiter handling and its interaction with dataflow

---

### SC-P1.4: `hasErrorNodes` Walk Could Be Expensive for Large ASTs

**Severity**: P1
**Component**: BashParser (§6.2)
**Category**: Performance / Correctness

`hasErrorNodes` does a "quick DFS" over the entire AST before extraction even begins. For large inputs (up to 128KB), this is a redundant full tree walk — the extractor will walk the same tree again during extraction.

More importantly, the hasErrorNodes check is used to set `ParseResult.HasError`, which is consumed by the eval pipeline (Batch 2) to decide whether to produce an Indeterminate assessment. But the extractor also walks the tree. The two walks could disagree about whether ERROR nodes exist (e.g., if hasErrorNodes checks a different set of node types than what the extractor encounters).

**Recommendation**: Merge ERROR node detection into the extraction walk. As the extractor encounters ERROR nodes, it sets a flag. This eliminates the redundant walk and ensures consistency between error detection and extraction.

---

### SC-P2.1: Short Flag Decomposition Creates False Negatives for Flags with Values

**Severity**: P2
**Component**: CommandExtractor (§6.3)
**Category**: False Negatives

The plan correctly notes that short flags with values are ambiguous: `-o output.txt` could mean flag `-o` with value `output.txt` or boolean flag `-o` plus positional arg `output.txt`. The plan treats all short flags as boolean.

However, this means `-c 'script body'` (as in `python -c '...'`, `bash -c '...'`) is treated as boolean flag `-c` with `'script body'` as a positional arg. While the inline detector can handle this by convention (take the first positional arg after `-c`), this creates a subtle issue:

```bash
python -Oc 'rm -rf /'
```

The `classifyArg` decomposition would produce flags `{"-O": "", "-c": ""}` and positional arg `'rm -rf /'`. But the actual Python behavior is that `-O` is a separate flag and `-c` takes the next argument. The decomposition happens to work here by coincidence.

But consider:

```bash
python -cO 'rm -rf /'
```

This would decompose to `{"-c": "", "-O": ""}`, and the positional arg is still the script body. But actual Python would interpret this as `-c` with value `O` (invalid), then `'rm -rf /'` as a script filename. The decomposition produces the wrong result but the inline detector would still trigger (it sees `-c` flag and a positional arg).

This is a minor concern since LLMs typically generate standard flag ordering, but it demonstrates that the flag decomposition can produce semantically incorrect results.

**Recommendation**: Document this as a known limitation. For inline detection specifically, consider matching against the raw argument list rather than decomposed flags to handle edge cases. This is P2 because LLM-generated commands almost always use canonical flag ordering.

---

### SC-P2.2: No Handling of Command Substitution Arguments in Dataflow

**Severity**: P2
**Component**: DataflowAnalyzer (§6.5)
**Category**: False Negatives

The dataflow analyzer tracks `$VAR` references, but command substitution in arguments is not addressed:

```bash
DIR=$(echo /); rm -rf $DIR
```

The plan's `Define` method receives the *text* of the value, but `$(echo /)` is a command substitution that would need to be evaluated to determine the actual value. The analyzer would store `$(echo /)` as the literal value of `DIR`, and `ResolveString` would substitute it literally.

This means `rm -rf $(echo /)` would have argument `$(echo /)`, not `/`. No pattern would match this as dangerous.

**Recommendation**: Document this as a known limitation. When a variable assignment's value contains command substitution (`$(...)` or `` `...` ``), the resolved value is indeterminate. Consider adding special handling: if a variable's value contains command substitution, treat it as unresolved (don't track it) rather than tracking the literal `$(...)` text, which could mislead matchers. Alternatively, flag commands that use unresolved variables as Indeterminate.

---

### SC-P2.3: Subshell Flattening Creates Asymmetric Correctness with Test Harness

**Severity**: P2
**Component**: DataflowAnalyzer (§6.5), Test Harness O2
**Category**: Correctness / Test Design

The plan documents subshell flattening as an intentional over-approximation. The test harness (O2: Bash Execution Comparison) calls out `(A=1); echo $A` as a case where "our over-approximation differs" from actual bash behavior.

However, the test harness only asks the developer to "document the intentional difference." There's no specification of *how many* false positives this creates in practice, or whether the over-approximation is bounded.

More concerning: the subshell flattening applies to pipelines too. In `cmd1 | cmd2`, variable assignments from `cmd1` are visible to `cmd2` in the analyzer but NOT in real bash (each pipeline stage is a subshell, except with `lastpipe` option). This means:

```bash
cat file | DANGER=true bash -c "rm -rf $DANGER"
```

The analyzer would resolve `$DANGER` from the pipeline's variable context, but in real bash `$DANGER` is unset in `bash -c` because it's in a separate process. This is conservative (over-approximation), but the test harness doesn't explicitly test this pipeline scoping case.

**Recommendation**: Add explicit pipeline scoping test cases to the test harness, documenting the over-approximation. Also add a counter-test: verify that the over-approximation doesn't create *unreasonably many* false positives for common pipeline patterns like `echo ... | grep ... | sort` (where no variable assignments propagate).

---

### SC-P2.4: Parser Pool `Reset()` Ordering with Deferred Put

**Severity**: P2
**Component**: BashParser (§6.2)
**Category**: Correctness / Concurrency

The parser pooling code has:

```go
p := bp.pool.Get().(*tsparser.Parser)
defer func() {
    p.Reset()
    bp.pool.Put(p)
}()
tree := p.ParseString(ctx, []byte(command))
```

If `ParseString` panics (e.g., due to a tree-sitter bug), the deferred function runs `p.Reset()` and then `bp.pool.Put(p)`. The parser is returned to the pool in a reset (but possibly corrupted) state.

The plan mentions that `recover()` is used in the extractor and inline detector, but the `Parse` method itself doesn't have a `recover()`. If tree-sitter's C-converted-to-Go code panics during parsing, the parser is returned to the pool in an unknown state. A subsequent `Get()` could receive a corrupted parser.

**Recommendation**: Add `recover()` in the BashParser.Parse method's defer. If a panic occurs, do NOT return the parser to the pool — let it be garbage collected. Create a fresh parser for the next call. Also, check whether the tree-sitter pure-Go port can panic at all, and if so, under what conditions.

---

### SC-P2.5: `WarnMatcherPanic` Defined but Not Used in Batch 1

**Severity**: P2
**Component**: Types (§6.1)
**Category**: Design Consistency

`WarnMatcherPanic` is defined in the warning codes but it's a concern for the matching pipeline (Batch 2), not for parsing (Batch 1). Including it in `types.go` within `internal/parse` is fine for shared type definitions, but the test harness doesn't test any matcher panic scenarios (since matchers are Batch 2).

More importantly, the `WarnMatcherPanic` recovery is described as happening in the eval pipeline, not in the parse package. But the plan says "The extractor and inline detector use `recover()` internally" (§7). This means there are two separate panic recovery mechanisms:
1. Parse-level recovery in extractor/inline detector
2. Pipeline-level recovery in eval (Batch 2)

The plan doesn't specify what warning code is emitted for parse-level panics. `WarnMatcherPanic` is specifically for matcher panics. A parse-level panic should probably produce `WarnPartialParse` or a new `WarnExtractorPanic` code.

**Recommendation**: Add a `WarnExtractorPanic` or `WarnParsePanic` warning code for panics caught during extraction or inline detection. `WarnMatcherPanic` should be reserved for Batch 2.

---

### SC-P2.6: Test Harness Property P2 Invariant May Be Violated by ERROR Nodes

**Severity**: P2
**Component**: Test Harness (§2, P2)
**Category**: Test Correctness

Property P2 states: "every `ExtractedCommand` in the result satisfies `cmd.Name != ""` (every extracted command has a name)."

However, tree-sitter's error recovery can produce `simple_command` / `command` nodes with missing or ERROR child nodes. If the `command_name` child is an ERROR node or is missing, the extracted command could have an empty `Name`.

The extractor walkNode table (§6.3) says ERROR nodes are skipped, but a `simple_command` node that *contains* an ERROR child (e.g., the command name is malformed) is not itself an ERROR node — it would be visited. The `extractSimpleCommand` function would find no `command_name` child and leave `cmd.Name` as `""`.

**Recommendation**: Either:
1. Have `extractSimpleCommand` skip commands with empty names (don't add them to the result), which preserves property P2, or
2. Relax property P2 to allow empty names for commands with ERROR children, and add filtering in the eval pipeline (Batch 2)

Option 1 is cleaner. Commands with no identifiable name can't be matched against any pattern anyway.

---

### SC-P3.1: `env` Command Wrapper Not Handled

**Severity**: P3
**Component**: CommandExtractor (§6.3)
**Category**: Coverage Gap

The `env` command is a POSIX utility that runs a command with a modified environment:

```bash
env RAILS_ENV=production rails db:reset
```

After extraction, this would produce a single command with name `env`, inline env `RAILS_ENV=production`, and args `["rails", "db:reset"]`. The actual command being run (`rails`) is buried in the argument list.

The plan doesn't mention `env` as a special case for extraction. Matchers looking for `rails db:reset` won't match because the command name is `env`.

**Recommendation**: Consider adding `env` (and `sudo`, `nice`, `nohup`, `time`, etc.) as "transparent prefix" commands — extract the actual command from their argument list. This could be a Batch 2 concern (matcher-level) rather than Batch 1, but the architecture should acknowledge it.

---

### SC-P3.2: No Specification for Handling `source` / `.` Built-in

**Severity**: P3
**Component**: CommandExtractor (§6.3)
**Category**: Coverage Gap

The `source` and `.` built-in commands execute a file in the current shell context:

```bash
source dangerous_script.sh
. ./setup_env.sh && rm -rf $CLEANUP_DIR
```

The plan doesn't address `source` — we can't analyze the sourced file's contents (we only have the command string). But `source` could set environment variables that are then used by subsequent commands. The dataflow analyzer won't track these.

**Recommendation**: Document as a known limitation. Consider emitting a warning when `source` or `.` is encountered, as subsequent variable references may be unresolvable.

---

### SC-P3.3: Test Harness Missing Negative Tests for Inline Detection

**Severity**: P3
**Component**: Test Harness (§3, E3)
**Category**: Test Coverage

The inline detection test examples (E3) focus on cases where detection *should* trigger. There are only two negative cases: `python script.py` and "no inline script." The test harness should include more false-positive traps:

- `python -c` with no following argument (edge case)
- `bash -c` where the argument is a variable reference (`bash -c "$CMD"`) — should this extract the literal `$CMD` or the resolved value?
- `ruby -e ''` (empty script body)
- `node --eval` (long flag form)
- Commands that have `-c` flags but aren't in the inline rules (e.g., `gcc -c file.c`)

**Recommendation**: Add negative and edge-case tests for inline detection to the test harness, particularly commands that have `-c`/`-e` flags but are NOT inline script triggers.

---

### SC-P3.4: `Normalize("/")` Returns Empty String

**Severity**: P3
**Component**: Normalizer (§6.4)
**Category**: Edge Case

The normalize function uses `strings.LastIndexByte(name, '/')` and returns `name[idx+1:]`. For the input `"/"`, this returns `""`. The test harness correctly lists this as an edge case, but the plan doesn't specify what should happen when the normalized command name is empty.

An empty command name would pass through extraction and potentially confuse matchers (or violate property P2 if the command name came from normalization of a path-only "command").

**Recommendation**: After normalization, if the name is empty (because the input was `/` or similar), either keep the original name or skip the command during extraction.

---

## Summary

| ID | Severity | Component | Summary |
|----|----------|-----------|---------|
| SC-P0.1 | P0 | DataflowAnalyzer | `&&` chain strong updates create false negatives when earlier values are dangerous |
| SC-P0.2 | P0 | InlineDetector | Flag-value ordering lost after classifyArg — inline script body may not be correctly identified |
| SC-P1.1 | P1 | DataflowAnalyzer | ResolveString expansion cap silently drops dangerous values — should be Indeterminate |
| SC-P1.2 | P1 | InlineDetector | `eval` command not in inline rules — major false negative vector |
| SC-P1.3 | P1 | InlineDetector | Heredoc detection algorithm unspecified, including pipe-to-bash pattern |
| SC-P1.4 | P1 | BashParser | Redundant hasErrorNodes walk; could disagree with extractor |
| SC-P2.1 | P2 | CommandExtractor | Short flag decomposition semantically incorrect for flags with values |
| SC-P2.2 | P2 | DataflowAnalyzer | Command substitution in variable assignments stored literally, not as indeterminate |
| SC-P2.3 | P2 | DataflowAnalyzer | Subshell/pipeline flattening lacks test coverage for false positive rate |
| SC-P2.4 | P2 | BashParser | Panicking parser returned to pool; needs recover() to discard corrupted parsers |
| SC-P2.5 | P2 | Types | WarnMatcherPanic in parse package; need separate WarnExtractorPanic |
| SC-P2.6 | P2 | Test Harness | Property P2 (`Name != ""`) may be violated by ERROR recovery |
| SC-P3.1 | P3 | CommandExtractor | `env`, `sudo`, `nice` etc. not handled as transparent prefixes |
| SC-P3.2 | P3 | CommandExtractor | `source`/`.` built-in not acknowledged as limitation |
| SC-P3.3 | P3 | Test Harness | Insufficient negative tests for inline detection |
| SC-P3.4 | P3 | Normalizer | `Normalize("/")` returns empty string, unhandled |
