# 03b: Database Packs — Systems Engineer Review

**Reviewer**: dcg-reviewer (systems engineering focus)
**Date**: 2026-03-01
**Plans Reviewed**:
- [03b-packs-database.md](./03b-packs-database.md)
- [03b-packs-database-test-harness.md](./03b-packs-database-test-harness.md)

**Cross-references**:
- [00-architecture.md](./00-architecture.md) — Layer 3 pack design
- [02-matching-framework.md](./02-matching-framework.md) — ArgContentMatcher, pipeline
- [03a-packs-core.md](./03a-packs-core.md) — Pack authoring guide (§4)
- [shaping.md](../shaping/shaping.md) — §A8 pack scope table

---

## Review Focus

Per assignment: SQL pattern correctness (can DROP TABLE be missed via casing,
extra whitespace, schema qualifiers?), ArgContentMatcher usage, env detection
integration for database packs, MongoDB shell expression parsing, Redis
command coverage, and consistency with the 03a pack authoring template.

---

## Summary

The plan is well-structured and follows the 03a pack authoring template
consistently. The `SQLContent` builder helper with `CheckFlagValues` extension
is a clean cross-plan update that benefits all content-matching packs. SQL
regex patterns are generally solid with `(?i)` case-insensitive matching and
`\s+` whitespace tolerance. Golden file coverage at 61 entries is reasonable.

Main concerns: the DELETE-without-WHERE negative lookahead has a subtle
regex correctness issue (P0-1), Redis mixed-case handling creates a real
evasion gap (P0-2), the `psql-select-safe` pattern can shadow destructive
multi-statement SQL (P1-1), and several MongoDB regex patterns have edge
cases (P1-3, P1-4).

**Findings**: 2 P0, 5 P1, 5 P2, 5 P3

---

## Findings

### P0-1: DELETE-without-WHERE regex negative lookahead is per-statement-fragile

**Location**: §4.2, lines 146-152; §5.1 D5, §5.2 D5, §5.3 D3

**Issue**: The DELETE-no-WHERE pattern uses the regex approach of matching
`\bDELETE\s+FROM\b` and then checking `Not(SQLContent(\bWHERE\b))`. This
is implemented as two separate matchers composed via `And()`:

```go
packs.And(
    packs.Name("psql"),
    packs.SQLContent(`\bDELETE\s+FROM\b`),
    packs.Not(packs.SQLContent(`\bWHERE\b`)),
)
```

The `SQLContent` matcher checks the entire flag value or arg string. For
multi-statement SQL like `psql -c "DELETE FROM users; SELECT * FROM orders WHERE id=1"`,
the `WHERE` from the SELECT statement causes the Not clause to match,
so the DELETE-without-WHERE pattern **fails to match**. This is a false
negative for the most dangerous variant: unbounded DELETE hidden among other
statements.

The plan acknowledges the lookahead fragility (§4.2, line 150-152) and
sets ConfidenceMedium, but the specific multi-statement failure mode is
worse than documented. The "negative lookahead" description in §4.2 implies
`(?!.*\bWHERE\b)` (within the same regex), but the actual implementation
uses two separate SQLContent matchers, which search the entire string
independently.

**Recommendation**: Two options:
(a) Use a single regex with a bounded negative lookahead:
`(?i)\bDELETE\s+FROM\s+\w+\s*(?:;|$)` (match DELETE FROM followed by
table name and then semicolon/EOL, without WHERE in between). This is
harder to get right but more precise.
(b) Accept the limitation and document it explicitly as "multi-statement
SQL with WHERE in another statement will cause false negatives for
DELETE-without-WHERE". Update OQ3 or add a new OQ.

Either way, add test cases for this specific scenario in the test harness.

**Impact**: False negative on unbounded DELETE in multi-statement SQL.

### P0-2: Redis mixed-case commands bypass all patterns

**Location**: §5.5, §5.5.1, test harness SEC3 lines 1029-1059

