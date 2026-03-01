# 03b: Database Packs — Security & Correctness Review

**Reviewer**: dcg-alt-reviewer (independent review)
**Plan**: [03b-packs-database.md](./03b-packs-database.md)
**Test Harness**: [03b-packs-database-test-harness.md](./03b-packs-database-test-harness.md)
**Date**: 2026-03-01

---

## Summary

Reviewed both the database packs plan and test harness with a focus on:
SQL injection-like bypasses, severity assignments, safe pattern completeness,
and env-sensitive escalation correctness.

**Findings**: 16 total — 2 P0, 4 P1, 6 P2, 4 P3

The most critical findings are: (1) DELETE FROM without WHERE produces a
false negative in multi-statement SQL when WHERE appears in a different
statement, and (2) the safe pattern psql-select-safe doesn't exclude
UPDATE/INSERT, creating a structural bypass that will bite when UPDATE
detection is added. The pg_dump -c short flag is simply missing from the
destructive pattern (implementation bug).

---

## P0: Critical Findings

### DB-P0.1: DELETE FROM without WHERE false negative in multi-statement SQL

**Location**: Plan §4.2, §5.1 (psql-delete-no-where), §5.2 (mysql-delete-no-where), §5.3 (sqlite3-delete-no-where)

**Issue**: The DELETE-without-WHERE detection uses:
```go
packs.SQLContent(`\bDELETE\s+FROM\b`),
packs.Not(packs.SQLContent(`\bWHERE\b`)),
```

The `Not(SQLContent(`\bWHERE\b`))` check looks for WHERE **anywhere** in the
entire argument string, not specifically after the DELETE FROM clause. In
multi-statement SQL, WHERE in a **different** statement causes a false negative.

**Trace**:
```
psql -c "DELETE FROM users; SELECT * FROM orders WHERE active=true"
```
1. SQLContent(`\bDELETE\s+FROM\b`) ✓ — DELETE FROM is present
2. Not(SQLContent(`\bWHERE\b`)) — WHERE IS present (in the SELECT) → false
3. Pattern doesn't match. **Result: "no match" → Allow**

The `DELETE FROM users` (with no WHERE clause) goes completely undetected
because WHERE in the unrelated SELECT satisfies the "has WHERE" check.

**Impact**: False negative. A destructive DELETE FROM without WHERE is not
flagged when combined with any statement containing WHERE. This is the most
common form of multi-statement SQL passed to psql/mysql by LLM agents
(e.g., `DELETE FROM old_data; SELECT count(*) FROM old_data WHERE ...`).

The plan §4.2 notes this is a heuristic and says it "will false-positive on
multi-statement inputs where WHERE appears in a different statement" — but
the terminology is inverted. The actual behavior is a **false negative**
(dangerous command not detected), not a false positive.

**Recommendation**: Two options:
(a) **Use the regex negative lookahead approach** from §4.2's table:
    `(?i)\bDELETE\s+FROM\b(?!.*\bWHERE\b)` — but this has the SAME problem
    since `.*` crosses statement boundaries.
(b) **Split on semicolons first**: Before applying the DELETE-no-WHERE check,
    split the SQL content on `;` and check each statement independently.
    This requires a pre-processing step in the matcher, not just regex.
(c) **Accept the gap but correctly document it**: Change ConfidenceMedium to
    ConfidenceLow and document this as a known false negative for
    multi-statement inputs. This is the minimum fix.

Option (b) is the most correct but requires matcher framework changes. At
minimum, do option (c) and fix the plan's incorrect terminology.

### DB-P0.2: psql-select-safe doesn't exclude UPDATE — structural bypass

**Location**: Plan §5.1, psql-select-safe (S1), mysql-select-safe (S1)

**Issue**: The safe patterns for psql and mysql exclude DROP, TRUNCATE,
DELETE, and ALTER from the `Not()` check. They do **not** exclude UPDATE or
INSERT. This means multi-statement SQL containing both SELECT and UPDATE
bypasses detection:

```
psql -c "SELECT * FROM users; UPDATE users SET admin=true"
```

