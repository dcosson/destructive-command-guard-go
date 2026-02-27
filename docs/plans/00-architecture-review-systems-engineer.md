# Review: 00-architecture (systems-engineer)

- Source doc: `docs/plans/00-architecture.md`
- Also reviewed: `docs/plans/00-plan-index.md`
- Reviewed commit: 25782a7d6d34544c06f53c1292477ae765d32daf
- Reviewer: systems-engineer

## Findings

### P2 - sync.Pool for parsers requires explicit external scanner reset protocol

**Problem**

Section 10 (Extreme Optimization) states: "Pool parsers with `sync.Pool` since grammar data is shared and read-only." Section 7 (Concurrency) adds: "Parser creation is cheap -- the grammar data is shared and read-only."

Both statements are incomplete. The tree-sitter-go `Parser` struct holds mutable state beyond just the parser internals -- it holds an `externalScanner` field. For bash, this is a `scanners/bash.Scanner` with mutable state including a `heredocs` slice, `lastGlobParenDepth`, `extWasInDoubleQuote`, and `extSawOutsideQuote`. `Parser.Reset()` does NOT clear the external scanner. The scanner is only implicitly reset during the first lex operation of a fresh parse when `externalScannerDeserialize` is called with a zero subtree, which routes to `Scanner.Deserialize(nil)` which calls `s.reset()`.

I verified that this implicit reset does happen for every fresh `Parse()` call (the initial stack has a zero last-external-token), so pooling IS safe in practice. However, the architecture doc should document this invariant explicitly rather than hand-waving that "grammar data is shared and read-only." The external scanner is stateful and per-parser-instance -- it is neither shared nor read-only. Future changes to tree-sitter-go or the external scanner could break this assumption silently.

Additionally, `Parse()` allocates a fresh `SubtreeArena` and `Stack` on every call (lines 249-253 of internal/parser/parser.go), which means the primary allocation savings from pooling are the `Parser` struct, the `Lexer`, and the external scanner instance. The architecture should acknowledge this so developers can make informed optimization decisions. The benefit of pooling is real but modest.

**Required fix**

1. Add a note in the concurrency/performance sections that the external scanner carries mutable state and relies on implicit reset via `Deserialize(nil)` during the first lex of each parse.
2. Clarify that `Parse()` creates a fresh arena and stack per call, so pooling saves the parser struct, lexer, and external scanner allocation -- not the arena/stack allocations.
3. Consider whether the pooling wrapper should call `parser.Reset()` explicitly before returning to pool as a defensive measure, even though `Parse()` calls it internally. Document whichever decision is made.

---

### P2 - Module path mismatch for tree-sitter-go dependency

**Problem**

Section 3 (Layer 0) lists the dependency as:

```
github.com/treesitter-go/treesitter
github.com/treesitter-go/treesitter/grammars/bash
```

The actual tree-sitter-go `go.mod` declares the module as `github.com/treesitter-go/treesitter`, which matches. However, there is no `grammars/` directory in tree-sitter-go. The grammar data lives in `internal/testgrammars/bash/language.go` (a 52K+ line file) and the external scanner in `scanners/bash/scanner.go`. The `internal/` prefix means the bash grammar package is currently NOT importable by external modules.

The architecture doc acknowledges this in D6: "Requires a change to tree-sitter-go to move grammars from `internal/testgrammars/` to a public `grammars/` package." And the plan index notes this as Open Question #1.

This is not a design flaw per se, but the fact that Batch 1 depends on a cross-repo prerequisite that moves files from `internal/` to public should be called out more prominently. The architecture should specify:

- The exact public package paths expected (e.g., `github.com/treesitter-go/treesitter/grammars/bash` for the language data AND what about the scanner at `scanners/bash`?)
- Whether the scanner package also needs to be restructured (currently `scanners/bash` is already public, but the language data that it pairs with is internal).
- What the fallback is if tree-sitter-go restructuring is delayed.

**Required fix**

1. In Section 3 or D6, specify the complete set of import paths that DCG will need from tree-sitter-go, including both language data and external scanner packages.
2. Add a note in the plan index that Batch 1 has a hard external dependency on tree-sitter-go restructuring and cannot proceed to completion without it.

---

### P1 - Evaluate() returns *Result (pointer) but should return Result (value)

**Problem**

Section 3 defines the API as:

```go
func Evaluate(command string, opts ...Option) *Result
```

`Result` is a small struct with 4 fields (Decision int, Assessment pointer, Matches slice, Command string). Returning `*Result` has several drawbacks:

1. **Nil ambiguity**: A `*Result` return forces every caller to nil-check. What does `nil` mean? Parse failure? Empty command? The doc says fail-open returns Allow, so there should always be a valid Result. If Evaluate never returns nil, the pointer is misleading. If it can return nil, the contract must specify when.

2. **Unnecessary heap allocation**: Returning by value lets the compiler stack-allocate in many cases. A `Result` with an int, a pointer, a slice header, and a string header is 56 bytes on amd64 -- well within value-return efficiency.

3. **API friction**: Value returns compose better (`if guard.Evaluate(cmd).Decision == guard.Allow`). Pointer returns require intermediate variables or risk nil dereference.

The `Assessment` field is already a pointer (`*Assessment`), which correctly signals its optionality (nil when no patterns match). The outer `Result` does not need the same treatment.

**Required fix**

Change the signature to `func Evaluate(command string, opts ...Option) Result`. If there is ever a case where no result can be produced (catastrophic internal failure), use a zero-value Result with `Decision: Allow` to maintain the fail-open invariant. Document that the zero value of Result is a valid "nothing found, allow" result.

---

### P2 - CommandMatcher interface may be too narrow for SQL/argument-content patterns

**Problem**

Section 3 defines `CommandMatcher` as:

```go
type CommandMatcher interface {
    Match(cmd ExtractedCommand) bool
}
```

D1 acknowledges: "Some patterns may still need raw-text matching for edge cases (e.g., SQL statements passed as arguments)."

For database packs, the primary danger is in the argument content: `psql -c "DROP TABLE users"`. The `ExtractedCommand` has `Args []string` which would contain `"DROP TABLE users"`. But the matcher needs to understand that this is SQL content within an argument, not just an arbitrary string.

The shaping doc lists patterns like `DROP TABLE`, `DROP DATABASE`, `TRUNCATE`, `DELETE FROM (no WHERE)`. These require regex or at least substring matching on argument content, not just structural matching on command name and flags. The interface is flexible enough (any implementation can inspect `Args`), but the architecture doc should explicitly describe the expected matcher implementation patterns:

- **Structural matchers**: command name + flag presence (e.g., `git push --force`)
- **Argument content matchers**: command name + regex/substring on argument values (e.g., `psql -c` with arg containing `DROP TABLE`)
- **Composite matchers**: AND/OR/NOT composition of the above

Without this, each pack implementor will reinvent argument matching differently, leading to inconsistent detection quality across packs.

**Required fix**

1. Add a subsection under the CommandMatcher description that enumerates the expected built-in matcher implementations: structural (flag/name), argument-content (regex on args), and composite.
2. Define the built-in matcher library that pack authors should compose from, rather than implementing `CommandMatcher` from scratch per pattern.

---

### P1 - Dataflow analysis scope and soundness guarantees are under-specified

**Problem**

Section 8 describes intraprocedural dataflow analysis for variable tracking. The description is clear about what it handles (linear sequences, `&&`/`||`) and what it does not (if/then/else, loops, functions). However, several cases are not addressed:

1. **Subshell scoping**: `(export FOO=bar); cmd` -- the export in a subshell does not affect the parent shell's environment. Does the dataflow analysis respect subshell boundaries? The doc does not say.

2. **Pipeline variable scoping**: `FOO=bar | cmd` -- in bash, each pipeline component runs in a subshell. Variables assigned in one pipeline stage are not visible in subsequent stages (except in the last stage with `lastpipe` option). The doc says "Walk the bash AST in execution order (respecting `&&`, `||`, `;`, pipes)" but does not clarify whether pipe-scoped variables are handled correctly.

3. **Conditional definitions**: `test -f prod.conf && export ENV=production; rails db:reset` -- if the `&&` branch is not taken, `ENV` is never set. The dataflow analysis would over-approximate by assuming the assignment happens. This is the right choice for a safety tool (over-approximation is conservative), but it should be explicitly documented as a design choice.

4. **Soundness claim**: The doc says the analysis covers ">95% of real-world LLM-generated commands" but provides no evidence for this claim. This percentage seems reasonable but is stated as fact.

**Required fix**

1. Document the scoping model: are subshells and pipeline stages treated as separate scopes or flattened? Recommend flattening (over-approximation) for safety with an explicit note that this is conservative.
2. Document the treatment of conditional branches (`&&`/`||`): state that both branches are merged (over-approximate) as a deliberate safety choice.
3. Either remove the ">95%" claim or mark it as a hypothesis to be validated during Batch 5 testing.

---

### P3 - Flags representation as map[string]string loses ordering and duplicates

**Problem**

`ExtractedCommand.Flags` is typed as `map[string]string`. This loses information:

1. **Duplicate flags**: Some commands accept repeated flags (e.g., `-v -v -v` for verbosity). A map cannot represent this.
2. **Flag ordering**: Flag order can matter for some commands. Maps in Go have non-deterministic iteration order.
3. **Combined short flags**: `rm -rf` is typically parsed as two flags (`-r`, `-f`). The doc does not specify how combined short flags are decomposed.

For the purpose of destructive command detection, these limitations are unlikely to cause real problems -- we are looking for presence of specific flags, not their order or repetition count. But the type choice should be documented as intentional with these trade-offs acknowledged.

**Required fix**

Add a brief note to the `ExtractedCommand` type documentation acknowledging that the map representation loses ordering and duplicate information, and explaining why this is acceptable for the use case (presence-based matching, not order-sensitive).

---

### P2 - Keyword pre-filter false negative risk with aliased/path-prefixed commands

**Problem**

Section 6 (D3) says the pre-filter uses `strings.Contains` to check for pack keywords like "git", "rm", "docker". The pipeline (Section 3, Layer 2) says normalization happens AFTER parsing. This means the pre-filter operates on the raw command string before normalization.

This is mostly fine because path-prefixed commands like `/usr/bin/git push --force` still contain the substring "git". However, there are edge cases:

1. **Busybox-style invocations**: Commands invoked through a multi-call binary (e.g., `busybox rm -rf /`) would pass the pre-filter for "rm" since "rm" appears in the raw string. Fine.

2. **Unusual aliases in scripts**: `alias g=git; g push --force` would NOT contain "git" and would skip the pre-filter. The doc says this is a mistake preventer for LLM-generated commands, and LLMs rarely generate alias-based evasion. But the pre-filter's false negative behavior should be documented.

3. **The pre-filter collects keywords from all enabled packs.** With 21 packs and ~50 keywords, a command like `echo "just a test"` contains "test" which might match a keyword if any pack uses it. The pre-filter would pass this to the parser even though it is harmless. This is not a false negative but affects the >90% skip-rate claim. Keyword selection quality matters.

**Required fix**

1. Document that the pre-filter may have false negatives for aliased commands and that this is acceptable given the threat model.
2. Add guidance in the pack authorship section that keywords should be chosen to be specific enough to avoid excessive pre-filter pass-through, and that keyword effectiveness should be measured as part of the benchmark suite.

---

### P3 - Policy interface lacks context parameter

**Problem**

The Policy interface is:

```go
type Policy interface {
    Decide(Assessment) Decision
}
```

This is clean and minimal. However, it prevents custom policies from considering context beyond severity and confidence. For example, a caller might want a policy that:

- Allows `High` severity commands if they are in specific packs (e.g., allow high-severity git commands but deny high-severity database commands)
- Makes different decisions based on the matched rule name

The current design forces all such logic into pre-evaluation (via allowlists/blocklists) rather than post-assessment policy. This is a deliberate simplicity choice and may be the right call. But if the Policy interface is ever extended, adding parameters is a breaking change.

**Required fix**

No change required -- this is a design trade-off worth noting. Consider documenting in the Policy interface section that the Assessment-only input is intentional to keep policies simple, and that per-pack/per-rule overrides should be handled via allowlists/blocklists or by composing multiple Evaluate calls with different pack sets.

---

### P1 - Plan index: Batch 4 dependency on 03a is too loose

**Problem**

The plan index states that Batch 4 (Public API & CLI) depends on `02, 03a`. The note says: "This doesn't strictly depend on ALL packs being done (03b-03e) -- it can proceed once the framework (02) and core packs (03a) are ready."

This is correct for the library API. However, the CLI's `dcgo packs` command (list all packs) and `dcgo test` command (evaluate with all packs) will produce incomplete output until all packs are implemented. If Batch 4 is implemented and tested while 03b-03e are still in progress, the CLI integration tests will either:

1. Only test with core packs (incomplete coverage)
2. Need to be re-run/extended after 03b-03e complete (work duplication)

More importantly, the hook mode binary shipped from Batch 4 would only protect against git and filesystem commands, missing databases, cloud, infrastructure, containers, and Kubernetes. If anyone starts using it before Batch 5, they get a false sense of coverage.

**Required fix**

1. Add a note to Batch 4 that the CLI should be designed to work with whatever packs are registered (it already would via the registry pattern), but integration tests in Batch 4 should only test with core packs and should be tagged/structured so Batch 5 can extend them with full-pack coverage.
2. Consider whether the `dcgo packs` list output should indicate pack availability status, or whether this is overengineering.

---

### P2 - No error type hierarchy or structured errors defined

