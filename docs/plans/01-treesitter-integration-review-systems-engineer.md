# 01: Tree-sitter Integration — Review (Systems Engineer)

**Reviewed doc**: [01-treesitter-integration.md](./01-treesitter-integration.md)
**Test harness doc**: [01-treesitter-integration-test-harness.md](./01-treesitter-integration-test-harness.md)
**Reviewer focus**: Correctness, performance, API design, concurrency safety, error handling

---

## Findings

### P0 — Critical

#### SE-01-P0.1: Tree lifetime vs parser pool reuse creates use-after-free risk

**Location**: Plan §6.2 `BashParser.Parse()`, lines 372-396

The `Parse()` method gets a parser from the pool, calls `ParseString()`, then
returns the parser to the pool via `defer`. The returned `*Tree` wraps the
tree-sitter tree, but the caller is expected to use it later (pass it to
`Extract()`). However, the tree's internal data may reference memory managed by
the parser. If a second goroutine grabs the same parser from the pool and calls
`ParseString()` again before the first goroutine finishes using the tree, the
first goroutine's tree could be invalidated.

The plan has a comment saying "The caller must not retain references to
tree-sitter nodes beyond the lifetime of the returned Tree" — but doesn't
address the parser-pool race. The parser is returned to the pool *immediately*
after `ParseString()` returns, while the caller still holds a reference to the
tree.

**Risk**: Memory corruption, data races, or incorrect extraction results under
concurrent load. This is the exact scenario the stress test S1 exercises.

**Recommendation**: Either (a) the parser should NOT be returned to the pool
until the tree is no longer needed (i.e., after `Extract()` completes — meaning
`Parse` and `Extract` must be a single atomic operation from the pool's
perspective), or (b) verify in tree-sitter-go's implementation that `Tree` owns
its own copy of the data and is fully independent of the `Parser` after
`ParseString()` returns. If (b) is confirmed, document this invariant explicitly
with a reference to the tree-sitter-go source. If tree-sitter-go follows the C
tree-sitter convention, the tree IS independent after parse, but this must be
verified, not assumed.

---

#### SE-01-P0.2: `ExtractedCommand` missing dataflow-resolved args exposure

**Location**: Plan §6.3 `extractSimpleCommand()`, §6.5 Dataflow integration

The plan shows `resolveNodeText(child)` being called in argument extraction,
which substitutes variable references with tracked values. But
`ExtractedCommand` stores `Args []string` with already-resolved values. This
means downstream consumers (Batch 2 matchers) receive resolved arguments but
have **no way to distinguish** between a literal argument and a
dataflow-resolved one.

This matters for two reasons:

1. **Multi-valued resolution**: When a variable has multiple possible values
   (from `||` branches), `ResolveString()` returns multiple strings. The current
   design stores a single `Args []string` — where do the alternative expansions
   go? The plan doesn't specify how multi-valued resolution maps to a single
   `ExtractedCommand`. Does it produce one `ExtractedCommand` per expansion? Or
   does it pick one? This is unspecified.

2. **Matcher confidence**: A matcher seeing `rm -rf /` as a literal is
   `ConfidenceHigh`. The same match via dataflow resolution (`DIR=/; rm -rf
   $DIR`) should arguably be `ConfidenceMedium` because it depends on the
   analysis. Without distinguishing resolved vs literal args, matchers can't
   adjust confidence.

**Recommendation**: Specify the multi-value expansion strategy explicitly. The
cleanest approach: when `ResolveString()` returns N values for an argument,
produce N `ExtractedCommand` variants (one per expansion). Add a field like
`DataflowResolved bool` to `ExtractedCommand` so matchers can adjust confidence.
Cap the expansion at 16 per the existing limit, documenting that this produces
at most 16 `ExtractedCommand` variants per source command.

---

### P1 — High

#### SE-01-P1.1: Short flag decomposition is incorrect for flags that take values