**Trace**:
1. psql-select-safe: Name("psql") ✓, Flags("-c") ✓,
   SQLContent(`\bSELECT\b`) ✓ (SELECT present),
   Not(DROP|TRUNCATE|DELETE|ALTER) — none present ✓
2. Safe pattern matches → short-circuit, skip destructive patterns
3. **Result: Allow** — UPDATE goes undetected

While there is currently no destructive pattern for UPDATE without WHERE,
this safe pattern design creates a structural bypass: **when UPDATE detection
is added (see DB-P1.3), the safe pattern will silently shadow it.**

Even without a future UPDATE pattern, the safe pattern is too broad: it
treats any SQL with SELECT as safe even when it contains data modification
(UPDATE, INSERT).

**Impact**: Structural design flaw. Currently causes no false negatives
(since there's no UPDATE pattern to bypass), but will cause false negatives
when UPDATE detection is added. The safe pattern should be defense-in-depth
against future pattern additions.

**Recommendation**: Add UPDATE and INSERT to the safe pattern exclusion list:
```go
packs.Not(packs.Or(
    packs.SQLContent(`\bDROP\b`),
    packs.SQLContent(`\bTRUNCATE\b`),
    packs.SQLContent(`\bDELETE\b`),
    packs.SQLContent(`\bALTER\b`),
    packs.SQLContent(`\bUPDATE\b`),   // ADD
    packs.SQLContent(`\bINSERT\b`),   // ADD
)),
```

Apply to all SQL safe patterns (psql-select-safe, mysql-select-safe,
sqlite3-readonly-safe).

---

## P1: High Findings

### DB-P1.1: pg_dump -c (short flag for --clean) missing from destructive pattern

**Location**: Plan §5.1, pg-dump-clean (D6)

**Issue**: The pg-dump-clean destructive pattern checks:
```go
packs.Or(
    packs.Flags("--clean"),
    // Note: pg_dump -c means --clean, but this conflicts with
    // psql -c which means --command. We only match pg_dump here.
),
```

The `Or()` only contains `--clean`. The short flag `-c` is NOT included
despite the comment acknowledging that `pg_dump -c` means `--clean`. The
safe pattern pg-dump-safe **correctly** excludes `-c`:
```go
packs.Not(packs.Flags("-c")), // pg_dump -c means --clean
```

This creates a gap: `pg_dump -c mydb` is correctly excluded from the safe
pattern (because -c is present) but then has no destructive pattern to match
(because pg-dump-clean only checks --clean, not -c).

**Result**: `pg_dump -c mydb` → "no match" → Allow. False negative.

Compare with pg_restore-clean which correctly includes both:
```go
packs.Or(packs.Flags("--clean"), packs.Flags("-c"))
```

**Recommendation**: Add `packs.Flags("-c")` to the pg-dump-clean Or():
```go
packs.Or(
    packs.Flags("--clean"),
    packs.Flags("-c"),
)
```

### DB-P1.2: Redis mixed case evasion not handled

**Location**: Plan §5.5.1, Test harness SEC3

**Issue**: Redis commands are case-insensitive (`FLUSHALL` = `flushall` =
`FlushAll`). The pack explicitly only matches uppercase and lowercase:
```go
packs.ArgAt(0, "FLUSHALL"),
packs.ArgAt(0, "flushall"),
```

The test harness SEC3 explicitly tests `FlushAll` and expects it to NOT
match. This is a known gap, not an oversight.

However, `redis-cli FlushAll` is a valid command that would flush all
databases, and it completely bypasses detection. While uncommon in LLM-
generated commands, it's a trivially exploitable evasion vector.

**Impact**: False negative for mixed-case Redis commands. The fix is
straightforward and has no downsides.

**Recommendation**: Use `ArgContentRegex` with `(?i)` instead of explicit
case matching:
```go
packs.ArgContentRegex(0, `(?i)^FLUSHALL$`),
```

Or, if maintaining the explicit approach, add a normalizer that lowercases
Redis command arguments before matching. The regex approach is simpler.

### DB-P1.3: Missing UPDATE without WHERE pattern across all SQL packs

**Location**: Plan §5.1-5.3 (not present)

**Issue**: `UPDATE users SET password='hacked'` (without WHERE) updates
every row in the table. This is as destructive as `DELETE FROM users`
(without WHERE), potentially more so because it silently corrupts data
rather than visibly deleting it.

None of the three SQL packs (PostgreSQL, MySQL, SQLite) have a destructive
pattern for UPDATE without WHERE. The plan's §4.2 SQL regex patterns table
does not list UPDATE at all.

**Examples not caught**:
- `psql -c "UPDATE users SET active=false"`
- `mysql -e "UPDATE orders SET status='cancelled'"`
- `sqlite3 test.db "UPDATE config SET value='reset'"`

**Impact**: False negative for a common destructive operation. LLM agents
frequently generate UPDATE statements, and UPDATE without WHERE is a
classic "accidental mass-modification" pattern.

**Recommendation**: Add destructive patterns for all three SQL packs:
```go
{
    Name: "psql-update-no-where",
    Match: packs.And(
        packs.Name("psql"),
        packs.SQLContent(`\bUPDATE\b`),
        packs.Not(packs.SQLContent(`\bWHERE\b`)),
    ),
    Severity: guard.Medium,
    Confidence: guard.ConfidenceMedium,
    // Same heuristic limitation as DELETE FROM without WHERE
}
```

The same multi-statement false-negative limitation (DB-P0.1) applies,
but the heuristic still catches the most common case.

### DB-P1.4: Missing DROP SCHEMA CASCADE pattern for PostgreSQL

**Location**: Plan §5.1 (not present), acknowledged in test harness MQ2

**Issue**: `DROP SCHEMA public CASCADE` in PostgreSQL drops all objects
(tables, views, functions, sequences) in the schema. This can be as
destructive as DROP DATABASE since the `public` schema typically contains
all user-created objects.

The test harness MQ2 lists "DROP SCHEMA" as a potential v2 addition, but
given its severity, it should be in v1.

**Recommendation**: Add a destructive pattern:
```go
{
    Name: "psql-drop-schema",
    Match: packs.And(
        packs.Name("psql"),
        packs.SQLContent(`\bDROP\s+SCHEMA\b`),
    ),
    Severity: guard.High,
    Confidence: guard.ConfidenceHigh,
    EnvSensitive: true,
}
```

---

## P2: Medium Findings

### DB-P2.1: Destructive SQL patterns don't require -c/-e flag

**Location**: Plan §5.1, all psql destructive patterns

**Issue**: Destructive patterns like psql-drop-database check:
```go
packs.And(
    packs.Name("psql"),
    packs.SQLContent(`\bDROP\s+DATABASE\b`),
)
```

They do not require `-c` or `--command` flag. Since `SQLContent` has
`CheckFlagValues: true`, it checks ALL flag values including `-h` (host),
`-d` (dbname), `-U` (username), etc.

If a database name coincidentally contains "DROP DATABASE" (e.g.,
`psql -d "DROP DATABASE test" -c "SELECT 1"`), the destructive pattern
would fire — a false positive.

In practice this is extremely unlikely. But the inconsistency with the safe
pattern (which DOES require -c) is worth noting. The destructive patterns
cast a wider net than necessary.

**Recommendation**: Consider adding `-c`/`--command` flag requirement to
psql destructive patterns for consistency. Or accept the wider net as
defense-in-depth (slightly false-positive-prone is safer than
false-negative-prone).

### DB-P2.2: MongoDB .drop() regex could match method names containing "drop"

**Location**: Plan §5.4, mongo-collection-drop (D2)

**Issue**: The regex `\.drop\s*\(` matches `.drop(` but could also match
method names that contain "drop" as a substring within a chain:
- `db.users.dropdown()` — hypothetical but shows the risk
- `db.users.findAndDropDuplicate()` — hypothetical

In practice, MongoDB shell methods are fixed and don't have names containing
"drop" except actual drop operations. The word boundary `\.drop\s*\(` is
actually quite precise because `.` and `(` serve as implicit boundaries.

**Impact**: Very low false positive risk in practice. P2 because the
theoretical bypass exists.

### DB-P2.3: sqlite3-interactive-safe name misleading

**Location**: Plan §5.3, sqlite3-interactive-safe (S2)

**Issue**: The pattern named "sqlite3-interactive-safe" matches ANY sqlite3
command that doesn't contain destructive SQL:
```go
packs.Not(packs.SQLContent(`\b(?:DROP|TRUNCATE|DELETE|ALTER)\b`)),
```

This matches `sqlite3 test.db "CREATE TABLE ..."`, `sqlite3 test.db "INSERT ..."`,
etc. — not just interactive sessions. The name suggests it only matches
`sqlite3 test.db` (no SQL argument), but it's much broader.

**Recommendation**: Either rename to `sqlite3-non-destructive-safe` or
narrow the pattern to actually check for interactive mode (no SQL positional
argument).

### DB-P2.4: Missing DROP INDEX patterns across SQL packs

**Location**: Plan §5.1-5.3 (not present), acknowledged in test harness MQ2

**Issue**: `DROP INDEX` is a destructive operation that can impact query
performance catastrophically (queries go from milliseconds to minutes).
While the data isn't lost, the operational impact can be severe, especially
in production.

Not covered in any SQL pack. Lower priority than DROP SCHEMA but still
worth flagging.

**Recommendation**: Add DROP INDEX patterns at Medium severity with
ConfidenceMedium (since DROP INDEX is sometimes intentional during
migrations).

### DB-P2.5: Plan terminology error — "false-positive" should be "false-negative"

**Location**: Plan §4.2

**Issue**: The plan says: "The 'no WHERE' check uses a negative lookahead.
This is a heuristic — it will **false-positive** on multi-statement inputs
where WHERE appears in a different statement."

This terminology is inverted. When WHERE appears in a different statement,
the heuristic thinks WHERE is present and decides the DELETE is safe. The
DELETE goes undetected. This is a **false negative** (dangerous command not
caught), not a false positive (safe command incorrectly flagged).

**Recommendation**: Fix the terminology in §4.2.

### DB-P2.6: redis-cli SCRIPT FLUSH, CLIENT KILL, DEBUG SEGFAULT not covered

**Location**: Plan §5.5 (not present)

**Issue**: Several Redis commands with operational or security impact are
not covered:
- `SCRIPT FLUSH` — clears all cached Lua scripts
- `CLIENT KILL` — disconnects client connections
- `DEBUG SEGFAULT` — crashes the Redis server (intended for testing)
- `DEBUG SET-ACTIVE-EXPIRE 0` — disables key expiration

`DEBUG SEGFAULT` in particular is essentially a DoS command. The current
redis-cli-interactive-safe pattern excludes "DEBUG" from its safe list,
so these commands would fall through to "no match" → Allow for the
specific ones not in the safe exclusion.

Wait — let me re-check. The interactive-safe pattern excludes `DEBUG`:
```go
packs.ArgAt(0, "DEBUG"),
packs.ArgAt(0, "debug"),
```

So `redis-cli DEBUG SEGFAULT` is NOT matched by the interactive-safe
pattern. But there's no destructive pattern for it either. Result:
"no match" → Allow.

**Recommendation**: Add a destructive pattern for `DEBUG` subcommands at
Medium severity. `SCRIPT FLUSH` could be added to the redis-cli safe
exclusion list and a Low severity destructive pattern.

---

## P3: Low Findings

### DB-P3.1: redis-del-wildcard pattern name misleading

**Location**: Plan §5.5, redis-del-wildcard (D3)

**Issue**: The pattern is named "redis-del-wildcard" but matches ALL
DEL/UNLINK commands, not just wildcard patterns. `redis-cli DEL mykey`
(single key deletion) is flagged with the same severity as mass deletion.

**Recommendation**: Rename to "redis-del" or "redis-key-delete" to
accurately reflect the pattern's scope.

### DB-P3.2: Database name containing SQL keywords could false positive

**Location**: Plan §5.1, all psql destructive patterns with SQLContent

**Issue**: A database named with SQL keywords (e.g., connecting to a DB
named "drop_database_test") could theoretically trigger a false positive
if the name appears in a flag value. Since CheckFlagValues checks ALL
flag values, `-d "drop database test"` would match the DROP DATABASE regex.

Extremely unlikely in practice.

### DB-P3.3: Environment escalation tested only at flag level

**Location**: Test harness SEC4

**Issue**: SEC4 verifies that `EnvSensitive` flags are correctly set on
patterns but doesn't test the actual severity escalation behavior (which
lives in the pipeline/env detection modules). The test says "Escalation
logic tested in env detection module (plan 04)."

