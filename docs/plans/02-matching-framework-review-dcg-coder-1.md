# Review: 02-matching-framework (dcg-coder-1)

- Source doc: `docs/plans/02-matching-framework.md`
- Reviewed commit: 5dbf12e
- Reviewer: dcg-coder-1

## Findings

### P1 - Matcher Set Still Lacks RawArgs Content Matcher Despite Foundation Need

**Problem**
Plan 02 consumes `RawArgs` in the input type (`docs/plans/02-matching-framework.md:47`) but only defines `ArgContentMatcher` over normalized `cmd.Args` (`docs/plans/02-matching-framework.md:606`) and exposes no `RawArgContent*` matcher/builder (`docs/plans/02-matching-framework.md:783`). This leaves the “raw-argument content” capability incomplete in the foundation matcher API.

**Required fix**
Add explicit raw-argument content matcher support (type + builder + tests), or remove `RawArgs`-based guidance from dependent docs and standardize that content matching is normalized-only.

---

### P2 - ArgContent API Leaves A Regex-Literal Footgun Unaddressed

**Problem**
`ArgContent()` is defined as substring matching (`docs/plans/02-matching-framework.md:783`), while regex semantics are separate (`ArgContentRegex`, `docs/plans/02-matching-framework.md:788`). The doc does not include a guardrail against anchored-regex literals being passed to `ArgContent`, which already causes downstream misuse in pack specs.

**Required fix**
Add an explicit anti-footgun rule: anchored/regex-like patterns must use `ArgContentRegex`, with examples and a validation/test requirement to prevent `ArgContent("^...")` misuse.

---

## Summary

2 findings: 0 P0, 1 P1, 1 P2, 0 P3

**Verdict**: Approved with revisions