**Location**: Plan §6.3 `classifyArg()`, lines 518-536

The plan acknowledges the ambiguity of `-o output.txt` but then says "short
flags are always treated as boolean, and the next token is a separate positional
arg." This is incorrect for important real-world patterns:

- `rm -rf /` — works correctly (both `-r` and `-f` are boolean)
- `git push -f origin main` — works correctly (`-f` is boolean)
- `psql -c "DROP TABLE users"` — **WRONG**: `-c` takes a value. Decomposing
  `-c` as a boolean flag means the SQL string becomes a positional arg. The
  inline detection (§6.6) expects to find `-c` as a flag and the next argument
  as the script body. But `classifyArg()` has already consumed the argument as
  a generic positional arg.
- `tar -czf archive.tar.gz /data` — `-f` takes a value. Decomposing `-czf`
  into `{-c, -z, -f}` all boolean loses the archive filename.

**Impact**: The inline script detection (§6.6) won't work for `bash -c "cmd"`,
`python -c "script"`, etc. because the flag `-c` is decomposed into a boolean
flag and the script body becomes a generic positional arg. The `inlineRule`
matching logic checks for the presence of the `-c` flag, but then needs to find
the script body — which is in `Args`, not associated with the flag.

**Recommendation**: The inline detector and matchers that care about flag values
should search `Args` for the value following a known flag. The `classifyArg()`
approach of treating all short flags as boolean is actually workable IF
downstream consumers know to look in `Args` for flag values. Document this
convention explicitly and ensure the inline detector implements it. Show the
concrete code path: `inlineRule` matches on flag presence → script body is
`Args[flagIndex+1]` or similar. Currently this linkage is not shown in the
inline detection code.

---

#### SE-01-P1.2: Combined short flag decomposition is unsafe for multi-byte runes

**Location**: Plan §6.3 `classifyArg()`, lines 527-530

```go
for _, c := range text[1:] {
    cmd.Flags["-"+string(c)] = ""
}
```

This iterates over runes, not bytes. For ASCII flags this is fine, but the
function doesn't guard against non-ASCII input. If a command has arguments like
`-ΩXYZ`, this would decompose into `{"-Ω", "-X", "-Y", "-Z"}` which is
nonsensical. More importantly, the `string(c)` conversion for multi-byte runes
could produce unexpected flag names that confuse matchers.

**Recommendation**: Add a guard: if `text[1:]` contains any non-ASCII byte, treat
the entire argument as a single flag (or a positional arg). Real shell flags
are always single ASCII characters.

---

#### SE-01-P1.3: `hasErrorNodes` DFS is called twice — redundant work

**Location**: Plan §6.2 `Parse()` line 388, §6.3 `Extract()` line 447

`Parse()` calls `hasErrorNodes(root)` to set warnings. Then `Extract()` calls
`hasErrorNodes(root)` again to set `ParseResult.HasError`. For a large AST,
this is a full DFS twice. While not a correctness issue, it's wasteful and the
plan specifically calls out this function.

**Recommendation**: Move the ERROR node detection into `Parse()` and store the
result on the `Tree` struct. `Extract()` reads it from there instead of
re-walking. This also simplifies the API — `Tree.HasError` is a property of
the parse, not the extraction.

---

#### SE-01-P1.4: Dataflow `&&` chain semantics are over-simplified

**Location**: Plan §6.5, scoping model table

The plan says for `&&` chains: "Left-side defs carry to right." But `&&` is
only half the story. Consider:

```bash
A=foo && B=bar || C=baz; echo $A $B $C
```

The plan's `||` handling says "may-alias, union of values." But the interaction
of `&&` and `||` in a `list` node creates more complex control flow. If
`A=foo && B=bar` succeeds, `C=baz` is skipped. If it fails (at A=foo), `C=baz`
runs but B is not set.

