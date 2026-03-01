# 03a: Core Packs — Systems Engineer Review

**Reviewer**: dcg-reviewer (systems engineering focus)
**Date**: 2026-02-28
**Plans Reviewed**:
- [03a-packs-core.md](./03a-packs-core.md)
- [03a-packs-core-test-harness.md](./03a-packs-core-test-harness.md)

**Cross-references**:
- [00-architecture.md](./00-architecture.md) — Layer 3 pack design
- [02-matching-framework.md](./02-matching-framework.md) — Matcher DSL, pipeline
- [shaping.md](../shaping/shaping.md) — §A8 pack scope table

---

## Review Focus

Per assignment: pattern completeness (are important destructive git/filesystem
commands covered?), false positive/negative analysis (do safe patterns correctly
exclude benign usage?), matcher composition correctness, golden file coverage,
and whether the pack authoring guide is clear enough for 03b-03e authors.
Cross-reference with shaping doc §A8.

---

## Summary

The plan is well-structured and thorough. The pack authoring guide (§4) is
excellent — clear, complete, and provides sufficient templates for 03b-03e
authors. Pattern interaction matrices and decision tables (§5.1.1, §5.1.2)
are valuable engineering artifacts. Golden file coverage at 60 entries is
solid. The test harness is comprehensive with property tests, reachability
tests, and comparison oracles.

Main concerns: a few false-negative gaps where commands can bypass detection
(P0-1, P0-2), some matcher composition subtleties that could cause
unexpected behavior (P1-1 through P1-4), and several coverage gaps worth
addressing before implementation (P2 findings).

**Findings**: 2 P0, 5 P1, 5 P2, 5 P3

---

## Findings

### P0-1: `rm -f` single-file falls into detection gap — no match, no safe

**Location**: §5.2 filesystem pack, §13 OQ2

**Issue**: `rm -f file.txt` is excluded by the safe pattern `rm-single-safe`
(because `-f` is in the Not clause) but has no destructive pattern (no `-r`).
This leaves it in a "no match" state. While the plan acknowledges this in
OQ2 and considers it correct, this creates a **semantic inconsistency**: the
safe pattern's `Not(-f)` implies `-f` is dangerous, but no destructive pattern
actually catches it. The test harness E2 even lists it as "Allow (no match —
see OQ2)" with an explicit callout.

The real problem: `rm -f` bypasses confirmation prompts that `rm` (without
flags) would show. While it's single-file (low risk), the inconsistency means
that `rm -f /important/production.db` passes through completely undetected.
For a tool guarding LLM agents, this is worth catching at Low severity.

**Recommendation**: Either (a) add a `Low` severity destructive pattern for
`rm -f` without `-r`, or (b) remove `-f` from the safe pattern's exclusion
list (let `rm -f file.txt` match the safe pattern). Option (b) is simpler
and more defensible since single-file `rm` with or without `-f` is equally
low-risk. The only reason to force-remove a single file is if it's read-only,
which is a normal operation.

**Impact**: False negative — commands that bypass confirmation pass undetected.

### P0-2: `git restore` missing — modern git replacement for checkout --

**Location**: §5.1, §13 OQ3

**Issue**: `git restore --staged .` and `git restore --worktree .` are the
modern replacements for `git checkout -- .`. The plan defers these to "a
future pack update" (OQ3). However, since DCG's primary use case is guarding
LLM agents, and LLMs increasingly generate `git restore` commands (it's the
recommended approach in git docs since 2.23+), deferring this creates a
significant false-negative gap from day one.

`git restore --worktree .` has identical destructive semantics to
`git checkout -- .` (discards all unstaged changes) and should be at least
High severity. `git restore --source HEAD .` is equivalent to
`git checkout HEAD -- .`. Both are one-liners that LLMs commonly generate.

