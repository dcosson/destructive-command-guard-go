# Review: 00-architecture (dcg-coder-1)

- Source doc: `docs/plans/00-architecture.md`
- Reviewed commit: 5dbf12e
- Reviewer: dcg-coder-1

## Findings

### P1 - Foundation Type Contract Drifts From Downstream Plans

**Problem**
`ExtractedCommand` in the architecture doc omits fields that downstream plans rely on (`RawName`, `RawArgs`, `DataflowResolved`, `StartByte`, `EndByte`) and the built-in matcher list omits `AnyNameMatcher` (`docs/plans/00-architecture.md:410`, `docs/plans/00-architecture.md:437`). This now diverges from the canonical parse/matcher contracts in plans 01 and 02.

**Required fix**
Update the architecture-level `ExtractedCommand` and matcher catalog to match the current foundation contracts used by plans 01/02, and explicitly note which fields are mandatory cross-plan API.

---

### P2 - Warning Code Set Is Stale Relative To Foundation Pipeline

**Problem**
Architecture `WarningCode` lists only four values (`docs/plans/00-architecture.md:187`) and does not include warning variants now specified in foundation plans (e.g., expansion cap, extractor panic, command substitution, unknown pack ID). This creates ambiguity for implementers using 00 as the top-level contract.

**Required fix**
Refresh the architecture warning taxonomy (or reference an authoritative warning type table) so 00 does not underspecify warning behaviors introduced in 01/02.

---

## Summary

2 findings: 0 P0, 1 P1, 1 P2, 0 P3

**Verdict**: Approved with revisions
