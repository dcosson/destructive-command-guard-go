# Review: 03c-packs-infra-cloud-test-harness (domain-packs-r2)

- Source doc: `docs/plans/03c-packs-infra-cloud-test-harness.md`
- Reviewed commit: fb3fa18
- Reviewer: domain-packs-r2
- Round: 2

## Findings

### P2 - Test harness lacks diagnostic test for ArgContent flag-value matching (Ansible)

**Problem**
E3 (line ~302), SEC2 (line ~898), and F3 (line ~446) all test Ansible patterns using commands where module names and arguments are in flag values (`m("-m", "file", "-a", "state=absent")`). Per the companion plan review, the underlying patterns use `ArgContent` which only checks `cmd.Args` (plan 02 §5.2.4), making all Ansible content-matching patterns non-functional.

These tests will fail during implementation, but the failures won't clearly diagnose the root cause. A developer would see "D2 doesn't match `ansible all -m file -a 'state=absent'`" and might look for bugs in the pattern logic rather than recognizing that `ArgContent` can't see flag values.

**Required fix**
Add a foundational property test (e.g., P8) that explicitly verifies ArgContent behavior with flag values:

```go
func TestPropertyAnsibleFlagValueMatching(t *testing.T) {
    // This test documents that Ansible patterns require CheckFlagValues
    cmd := cmd("ansible", []string{"all"}, m("-m", "file", "-a", "state=absent"))
    // ArgContent("file") must match the -m flag value
    matcher := packs.ArgContent("file")
    assert.True(t, matcher.Match(cmd), "ArgContent must check flag values for Ansible patterns")
}
```

This makes the dependency on flag-value matching explicit and provides a clear diagnostic when it fails.

---

### P2 - Missing `terraform import` test case in E1

**Problem**
In E1 (line ~237), the Terraform pattern matrix has 25 cases but does not include `terraform import`. The plan doc §5.1.1 (line ~446) claims "terraform import: Classified as safe" but no safe pattern matches `terraform import` (it's actually Indeterminate).

The test harness should explicitly test `terraform import` to document the expected behavior. Without this test case, the discrepancy between the plan note ("safe") and reality (Indeterminate) would go undetected until manual QA.

**Required fix**
Add to E1:
```
terraform import aws_instance.web i-1234          → Indeterminate (no pattern match)
```
With a comment noting this is intentional per §5.1.1 — import is safe behavior but not pattern-classified. If the plan is updated to add "import" to S4's Or clause, change the expected result to Allow.

---

### P3 - SEC3 tests pattern metadata but not actual escalation behavior

**Problem**
SEC3 `TestSecurityEnvSensitivityEscalation` (line ~943) checks `dp.EnvSensitive` and `dp.Severity` — both are static properties of the pattern struct. It does not test that when the env detector identifies a production environment, the severity is actually escalated (e.g., Medium → High, High → Critical).

The test is named "Escalation" but only tests pre-conditions. This is the same issue flagged in the 03b test harness review for SEC4.

**Required fix**
Either:
(a) Rename to `TestSecurityEnvSensitivityPreConditions` to match what it actually tests, OR
(b) Add an integration-style assertion that demonstrates escalation end-to-end, even with mocked env detection (e.g., verify that a Medium pattern in a "production" context produces a higher effective severity).

---

## Summary

3 findings: 0 P0, 0 P1, 2 P2, 1 P3

**Verdict**: Approved with revisions

The test harness is comprehensive with good coverage across property tests (P1-P7), deterministic examples (E1-E8), fault injection (F1-F3), comparison oracles (O1-O4), benchmarks (B1-B3), stress tests (S1-S2), and security tests (SEC1-SEC3). The AWS service isolation test (P7) and cross-cloud consistency oracle (O2) are strong additions specific to the infra/cloud domain. The main gap is the missing diagnostic test for Ansible's dependency on ArgContent flag-value matching — the existing tests will fail but won't clearly point to the root cause. The R1 disposition table shows clean incorporation of all 8 R1 findings.
