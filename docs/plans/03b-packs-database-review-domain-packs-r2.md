# Review: 03b-packs-database (domain-packs-r2)

- Source doc: `docs/plans/03b-packs-database.md`
- Test harness: `docs/plans/03b-packs-database-test-harness.md`
- Reviewed commit: f304e71
- Reviewer: domain-packs-r2
- Round: 2

## Findings

### P1 - SQLite S2 (sqlite3-non-destructive-safe) may shadow D2 (sqlite3-dot-drop)

**Problem**
In §5.3 (line ~797), the safe pattern `sqlite3-non-destructive-safe` (S2) uses:
```go
packs.Not(packs.SQLContent(`\b(?:DROP|TRUNCATE|DELETE|ALTER|UPDATE)\b`)),
packs.Not(packs.ArgContentRegex(`\.drop`)),
```

The `SQLContent` helper wraps `ArgContentRegex` with `CheckFlagValues: true` and `(?i)` prefix, making the pattern `(?i)\b(?:DROP|TRUNCATE|DELETE|ALTER|UPDATE)\b`. The word-boundary `\b` before `DROP` means the regex needs a word boundary before the keyword.

For the `.drop` command (e.g., `sqlite3 test.db ".drop trigger foo"`), the first Not clause uses `SQLContent(\bDROP\b)` which matches the word `DROP` in `.drop` — actually wait, `.drop` contains `drop` which matches `\bDROP\b` since the `.` is a non-word character providing the boundary. So the first Not clause would catch `.drop trigger foo` as containing the word `DROP`.

However, the concern is the interaction between S2 and D2. D2 (`sqlite3-dot-drop`) uses `packs.ArgContent(\\.drop)` — this is a **substring** matcher, not a regex matcher. `ArgContent` does substring matching against `cmd.Args` **only** (no flag values). The literal string being matched is `\.drop`.

This creates a discrepancy: D2 uses `ArgContent(\\.drop)` which is a **literal substring** match for the backslash-dot-drop string `\.drop`. But the actual argument value would be `.drop trigger foo` — no literal backslash. The backslash in the ArgContent parameter is not regex-interpreted since ArgContent does substring matching. So D2 is matching the literal string `\.drop` (backslash + dot + drop) which will **never** appear in real arguments.

**Required fix**
D2 should use `packs.ArgContentRegex(\\.drop)` (regex matcher where `\\.` matches a literal dot) or `packs.ArgContent(".drop")` (literal substring match for dot-drop). The current `packs.ArgContent(\\.drop)` matches the literal string backslash-dot-drop, which is not what `.drop trigger foo` contains. This means D2 is **unreachable** — it will never match any real command.

Verify by checking whether `ArgContent` interprets its argument as a Go raw string or processes escape sequences. If the plan intends the Go source to be `packs.ArgContent("\\.drop")`, Go interprets `\\.` as `\.` (literal backslash-dot), which still doesn't match `.drop`. The correct form is `packs.ArgContent(".drop")`.

---

### P2 - mysqladmin-drop D2 uses undefined `packs.Arg()` builder

**Problem**
In §5.2 (line ~640), the `mysqladmin-drop` (D2) pattern uses:
```go
packs.Or(
    packs.ArgAt(0, "drop"),
    packs.Arg("drop"),
),
```

The `packs.Arg("drop")` builder function is not defined anywhere in this plan doc, the matcher naming table in §4.5, or referenced in plan 02's matching framework. The §4.5 table lists `ArgContent(substring)`, `ArgContentRegex(pattern)`, `SQLContent(pattern)`, `ArgAt(idx, value)`, and `ArgAtFold(idx, value)` — but no `Arg()`.

This is likely intended to be `packs.ArgContent("drop")` for a positional-agnostic substring match, to catch `mysqladmin` commands where `drop` may not be at position 0 (e.g., `mysqladmin -u root drop mydb` where `-u root` shifts arg positions). But this needs to be clarified since `Arg()` is not a documented builder.

Also note the same `packs.Arg()` usage in D7 (`mysqladmin-flush`) at line ~713 with `packs.Arg("flush-hosts")`, `packs.Arg("flush-logs")`, etc.

