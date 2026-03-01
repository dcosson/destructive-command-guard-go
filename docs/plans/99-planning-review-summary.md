# Planning Review Summary

This document summarizes the review process across all planning documents for the destructive-command-guard-go project. The review covered **19 documents** across 1 round of review: the architecture doc (00), 9 plan docs (01–05, with 03 split into 5 sub-plans), and 9 companion test harness docs. Each document was independently reviewed by two reviewers (security-correctness and systems-engineer perspectives), and findings were incorporated back into the source documents.

## Overall Aggregate

| Metric | Value |
|--------|-------|
| Total findings | 371 |
| Incorporated | 319 (86.0%) |
| Not Incorporated | 47 (12.7%) |
| N/A | 5 (1.3%) |
| Incorporation rate (excl. N/A) | 319/366 (87.2%) |

### Convergence Table

| Round | Total Findings | Incorporated | Not Incorporated | Deferred | N/A | Trend |
|-------|---------------|-------------|-----------------|----------|-----|-------|
| R1 | 371 | 319 | 47 | 0 | 5 | — |

### Severity Breakdown

| Severity | Count | Percentage |
|----------|-------|------------|
| P0 | 40 | 10.8% |
| P1 | 103 | 27.8% |
| P2 | 126 | 34.0% |
| P3 | 102 | 27.5% |

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
| 04-api-and-cli.md | 28 |
| 04-api-and-cli-test-harness.md | 7 |
| 05-testing-and-benchmarks.md | 28 |
| 05-testing-and-benchmarks-test-harness.md | 6 |

## Round 1 Review Summary

### Incorporation Rates

| Severity | Total | Incorporated | Not Incorporated | Rate |
|----------|-------|-------------|-----------------|------|
| P0 | 40 | 39 | 1 | 97.5% |
| P1 | 103 | 89 | 14 | 86.4% |
| P2 | 126 | 106 | 20 | 84.1% |
| P3 | 102 | 85 | 12 | 83.3% |

### Finding Themes

The dominant theme across all reviews was **correctness and false-negative analysis** (~24% of findings). Both reviewers independently identified cases where destructive commands would bypass detection. These ranged from critical bypass vectors — `&&` chain dataflow producing false negatives by discarding dangerous variable values, allowlist glob `*` matching command separators to enable compound injection, Redis mixed-case commands bypassing all patterns — to subtle edge cases like `git push origin :main` (colon refspec) silently deleting a remote branch without triggering the force-push detection.

**Missing pattern coverage** was the second largest theme (~21%). Across the five pack plans (03a–03e), reviewers systematically identified commands and flags not yet covered: `git reflog expire`, `docker system prune --volumes`, `rsync --remove-source-files`, `aws s3 mv`, `gcloud storage rm`, and others. Many of these were incorporated; a subset were deferred to v2 scope.

**Safe pattern over-breadth and shadowing** was a particularly high-severity theme (~7% of findings but containing 6 P0s). In multiple packs, safe patterns were too broad and would short-circuit destructive pattern evaluation: Helm S2 shadowing D3/D4 destructive patterns, Vault S2 allowing `vault auth disable` to pass as safe, Ansible S1 including the `command` module (which runs arbitrary commands) in its safe list, and GCP gcloud S1 missing a `Not(delete)` guard.

**Cross-document consistency** (~8%) surfaced primarily from the systems-engineer reviewer, who tracked type definitions and API contracts across plan boundaries. Key findings included `ExtractedCommand` struct divergence between plans 01 and 02, `ParseResult` not exposing exported vars needed by downstream matching, and the pack authoring guide template omitting fields added in plan 01.

**Testing gaps** (~13%) spanned both plan docs and test harness docs. The two highest-severity testing findings were that fuzz testing never exercised allowlist/blocklist paths (a huge gap in coverage) and that mutation testing excluded safe patterns entirely, meaning safe-pattern bugs would be undetectable. Both were resolved with new fuzz targets and expanded mutation scope.

**API contracts and interface design** (~8%) covered nil safety, return types, and error handling in the public API. Notable P0s included unbounded `io.ReadAll(stdin)` creating an OOM vector, `WithPolicy(nil)` causing a nil pointer dereference, and `buildReason` using `Matches[0]` instead of the highest-severity match.

**Performance** (~5%) and **documentation clarity** (~6%) rounded out the remaining themes, with findings about O(n) keyword lookups, unbounded caches, misleading terminology ("false-positive" where "false-negative" was meant), and aspirational benchmark targets without measured baselines.

### Non-Incorporation Analysis

47 findings were not incorporated. The reasons break down as follows:

**V1 scope exclusion / deferred to v2** (35%): The most common reason. Valid findings that were judged low-priority for the initial release. Examples include SQLite ALTER TABLE DROP COLUMN (uncommon in LLM-generated commands), Azure `--yes`/`-y` auto-approve handling (inconsistent Azure CLI behavior), and `rsync --dry-run` severity reduction.

**Intentional design choice** (26%): Findings where the reviewer identified a real trade-off but the current design is deliberate. Examples include the Tree wrapper intentionally not abstracting tree-sitter types within the same package, `rm -f` patterns intentionally casting a wider net as defense-in-depth, and `kubectl rollout restart` being intentionally classified as safe (standard operational practice).