**Issue**: Redis commands are case-insensitive (`FLUSHALL` = `Flushall` =
`flushall`). The plan explicitly matches only uppercase `FLUSHALL` and
lowercase `flushall` (lines 1060-1064). The test harness SEC3 test at
line 1031 confirms: `FlushAll → wantDeny: false` — mixed case is
intentionally NOT caught.

This is documented as a design choice (§5.5.1, lines 1158-1162), but it's
a genuine evasion vector. While most CLI usage will be uppercase or
lowercase, Redis itself accepts any case. An LLM agent generating
`redis-cli Flushall` or `redis-cli FLUSHall` would bypass detection entirely.

The plan even acknowledges the fix is trivial: "the implementation can
switch to `ArgContentRegex` with `(?i)`" (test harness line 1059). Given
that this is trivial to fix and creates a real evasion gap, it should be
fixed in the initial implementation, not deferred.

**Recommendation**: Use case-insensitive matching for all Redis commands.
Either:
(a) Use `ArgContentRegex` with `(?i)` patterns instead of `ArgAt()` with
explicit upper/lowercase pairs. This is cleaner and catches all case
variations.
(b) Use a custom `ArgAtCaseInsensitive(idx, value)` matcher that does
`strings.EqualFold()`.

Option (b) is simpler since Redis commands are exact matches (no regex
needed). This could be a small addition to the plan 02 matcher DSL.

**Impact**: False negative on all mixed-case Redis commands.

### P1-1: `psql-select-safe` shadows multi-statement destructive SQL

**Location**: §5.1 S1, lines 269-291

**Issue**: The `psql-select-safe` pattern matches when:
- Command is `psql`
- Has `-c` or `--command` flag
- Content matches SELECT or EXPLAIN
- Content does NOT match DROP, TRUNCATE, DELETE, or ALTER

For `psql -c "SELECT 1; DROP TABLE users"`, the safe pattern correctly
does NOT match (the Not clause catches DROP). This is tested in E7
(test harness line 427). Good.

However, consider `psql -c "EXPLAIN DROP TABLE users"` — this is technically
valid PostgreSQL (EXPLAIN analyzes the plan for any statement). The safe
pattern would NOT match because DROP is present in the Not clause. But
the actual command IS exploratory/read-only (EXPLAIN doesn't execute the
DROP). This is a minor false positive — EXPLAIN with destructive SQL gets
flagged. Acceptable but worth documenting.

More concerning: `psql -c "SELECT 'hello'"` without DROP/TRUNCATE/DELETE/ALTER
matches the safe pattern and short-circuits the pack. This means any psql
command with SELECT and without destructive keywords is allowed. What about
`psql -c "SELECT * FROM users; INSERT INTO log VALUES('test')"`? The INSERT
is a write operation but not in the Not-list. The safe pattern allows it
through.

**Recommendation**: Add INSERT, UPDATE, and GRANT to the Not-clause of
`psql-select-safe`, or document that non-destructive write operations
(INSERT, UPDATE) are intentionally not flagged. Given DCG's purpose as a
"mistake-preventer" for destructive operations, this is probably fine —
INSERT/UPDATE don't destroy data — but it should be an explicit decision.

**Impact**: Non-destructive write operations pass through as "safe" due to
SELECT-based safe pattern. Low risk since they don't destroy data.

### P1-2: `pg_dump -c` flag means `--clean` but also caught by `psql-select-safe`

**Location**: §5.1 S2 (pg-dump-safe, line 295-299) and D6 (pg-dump-clean,
lines 397-413)

**Issue**: The plan correctly notes that `pg_dump -c` means `--clean`
(line 405), which is different from `psql -c` meaning `--command`. The
`pg-dump-safe` pattern excludes both `--clean` and `-c` for pg_dump.

However, consider `pg_dump -c mydb`: this correctly matches `pg-dump-clean`
(destructive). But the pattern only checks `packs.Flags("--clean")` in the
Or clause — looking at the code more carefully, line 400-407 shows:

```go
Match: packs.And(
    packs.Name("pg_dump"),
    packs.Or(
        packs.Flags("--clean"),
        // Note: pg_dump -c means --clean, but this conflicts with
        // psql -c which means --command. We only match pg_dump here.
    ),
),
```