This is fine architecturally — the escalation IS tested elsewhere. But the
test harness could benefit from at least one integration test showing the
full escalation path.

### DB-P3.4: mongo keyword word-boundary behavior relies on pre-filter implementation

**Location**: Plan §5.4, keywords

**Issue**: The MongoDB pack has keywords `["mongo", "mongosh", "mongodump", "mongorestore"]`.
The keyword "mongo" is a prefix of the other three. Whether "mongo" matches
within "mongosh" depends on the pre-filter's word-boundary implementation
(plan 02). If word boundaries are incorrect, "mongo" would trigger the pack
for `mongosh` commands even when the pack's Name() matchers are working
correctly — this would just waste time on unnecessary evaluation, not cause
incorrect results.

Not a correctness issue, just a dependency on plan 02's word-boundary
implementation being correct.

---

## Cross-Cutting Observations

### SQL Injection-Like Pattern Bypass

The SQL content matching is fundamentally limited by being a regex heuristic
over command-line argument strings rather than a SQL parser. The main bypass
vectors are:

1. **Multi-statement SQL** (DB-P0.1): WHERE in a different statement
   suppresses DELETE-no-WHERE detection.
2. **SQL in string literals**: `SELECT 'DROP TABLE users'` triggers DROP
   TABLE detection (false positive, but intentionally accepted).
