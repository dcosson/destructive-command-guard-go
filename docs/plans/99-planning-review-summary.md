# Planning Review Summary

This document summarizes the review process across all planning documents for the destructive-command-guard-go project. The review covered **21 documents** across **3 rounds** of review: the architecture doc (00), 11 plan docs (01–05, with 03 split into 7 sub-plans including 03f and 03g), and 9 companion test harness docs. Each document was independently reviewed and findings were incorporated back into the source documents.

## Overall Aggregate

| Metric | Value |
|--------|-------|
| Total findings | 459 |
| Incorporated | 400 (87.1%) |
| Not Incorporated | 54 (11.8%) |
| N/A | 5 (1.1%) |
| Incorporation rate (excl. N/A) | 400/454 (88.1%) |

### Convergence Table

| Round | Total Findings | Incorporated | Not Incorporated | Deferred | N/A | Trend |
|-------|---------------|-------------|-----------------|----------|-----|-------|
| R1 | 415 | 357 | 53 | 0 | 5 | — |
| R2 | 41 | 40 | 1 | 0 | 0 | ↓90% |
| R3 | 3 | 3 | 0 | 0 | 0 | ↓93% |

### Severity Breakdown

| Severity | Count | Percentage |
|----------|-------|------------|
| P0 | 43 | 9.4% |
| P1 | 124 | 27.0% |
| P2 | 169 | 36.8% |
| P3 | 123 | 26.8% |

### Per-Document Finding Counts

| Document | Findings |
|----------|----------|
| 00-architecture.md | 29 |
| 01-treesitter-integration.md | 35 |
| 01-treesitter-integration-test-harness.md | 7 |
| 02-matching-framework.md | 36 |
| 02-matching-framework-test-harness.md | 5 |
| 03a-packs-core.md | 34 |
| 03a-packs-core-test-harness.md | 7 |
| 03b-packs-database.md | 41 |
| 03b-packs-database-test-harness.md | 11 |
| 03c-packs-infra-cloud.md | 38 |
| 03c-packs-infra-cloud-test-harness.md | 11 |
| 03d-packs-containers-k8s.md | 33 |
| 03d-packs-containers-k8s-test-harness.md | 13 |
| 03e-packs-other.md | 34 |
| 03e-packs-other-test-harness.md | 7 |
| 03f-packs-personal-files.md | 21 |
| 03g-packs-macos.md | 24 |
| 04-api-and-cli.md | 28 |
| 04-api-and-cli-test-harness.md | 8 |
| 05-testing-and-benchmarks.md | 29 |
| 05-testing-and-benchmarks-test-harness.md | 8 |

## Round 1 Review Summary

### Incorporation Rates

| Severity | Total | Incorporated | Not Incorporated | Rate |
|----------|-------|-------------|-----------------|------|
| P0 | 43 | 42 | 1 | 97.7% |
| P1 | 114 | 99 | 15 | 86.8% |
| P2 | 145 | 120 | 25 | 82.8% |
| P3 | 113 | 96 | 12 | 85.0% |

### Finding Themes

The dominant theme across all reviews was **correctness and false-negative analysis** (~24% of findings). Both reviewers independently identified cases where destructive commands would bypass detection. These ranged from critical bypass vectors — `&&` chain dataflow producing false negatives by discarding dangerous variable values, allowlist glob `*` matching command separators to enable compound injection, Redis mixed-case commands bypassing all patterns — to subtle edge cases like `git push origin :main` (colon refspec) silently deleting a remote branch without triggering the force-push detection.

**Missing pattern coverage** was the second largest theme (~21%). Across the seven pack plans (03a–03g), reviewers systematically identified commands and flags not yet covered: `git reflog expire`, `docker system prune --volumes`, `rsync --remove-source-files`, `aws s3 mv`, `gcloud storage rm`, JXA (JavaScript for Automation) bypassing all osascript patterns, `launchctl kickstart`/`kill`, and others. Many of these were incorporated; a subset were deferred to v2 scope.

**Safe pattern over-breadth and shadowing** was a particularly high-severity theme (~7% of findings but containing 9 P0s). In multiple packs, safe patterns were too broad and would short-circuit destructive pattern evaluation: Helm S2 shadowing D3/D4 destructive patterns, Vault S2 allowing `vault auth disable` to pass as safe, Ansible S1 including the `command` module (which runs arbitrary commands) in its safe list, GCP gcloud S1 missing a `Not(delete)` guard, Finder S2 "benign" safe pattern shadowing multi-tell scripts containing Messages/Mail sends, and diskutil apfs S3 matching destructive subcommands as safe. This theme persisted into the two newest packs (03f, 03g), confirming it as a systemic risk inherent in the safe-pattern-first evaluation model.

