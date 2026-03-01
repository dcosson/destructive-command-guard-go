# 03a: Core Packs — Security & Correctness Review

**Reviewer**: dcg-alt-reviewer (independent review)
**Plan**: [03a-packs-core.md](./03a-packs-core.md)
**Test Harness**: [03a-packs-core-test-harness.md](./03a-packs-core-test-harness.md)
**Date**: 2026-02-28

---

## Summary

Reviewed both the core packs plan and test harness with a focus on:
severity/confidence assignments, safe pattern bypass edge cases, rm -rf
robustness against path tricks, git force push variant coverage, and pack
authoring template gaps for 03b-03e.

**Findings**: 16 total — 2 P0, 4 P1, 6 P2, 4 P3

The most critical findings are two git push bypass vectors where dangerous
operations (remote branch deletion via colon refspec, force push via +refspec)
pass through the safe pattern completely undetected. The rm -rf patterns are
reasonably robust but miss path traversal cases at the Critical severity tier.
The pack authoring template has gaps around ExtractedCommand fields that will
cause confusion in 03b-03e.

---

## P0: Critical Findings

### CP-P0.1: git push colon refspec deletes remote branch, bypasses all patterns

**Location**: Plan §5.1, git-push-safe (S1)

**Issue**: `git push origin :branch-name` is the refspec deletion syntax — it
pushes an empty ref to the remote, effectively deleting the remote branch. This
is functionally equivalent to `git push origin --delete branch-name`.

However, the git-push-safe pattern only excludes `--force`, `-f`, `--mirror`,
`--delete`, and `-d`. The colon refspec form has none of these flags.

**Trace**:
1. `git push origin :main` enters pack evaluation
2. git-push-safe checks: Name("git") ✓, ArgAt(0, "push") ✓,
   Not(Or(--force, -f, --mirror, --delete, -d)) — none present, so ✓
3. Safe pattern matches → short-circuit, skip all destructive patterns
4. **Result: Allow** — remote branch deletion goes completely undetected

**Impact**: False negative. A dangerous remote branch deletion command is
marked as safe. No severity assessment, no warning, no Ask prompt.

**Recommendation**: Add `ArgPrefix(":")` or `ArgContent("^:")` to the safe
pattern's exclusion list. This requires care to not false-positive on normal
refspecs like `local:remote` — the pattern should specifically match args
that START with `:` (empty left side = deletion). Alternatively, add a new
destructive pattern `git-push-refspec-delete` that matches `ArgContent("^:")`
in conjunction with `git push`.

### CP-P0.2: git push +refspec force pushes individual refs, bypasses all patterns

**Location**: Plan §5.1, git-push-safe (S1), git-push-force (D1)

**Issue**: `git push origin +main:main` uses the `+` prefix in the refspec to
force-push a specific ref. This is equivalent to `git push --force origin main`
for that particular ref, but doesn't use the `--force` or `-f` flags.

Like CP-P0.1, this bypasses git-push-safe because no excluded flags are present.

**Trace**:
1. `git push origin +main:main` enters pack evaluation
2. git-push-safe: Name("git") ✓, ArgAt(0, "push") ✓,
   Not(Or(--force, -f, --mirror, --delete, -d)) — none present, ✓
3. Safe pattern matches → short-circuit
4. **Result: Allow** — force push goes completely undetected

**Impact**: False negative. An individual ref force push bypasses all
detection. While less catastrophic than `--force` (which affects all pushed
refs), it can still overwrite remote history for the targeted ref.