The plan doesn't specify how the extractor handles mixed `&&`/`||` chains with
more than two elements. The walk strategy table (§6.3) says `list` nodes are
"Walk children — these are `;`, `&&`, `||` chains" but doesn't specify how the
dataflow analyzer handles the interleaving of operators.

**Recommendation**: Specify the handling of mixed operator chains. The simplest
correct approach for a safety tool: treat ALL branches in a `list` node as
may-execute (union all definitions). This is over-approximate but safe. Document
the specific AST structure tree-sitter produces for `a && b || c` (is it
left-associative? does it nest `&&` inside `||`?) and show how the walker
handles it.

---

#### SE-01-P1.5: Inline detection has no mechanism for `eval` and `xargs`

**Location**: Plan §6.6 `InlineDetector`

The plan covers `bash -c`, `python -c`, heredocs, and multi-language shell
invocations. But it doesn't address:

- `eval "rm -rf /"` — eval executes its argument as a shell command
- `xargs rm -rf` — xargs feeds stdin as arguments to a command (the `rm -rf`
  IS the command, it just gets more arguments)
- `find . -exec rm -rf {} \;` — `-exec` runs a command for each found file
- `env RAILS_ENV=production rails db:reset` — `env` runs a command with
  modified environment

These are common patterns in real-world commands. `eval` in particular is
dangerous because it's a shell built-in that executes arbitrary strings.

**Recommendation**: Add `eval` to the inline detection rules as a special case
(its first positional argument is a bash script). Consider adding `xargs` and
`find -exec` as patterns that "pass through" to the underlying command. `env`
should be handled by the normalizer or extractor — strip `env` from the command
and promote its arguments to the inner command + inline env vars.

---

### P2 — Medium

#### SE-01-P2.1: `sync.Pool` pre-warming is mentioned but not implemented

**Location**: Plan §6.2 `NewBashParser()`, line 341

The comment says "pre-warmed pool" but the code only sets the `New` function.
`sync.Pool` doesn't pre-allocate — it creates on first `Get()`. For the first
concurrent burst, all goroutines will allocate new parsers simultaneously.

**Recommendation**: Either remove the "pre-warmed" claim, or actually pre-warm
by calling `pool.Put(bp.newParser())` once in the constructor.

---

#### SE-01-P2.2: Tree wrapper leaks tree-sitter types into internal API

**Location**: Plan §6.2 `Tree` struct, line 399

The `Tree` struct wraps `*tsTree` and exposes it to `Extract()` via
`tree.inner`. This creates a tight coupling — `extract.go` directly accesses
tree-sitter types through the wrapper. If tree-sitter-go changes its API,
both `bash.go` and `extract.go` need updating.

**Recommendation**: Consider having `Tree` expose a cursor or node-walking
interface rather than the raw tree-sitter tree. This would isolate tree-sitter
API surface to `bash.go` only. However, this may add overhead. If the tight
coupling is acceptable (both files are in the same package), document it as an
intentional choice rather than leaving it implicit.

---

#### SE-01-P2.3: `InlineDetector` parser pool proliferation

**Location**: Plan §6.6, §6.7

Each supported language gets its own `sync.Pool` of parsers. With 6 languages,
that's 6 pools plus the bash parser pool. Under concurrent load, each pool
could grow independently. Since inline script detection is rare (most commands
are simple bash), these pools will mostly sit empty but still consume memory
for the `sync.Pool` internal structures.

**Recommendation**: Use lazy initialization for language parsers (as mentioned
in §11). The pools should only be created when first needed. The plan mentions
this in §11 ("lazy grammar initialization") but the code in §6.7 shows eager
initialization in `SupportedLanguages`. Reconcile these — show the lazy path
in the detailed design.

---

#### SE-01-P2.4: No specification for how heredoc body text is passed to inline detection

**Location**: Plan §6.6 `detectHeredocs`, lines 751-757