**Recommendation**: Add `git restore` patterns to the core.git pack in this
plan, not deferred. At minimum: `git-restore-worktree-all` (High) for
`git restore --worktree .` or `git restore .`, and a safe pattern for
`git restore --staged` (which only unstages, doesn't discard changes).

The keyword list already includes `"git"` so the Aho-Corasick pre-filter
doesn't need changes. Only 2-3 new patterns are needed.

**Impact**: False negative on a common modern git command that LLMs generate.

### P1-1: Safe pattern for `git-push-safe` doesn't exclude `--force-with-lease` — evaluation order dependency

**Location**: §5.1, lines 336-351

**Issue**: The `git-push-safe` pattern matches `git push` when none of
`--force`, `-f`, `--mirror`, `--delete`, `-d` are present. But it does NOT
exclude `--force-with-lease`. This means `git push --force-with-lease origin main`
matches BOTH `git-push-safe` AND `git-push-force-with-lease`.

Since safe patterns short-circuit the entire pack (§4.5), whichever safe
pattern matches first wins. Both would result in "Allow", so the behavior
is correct. But the evaluation semantics are order-dependent in a non-obvious
way. If `git-push-safe` is evaluated before `git-push-force-with-lease`,
the `--force-with-lease` safe pattern is dead code for normal cases.

The `git-push-force-with-lease` safe pattern only becomes meaningful if
`git-push-safe` were changed to exclude `--force-with-lease` — which it
probably should for clarity.

**Recommendation**: Add `packs.Flags("--force-with-lease")` to
`git-push-safe`'s Not clause. This makes each safe pattern's scope
non-overlapping and eliminates the order dependency. Update the interaction
matrix accordingly.

**Impact**: Currently correct behavior but fragile — future changes to
`git-push-safe` could silently shadow `git-push-force-with-lease`.

### P1-2: `chmod 000` excluded from safe but has no destructive pattern

**Location**: §5.2, lines 718-720

**Issue**: The `chmod-single-safe` pattern excludes both `777` and `000`
from safe matching (`Not(Arg("777"), Arg("000"))`). There's a destructive
pattern `chmod-777` for `chmod 777`, but no corresponding `chmod-000`
destructive pattern. This means `chmod 000 file.txt` falls into the same
gap as `rm -f` — excluded from safe, no destructive match, passes through
undetected.

`chmod 000` removes all permissions from a file, making it inaccessible.
This is arguably more destructive than `chmod 777` (which at least leaves
the file accessible).

**Recommendation**: Either (a) add a `chmod-000` destructive pattern at
Medium severity, or (b) remove `000` from the safe pattern exclusion. If
the intent is to catch `chmod 000`, add the pattern.

**Impact**: False negative for `chmod 000` operations.

### P1-3: `dd` ArgPrefix("of=") may not match if tree-sitter extracts `of=` as a flag

**Location**: §5.2, lines 818-823

**Issue**: The `dd-write` pattern uses `packs.ArgPrefix("of=")` to detect
`dd` commands with an output file. However, `dd` uses a non-standard
argument syntax (`key=value` without dashes). The tree-sitter extractor
(plan 01) may or may not decompose `of=/dev/sda` into the Args slice vs
the Flags map. The plan 01 normalizer behavior for `dd`-style arguments
isn't explicitly specified.

If the tree-sitter parser puts `of=/dev/sda` into Flags (as a key-value
pair), then `ArgPrefix("of=")` would NOT match. The test cases in §7
use `cmd("dd", []string{"if=/dev/zero", "of=/dev/sda", "bs=4M"}, nil)`
which assumes these go into Args, but this assumption needs to be
validated against plan 01's extractor behavior.

**Recommendation**: Add an explicit note in the plan about which field
`dd`-style `key=value` arguments land in after tree-sitter extraction.
Cross-reference with plan 01's CommandExtractor. If there's ambiguity,
the pattern should check BOTH Args and Flags, or plan 01 should guarantee
these go into Args.

**Impact**: Potential false negative on all `dd` commands if extractor
behavior doesn't match assumption.

### P1-4: `git checkout -- file` safe pattern overlap with D11

**Location**: §5.1, lines 607-622

**Issue**: The D11 pattern `git-checkout-discard-file` matches
`git checkout -- <file>` where the file is NOT `.`. However, the git-push-safe
pattern S1 doesn't apply here since it's for push. The actual concern is
that `git checkout main` (switching branches) has no safe pattern — it matches
nothing (no safe, no destructive). This is fine.

But there's a subtlety: `git checkout -- .` (D4, git-checkout-discard-all)
and `git checkout -- file` (D11, git-checkout-discard-file) use
`packs.Flags("--")` to detect the `--` separator. The tree-sitter extractor
(plan 01) must correctly parse `--` as a flag (option terminator) rather than
as an argument. If `--` goes into Args instead of Flags, both D4 and D11
fail to match.

The test cases use `m("--", "")` which puts `--` in the Flags map. This
must be guaranteed by plan 01's extractor.

**Recommendation**: Verify that plan 01's CommandExtractor puts `--` into
the Flags map. If not, the matcher needs to use `Arg("--")` instead or
a custom matcher. This is a cross-plan contract that should be documented
explicitly.

**Impact**: If `--` goes to Args, both checkout-discard patterns are dead.

### P1-5: Safe-before-destructive short-circuit masks multi-match for benign commands

**Location**: §4.5, §8 state diagram

**Issue**: The safe-before-destructive design means that if ANY safe pattern
matches, NO destructive patterns in that pack are evaluated. This is
documented and tested. But consider `git branch -d merged-branch`:

1. `git-branch-safe` matches (no `-D`, no `--force`)
2. Short-circuit: skip all destructive patterns
3. Result: Allow

This is correct. But now consider `git push -u origin main`:

1. `git-push-safe` matches (no `--force`, no `--mirror`, no `--delete`)
2. Short-circuit: skip all destructive patterns
3. Result: Allow

Also correct. But `git push --set-upstream origin main` with `-u` flag:
the safe pattern doesn't exclude `-u` (it shouldn't — `-u` is benign).
This is fine. However, if a future destructive pattern were added that
involves `--set-upstream` (unlikely but possible), the safe pattern would
prevent it from ever matching.

