# Planning Review Summary

This document summarizes the review process across all planning documents for the destructive-command-guard-go project. The review covered **21 documents** across 1 round of review: the architecture doc (00), 11 plan docs (01–05, with 03 split into 7 sub-plans including 03f and 03g), and 9 companion test harness docs. Each document was independently reviewed by two reviewers (security-correctness and systems-engineer perspectives), and findings were incorporated back into the source documents.

## Overall Aggregate

| Metric | Value |
|--------|-------|
| Total findings | 415 |
| Incorporated | 357 (86.0%) |
| Not Incorporated | 53 (12.8%) |
| N/A | 5 (1.2%) |
| Incorporation rate (excl. N/A) | 357/410 (87.1%) |

### Convergence Table

| Round | Total Findings | Incorporated | Not Incorporated | Deferred | N/A | Trend |
|-------|---------------|-------------|-----------------|----------|-----|-------|
| R1 | 415 | 357 | 53 | 0 | 5 | — |

### Severity Breakdown

| Severity | Count | Percentage |
|----------|-------|------------|
| P0 | 43 | 10.4% |
| P1 | 114 | 27.5% |
| P2 | 145 | 34.9% |
| P3 | 113 | 27.2% |

### Per-Document Finding Counts

| Document | Findings |
|----------|----------|
| 00-architecture.md | 27 |
| 01-treesitter-integration.md | 34 |
| 01-treesitter-integration-test-harness.md | 6 |
| 02-matching-framework.md | 34 |
| 02-matching-framework-test-harness.md | 3 |
| 03a-packs-core.md | 33 |
| 03a-packs-core-test-harness.md | 7 |
| 03b-packs-database.md | 33 |
| 03b-packs-database-test-harness.md | 6 |
| 03c-packs-infra-cloud.md | 33 |
| 03c-packs-infra-cloud-test-harness.md | 7 |
| 03d-packs-containers-k8s.md | 31 |
| 03d-packs-containers-k8s-test-harness.md | 10 |
| 03e-packs-other.md | 31 |
| 03e-packs-other-test-harness.md | 7 |
| 03f-packs-personal-files.md | 21 |
| 03g-packs-macos.md | 23 |
| 04-api-and-cli.md | 28 |
| 04-api-and-cli-test-harness.md | 7 |
| 05-testing-and-benchmarks.md | 28 |
| 05-testing-and-benchmarks-test-harness.md | 6 |

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

## Document Metrics

| Category | Files | Total Lines |
|----------|-------|-------------|
| Plan docs (00–05) | 13 | 24,722 |
| Test harness docs | 9 | 9,080 |
| Review docs (deleted after incorporation) | 24 | 9,454 |
| **Total** | **46** | **43,256** |

### Per-Document Line Counts

**Plan docs:**

| Document | Lines |
|----------|-------|
| 00-plan-index.md | 173 |
| 00-architecture.md | 1,050 |
| 01-treesitter-integration.md | 1,615 |
| 02-matching-framework.md | 3,061 |
| 03a-packs-core.md | 2,886 |
| 03b-packs-database.md | 2,270 |
| 03c-packs-infra-cloud.md | 2,261 |
| 03d-packs-containers-k8s.md | 1,876 |
| 03e-packs-other.md | 2,802 |
| 03f-packs-personal-files.md | 838 |
| 03g-packs-macos.md | 1,492 |
| 04-api-and-cli.md | 2,141 |
| 05-testing-and-benchmarks.md | 2,257 |

**Test harness docs:**

| Document | Lines |
|----------|-------|
| 01-treesitter-integration-test-harness.md | 967 |
| 02-matching-framework-test-harness.md | 1,115 |
| 03a-packs-core-test-harness.md | 753 |
| 03b-packs-database-test-harness.md | 1,261 |
| 03c-packs-infra-cloud-test-harness.md | 1,093 |
| 03d-packs-containers-k8s-test-harness.md | 993 |
| 03e-packs-other-test-harness.md | 1,022 |
| 04-api-and-cli-test-harness.md | 1,022 |
| 05-testing-and-benchmarks-test-harness.md | 854 |

## Quality Signals

**Severity distribution is healthy.** P0 findings represent 10.4% of total findings — high enough to demonstrate that reviewers found genuinely critical issues, but low enough to indicate that the plan drafts were substantially correct before review. The majority of findings (62.2%) are P2/P3, indicating refinements rather than fundamental flaws.

**Incorporation rate scales with severity.** P0 findings have a 97.7% incorporation rate, while P2 findings are at 82.8%. This gradient is expected and healthy: higher-severity findings are almost always worth incorporating, while lower-severity findings are more likely to be intentional design trade-offs or v2 scope. P3 incorporation (85.0%) is notably higher than P2 (82.8%), reflecting the high incorporation rate of the newer 03f/03g plans where P3 findings tended to be straightforward additions (golden file expansion, missing test cases) rather than debatable design choices.

**Non-incorporation reasons are well-justified.** No P0 findings were rejected outright — the single non-incorporated P0 was an N/A duplicate. Non-incorporation reasons are dominated by v1 scope exclusion (34%) and intentional design choices (28%), both of which indicate thoughtful triage rather than dismissal.

**Reviewer complementarity is strong.** The security-correctness and systems-engineer perspectives found substantially different categories of issues. The ~18 independently overlapping findings provide confidence that both reviewers engaged deeply with the material, while the non-overlapping findings demonstrate that dual-reviewer coverage catches issues that a single reviewer would miss.

**Safe pattern shadowing was the most impactful finding pattern.** Nine P0 findings across six different pack plans identified safe patterns that would silently suppress destructive pattern evaluation. This pattern was caught consistently across all plan batches — including the two newest packs (03f, 03g) drafted after the original round of reviews — confirming it is a systemic risk in the pack design approach that implementation should be particularly vigilant about through explicit testing.

**The two newest packs (03f, 03g) introduced novel detection challenges.** Plan 03f's command-agnostic AnyName matching represents a fundamentally different detection approach from all prior packs, relying on argument content (path components) rather than command names for pre-filtering. Plan 03g's macOS communication pack requires regex-based sub-language detection for AppleScript within osascript invocations, since no mature tree-sitter AppleScript grammar exists. Both plans also exposed a parser limitation (dscl flag decomposition) that requires a new RawArgContent matcher in plan 02.

**Plan 02 (matching framework) is the most complex document** at 3,061 lines (up from 2,990 after AnyNameMatcher addition), reflecting its role as the core matching engine that all packs depend on. It also had the highest finding density after the architecture doc, which is expected given its central position in the dependency graph.

**This is Round 1 of review.** All plans have gone through one complete review cycle with two independent reviewers each. Whether a second round of review is warranted depends on whether the incorporated changes introduce new cross-document consistency issues. The 415 R1 findings with 87.1% incorporation rate suggest the plans are substantially mature, though a targeted R2 focusing on cross-document API contracts (particularly the new AnyNameMatcher and RawArgContent matcher additions to plan 02) and safe-pattern completeness could be valuable.