3. **SQL comments**: `-- DROP TABLE users` triggers detection (false
   positive, but intentionally accepted).
4. **File execution**: `psql -f script.sql` bypasses all content-based
   detection (known limitation, documented).
5. **Piped SQL**: `echo "DROP TABLE" | psql` bypasses detection (known
   limitation, documented).

The plan correctly documents 4 and 5 as known limitations. Items 2 and 3
are correctly handled as acceptable false positives. Item 1 is the only
unacknowledged false negative.

### Severity Assignments

Severity assignments are well-calibrated and consistent across database
types:
- **High**: DROP DATABASE, DROP TABLE, TRUNCATE — correct (irreversible
  data loss)
- **Medium**: DELETE FROM without WHERE, ALTER TABLE DROP, flush/clean
  operations — correct (scoped or partially recoverable)
- **ConfidenceMedium** for heuristic patterns (DELETE-no-WHERE, ALTER DROP)
  correctly reflects the regex heuristic nature.

### Safe Pattern Completeness

Safe patterns properly exclude destructive variants with one structural gap
(DB-P0.2: missing UPDATE/INSERT exclusion). The safe patterns for interactive
modes (psql without -c, mysql without -e, mongosh without --eval) are well
designed.

### Env-Sensitive Escalation

The env-sensitivity assignments are sound:
- PostgreSQL/MySQL/MongoDB/Redis: env-sensitive for data-destructive
  operations (DROP, TRUNCATE, DELETE). Non-env-sensitive for operational
  patterns (pg_dump --clean, ALTER TABLE) — these are equally risky in any
  environment.
- SQLite: correctly not env-sensitive (local file database).

One inconsistency: `psql-delete-no-where` is env-sensitive but
`psql-alter-drop` is not. ALTER TABLE DROP in production can be more
impactful than in dev. Consider making psql-alter-drop env-sensitive as
well, or documenting why it's different.