The heredoc detection function signature takes `(node Node, cmd *ExtractedCommand, source string)` but the plan says heredocs are detected "structurally from the bash AST." The issue is that heredoc body nodes in tree-sitter bash are
siblings of the redirected command, not children. The plan doesn't show how
the extractor connects a `heredoc_redirect` on a `bash` command to the
`heredoc_body` that appears later in the AST.

Tree-sitter bash represents heredocs like:

```
(redirected_statement
  body: (command name: (command_name))
  redirect: (heredoc_redirect)
  (heredoc_body))
```

The `heredoc_body` is a child of the `redirected_statement`, not the
`heredoc_redirect`. The extractor's walk strategy table (§6.3) lists
`redirected_statement` as "Walk inner command, note redirections" but doesn't
specify heredoc body extraction.

**Recommendation**: Show the AST structure for heredocs as produced by
tree-sitter bash, and specify exactly how the extractor traverses from
`redirected_statement` to `heredoc_body` to extract the script text. Include a
concrete example in the E3 test set.

---

#### SE-01-P2.5: `ParseResult` conflates parse-level and extract-level concerns

**Location**: Plan §6.1 `ParseResult`, §6.3 `Extract()`

`ParseResult` has `HasError` (from parsing) and `Warnings` (accumulated during
extraction). The `Extract()` method calls `hasErrorNodes(root)` to set
`HasError`. But `Parse()` also returns warnings including `WarnPartialParse`.
There's duplication: the caller of `Parse` gets warnings, then `Extract` re-checks
and produces more warnings.

The question is: who assembles the final `ParseResult`? The plan shows `Parse()`
returning `(*Tree, []Warning)` and `Extract()` returning `ParseResult`. But
the `ParseResult` from `Extract()` doesn't include the warnings from `Parse()`.
The caller would need to merge them.

**Recommendation**: Either (a) have a single entry point `ParseAndExtract()`
that merges all warnings into one `ParseResult`, or (b) have `Extract()` accept
the parse warnings and merge them. The plan already mentions `ParseAndExtract`
in §6.6 line 804 (`id.bashParser.ParseAndExtract(...)`) but doesn't define it
in §6.2. Define it.

---

#### SE-01-P2.6: Implementation order risk — dataflow before extraction is complete

**Location**: Plan §12 Implementation Order

The plan says: step 5 is `CommandExtractor` without dataflow or inline, step 6
is `DataflowAnalyzer` integrated into extractor, step 7 is `InlineDetector`.

The risk is that step 5 establishes an extraction architecture (walk strategy,
context threading) that step 6 then needs to refactor to integrate dataflow.
Dataflow requires the walker to carry an `*DataflowAnalyzer` and call
`Define()` at assignment nodes — this changes the walk's control flow.

**Recommendation**: Consider implementing a minimal dataflow stub (just
`Define` + `Resolve` for simple sequential assignments) as part of step 5
rather than retrofitting. The `||` branch handling can come in step 6 as an
extension. This avoids restructuring the walker.

---

### P3 — Low

#### SE-01-P3.1: `ExtractedCommand.Flags` should use `[]Flag` not `map[string]string`

**Location**: Plan §6.1 `ExtractedCommand`

Using `map[string]string` for flags loses information:
- Duplicate flags (e.g., `-v -v -v` for verbosity) become a single entry
- Flag ordering is lost
- Empty string value for boolean flags is ambiguous with flags whose value is
  actually an empty string

For destructive pattern matching (checking flag presence), this works. But it's
a lossy representation that could cause issues for future matchers.

**Recommendation**: Consider `Flags []Flag` where `Flag struct { Name, Value
string }`. Matchers can still check presence with a helper function. This
preserves all information. However, if the architecture has already decided on
`map[string]string` and this was reviewed (SE-P3.1, SA-P2.4 in the architecture
review), this is fine as-is.

---

#### SE-01-P3.2: Missing specification for command substitution variable extraction

**Location**: Plan §6.3 walk strategy, §6.5 dataflow scoping

