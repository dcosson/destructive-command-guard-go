# Review: 03d-packs-containers-k8s-test-harness (domain-packs-r2)

- Source doc: `docs/plans/03d-packs-containers-k8s-test-harness.md`
- Reviewed commit: 5dbf12e
- Reviewer: domain-packs-r2
- Round: 2

## Findings

### P2 - SEC2 doesn't assert pattern match — severity check silently skipped if no pattern matches

**Problem**
In SEC2 `TestSecurityKubectlDeleteResourceEscalation` (lines ~803-847), the test iterates through patterns and asserts severity only when a match is found:

```go
for _, dp := range kubectlPack.Destructive {
    if dp.Match.Match(testCmd) {
        assert.GreaterOrEqual(t, int(dp.Severity), int(tt.minSeverity), ...)
        break
    }
}
```

If no pattern matches the test command at all, the inner `assert` is never reached and the test silently passes. The same issue applies to the `genericResources` loop (lines ~834-846).

This means SEC2 doesn't actually verify that each resource type IS matched — only that IF matched, the severity is correct. A regression that breaks the delete pattern entirely (e.g., a typo in the ArgAt matcher) would pass SEC2 without any assertion firing.

**Required fix**
Add a `matched` boolean and assert it's true after the loop:

```go
matched := false
for _, dp := range kubectlPack.Destructive {
    if dp.Match.Match(testCmd) {
        matched = true
        assert.GreaterOrEqual(t, int(dp.Severity), int(tt.minSeverity), ...)
        break
    }
}
assert.True(t, matched, "kubectl delete %s should match at least one pattern", tt.resource)
```

Apply the same fix to the genericResources loop.

---

### P2 - MQ3 lists `kubectl delete secret` as Ask/Medium (catch-all), contradicts R1 incorporation

**Problem**
In MQ3 (line ~921):
```
9. `kubectl delete secret` → Ask/Medium (catch-all)
```

But R1 finding #7 (disposition table line ~991) states: "SEC2 secret still at Medium instead of High | Incorporated | Moved secret/secrets from genericResources to highImpact". SEC2 (line ~817) correctly lists `{"secret", guard.High}` and `{"secrets", guard.High}` in the highImpact tier. P8 (line ~289) also lists `"secret", "secrets"` in specificResources that should be excluded from D8 catch-all.

MQ3 was not updated when finding #7 was incorporated — it still shows secret at Medium via catch-all. This creates a contradictory manual QA step that would either confuse testers or incorrectly pass if someone follows MQ3 literally.

**Required fix**
Update MQ3 line 921 to:
```
9. `kubectl delete secret` → Deny/High (specific resource)
```

---

### P3 - SEC3 named "EnvEscalation" but only tests static properties, not actual escalation

**Problem**
In SEC3 `TestSecurityContainerK8sEnvEscalation` (lines ~855-877), the test verifies `dp.EnvSensitive` and `dp.Severity` — both static struct fields. It does not test that when an environment detector identifies a production context, the severity is actually escalated (e.g., High → Critical).

This is the same issue flagged in the 03b test harness review (SEC4) and the 03c test harness review (SEC3). The test is correctly verifying pre-conditions for escalation, but the name "EnvEscalation" implies it tests the escalation behavior itself.

**Required fix**
Either:
(a) Rename to `TestSecurityContainerK8sEnvSensitivityPreConditions`, or
(b) Add an integration-style assertion demonstrating actual escalation end-to-end with mocked env detection.

---

## Summary

3 findings: 0 P0, 0 P1, 2 P2, 1 P3

**Verdict**: Approved with revisions

The test harness is well-structured with comprehensive coverage: 8 property tests (P1-P8), 7 deterministic example suites (E1-E7), 3 fault injection tests (F1-F3), 3 comparison oracles (O1-O3), 2 benchmarks (B1-B2), 1 stress test (S1), 3 security tests (SEC1-SEC3), and 3 manual QA plans (MQ1-MQ3). The R1 disposition table shows clean incorporation of all 10 R1 findings. The domain-specific property tests — Docker dual-syntax parity (P4), Compose dual-naming parity (P5), split env sensitivity (P3), and kubectl catch-all completeness (P8) — are strong additions. The main gap is SEC2's silent pass-through when no pattern matches, which could mask regressions in the kubectl resource type routing.