This is a design property, not a bug, but it should be explicitly documented
in the pack authoring guide (§4.5) as a **caveat**: "If you add a new
destructive pattern, verify it's not shadowed by an existing safe pattern."
The reachability test catches this, but the authoring guide should warn
authors proactively.

**Recommendation**: Add a warning in §4.5 about the shadowing risk and
reference the reachability test as the safety net.

**Impact**: Future pack authors may add dead destructive patterns without
understanding why they don't match.

### P2-1: Shaping doc says `git checkout -- .` but plan also catches `git checkout .` (without --)

**Location**: §5.1 D4, shaping.md line 82

**Issue**: The shaping doc lists `git checkout -- .` as a pattern. The plan's
D4 pattern (`git-checkout-discard-all`) checks for `Flags("--")` AND
`Arg(".")`. But `git checkout .` (without `--`) is also destructive — it
discards all changes just like `git checkout -- .`.

The current pattern requires `--` to be present, so `git checkout .` without
`--` would not match D4. There's no test case for `git checkout .` (without `--`).

**Recommendation**: Either (a) add a pattern for `git checkout .` without
`--`, or (b) verify that `git checkout .` (without `--`) actually discards
changes and add a test case documenting the expected behavior. In modern
git, `git checkout .` without `--` does discard changes.

**Impact**: False negative for a common shorthand.

### P2-2: Golden file count is 60, plan summary says 80+

**Location**: §1 summary, §6

**Issue**: The summary (§1, line 33) says "80+ entries covering both packs"
but §6 tallies "33 (core.git) + 27 (core.filesystem) = 60 entries" (line 1387).
This is an inconsistency.

**Recommendation**: Fix the summary to say "60+" or increase golden file
entries to 80+. Given the plan already has comprehensive entries, 60 is
fine — just fix the number.

**Impact**: Documentation inconsistency.

### P2-3: `ArgPrefix` matcher not defined in plan 02 matcher DSL

**Location**: §5.2, line 822

**Issue**: The `dd-write` pattern uses `packs.ArgPrefix("of=")` but plan 02's
matcher DSL (which defines the available builders) only lists `Name()`,
`Flags()`, `ArgAt()`, `Arg()`, `And()`, `Or()`, `Not()`. There is no
`ArgPrefix()` builder in the plan 02 specification. Similarly, the git
pack uses `Arg(".")` but plan 02 defines `Arg()` for checking if any arg
equals a value.

`ArgPrefix` is semantically different — it checks if any arg starts with
a prefix, not equals. This needs to either be added to plan 02's DSL or
the `dd-write` pattern needs to be rewritten using existing matchers.

**Recommendation**: Add `ArgPrefix(prefix string) CommandMatcher` to plan 02's
builder DSL, or use a custom matcher. Since `dd`'s `key=value` syntax is
unique, `ArgPrefix` is a reasonable addition. File a cross-plan update to
plan 02.

**Impact**: Build failure — pattern references a builder that doesn't exist.

### P2-4: Missing test for `rm -f` without `-r` confirming it passes through

**Location**: §7 testing, E2 test matrix

