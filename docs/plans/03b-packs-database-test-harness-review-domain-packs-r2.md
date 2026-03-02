# Review: 03b-packs-database-test-harness (domain-packs-r2)

- Source doc: `docs/plans/03b-packs-database-test-harness.md`
- Reviewed commit: f304e71
- Reviewer: domain-packs-r2
- Round: 2

## Findings

### P2 - F3 SQL injection test has incorrect expected match for "comment bypass" case

**Problem**
In F3 (line ~562), the test `TestFaultSQLInjectionStrings` includes:
```go
{"comment bypass", "SELECT * FROM users /* DROP TABLE users */", true},
```

The expected result is `wantMatch: true` for the `psql-drop-table` pattern because `DROP TABLE` appears inside a SQL block comment. The test comment says "FP but acceptable."

However, this specific case (`/* DROP TABLE users */`) would also trigger the `psql-select-safe` (S1) safe pattern because the SQL contains `SELECT`. S1 checks for `Not(SQLContent(\bDROP\b))`, and since `DROP` is present inside the comment, S1's Not clause would correctly exclude this command from safe matching. So both the safe pattern fails AND the destructive pattern matches — this is correct and not a false positive at all. The command genuinely contains DROP TABLE text.

The test and comment are misleading by calling this a "FP". This IS a true positive — the SQL string contains DROP TABLE keywords. The fact that they're in a comment is not something our regex-based heuristic can distinguish, but from a safety perspective, flagging it is correct.

**Required fix**
Update the test comment from "FP but acceptable" to something like "TP — DROP TABLE keyword present in comment text, correctly detected by keyword heuristic". This prevents future maintainers from treating this as a known false positive to be fixed.

---

### P2 - E7 multi-statement edge case E7.4 (`DELETE FROM users\nWHERE id > 100`) may not behave as documented

**Problem**
In E7 (line ~465), the test case:
```
psql -c "DELETE FROM users\nWHERE id > 100"      → Allow (WHERE on next line)
```

expects `Allow` because WHERE is present (the `Not(SQLContent(\bWHERE\b))` check sees WHERE and prevents the delete-no-where pattern from matching).

However, this depends on whether the tree-sitter extractor preserves the literal `\n` as a newline or as the two-character sequence backslash-n. If the `-c` flag value in the extracted command is `DELETE FROM users\nWHERE id > 100` (with literal newline), then `\bWHERE\b` matches and the command is correctly allowed. But if it's the literal characters `\n` (backslash, n), then WHERE is still present after the `\n` substring so it still matches.

The real question is about the `\s+` in `\bDELETE\s+FROM\b` — does `\s+` match across newlines? In Go's `regexp` package, `\s` matches `[\t\n\f\r ]` by default (without needing DOTALL mode). So `DELETE\s+FROM` would match across a newline between DELETE and FROM. This is fine.

Actually the concern is the opposite: this test case documents expected Allow behavior, which is correct. But there's no test case for `psql -c "DELETE FROM users; SELECT 1 WHERE true"` — the known false-negative case documented in §4.2/OQ4. This known-bad case should be in E7 with a comment marking it as a known false negative.

**Required fix**
Add a test case to E7 documenting the known false-negative from §4.2:
```
psql -c "DELETE FROM users; SELECT 1 WHERE true"  → Allow (KNOWN FALSE NEGATIVE — WHERE in second statement masks missing WHERE in DELETE)
```
This makes the test harness explicitly track the known limitation for regression testing.

---

### P2 - B1 benchmark "interactive" test command puts connection args in Args instead of Flags

**Problem**
In B1 (line ~732), the interactive command for PostgreSQL is:
```go
"interactive": cmd("psql", []string{"-h", "localhost", "mydb"}, nil),
```

This puts `-h` and `localhost` in `Args` rather than `Flags`. In a real extracted command, `-h` would be extracted as a flag key with `localhost` as its value, and `mydb` would be in Args. The benchmark test uses a different command structure than the real pipeline would produce.

This doesn't affect correctness (benchmarks still run), but it means the benchmark doesn't accurately reflect real-world matching performance since the pattern matchers will see different `Args` vs `Flags` distributions.

**Required fix**
Update the interactive benchmark command to:
```go
"interactive": cmd("psql", []string{"mydb"}, m("-h", "localhost")),
```
This more accurately represents the extracted command structure and ensures the benchmark reflects real matching paths.

---

### P3 - O2 cross-database consistency test doesn't verify confidence alignment

**Problem**
In O2 (line ~666), the `TestComparisonCrossDatabaseConsistency` test collects and compares severities across databases, but only asserts severity equality. The test collects `confidences` but never asserts on them.

For consistency, equivalent operations should also have the same confidence level. For example, DROP TABLE should be ConfidenceHigh across all three SQL databases. DELETE without WHERE should be ConfidenceMedium across all three. TRUNCATE is an expected divergence (SQLite uses ConfidenceLow).

**Required fix**
Add confidence assertions to O2, with documented expected divergences (e.g., SQLite TRUNCATE ConfidenceLow vs PostgreSQL/MySQL ConfidenceHigh).

---

### P3 - SEC4 env sensitivity test only checks flag and severity, doesn't test actual escalation

**Problem**
In SEC4 (line ~1109), the `TestSecurityEnvSensitivityEscalation` test verifies that patterns have `EnvSensitive: true` and the correct base severity. However, it doesn't test that when the env detector flags a production environment, the severity is actually escalated. The test comment says "Escalation logic tested in env detection module (plan 04)."

This is architecturally correct (escalation is plan 04's responsibility), but the test is named "Escalation" which is misleading — it only tests the pre-conditions for escalation, not escalation itself.

**Required fix**
Rename to `TestSecurityEnvSensitivityPreConditions` or add a brief integration-style test that demonstrates escalation end-to-end (even if the env detection is mocked).

---

## Summary

5 findings: 0 P0, 0 P1, 3 P2, 2 P3

**Verdict**: Approved with revisions

The test harness is comprehensive with good coverage across property tests, deterministic examples, fault injection, security tests, benchmarks, and stress tests. The findings are relatively minor — mainly about test accuracy and documentation. The known false-negative case from §4.2 should be explicitly tracked in the test harness (P2), and a few test commands should better reflect real extracted command structures. Overall, the harness provides strong coverage for the database packs.
