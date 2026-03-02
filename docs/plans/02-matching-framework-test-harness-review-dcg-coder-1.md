# Review: 02-matching-framework-test-harness (dcg-coder-1)

- Source doc: `docs/plans/02-matching-framework-test-harness.md`
- Reviewed commit: 5dbf12e
- Reviewer: dcg-coder-1

## Findings

### P2 - Harness Coverage Omits AnyNameMatcher-Specific Behavior

**Problem**
Deterministic and benchmark sections enumerate many matcher paths (`docs/plans/02-matching-framework-test-harness.md:220`, `docs/plans/02-matching-framework-test-harness.md:718`) but do not include AnyName-driven command-agnostic pack behavior. Given AnyName is a new R1 addition in the foundation chain, this leaves a direct regression gap.

**Required fix**
Add unit/property/integration coverage for AnyNameMatcher with argument-content keywords, including pre-filter + candidate pack selection behavior for command-agnostic rules.

---

### P2 - Harness Lacks Explicit Regression For ArgContent Regex-Literal Misuse

**Problem**
The harness exercises glob injection and regex DoS (`docs/plans/02-matching-framework-test-harness.md:885`) but does not define a test that distinguishes `ArgContent("^:")` literal behavior from `ArgContentRegex("^:")` regex behavior. This omission permits pack-level false negatives to slip through.

**Required fix**
Add matcher-semantic regression tests that enforce the intended split between literal substring and regex matching, and fail when regex-literal strings are incorrectly used with `ArgContent`.

---

## Summary

2 findings: 0 P0, 0 P1, 2 P2, 0 P3

**Verdict**: Approved with revisions