**Recommendation**: Add `ArgContent("^\\+")` (arg starting with `+`) to the
safe pattern's exclusion list. Also add a destructive pattern
`git-push-force-refspec` that matches args starting with `+` in conjunction
with `git push`. Consider ConfidenceMedium since `+` at arg start could
theoretically appear in other contexts (though it's very rare outside refspecs).

---

## P1: High Findings

### CP-P1.1: rm -rf path traversal bypasses Critical severity classification

**Location**: Plan §5.2, rm-rf-root (D1)

**Issue**: The rm-rf-root pattern checks for literal `/`, `/*`, or `/..` as
arguments. Path traversal sequences that resolve to root at runtime are not
detected at Critical severity:

- `rm -rf /tmp/../..` — resolves to `/` on the filesystem
- `rm -rf /home/../../..` — resolves to `/`
- `rm -rf //` — double slash, equivalent to `/` on most systems
- `rm -rf /./` — self-reference in root

These commands ARE caught by rm-recursive-force (D3, High severity), so they
are not false negatives. But they are misclassified as High instead of Critical.

**Impact**: Severity underclassification. Under StrictPolicy, both High and
Critical map to Deny, so no operational difference. Under InteractivePolicy,
both map to Deny for ConfidenceHigh, so also no difference. The practical
impact is limited to observability — the severity label doesn't reflect the
true risk.

**Recommendation**: Either:
(a) Add more path traversal patterns to rm-rf-root's Arg match: `/./`,
    `//`, and an ArgContent matcher for `^/(\.\./)+\.?\.$` style patterns.
(b) Accept the gap and document it — High severity is still caught, and
    path canonicalization is unreliable at static analysis time. Note in the
    plan that rm-rf-root targets common literal forms and rm-recursive-force
    is the catch-all.

Option (b) seems more pragmatic. The risk is low since the command is still
flagged.

### CP-P1.2: git rebase --abort / --continue are false positives

**Location**: Plan §5.1, git-rebase (D5), acknowledged in OQ1

**Issue**: `git rebase --abort` is a recovery command that cancels a rebase
and restores the branch to its pre-rebase state. `git rebase --continue`
continues an in-progress rebase. Neither is destructive — both are part of
the rebase workflow's safety mechanisms.

The current pattern matches ALL `git rebase` subcommands at High severity with
ConfidenceMedium. The test harness (E1) explicitly tests `git rebase --abort`
and expects Deny/High.

**Impact**: False positive. Users running `git rebase --abort` (trying to
RECOVER from a bad rebase) will be denied. This is particularly bad UX because
the user is already in a problematic state and the tool is blocking the
recovery action.

**Recommendation**: Add a safe pattern for rebase recovery commands:
```go
{
    Name: "git-rebase-recovery",
    Match: packs.And(
        packs.Name("git"),
        packs.ArgAt(0, "rebase"),
        packs.Or(
            packs.Flags("--abort"),
            packs.Flags("--continue"),
            packs.Flags("--skip"),
        ),
    ),
}
```

### CP-P1.3: Missing git reflog expire and git gc --prune patterns

**Location**: Plan §5.1 (not present)

**Issue**: Two git commands that can make previously recoverable operations
permanently irreversible:

- `git reflog expire --expire=now --all` — removes reflog entries, eliminating
  the safety net for recovering from `git reset --hard` and `git rebase`
- `git gc --prune=now` — permanently removes unreachable objects, making
  recovery from reflog impossible

Without reflog entries and reachable objects, `git reset --hard` becomes
truly irreversible. These commands effectively escalate the severity of
other git operations retroactively.

**Impact**: False negative. These commands go completely undetected. While
rarely generated by LLMs currently, they ARE valid git commands and could
appear in cleanup scripts or agent-generated workflows.

**Recommendation**: Add two destructive patterns:
- `git-reflog-expire`: Match `git reflog expire`, Severity High,
  ConfidenceHigh
- `git-gc-prune`: Match `git gc` with `--prune` flag, Severity Medium,
  ConfidenceMedium (since `git gc` without `--prune=now` is generally safe)

### CP-P1.4: Pack authoring template omits RawArgs, DataflowResolved, InlineEnv

**Location**: Plan §4 (Pack Authoring Guide), §4.2 (Template), §4.7 (Test Template)

**Issue**: The pack authoring guide (§4) defines how to write packs using
the builder DSL. However, it only references `Name`, `Args`, `Flags` in the
`cmd()` test helper and the builder functions (`Name()`, `Flags()`, `ArgAt()`,
`ArgPrefix()`, `ArgContent()`). It does not mention:

- `RawArgs` — pre-normalization args (added in plan 01 review incorporation)
- `DataflowResolved` — whether variable references were resolved
- `InlineEnv` — environment variables set inline (`FOO=bar command`)

For plans 03b-03e, this matters:
- Database packs (03b) may need to match on raw SQL in args that contain
  variable references: `psql -c "DROP TABLE $TABLE"`. If DataflowResolved is
  false, the variable might not be resolved, and ArgContent won't match the
  expected value.
- Infrastructure packs (03c) may need to check InlineEnv for things like
  `AWS_PROFILE=production terraform destroy`.
- The template doesn't explain WHEN pack authors should care about
  DataflowResolved — e.g., should patterns be written to match both resolved
  and unresolved forms?

**Impact**: Template gap. Authors of 03b-03e will build patterns using only
Args/Flags and may miss cases where dataflow resolution affects matching.

**Recommendation**: Add a §4.10 "Advanced Matching" section covering:
1. RawArgs vs Args — when to use each
2. DataflowResolved — how to handle unresolved variables (consider adding a
   matcher `packs.DataflowResolved()` that checks the field)
3. InlineEnv — how to match inline environment variable assignments
4. A decision flowchart: "If your pattern matches argument CONTENT (not just
   structure), consider whether variable resolution affects it."