**Issue**: The E2 test matrix includes `rm -f file.txt → Allow (no match — see OQ2)`.
However, there is no unit test in `filesystem_test.go` for this specific case.
The `TestRmSingleSafe` test checks that `rm -f file (not safe)` returns
false for the safe pattern, but doesn't verify the overall pipeline behavior
(that it passes through with Allow).

This is the test for the gap identified in P0-1. Regardless of whether P0-1
is addressed, there should be a golden file entry and/or pipeline test
confirming the expected behavior.

**Recommendation**: Add a golden file entry for `rm -f file.txt` with the
expected decision. If the decision is to add a Low destructive pattern
(per P0-1), add the test for that instead.

**Impact**: Untested behavior for a common command variant.

### P2-5: `git rebase --abort` classified as destructive (OQ1) — confidence doesn't compensate

**Location**: §5.1 D5, §13 OQ1

**Issue**: `git rebase --abort` is a recovery command — it undoes a
partially-completed rebase and restores the branch to its pre-rebase state.
The plan flags it as `High/Medium` (High severity, Medium confidence) and
acknowledges in OQ1 that it should potentially be excluded.

Setting Medium confidence doesn't fully compensate because the policy engine
maps `High/Medium` to `Deny` in strict mode and `Ask` in interactive mode.
A user trying to recover from a bad rebase by running `git rebase --abort`
will be blocked or warned, which is counterproductive.

`git rebase --continue` is similarly a recovery command that's currently
caught.

**Recommendation**: Add `Not(Or(Flags("--abort"), Flags("--continue")))` to
the `git-rebase` destructive pattern. These are unambiguously safe recovery
operations and should not be flagged.

**Impact**: False positive that blocks recovery operations — particularly
bad UX.

### P3-1: Pack authoring guide §4.3 category list may be incomplete

**Location**: §4.3, line 158

**Issue**: The pack ID convention lists categories: `core`, `database`,
`containers`, `infrastructure`, `cloud`, `kubernetes`, `frameworks`,
`remote`, `secrets`, `platform`. But the plan index (00-plan-index.md)
groups packs as: core, database, infra-cloud, containers-k8s, other.
The "other" category doesn't map to any of the listed categories.

Plan 03e (packs-other) will need to use category names not listed here.

**Recommendation**: Either expand the category list or note that it's
extensible. Add `other` or `misc` as a catch-all category.

**Impact**: Minor — 03e authors may be confused about category naming.

### P3-2: No test for git commands with path-prefixed binaries in unit tests

**Location**: §7.1 unit tests

**Issue**: The golden file corpus includes `/usr/bin/git push --force`
(line 1173) to test path-prefixed binaries. However, the unit tests in
`git_test.go` don't include any path-prefixed test cases. The unit tests
use `cmd("git", ...)` which has already been normalized.

This is acceptable since path normalization is plan 01's responsibility,
but it would be good to have at least one unit test confirming that
`Name("git")` matches the already-normalized name (which it does by
construction, but documenting this assumption in tests is valuable).

**Recommendation**: Add a comment in the unit tests noting that path
normalization is handled by plan 01's extractor and that unit tests
operate on normalized commands.

**Impact**: Minor — documentation of cross-plan assumption.

### P3-3: Benchmark targets are aggressive — validate with real measurements

**Location**: Test harness B1

**Issue**: The test harness sets targets of "Safe pattern match: < 100ns"
and "Destructive pattern match: < 200ns". These are quite aggressive.
The matchers involve map lookups (`Flags`), string comparisons (`Name`,
`ArgAt`), and boolean composition (`And`, `Or`, `Not`). A single map
lookup is ~50ns in Go, and each pattern has 2-5 matchers composed.

The targets are aspirational, not verified. Until there's a real
implementation to benchmark, these numbers may need adjustment.

**Recommendation**: Mark these as "initial targets — adjust after baseline
measurement" and add a note that the first implementation should establish
actual baseline before freezing targets.

**Impact**: Minor — unrealistic targets may cause unnecessary optimization.

### P3-4: Test harness P6 is redundant with P2

**Location**: Test harness, P6 (line 125-133)

**Issue**: P6 ("Safe/Destructive Mutual Exclusion on Reachability Set") is
explicitly described as "identical to P2 but with the additional constraint
that the reachability command set covers every destructive pattern." However,
P1 already ensures every destructive pattern has a reachability command.
So P1 + P2 = P6. The additional test adds no new coverage.