**Already covered elsewhere** (13%): Concerns already addressed by existing documentation, tests, or another plan's scope. Examples include env escalation being tested in plan 04 rather than the pack plan, and findings that duplicate existing open questions.

**Negligible real-world impact** (13%): Theoretically valid but extremely rare edge cases like `checkout :/` quoted glob pathspecs, `ansible --module-name=command` long-flag edge cases, and file name collisions with SQL keywords.

**Deferred to implementation** (6%): Valid findings that will be resolved during implementation based on profiling data. The Oracle divergence schema and `processEnv` caching fall in this category.

**Framework limitation** (7%): The matching DSL fundamentally cannot express what the finding requires. `kubectl scale --replicas=0` cannot be detected because the framework cannot match on flag values — documented as a known gap.

### Notable P0 Resolutions

All 40 P0 findings were incorporated except 1 (an N/A duplicate). Key P0 resolutions include:

- **Dataflow false negatives**: May-alias approach adopted for `&&` chains, tracking union of all possible values instead of strong updates
- **Allowlist injection bypass**: Glob `*` restricted from matching `;`, `|`, `&`, newline, backtick, `$`, `(`, `)`
- **Safe pattern shadowing** (6 instances across packs): Each safe pattern updated with explicit `Not()` guards for the shadowed destructive flags/subcommands
- **Parser pool use-after-free risk**: Tree independence invariant documented with stress test to catch violations
- **Unbounded stdin OOM**: `io.LimitReader(stdin, 1MB)` added
- **Fuzz and mutation testing gaps**: `FuzzEvaluateWithAllowlist` added; mutation testing extended to safe+destructive patterns

### Reviewer Perspective Differences

The **security-correctness reviewer** focused primarily on bypass vectors and false negatives — ways destructive commands could evade detection. Nearly all safe-pattern shadowing P0s originated from this perspective, along with case-sensitivity bypasses, refspec syntax bypasses, and compound injection through command separators. This reviewer also consistently flagged missing negative test cases.

The **systems-engineer reviewer** focused on cross-document consistency, API contracts, and implementation feasibility. Most P0 findings from this reviewer related to runtime safety (nil dereferences, unbounded memory, implementation ordering risks) rather than detection bypasses. This reviewer was more likely to identify performance issues, benchmark methodology gaps, and test infrastructure quality problems (brittle memory assertions, CV thresholds, golden file versioning).

Both reviewers independently identified several of the same issues (~15 overlapping findings), which were marked as duplicates during incorporation. The complementary perspectives provided substantially broader coverage than either reviewer alone would have achieved.

## Document Metrics

| Category | Files | Total Lines |
|----------|-------|-------------|
| Plan docs (00–05) | 11 | 31,418 |
| Test harness docs | 9 | 9,080 |
| Review docs (deleted after incorporation) | 20 | 8,138 |
| **Total** | **40** | **48,636** |

### Per-Document Line Counts

**Plan docs:**

| Document | Lines |
|----------|-------|
| 00-plan-index.md | 169 |
| 00-architecture.md | 1,050 |
| 01-treesitter-integration.md | 1,636 |
| 02-matching-framework.md | 2,990 |
| 03a-packs-core.md | 2,886 |
| 03b-packs-database.md | 2,270 |
| 03c-packs-infra-cloud.md | 2,261 |
| 03d-packs-containers-k8s.md | 1,876 |
| 03e-packs-other.md | 2,802 |
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

**Severity distribution is healthy.** P0 findings represent 10.8% of total findings — high enough to demonstrate that reviewers found genuinely critical issues, but low enough to indicate that the plan drafts were substantially correct before review. The majority of findings (61.5%) are P2/P3, indicating refinements rather than fundamental flaws.

**Incorporation rate scales with severity.** P0 findings have a 97.5% incorporation rate, while P3 findings are at 83.3%. This gradient is expected and healthy: higher-severity findings are almost always worth incorporating, while lower-severity findings are more likely to be intentional design trade-offs or v2 scope.

**Non-incorporation reasons are well-justified.** No P0 findings were rejected outright — the single non-incorporated P0 was an N/A duplicate. Non-incorporation reasons are dominated by v1 scope exclusion (35%) and intentional design choices (26%), both of which indicate thoughtful triage rather than dismissal.

**Reviewer complementarity is strong.** The security-correctness and systems-engineer perspectives found substantially different categories of issues. The ~15 independently overlapping findings provide confidence that both reviewers engaged deeply with the material, while the non-overlapping findings demonstrate that dual-reviewer coverage catches issues that a single reviewer would miss.

**Safe pattern shadowing was the most impactful finding pattern.** Six P0 findings across four different pack plans identified safe patterns that would silently suppress destructive pattern evaluation. This pattern was caught consistently across plans, suggesting it is a systemic risk in the pack design approach that implementation should be particularly vigilant about through explicit testing.

**Plan 02 (matching framework) is the most complex document** at 2,990 lines, reflecting its role as the core matching engine that all packs depend on. It also had the highest finding density after the architecture doc, which is expected given its central position in the dependency graph.

**This is Round 1 of review.** All plans have gone through one complete review cycle with two independent reviewers each. Whether a second round of review is warranted depends on whether the incorporated changes introduce new cross-document consistency issues. The 371 R1 findings with 87.2% incorporation rate suggest the plans are substantially mature, though a targeted R2 focusing on cross-document API contracts and safe-pattern completeness could be valuable.