---

## P2: Medium Findings

### CP-P2.1: chmod 000 excluded from safe pattern but has no destructive pattern

**Location**: Plan §5.2, chmod-single-safe (S2)

**Issue**: The chmod-single-safe pattern excludes `Arg("777")` and
`Arg("000")`. This means `chmod 000 file` does NOT match the safe pattern.
However, there's no destructive pattern for chmod 000. The command falls
through as "no match" and is Allowed.

`chmod 000` makes a file completely inaccessible (no read, write, or execute
for anyone). While recoverable, it's arguably at least as concerning as
`chmod 777` which IS flagged.

**Recommendation**: Either add a `chmod-000` destructive pattern at Medium
severity, or remove `000` from the safe pattern exclusion to let it pass as
safe (since it's recoverable with `chmod 644`).

### CP-P2.2: Missing git restore patterns

**Location**: Plan §5.1, acknowledged in OQ3

**Issue**: `git restore` is the modern replacement for `git checkout --` for
file restoration. Patterns not covered:
- `git restore --worktree file` — discards working tree changes (= `checkout -- file`)
- `git restore --source HEAD~3 --worktree .` — restores all files to older state
- `git restore --staged --worktree .` — discards both staged and unstaged changes

These are equivalent in destructiveness to the `git checkout --` variants
but not caught by any pattern.

**Recommendation**: Add corresponding patterns in git pack, mirroring the
checkout discard patterns.

### CP-P2.3: Missing git filter-branch / git filter-repo patterns

**Location**: Plan §5.1 (not present)

**Issue**: `git filter-branch` and `git filter-repo` rewrite entire
repository history. They're more destructive than `git rebase` since they
can modify ALL commits, not just recent ones. Not covered.

**Recommendation**: Add `git-filter-branch` pattern, Severity High,
ConfidenceMedium.

### CP-P2.4: Missing truncate command in filesystem pack

**Location**: Plan §5.2 (not present)

**Issue**: `truncate -s 0 file` empties a file's contents completely. This
is destructive (irreversible data loss) but not covered. Also: `> file` and
`: > file` redirect forms that truncate files, though these may be harder to
detect structurally.

**Recommendation**: Add `truncate` to filesystem pack keywords and add a
destructive pattern for `truncate -s 0` or `truncate --size 0`. Severity
Medium, ConfidenceHigh.

### CP-P2.5: SEC1 evasion tests use raw command strings, not ExtractedCommands

**Location**: Test harness §SEC1

**Issue**: The SEC1 pattern evasion tests pass raw command strings through
the full pipeline (`pipeline.Run(ctx, tt.command, strictCfg)`). This means
evasion detection depends on the parser (plan 01) and normalizer working
correctly — the test doesn't isolate pack pattern behavior.

This isn't wrong, but it means SEC1 is an integration test, not a unit test
for pack robustness. If the parser has a bug that causes bad extraction, SEC1
won't reveal a pack-level pattern gap.

**Recommendation**: Add pack-level evasion tests that construct
ExtractedCommand values directly, testing that the patterns themselves handle
edge cases. The pipeline-level tests in SEC1 are still valuable as integration
tests.

### CP-P2.6: Golden file decision expectations depend on policy choice

**Location**: Plan §6, test harness §E1/E2

**Issue**: Golden file entries specify expected `decision` (Allow/Deny/Ask)
but the decision depends on which policy is active. The plan doesn't specify
which policy the golden file tests use. For example:

- D11 `git checkout -- file` → decision: Allow (with severity: Low). This
  is only true under PermissivePolicy (Low severity → Allow). Under
  StrictPolicy, Low → Ask.
- D6 `git clean -f` → decision: Ask (Medium). Under StrictPolicy, Medium → Deny.

The golden file format needs to either specify the policy or only assert on
severity/confidence (policy-independent properties).

**Recommendation**: Specify the policy in the golden file format header
(e.g., `policy: interactive`) or update the golden file test runner to assert
on severity and confidence only, with decision being derived from the policy.

---

## P3: Low Findings

### CP-P3.1: Filesystem pack keyword "mv" causes unnecessary pre-filter passes

**Location**: Plan §5.2, keyword list

**Issue**: The keyword `"mv"` is very common in everyday commands. Every
`mv file.txt dir/` command will pass the Aho-Corasick pre-filter and trigger
full parsing + pack evaluation. The only mv destructive pattern is
mv-to-devnull, which is relatively uncommon. The vast majority of mv commands
will pass through the entire pack evaluation chain only to match the safe
pattern or no pattern at all.

**Impact**: Performance only, not correctness. With word-boundary matching
in the pre-filter, the false positive rate from "mv" should be manageable.

### CP-P3.2: Test harness E2 comment "higher priority" for chmod -R 777 is misleading

**Location**: Test harness §E2, line 255

**Issue**: The entry `chmod -R 777 ./app → Ask/Medium (chmod-recursive,
higher priority)` says "higher priority" but both chmod-recursive and
chmod-777 have the same severity (Medium). The actual selection between
co-equal matches is implementation-dependent (evaluation order, first match,
etc.) not priority-based.

**Recommendation**: Rephrase to "both chmod-recursive and chmod-777 match;
chmod-recursive listed as primary match due to evaluation order."

### CP-P3.3: git checkout -- . only matches literal "." pathspec

**Location**: Plan §5.1, git-checkout-discard-all (D4)

**Issue**: The pattern checks `packs.Arg(".")` which only matches the
literal `.` argument. Other pathspec forms that mean "all files":
- `git checkout -- '*'` (quoted glob)
- `git checkout -- :/` (magic pathspec for repo root)

These are unlikely to be generated by LLMs but are valid git syntax.

### CP-P3.4: Comparison oracle O1 lacks structured divergence schema

**Location**: Test harness §O1

**Issue**: The comparison oracle logs divergences with `t.Logf` but doesn't
have a structured categorization system. Divergences are categorized in
comments as "intentional improvement," "intentional divergence," or "bug" but
this categorization isn't machine-readable or tracked over time.

**Recommendation**: Add a divergence tracking file (e.g., JSON or YAML) that
records each known divergence with its category, justification, and the
relevant commands. This enables tracking divergence count over time.

---

## Cross-Cutting Observations

### Severity/Confidence Assignments

The severity guidelines (§4.6) are sound. The actual assignments are generally
well-calibrated:
- Critical for rm -rf / and mkfs: correct (system-wide irreversible)
- High for rm -rf, git push --force, git reset --hard: correct (scoped but
  irreversible)
- Medium for git clean, git branch -D, chmod -R: correct (recoverable or
  scoped)
- Low for git checkout -- file: correct (single file, recoverable)

One exception: git rebase at High/ConfidenceMedium is appropriate for the
destructive case but overbroad due to --abort (CP-P1.2).

### Safe Pattern Edge Cases

The safe-before-destructive ordering is well-designed. Safe patterns correctly
use `Not()` composition to exclude destructive variants. The pattern
interaction matrices (§5.1.1, §5.2.2) are thorough and match the actual
pattern definitions.

The main gaps are in git push (CP-P0.1, CP-P0.2) where refspec syntax
bypasses flag-based detection. All other safe patterns are appropriately
narrow.

### rm -rf Robustness

The rm patterns are reasonably robust for common cases. The main gap is path
traversal (CP-P1.1) where runtime-equivalent paths to root aren't detected at
the correct severity tier. The catch-all rm-recursive-force (High) ensures no
false negatives.

### Git Force Push Coverage

Missing the +refspec force push syntax (CP-P0.2). The --force and -f flag
forms are covered. --force-with-lease is correctly handled as safe (with the
critical check that --force + --force-with-lease → --force wins).

### Pack Authoring Template

The template (§4) is well-structured and covers the common case. The main gap
is around advanced ExtractedCommand fields (CP-P1.4) that 03b-03e authors will
need. The file structure, naming conventions, import flow, keyword rules, and
testing patterns are all solid and should transfer well to subsequent packs.