**Problem**

The architecture doc defines the Result type with Decision/Assessment/Matches but does not define any error types. The doc says "Fail-open on parse errors" but does not specify how callers learn that a fail-open occurred. Questions:

1. If tree-sitter fails to parse, does the Result include any indication that parsing failed? Or is it indistinguishable from a genuinely safe command?
2. If a pack's CommandMatcher panics (bug), does Evaluate recover and fail-open? Is the panic logged?
3. If the command string is empty or nil, what happens?

For a library API, callers may want observability into fail-open events for monitoring, even if the decision is Allow. This is important for the h2 integration where operators want to know "how often are we failing open?"

**Required fix**

1. Add a `Warnings []string` or `Flags` field to Result that can indicate conditions like `ParseFailed`, `TimeoutExceeded`, `MatcherPanic`. These are informational, not decision-changing.
2. Document the behavior for empty/nil command strings (presumably Allow with no matches).
3. Specify whether Evaluate uses `recover()` to catch panics from matchers.

---

### P3 - Inline script detection recursion depth is unbounded

**Problem**

The inline script detection flow (Section 4, Data Flow: Inline Script Detection) shows recursive evaluation: `python -c "os.system('rm -rf /')"` extracts `rm -rf /` and recursively evaluates it through the main pipeline.

What about `bash -c "bash -c \"bash -c ...\""` or `python -c "os.system('python -c ...')"` -- nested inline scripts? Is there a recursion depth limit?

For the threat model (LLM mistake prevention), deeply nested inline scripts are unlikely. But a fuzz test could easily generate one, and without a depth limit, this could stack overflow.

**Required fix**

Add a maximum inline script recursion depth (e.g., 3 levels) to the inline script detection section. Document that commands exceeding this depth are treated as opaque (fail-open on the nested portion).

---

### P2 - Plan index missing: mutation testing and golden file corpus plans

**Problem**

Section 9 (URP) describes four significant testing artifacts:

1. Mutation testing harness for pattern packs
2. Golden file corpus of ~500+ commands
3. Fuzz testing
4. Grammar-derived coverage analysis

These are substantial engineering efforts. The plan index places all of this in Batch 5 (`05-testing-and-benchmarks`), which is described as: "Benchmark suite, comparison tests, fuzz testing, end-to-end tests, performance profiling and optimization."

The mutation testing harness and golden file corpus are not mentioned in the Batch 5 description. These are distinct work items that deserve explicit mention in the plan index. The grammar-derived coverage analysis is also a non-trivial tool to build. Lumping all of this into one plan risks under-estimating the scope of Batch 5.

**Required fix**

1. Expand the Batch 5 description in the plan index to explicitly list: mutation testing harness, golden file corpus, fuzz testing, grammar-derived coverage analysis, comparison testing, and benchmarks.
2. Consider whether Batch 5 should be split into 5a (benchmarks + profiling) and 5b (URP testing harnesses), since they serve different purposes and could be parallelized.

---

### P3 - Allowlist/blocklist matching semantics not specified

**Problem**

The API defines `WithAllowlist(patterns ...string)` and `WithBlocklist(patterns ...string)` but does not specify what "patterns" means. Are these:

- Exact command string matches?
- Glob patterns against the full command?
- Glob patterns against just the command name?
- Regular expressions?
- Prefix matches?

The pipeline description says allowlist/blocklist check is step 1, before parsing. This means matching operates on the raw command string, not the extracted command. But the semantics of the match are undefined.

**Required fix**

Specify the matching semantics for allowlist and blocklist patterns (glob, regex, or prefix), and whether they match against the full raw command string or just a normalized command name. Also specify precedence: what happens if a command matches both an allowlist and a blocklist entry?

---

## Summary

12 findings: 0 P0, 3 P1, 5 P2, 4 P3

**Verdict**: Approved with revisions

The architecture is well-structured and thoughtfully designed. The tree-sitter-first approach is sound and the assessment/policy separation is good API design. The layering and package structure are clean with no circular dependency risks.

The three P1 findings that should be addressed before implementation begins:

1. **Evaluate return type**: Returning `*Result` introduces nil ambiguity and unnecessary heap allocation. Switch to value return.
2. **Dataflow analysis scoping**: The subshell/pipeline/conditional scoping behavior needs to be specified before implementation, as it affects correctness of the analysis.
3. **Batch 4 dependency gap**: The plan index should clarify how incomplete pack coverage during Batch 4 development is handled for testing and for any early users of the hook binary.

The P2 findings are improvements that strengthen the design but are not blocking. Most can be addressed during the sub-plan drafting phase for the relevant batches.