**Cross-document consistency** (~8%) surfaced primarily from the systems-engineer reviewer, who tracked type definitions and API contracts across plan boundaries. Key findings included `ExtractedCommand` struct divergence between plans 01 and 02, `ParseResult` not exposing exported vars needed by downstream matching, the pack authoring guide template omitting fields added in plan 01, and a critical parser limitation where `dscl -delete` flag decomposition breaks ArgContent-based matching (requiring a new RawArgContent matcher in plan 02).

**Testing gaps** (~13%) spanned both plan docs and test harness docs. The two highest-severity testing findings were that fuzz testing never exercised allowlist/blocklist paths (a huge gap in coverage) and that mutation testing excluded safe patterns entirely, meaning safe-pattern bugs would be undetectable. Both were resolved with new fuzz targets and expanded mutation scope. The newer packs (03f, 03g) additionally prompted expansion of golden file test suites to 30+ and 55+ entries respectively, establishing a higher baseline for pattern coverage evidence.

**API contracts and interface design** (~8%) covered nil safety, return types, and error handling in the public API. Notable P0s included unbounded `io.ReadAll(stdin)` creating an OOM vector, `WithPolicy(nil)` causing a nil pointer dereference, and `buildReason` using `Matches[0]` instead of the highest-severity match.

**Platform-conditional and path-based detection** (~4%) emerged as a new theme with plans 03f and 03g. Reviewers identified challenges specific to command-agnostic AnyName matching — source-path false positives where `cp /sensitive/path /safe/dest` triggers despite being non-destructive, redirect detection blind spots in the parser, and case sensitivity mismatches on case-insensitive filesystems (macOS HFS+). The build-tag testing strategy for darwin-only packs also drew attention, with reviewers requiring that pattern extraction remain testable on all platforms with only `init()` registration gated by build tags.

**Performance** (~5%) and **documentation clarity** (~6%) rounded out the remaining themes, with findings about O(n) keyword lookups, unbounded caches, misleading terminology ("false-positive" where "false-negative" was meant), and aspirational benchmark targets without measured baselines.

### Non-Incorporation Analysis

53 findings were not incorporated. The reasons break down as follows:

**V1 scope exclusion / deferred to v2** (34%): The most common reason. Valid findings that were judged low-priority for the initial release. Examples include SQLite ALTER TABLE DROP COLUMN (uncommon in LLM-generated commands), Azure `--yes`/`-y` auto-approve handling (inconsistent Azure CLI behavior), `rsync --dry-run` severity reduction, osascript script-file cross-pack detection, and `mdfind` content-search severity elevation.

**Intentional design choice** (28%): Findings where the reviewer identified a real trade-off but the current design is deliberate. Examples include the Tree wrapper intentionally not abstracting tree-sitter types within the same package, `rm -f` patterns intentionally casting a wider net as defense-in-depth, `kubectl rollout restart` being intentionally classified as safe (standard operational practice), personal.files deliberately having no safe patterns (all access warrants flagging), and `cp --no-clobber` to personal directories still warranting Medium severity.

**Already covered elsewhere** (11%): Concerns already addressed by existing documentation, tests, or another plan's scope. Examples include env escalation being tested in plan 04 rather than the pack plan, and findings that duplicate existing open questions.

**Negligible real-world impact** (11%): Theoretically valid but extremely rare edge cases like `checkout :/` quoted glob pathspecs, `ansible --module-name=command` long-flag edge cases, and file name collisions with SQL keywords.

**Framework limitation** (9%): The matching DSL fundamentally cannot express what the finding requires. `kubectl scale --replicas=0` cannot be detected because the framework cannot match on flag values — documented as a known gap. Similarly, `cp` source-path inflation cannot be resolved without ArgAt positional awareness.

**Deferred to implementation** (7%): Valid findings that will be resolved during implementation based on profiling data. The Oracle divergence schema, `processEnv` caching, and case-insensitive matching on macOS (which would increase Linux false positives) fall in this category.

### Notable P0 Resolutions

All 43 P0 findings were incorporated except 1 (an N/A duplicate). Key P0 resolutions include:

- **Dataflow false negatives**: May-alias approach adopted for `&&` chains, tracking union of all possible values instead of strong updates
- **Allowlist injection bypass**: Glob `*` restricted from matching `;`, `|`, `&`, newline, backtick, `$`, `(`, `)`
- **Safe pattern shadowing** (9 instances across 6 packs): Each safe pattern updated with explicit `Not()` guards for the shadowed destructive flags/subcommands. The two newest instances — Finder S2 shadowing multi-tell osascript scripts and diskutil apfs S3 matching destructive subcommands — demonstrate this pattern persists as packs grow
- **SSH regex backtracking**: `sshPrivateKeyRe` catch-all matched `.pub` files via backtracking; fixed with negative lookahead `(?!\.pub)`
- **Parser pool use-after-free risk**: Tree independence invariant documented with stress test to catch violations
- **Unbounded stdin OOM**: `io.LimitReader(stdin, 1MB)` added
- **Fuzz and mutation testing gaps**: `FuzzEvaluateWithAllowlist` added; mutation testing extended to safe+destructive patterns

### Reviewer Perspective Differences

The **security-correctness reviewer** focused primarily on bypass vectors and false negatives — ways destructive commands could evade detection. Nearly all safe-pattern shadowing P0s originated from this perspective, along with case-sensitivity bypasses, refspec syntax bypasses, compound injection through command separators, and JXA scripting bypass vectors. This reviewer also consistently flagged missing negative test cases.

The **systems-engineer reviewer** focused on cross-document consistency, API contracts, and implementation feasibility. Most P0 findings from this reviewer related to runtime safety (nil dereferences, unbounded memory, implementation ordering risks) and parser limitations (dscl flag decomposition) rather than detection bypasses. This reviewer was more likely to identify performance issues, benchmark methodology gaps, test infrastructure quality problems (brittle memory assertions, CV thresholds, golden file versioning), and build-tag CI coverage concerns.

Both reviewers independently identified several of the same issues (~18 overlapping findings across all plans), which were marked as duplicates during incorporation. The complementary perspectives provided substantially broader coverage than either reviewer alone would have achieved.

## Round 2 Review Summary

### Incorporation Rates

| Severity | Total | Incorporated | Not Incorporated | Rate |
|----------|-------|-------------|-----------------|------|
| P0 | 0 | 0 | 0 | N/A |
| P1 | 9 | 9 | 0 | 100.0% |
| P2 | 22 | 21 | 1 | 95.5% |
| P3 | 10 | 10 | 0 | 100.0% |

Round 2 findings were concentrated in **foundation-chain contract alignment** between plans 00, 01, and 02. The main theme was locking shared type boundaries so downstream docs no longer drift: `ExtractedCommand`/warning taxonomy in 00, `ParseResult` warning/export contracts in 01, and raw-argument matcher support plus matcher-footgun guidance in 02.

The second theme was **matcher semantic hardening** in the framework and core-pack docs. R2 closed the `ArgContent` vs `ArgContentRegex` ambiguity with explicit anti-footgun rules and regression tests, then propagated that correction into 03a by replacing regex-like `ArgContent` usages that would otherwise create false negatives.

The third theme was **test harness coverage for newly introduced primitives**. R2 added explicit coverage for `AnyName` command-agnostic behavior and parse boundary contract assertions (including mixed warning scenarios), reducing the chance that future edits regress cross-plan API compatibility without immediate detection.

Round 2 also showed strong convergence characteristics: only 17 of 21 docs produced new findings, and 4 docs were clean in R2. The largest residual finding cluster was in 03b/03c (database + infra-cloud batches), with foundation-chain docs mostly narrowed to one or two precise contract fixes per document.

Only 1 R2 finding (2.4%) was not incorporated. The non-incorporation reason was a reviewer-withdrawn/confirmed-correct case rather than unresolved disagreement, so no material corrective work was deferred from R2.

## Round 3 Review Summary

### Incorporation Rates

| Severity | Total | Incorporated | Not Incorporated | Rate |
|----------|-------|-------------|-----------------|------|
| P0 | 0 | 0 | 0 | N/A |
| P1 | 1 | 1 | 0 | 100.0% |
| P2 | 2 | 2 | 0 | 100.0% |
| P3 | 0 | 0 | 0 | N/A |

Round 3 findings were narrowly concentrated in **residual contract drift** rather than broad design issues. All three findings targeted recently changed interfaces/semantics and validated as real implementation blockers if left unresolved.

The dominant theme was **test harness API-shape alignment**. The key fix was in 05-testing-and-benchmarks-test-harness, where O1 self-comparison referenced a top-level severity field that does not exist in the plan 04 `guard.Result` contract; this was corrected to assessment-based severity access.

A second theme was **Ansible flag-value matching closure** in the infra/cloud chain (03c + 03c-th). Round 3 verified and incorporated the final pass to keep Ansible module/argument matching explicitly tied to flag-value-aware content matching, avoiding regressions to arg-only semantics.