Wait — the Or clause only has ONE arm: `packs.Flags("--clean")`. The
comment mentions `-c` but it's not in the Or clause. This means `pg_dump -c`
(short form of --clean) would NOT match the destructive pattern, and WOULD
match the safe pattern `pg-dump-safe` (which excludes `--clean` but only
the long form, and excludes `-c` separately — actually looking at lines
297-298: `Not(packs.Flags("--clean"))` and `Not(packs.Flags("-c"))`, so the
safe pattern does correctly exclude `-c`).

The problem is on the destructive side: `pg-dump-clean` only checks for
`Flags("--clean")`, not `Flags("-c")`. So `pg_dump -c mydb` is excluded
from the safe pattern (good) AND not matched by the destructive pattern
(bad). It falls through as "no match" → Allow.

**Recommendation**: Add `packs.Flags("-c")` to the `pg-dump-clean`
destructive pattern's Or clause. The comment on line 404-405 shows the
author was aware of the `-c` issue but forgot to add it to the Or.

**Impact**: `pg_dump -c` (short form of --clean) bypasses destructive
detection.

### P1-3: MongoDB `\.drop\s*\(` regex matches `dropDatabase` too broadly

**Location**: §5.4 D2, lines 867-883

**Issue**: The `mongo-collection-drop` pattern (D2) uses:
```go
packs.SQLContent(`\.drop\s*\(`),
packs.Not(packs.SQLContent(`dropDatabase`)),
```

This catches `db.users.drop()` (collection drop) while excluding
`db.dropDatabase()`. However, the regex `\.drop\s*\(` will also match:
- `db.users.dropIndex("idx_name")` — which drops a single index, not the
  collection
- `db.users.dropIndexes()` — drops all non-_id indexes

Both are destructive but semantically different from collection drop.
`dropIndex` is more targeted (Medium/ConfidenceHigh) while `drop()` is
complete collection destruction (High). Currently both get classified as
High/ConfidenceHigh via `mongo-collection-drop`, which over-classifies
`dropIndex`.

**Recommendation**: Either:
(a) Add specific patterns for `dropIndex` and `dropIndexes` at Medium
severity, and exclude them from the `mongo-collection-drop` pattern with
`Not(SQLContent(\`dropIndex\`))`.
(b) Accept the over-classification and document it. For a mistake-preventer,
over-classifying is better than under-classifying.

Option (b) is simpler and defensible. But document it in §5.4.1.

**Impact**: Over-classification of `dropIndex`/`dropIndexes` as High instead
of Medium.

### P1-4: MongoDB `deleteMany({})` empty-filter regex is too strict

**Location**: §5.4 D3, lines 887-902

**Issue**: The `mongo-delete-many-all` pattern matches:
```
deleteMany\s*\(\s*\{\s*\}\s*\)
```

This requires exactly `deleteMany({})` with optional whitespace. But
MongoDB's JavaScript syntax allows variations:
- `db.users.deleteMany( {} )` — whitespace inside parens (matched by `\s*`)
- `db.users.deleteMany({  })` — whitespace inside braces (matched)
- `db.users.deleteMany()` — no argument (uses default empty filter in
  some MongoDB versions)

The last case — `deleteMany()` with no argument — is NOT matched because
the regex requires `\{\s*\}`. In older MongoDB versions, `deleteMany()` is
equivalent to `deleteMany({})`. This is a false negative.

Also, `db.users.deleteMany(null)` or `db.users.deleteMany(undefined)` could
behave as delete-all in some MongoDB drivers.

**Recommendation**: Extend the regex to also match `deleteMany()` with empty
parens: `deleteMany\s*\(\s*(?:\{\s*\})?\s*\)`. This catches both
`deleteMany({})` and `deleteMany()`. Document that `deleteMany(null)` and
`deleteMany(undefined)` are not caught (edge case).

**Impact**: False negative on `deleteMany()` without explicit empty filter.

### P1-5: `ArgContent` vs `SQLContent` vs `ArgContentRegex` naming confusion

**Location**: §4.4 (CheckFlagValues), §5.3 (sqlite), throughout

**Issue**: The plan introduces several related matchers/builders:
- `ArgContent(substring)` — checks args for substring (from plan 02)
- `ArgContentRegex(regex)` — checks args for regex (from plan 02)
- `ArgContentMatcher{..., CheckFlagValues: true}` — struct with optional flag checking
- `SQLContent(pattern)` — convenience builder, wraps `ArgContentMatcher` with `(?i)` and `CheckFlagValues: true`

The SQLite pack (§5.3) uses BOTH `SQLContent` and `ArgContent`:
- Line 690: `packs.ArgContent("^\\.")` for sqlite3 dot-commands
- Line 706: `packs.Not(packs.SQLContent(...))`

`ArgContent` checks only args, while `SQLContent` checks args AND flag
values. Since sqlite3 passes SQL as positional args (not flags), using
`SQLContent` is correct but unnecessary — `ArgContent` would suffice.
More importantly, the inconsistency is confusing for pack authors.

Also, `ArgContent("^\\.")` uses a regex-like pattern `^` but `ArgContent`
is substring-based (from plan 02's definition). If `ArgContent` does
substring matching, `^` is literal, not a regex anchor. The plan may be
assuming `ArgContent` does regex matching.

**Recommendation**:
(a) Clarify in §4 whether `ArgContent` does substring or regex matching.
Cross-reference plan 02's definition.
(b) Consider adding a note in the pack authoring guide about when to use
`ArgContent` vs `SQLContent` vs `ArgContentRegex`.
(c) Fix the sqlite `ArgContent("^\\.")` if `ArgContent` is substring-based
— it should use `ArgContentRegex` or a new `ArgContentPrefix` matcher.

**Impact**: Potential build error if ArgContent doesn't support regex. Pack
author confusion about which matcher to use.

### P2-1: `psql --command` long form not in destructive patterns

**Location**: §5.1 D1-D8

**Issue**: All psql destructive patterns use `packs.Name("psql")` and
`packs.SQLContent(...)` but don't explicitly require `-c` or `--command`
flags. `SQLContent` with `CheckFlagValues: true` checks all flag values.

This means `psql --command "DROP TABLE users"` will match because `SQLContent`
finds `DROP TABLE` in the `--command` flag value. Good.

But what about `psql -c "SELECT 1" --command "DROP TABLE users"` (two SQL
flags)? The safe pattern S1 requires `-c` OR `--command` AND SELECT content.
If `-c` has SELECT and `--command` has DROP TABLE, the safe pattern checks
all flag values: it finds SELECT (positive match) but also finds DROP
(negative match via Not clause). The Not clause correctly prevents the safe
match. The destructive pattern then finds DROP TABLE. Correct behavior.

However, the E7 test case for `--command` (test harness line 435) is:
`psql --command "DROP TABLE users" → Deny/High`. This is good coverage.
But there's no test for the dual-flag case described above. Add one.

**Recommendation**: Add a golden file entry or test case for
`psql -c "SELECT 1" --command "DROP TABLE users"` to verify correct
behavior with multiple SQL-carrying flags.

**Impact**: Untested edge case (likely works correctly, but verify).

### P2-2: Shaping doc lists `DELETE FROM (no WHERE)` for PostgreSQL/MySQL but plan doesn't list `DROP DATABASE` for SQLite

**Location**: Shaping doc line 86, §5.3

**Issue**: The shaping doc's sqlite entry lists: `DROP TABLE`, `.drop`,
`DELETE FROM (no WHERE)`. The plan covers all three. However, the plan also
adds `sqlite3-truncate` (D4) which catches TRUNCATE even though it's not
valid SQLite SQL. This is fine — it's a "catch intent" pattern at
ConfidenceLow.

The plan does NOT have a `DROP DATABASE` pattern for SQLite, which makes
sense (SQLite doesn't have databases in the traditional sense — the "database"
is just a file). No issue, just confirming the shaping alignment is correct.

However, the shaping doc doesn't mention `ALTER TABLE DROP` for SQLite,
and the plan doesn't have it either. SQLite does support `ALTER TABLE DROP
COLUMN` (since version 3.35.0). This could be added as a Low/ConfidenceMedium
pattern, but it's a minor gap.

**Recommendation**: Consider adding `ALTER TABLE DROP COLUMN` for SQLite
(present in PostgreSQL and MySQL packs). Low priority since it's less
commonly generated by LLMs for SQLite.

**Impact**: Minor coverage gap for SQLite ALTER TABLE DROP.

### P2-3: `mongo` keyword will trigger pack for ALL commands starting with `mongo`

**Location**: §5.4, line 798

**Issue**: The MongoDB pack keywords are `["mongo", "mongosh", "mongodump", "mongorestore"]`.
The keyword `"mongo"` is a prefix of `mongosh`, `mongodump`, and
`mongorestore`. The Aho-Corasick pre-filter with word-boundary matching
will trigger on all of these commands, which is correct since they all
belong to the same pack.

However, `mongo` is also a prefix of `mongodb` (common in paths like
`/usr/local/mongodb/bin/`). If a command name after normalization is
`mongodb` (unlikely but possible), the pre-filter would trigger but no
patterns would match (no `Name("mongodb")` pattern exists). This is a
harmless false trigger.

More relevant: the `mongo` keyword also exists in `mongos` (MongoDB
shard router), which is not covered by the pack. Running destructive
operations through `mongos --eval "db.dropDatabase()"` would trigger the
pre-filter (keyword `mongo` matches) but NOT match any pattern (no
`Name("mongos")` check).

**Recommendation**: Add `packs.Name("mongos")` to the MongoDB destructive
patterns' Or clauses alongside `mongosh` and `mongo`. `mongos` supports
`--eval` and can execute all the same destructive JavaScript expressions.

**Impact**: False negative for destructive commands executed through `mongos`.

### P2-4: Redis `SET` command in interactive-safe exclusion list is too broad

**Location**: §5.5 S2, lines 1039-1040

**Issue**: The `redis-cli-interactive-safe` pattern excludes `SET` and `set`
from matching (lines 1039-1040). This means `redis-cli SET mykey myvalue`
does NOT match the safe pattern and falls through to destructive matching.
But there's no destructive pattern for `SET` — it falls through as "no match"
→ Allow.

This is the correct outcome (SET is not destructive). But the exclusion of
SET from the safe pattern is unnecessary and creates complexity. The purpose
of the exclusion list is to prevent the interactive-safe pattern from matching
commands that should be caught by destructive patterns. Since there's no
destructive SET pattern, excluding it serves no purpose.

Similarly, `DEBUG` is excluded (line 1043-1044) but has no corresponding
destructive pattern. `DEBUG` commands can be dangerous (e.g.,
`DEBUG SEGFAULT`), so either add a destructive pattern or document why it's
excluded.

**Recommendation**: Either (a) remove SET and DEBUG from the interactive-safe
exclusion list, or (b) add destructive patterns for them. For `SET`, remove
from exclusion. For `DEBUG`, add a destructive pattern:
`redis-debug` at Medium severity since `DEBUG SEGFAULT` crashes Redis.

**Impact**: Unnecessary complexity in safe pattern. Missing destructive
pattern for `redis-cli DEBUG SEGFAULT`.

### P2-5: Summary says "80+" golden file entries but actual count is 61

**Location**: §1 line 42, §6 line 1603-1604

**Issue**: The summary (§1, line 42) says "80+ golden file entries across
all 5 packs" but the actual count in §6 is 61. Same inconsistency as found
in 03a plan.

**Recommendation**: Fix to "61 entries" or add more entries to reach 80+.

**Impact**: Documentation inconsistency.

### P3-1: `pg-dump-clean` and `pg-restore-clean` are NOT env-sensitive

**Location**: §5.1 D6, D7

**Issue**: The `pg-dump-clean` and `pg-restore-clean` patterns have
`EnvSensitive: false`. The rationale is that `pg_dump --clean` generates
DROP statements in the dump output (risk is at restore time, not dump time),
and `pg_restore --clean` drops objects.

However, `pg_restore --clean` executed against a production database is
significantly more dangerous than against a dev database. This is the same
argument that applies to DROP TABLE. The fact that it's bundled with a
restore operation doesn't change the destructiveness of the DROP phase.

**Recommendation**: Consider making `pg-restore-clean` env-sensitive.
`pg-dump-clean` doesn't execute DDL so it's correctly not env-sensitive.
But `pg-restore --clean` actively drops and recreates objects.

**Impact**: `pg_restore --clean` against production doesn't get severity
escalation. Low impact if the base Medium severity is sufficient.

### P3-2: Redis `CONFIG SET` doesn't distinguish read-only configs from dangerous ones

**Location**: §5.5 D4, lines 1112-1133

**Issue**: The `redis-config-set` pattern matches any `CONFIG SET` command
at Medium severity. But `CONFIG SET loglevel debug` is benign (changes log
level), while `CONFIG SET maxmemory 1` could cause data loss (eviction),
and `CONFIG SET requirepass ""` removes authentication (security risk).

All are treated equally at Medium severity.

**Recommendation**: This is fine for v1 — distinguishing dangerous config
parameters from benign ones would require an enumeration of all Redis
config options. Document as a potential v2 refinement. The Medium severity
is appropriate as a "proceed with caution" signal.

**Impact**: Minor — over-classification of benign CONFIG SET operations.

### P3-3: No test for psql `\dt` and other backslash meta-commands in safe pattern

**Location**: §5.1 S1, line 282; test harness E1

**Issue**: The `psql-select-safe` pattern includes `packs.SQLContent(\`\\\\d\`)`
to match psql meta-commands like `\dt`, `\d+`, `\dn`. However, there's no
golden file entry or E1 test case for `psql -c "\dt"` or `psql -c "\d users"`.

These are common LLM-generated commands (schema inspection) and should be
verified.

**Recommendation**: Add test cases for:
- `psql -c "\dt"` → Allow (safe pattern)
- `psql -c "\d users"` → Allow (safe pattern)
- `psql -c "\d+"` → Allow (safe pattern)

**Impact**: Untested safe pattern path for common commands.

### P3-4: Test harness P4 SQL case insensitivity test doesn't test actual pack patterns

**Location**: Test harness P4, lines 126-166

**Issue**: The P4 property test generates random case variations and tests
them against a generic regex. But it doesn't test the actual pack patterns
with these variations. The test constructs its own regex:
```go
re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(kw[:4]) + `\b`)
```

This doesn't validate that the pack patterns themselves handle case
correctly — it only validates that case-insensitive regex works in general.

**Recommendation**: Modify P4 to generate random case variations and test
them against the actual pack destructive patterns (e.g., feed
`psql -c "dRoP tAbLe users"` through `psql-drop-table`'s matcher).

**Impact**: Property test doesn't actually test the code under test.

### P3-5: No mention of `createdb --template` risk

**Location**: §5.1 S3, line 302-305

**Issue**: `createdb` is classified as universally safe. However,
`createdb --template production_db new_db` copies an entire production
database. While this is read-only from the source database's perspective
(it doesn't modify `production_db`), it could be dangerous if it triggers
heavy I/O on a production server or exposes production data.

This is a very edge case and probably not worth a pattern. But it should
be mentioned in a note similar to the "Notes" sections for other packs.

**Recommendation**: Add a note in §5.1.1 about `createdb --template`
as a known benign-but-potentially-expensive operation that we don't flag.

**Impact**: Documentation completeness.

---

## Shaping Doc Cross-Reference (§A8)

| Shaping Pattern | Plan Coverage | Status |
|----------------|---------------|--------|
| **PostgreSQL** | | |
| `DROP TABLE` | D3 psql-drop-table (High) | Covered |
| `DROP DATABASE` | D1 psql-drop-database (High) | Covered |
| `TRUNCATE` | D4 psql-truncate (High) | Covered |
| `DELETE FROM` (no WHERE) | D5 psql-delete-no-where (Medium/ConfMed) | Covered (but see P0-1) |
| `dropdb` | D2 dropdb (High) | Covered |
| **MySQL** | | |
| `DROP TABLE` | D3 mysql-drop-table (High) | Covered |
| `DROP DATABASE` | D1 mysql-drop-database (High) | Covered |
| `TRUNCATE` | D4 mysql-truncate (High) | Covered |
| `DELETE FROM` (no WHERE) | D5 mysql-delete-no-where (Medium/ConfMed) | Covered |
| `mysqladmin drop` | D2 mysqladmin-drop (High) | Covered |
| **SQLite** | | |
| `DROP TABLE` | D1 sqlite3-drop-table (High) | Covered |
| `.drop` | D2 sqlite3-dot-drop (High/ConfMed) | Covered |
| `DELETE FROM` (no WHERE) | D3 sqlite3-delete-no-where (Medium/ConfMed) | Covered |
| **MongoDB** | | |
| `db.dropDatabase()` | D1 mongo-drop-database (High) | Covered |
| `db.collection.drop()` | D2 mongo-collection-drop (High) | Covered |
| `db.collection.deleteMany({})` | D3 mongo-delete-many-all (Medium/ConfHigh) | Covered |
| **Redis** | | |
| `FLUSHALL` | D1 redis-flushall (High) | Covered |
| `FLUSHDB` | D2 redis-flushdb (High) | Covered |
| `DEL *` | D3 redis-del-wildcard (Medium/ConfMed) | Covered (any DEL, not just wildcard) |

All shaping doc patterns are accounted for. Additional patterns beyond
shaping: `ALTER TABLE DROP`, `pg_dump --clean`, `pg_restore --clean`,
`mysqladmin flush`, `sqlite3 TRUNCATE`, `mongorestore --drop`,
`deleteMany(filter)`, `remove({})`, `CONFIG SET`, `SHUTDOWN`. These are
reasonable additions.

**Gaps identified**:
- `mongos` not covered (P2-3)
- `redis-cli DEBUG SEGFAULT` not covered (P2-4)
- SQLite `ALTER TABLE DROP COLUMN` not covered (P2-2)

---

## Consistency with 03a Pack Authoring Template

The plan follows the 03a template well:

1. **File structure** (§4.1) — Correctly uses `internal/packs/database/` ✓
2. **Pack file structure** (§4.2) — init() + var declaration ✓
3. **Pack ID convention** (§4.3) — `database.{tool}` ✓
4. **Keywords** — Specific command names ✓
5. **Safe patterns** — Short-circuit with Not-exclusions ✓
6. **Destructive patterns** — Severity, Confidence, Reason, Remediation ✓
7. **Test file template** (§4.7) — Table-driven with match + near-miss ✓
8. **Golden file entries** (§4.8) — 3+ entries per pattern ✓
9. **Registration** (§4.9) — init() + blank import ✓
10. **Reachability tests** — Every destructive pattern has one ✓
11. **Pattern interaction matrix** — Present for PostgreSQL ✓

Deviations:
- Pattern interaction matrices only shown for PostgreSQL, not MySQL/SQLite/
  MongoDB/Redis (minor — they're simpler packs)
- `EnvSensitive` field used for first time (correctly documented)
- New `SQLContent` builder introduced (good — extends DSL cleanly)

---

## Disposition Summary

| Priority | Count | Findings |
|----------|-------|----------|
| P0 | 2 | DELETE-no-WHERE multi-stmt false negative (P0-1), Redis mixed-case evasion (P0-2) |
| P1 | 5 | psql-select-safe INSERT/UPDATE gap (P1-1), pg_dump -c not in destructive (P1-2), MongoDB dropIndex over-classification (P1-3), deleteMany() empty-parens (P1-4), ArgContent vs SQLContent naming (P1-5) |
| P2 | 5 | dual SQL flag test missing (P2-1), SQLite ALTER TABLE DROP gap (P2-2), mongos not covered (P2-3), Redis DEBUG/SET in exclusion (P2-4), golden count mismatch (P2-5) |
| P3 | 5 | pg-restore-clean not env-sensitive (P3-1), CONFIG SET granularity (P3-2), psql meta-command tests missing (P3-3), P4 test doesn't test real patterns (P3-4), createdb --template note (P3-5) |