**Required fix**
Either: (a) Define `packs.Arg()` as a new builder in the matcher naming table §4.5 with clear semantics (e.g., exact match against any positional arg), or (b) Replace all `packs.Arg()` calls with the appropriate existing builder (`ArgContent()` for substring matching, or a new `ArgAny()` for exact matching at any position). Specify the exact semantics — substring vs exact match matters for "drop" which could substring-match other words.

---

### P2 - MongoDB D6 (mongo-delete-many) negative lookahead regex may be fragile

**Problem**
In §5.4 (line ~1054), the `mongo-delete-many` (D6) pattern uses:
```go
packs.SQLContent(`deleteMany\s*\(`),
packs.Not(packs.SQLContent(`deleteMany\s*\(\s*(?:\{\s*\})?\s*\)`)),
```

This D6 matches `deleteMany(` but NOT `deleteMany({})` or `deleteMany()`. The intent is to catch `deleteMany` with a non-empty filter at Medium/ConfidenceMedium while D3 catches the empty-filter case at Medium/ConfidenceHigh.

However, the negative pattern `deleteMany\s*\(\s*(?:\{\s*\})?\s*\)` requires a closing `)` to match. For an invocation like `mongosh --eval "db.users.deleteMany({status: 'inactive'})"`, the D3 pattern checks `deleteMany\s*\(\s*(?:\{\s*\})?\s*\)` against the full eval string. The `(?:\{\s*\})?\s*\)` part means: optionally match `{}`, then match `)`. For `deleteMany({status: 'inactive'})`, the regex engine sees `deleteMany(` then tries the optional `{}` — it doesn't match `{status:`, so it skips it, then looks for `\s*\)`. The next character after `(` is `{`, not `)`, so the overall D3 regex **doesn't** match this string. Good — D6 will correctly catch it.

But consider `deleteMany( )` (with a space but no braces). D3's regex: `deleteMany\s*\(\s*(?:\{\s*\})?\s*\)` — after `deleteMany(`, it sees space, the optional `{}` group doesn't match, then `\s*` matches the space, then `\)` matches. So D3 matches `deleteMany( )`. This means D3 catches `deleteMany()`, `deleteMany( )`, and `deleteMany({})`. The Not clause in D6 would exclude these. This seems correct.

No required fix for this specific item; upon analysis the regex logic is correct. Withdrawing this as a finding.

---

### P2 - Inconsistent EnvSensitive for pg-dump-clean (D6) and psql-alter-drop (D8)

**Problem**
In §5.1 (line ~458), `pg-dump-clean` (D6) has `EnvSensitive: false`. The rationale is that pg_dump is a backup tool and the dump file itself is not dangerous. This is correct — generating a dump with DROP statements isn't environment-specific.

However, `psql-alter-drop` (D8) at line ~488 also has `EnvSensitive: false`. ALTER TABLE DROP COLUMN in production is significantly more dangerous than in development — it permanently removes a column and all its data. This is more directly destructive than pg_dump --clean (which only generates SQL).

For cross-pack consistency, other plan docs (03c, 03d) mark all structurally destructive operations as env-sensitive. ALTER TABLE DROP COLUMN in production could cause data loss and application failures, which is the core scenario env sensitivity is designed for.

**Required fix**
Consider changing `psql-alter-drop` (D8) `EnvSensitive` to `true`, consistent with how other packs handle operations that permanently modify schema in production. If the intent is to keep it false, add a design note explaining why ALTER TABLE DROP COLUMN is not env-sensitive when DROP TABLE is.

---

### P2 - mysql-alter-drop (D6) also not env-sensitive, same concern

**Problem**
In §5.2 (line ~705), MySQL `mysql-alter-drop` (D6) has `EnvSensitive: false`, same as the PostgreSQL equivalent. This creates a gap where `ALTER TABLE users DROP COLUMN email` on a production MySQL database would not have elevated severity.

**Required fix**
Same recommendation as above — consider making ALTER TABLE DROP operations env-sensitive for consistency, or document the rationale for excluding them.

---

### P2 - Redis S2 (redis-cli-interactive-safe) exclusion list may miss new destructive commands

**Problem**
In §5.5 (line ~1143), Redis safe pattern S2 uses a hardcoded exclusion list:
```go
packs.Not(packs.Or(
    packs.ArgAtFold(0, "FLUSHALL"),
    packs.ArgAtFold(0, "FLUSHDB"),
    packs.ArgAtFold(0, "DEL"),
    packs.ArgAtFold(0, "UNLINK"),
    packs.ArgAtFold(0, "CONFIG"),
    packs.ArgAtFold(0, "DEBUG"),
    packs.ArgAtFold(0, "SHUTDOWN"),
)),
```

This exclusion list must be kept in sync with the destructive patterns D1-D6. If a new destructive pattern is added in v2 (e.g., `SWAPDB`, `SCRIPT FLUSH`, `CLIENT KILL` as mentioned in §13 OQ6), the safe pattern S2 must be updated simultaneously or it will shadow the new destructive pattern.

The other SQL packs avoid this by using positive matching for safe patterns (e.g., psql-select-safe requires SELECT/EXPLAIN content). The Redis S2 uses a negative exclusion approach which is more maintenance-prone.

**Required fix**
Add a design note in §5.5.1 explicitly documenting that S2's exclusion list must be updated whenever new destructive patterns are added. Alternatively, consider restructuring S2 to use S1's positive-match approach (match only known safe commands like GET, SET, KEYS, etc.) rather than "anything not in the destructive list." The current S1 already covers read-only commands — S2's main purpose is to catch "interactive mode" (no command arg at all). For `redis-cli -h localhost` with no command, `Args` would be empty, so `ArgAtFold(0, ...)` would return false for all checks. This means S2 already works for interactive mode without the exclusion list. For `redis-cli SET mykey myvalue`, the SET command at ArgAt(0) is not in S1's safe list — it falls through as Indeterminate. This is correct behavior.

---

### P3 - Test harness P3 env sensitivity test only checks pack-level, not pattern-level

**Problem**
In the test harness (line ~92), `TestPropertyEnvSensitivityConsistency` checks whether each pack has *at least one* env-sensitive pattern. But it doesn't verify which specific patterns are env-sensitive vs. not. The plan doc states PostgreSQL has 7 of 10 destructive patterns env-sensitive (D6 pg-dump-clean and D8 psql-alter-drop are not). A bug where D1-D4 lost their env-sensitive flags would pass P3 as long as D5 still had it.

The unit test `TestPgEnvSensitiveFlags` in §7.1 (line ~1999) does check per-pattern, but the test harness P3 property test should be strengthened to match.

**Required fix**
Enhance P3 to verify pattern-level env sensitivity counts (e.g., PostgreSQL should have exactly 7 env-sensitive destructive patterns out of 10, or verify specific pattern names). The per-pack unit tests already do this — P3 in the harness could simply cross-reference.

---

### P3 - Test harness missing explicit test for Redis CONFIG GET safe handling

**Problem**
The Redis pack has `redis-config-set` (D4) matching `CONFIG` + `SET`/`RESETSTAT`. But `redis-cli CONFIG GET maxmemory` should be safe. S1 doesn't include CONFIG GET (it lists GET, KEYS, INFO, etc. but not CONFIG). S2 excludes CONFIG entirely from its safe match.

So `redis-cli CONFIG GET maxmemory` would match S2 — wait, S2 excludes `ArgAtFold(0, "CONFIG")`, so CONFIG GET falls through as Indeterminate. This means a read-only operation (`CONFIG GET`) is not classified as safe.

This isn't a bug in the plan (Indeterminate is acceptable), but the test harness should explicitly test `redis-cli CONFIG GET` to document expected behavior.

**Required fix**
Add `redis-cli CONFIG GET maxmemory` as an edge case in E5 with expected result "Indeterminate (no match)" and add a design note explaining CONFIG GET is not safe-listed because the CONFIG namespace includes destructive subcommands. Alternatively, add CONFIG GET to S1 with appropriate Not guards.

---

## Summary

7 findings: 0 P0, 1 P1, 4 P2, 2 P3

**Verdict**: Approved with revisions

The plan is substantially correct after R1 incorporation. The P1 finding about sqlite3 `ArgContent` vs `ArgContentRegex` for `.drop` matching is the most concerning — it appears to make the D2 pattern unreachable. The P2 findings about the undefined `Arg()` builder and env-sensitivity consistency should be addressed before implementation to avoid confusion and cross-pack inconsistency. The overall architecture, pattern design, and test coverage are solid.