Convergence behavior remained strong: total findings dropped from 41 in R2 to 3 in R3 (↓93%), and all 3 were incorporated. No new P0s surfaced in R3, and no deferrals were needed.

## Document Metrics

| Category | Files | Total Lines |
|----------|-------|-------------|
| Plan docs (00–05) | 13 | 24,966 |
| Test harness docs | 9 | 9,315 |
| Review docs (deleted after incorporation) | 66 | 10,815 |
| **Total** | **88** | **45,096** |

### Per-Document Line Counts

**Plan docs:**

| Document | Lines |
|----------|-------|
| 00-plan-index.md | 173 |
| 00-architecture.md | 1,071 |
| 01-treesitter-integration.md | 1,618 |
| 02-matching-framework.md | 3,127 |
| 03a-packs-core.md | 2,898 |
| 03b-packs-database.md | 2,304 |
| 03c-packs-infra-cloud.md | 2,292 |
| 03d-packs-containers-k8s.md | 1,910 |
| 03e-packs-other.md | 2,812 |
| 03f-packs-personal-files.md | 842 |
| 03g-packs-macos.md | 1,507 |
| 04-api-and-cli.md | 2,145 |
| 05-testing-and-benchmarks.md | 2,267 |

**Test harness docs:**

| Document | Lines |
|----------|-------|
| 01-treesitter-integration-test-harness.md | 1,012 |
| 02-matching-framework-test-harness.md | 1,176 |
| 03a-packs-core-test-harness.md | 761 |
| 03b-packs-database-test-harness.md | 1,283 |
| 03c-packs-infra-cloud-test-harness.md | 1,129 |
| 03d-packs-containers-k8s-test-harness.md | 1,011 |
| 03e-packs-other-test-harness.md | 1,026 |
| 04-api-and-cli-test-harness.md | 1,033 |
| 05-testing-and-benchmarks-test-harness.md | 884 |

## Quality Signals

**Severity distribution is healthy.** P0 findings represent 9.4% of total findings — high enough to demonstrate that reviewers found genuinely critical issues, but low enough to indicate that the plan drafts were substantially correct before review. The majority of findings (63.6%) are P2/P3, indicating refinements rather than fundamental flaws.

**Incorporation rate scales with severity.** P0 findings are near-fully incorporated, while most non-incorporations cluster in lower-severity findings where v1 scope boundaries or explicit design trade-offs were documented. This gradient is expected and healthy: higher-severity findings are almost always incorporated, while lower-severity findings are more likely to reflect intentional scope control.

**Non-incorporation reasons are well-justified.** No P0 findings were rejected outright — the single non-incorporated P0 was an N/A duplicate. Non-incorporation reasons are dominated by v1 scope exclusion (34%) and intentional design choices (28%), both of which indicate thoughtful triage rather than dismissal.

**Reviewer complementarity is strong.** The security-correctness and systems-engineer perspectives found substantially different categories of issues. The ~18 independently overlapping findings provide confidence that both reviewers engaged deeply with the material, while the non-overlapping findings demonstrate that dual-reviewer coverage catches issues that a single reviewer would miss.

**Safe pattern shadowing was the most impactful finding pattern.** Nine P0 findings across six different pack plans identified safe patterns that would silently suppress destructive pattern evaluation. This pattern was caught consistently across all plan batches — including the two newest packs (03f, 03g) drafted after the original round of reviews — confirming it is a systemic risk in the pack design approach that implementation should be particularly vigilant about through explicit testing.

**The two newest packs (03f, 03g) introduced novel detection challenges.** Plan 03f's command-agnostic AnyName matching represents a fundamentally different detection approach from all prior packs, relying on argument content (path components) rather than command names for pre-filtering. Plan 03g's macOS communication pack requires regex-based sub-language detection for AppleScript within osascript invocations, since no mature tree-sitter AppleScript grammar exists. Both plans also exposed a parser limitation (dscl flag decomposition) that requires a new RawArgContent matcher in plan 02.

**Plan 02 (matching framework) is the most complex document** at 3,127 lines, reflecting its role as the core matching engine that all packs depend on. It also had the highest finding density after the architecture doc, which is expected given its central position in the dependency graph.

**Convergence signal through R3 is very strong.** Total findings dropped from 415 in R1 to 41 in R2 (↓90%), then to 3 in R3 (↓93% from R2). All R3 findings were incorporated (100%). This pattern indicates the bulk of high-risk structural issues were resolved in R1, R2 closed residual contract/coverage drift, and R3 was primarily final contract polish.

**R3 scope narrowed to three docs with genuine residual issues.** Only 03c, 03c test harness, and 05 test harness produced new findings in R3; all other assigned docs converged cleanly with no new actionable issues.