**Recommendation**: Either remove P6 or clarify what additional constraint
it adds beyond P1 + P2. If the intent is a single "everything is consistent"
meta-test, rename it and document the distinction.

**Impact**: Minor — redundant test, no correctness issue.

### P3-5: Filesystem pack keyword `mv` may cause false pre-filter triggers

**Location**: §5.2, line 688

**Issue**: The keyword `"mv"` is only 2 characters and could trigger the
Aho-Corasick pre-filter on many commands that contain `mv` as a substring.
However, plan 02 specifies word-boundary matching for the pre-filter, so
this should be fine — `mv` won't match inside `mvn`, `xmvfb`, etc.

The concern is marginal: the pre-filter's purpose is to quickly reject
non-matching commands, and a false trigger only costs one pack evaluation
(cheap). Still, it's worth verifying that the word-boundary matching in
plan 02's Aho-Corasick implementation correctly handles 2-character keywords.

**Recommendation**: Add a test case in the test harness for a command like
`mvn clean install` to verify it does NOT trigger the filesystem pack
evaluation. This validates the word-boundary contract.

**Impact**: Minor — potential unnecessary pack evaluation on `mvn` commands.

---

## Shaping Doc Cross-Reference (§A8)

| Shaping Pattern | Plan Coverage | Status |
|----------------|---------------|--------|
| `git reset --hard` | D3 git-reset-hard (High) | Covered |
| `git push --force` | D1 git-push-force (High) | Covered |
| `git clean -fd` | D7 git-clean-force-dirs (Medium) | Covered |
| `git branch -D` | D8 git-branch-force-delete (Medium) | Covered |
| `git checkout -- .` | D4 git-checkout-discard-all (High) | Covered |
| `git rebase` on shared branches | D5 git-rebase (High/Medium) | Covered (but see P2-5 for --abort) |
| `rm -rf` | D1 rm-rf-root (Critical), D3 rm-recursive-force (High) | Covered |
| `dd of=` | D4 dd-write (High) | Covered (but see P1-3 for ArgPrefix) |
| `shred` | D5 shred-any (High) | Covered |
| `chmod -R 777` | D7 chmod-recursive (Medium), D8 chmod-777 (Medium) | Covered |
| `mkfs` | D2 mkfs-any (Critical) | Covered |
| `mv` to /dev/null | D10 mv-to-devnull (Medium) | Covered |

All shaping doc patterns are accounted for. Additional patterns in the plan
beyond shaping: `git push --mirror`, `git push --delete`, `git stash drop/clear`,
`git checkout -- <file>`, `chown -R`, `rm -r` (without -f). These are
reasonable additions.

**Gap**: `git restore` (modern replacement for `git checkout --`) is listed
neither in shaping nor in the plan. See P0-2.

---

## Pack Authoring Guide Assessment (§4)

The guide is **clear and complete** for 03b-03e authors. Strengths:

1. **File structure template** (§4.1) — unambiguous directory layout
2. **Pack file template** (§4.2) — copy-paste ready
3. **Keyword selection rules** (§4.4) — specific, actionable
4. **Safe/destructive pattern rules** (§4.5, §4.6) — well-explained with examples
5. **Test file template** (§4.7) — includes match and near-miss cases
6. **Golden file template** (§4.8) — 3-entry minimum per pattern
7. **Registration pattern** (§4.9) — clear init() + blank import

Gaps:
- No mention of the safe-pattern shadowing risk (see P1-5)
- No guidance on when to use `Arg()` vs `ArgAt()` vs `ArgPrefix()` (if
  ArgPrefix is added per P2-3)
- No example of an `EnvSensitive` pattern (deferred to 03b, but a note
  pointing to it would help)

---

## Disposition Summary

| Priority | Count | Findings |
|----------|-------|----------|
| P0 | 2 | rm -f gap (P0-1), git restore missing (P0-2) |
| P1 | 5 | push-safe overlap (P1-1), chmod 000 gap (P1-2), dd ArgPrefix extraction (P1-3), checkout -- flag contract (P1-4), shadowing caveat (P1-5) |
| P2 | 5 | checkout . without -- (P2-1), golden count mismatch (P2-2), ArgPrefix undefined (P2-3), rm -f test missing (P2-4), rebase --abort FP (P2-5) |
| P3 | 5 | category list (P3-1), path-prefix unit test (P3-2), benchmark targets (P3-3), P6 redundancy (P3-4), mv keyword (P3-5) |