The plan says command substitutions `$(...)` are walked, and the walk strategy
table says "Walk children." But it doesn't specify how variables assigned inside
command substitutions interact with dataflow.

```bash
FILE=$(mktemp); rm -rf $FILE
```

Here `FILE` is assigned via command substitution. The dataflow analyzer can't
know the output of `mktemp`. Should it track `FILE` as "unknown value" or
ignore it? If ignored, `$FILE` in `rm -rf $FILE` won't be resolved, which is
the safe outcome. But the plan doesn't specify this.

**Recommendation**: Document that command substitution assignments produce
"unknown" values in the dataflow analyzer (the variable is tracked as defined
but with no known value). This prevents false matches when `$FILE` is left
unresolved.

---

#### SE-01-P3.3: Test harness P2 invariant is too strict

**Location**: Test harness §2, P2: Extract Output Consistency

The invariant states `cmd.Name != ""` for every extracted command. But the plan
§6.3 shows that `cmd.Name` is set from the `command_name` child of a
`simple_command` node. If tree-sitter produces a `simple_command` with no
`command_name` child (possible with error recovery), `cmd.Name` would be empty.

The property test should either (a) allow empty names and filter them out in
a post-processing step, or (b) have the extractor skip commands with empty names.
The plan doesn't specify which.

**Recommendation**: Have the extractor skip `simple_command` nodes that have no
`command_name` child (these are bare variable assignments like `FOO=bar` without
a command, which tree-sitter may parse as `simple_command` in some contexts).
Update the P2 invariant to reflect this: "every command in the output has a
non-empty name" is correct IF the extractor filters appropriately.

---

#### SE-01-P3.4: Benchmark targets in test harness are aspirational without baseline

**Location**: Test harness §6, B1-B6

The benchmark targets (< 100us for short commands, < 200us for medium, etc.)
have no basis — this is a greenfield project with no existing performance data.
Tree-sitter-go's parse performance may or may not meet these targets.

**Recommendation**: Remove specific targets for v1 and instead focus on
recording baselines. The plan already says "no specific targets required for v1"
in the exit criteria (§12), which contradicts the targets in §6. Reconcile by
removing §6 targets and marking them as "to be established after baseline."

---

#### SE-01-P3.5: Soak test memory growth assertion is brittle

**Location**: Test harness §7, S2

The test asserts `growth > 100*1024*1024` is a failure. But `HeapAlloc` after
`runtime.GC()` is an approximation. The comparison `m.HeapAlloc - startAlloc`
uses `HeapAlloc` which can fluctuate. Also, `startAlloc` is `TotalAlloc` (total
bytes allocated ever) while the growth check uses `HeapAlloc` (current heap) —
this is comparing apples to oranges.

**Recommendation**: Use `HeapInuse` for both measurements (current heap in use
after GC), or use `HeapAlloc` for both. Also, 100MB is an arbitrary threshold —
document the rationale (expected steady-state heap size, number of live parsers
in pool, etc.).

---

## Summary

| Priority | Count | Key Themes |
|----------|-------|------------|
| P0 | 2 | Parser pool lifetime safety; dataflow multi-value expansion unspecified |
| P1 | 5 | Flag-value association for inline detection; mixed `&&`/`||` chains; missing `eval`/`xargs` patterns |
| P2 | 6 | Pool pre-warming; API coupling; heredoc traversal; warning merging |
| P3 | 5 | Flag representation; command substitution; test calibration |

**Overall assessment**: The plan is thorough and well-structured. The dataflow
analysis and inline script detection are ambitious and correctly scoped. The
two P0 findings — parser pool lifetime and multi-value expansion — need
resolution before implementation because they affect the core data model. The
P1 findings around flag-value association for inline detection are
architecturally important: if `-c` flag values can't be found, the entire
inline detection feature doesn't work. The remaining findings are refinements
that can be addressed during implementation.
