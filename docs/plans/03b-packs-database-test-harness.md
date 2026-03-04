# 03b: Database Packs — Test Harness

**Plan**: [03b-packs-database.md](./03b-packs-database.md)
**Architecture**: [00-architecture.md](./00-architecture.md)
**Core Pack Test Harness**: [03a-packs-core-test-harness.md](./03a-packs-core-test-harness.md)

---

## Overview

This document specifies the test harness for the database packs (plan 03b).
It covers property-based tests, deterministic examples, fault injection,
comparison oracles, benchmarks, stress tests, security tests, manual QA,
CI tier mapping, and exit criteria.

The test harness complements the unit tests described in the plan doc §7.
Unit tests verify individual pattern behavior. This harness verifies
system-level properties, cross-pattern interactions, and robustness.

Database packs introduce testing challenges not present in core packs:
- **SQL content matching via regex** — requires case variation, whitespace, and
  multi-statement testing that goes beyond simple flag/arg matching
- **Environment sensitivity** — 4 of 5 packs escalate severity in production,
  requiring env-aware test fixtures
- **CheckFlagValues extension** — a cross-plan change to ArgContentMatcher
  that must be tested in isolation and in combination with packs
- **Heuristic patterns** — DELETE FROM without WHERE uses regex negative
  lookahead with ConfidenceMedium; edge cases need thorough coverage

---

## P: Property-Based Tests

### P1: Every Destructive Pattern Has a Matching Command

**Invariant**: For each destructive pattern in each database pack, there
exists at least one `ExtractedCommand` that the pattern matches.

This is the same property as 03a P1, applied to database packs. The shared
test in the registry test suite covers all packs automatically. Database-
specific reachability commands are documented in §7.1 of the plan doc.

```go
func TestPropertyEveryDbDestructivePatternReachable(t *testing.T) {
    dbPacks := []packs.Pack{pgPack, mysqlPack, sqlitePack, mongoPack, redisPack}
    for _, pack := range dbPacks {
        for _, dp := range pack.Destructive {
            t.Run(pack.ID+"/"+dp.Name, func(t *testing.T) {
                cmd := getReachabilityCommand(pack.ID, dp.Name)
                assert.True(t, dp.Match.Match(cmd),
                    "pattern %s has no matching reachability command", dp.Name)
            })
        }
    }
}
```

### P2: Safe Patterns Never Match Destructive Reachability Commands

**Invariant**: For each destructive pattern's reachability command, no safe
pattern in the same pack matches it.

This is critical for database packs because safe patterns (e.g.,
`psql-select-safe`) use `Not(SQLContent(...))` exclusions to avoid blocking
destructive patterns. A subtle regex error could cause a safe pattern to
shadow a destructive one.

```go
func TestPropertyDbSafePatternsNeverBlockDestructive(t *testing.T) {
    dbPacks := []packs.Pack{pgPack, mysqlPack, sqlitePack, mongoPack, redisPack}
    for _, pack := range dbPacks {
        for _, dp := range pack.Destructive {
            cmd := getReachabilityCommand(pack.ID, dp.Name)
            for _, sp := range pack.Safe {
                assert.False(t, sp.Match.Match(cmd),
                    "safe pattern %s blocks destructive %s in pack %s",
                    sp.Name, dp.Name, pack.ID)
            }
        }
    }
}
```

### P3: Environment Sensitivity Consistency

**Invariant**: For each database pack, the env-sensitive destructive patterns
match the expected set documented in the plan.

This is unique to database packs — core packs have no env-sensitive patterns.

```go
func TestPropertyEnvSensitivityConsistency(t *testing.T) {
    // Packs that should have env-sensitive patterns
    envSensitivePacks := map[string]bool{
        "database.postgresql": true,
        "database.mysql":      true,
        "database.mongodb":    true,
        "database.redis":      true,
        "database.sqlite":     false,
    }

    dbPacks := []packs.Pack{pgPack, mysqlPack, sqlitePack, mongoPack, redisPack}
    for _, pack := range dbPacks {
        t.Run(pack.ID, func(t *testing.T) {
            hasEnvSensitive := false
            for _, dp := range pack.Destructive {
                if dp.EnvSensitive {
                    hasEnvSensitive = true
                    break
                }
            }
            expected := envSensitivePacks[pack.ID]
            assert.Equal(t, expected, hasEnvSensitive,
                "pack %s env sensitivity mismatch", pack.ID)
        })
    }

    // SQLite must have zero env-sensitive patterns
    for _, dp := range sqlitePack.Destructive {
        assert.False(t, dp.EnvSensitive,
            "sqlite3 pattern %s should not be env-sensitive", dp.Name)
    }
}
```

### P4: SQL Regex Case Insensitivity

**Invariant**: For every SQL-based destructive pattern, the actual pack
matcher handles case variations correctly. This tests the real patterns,
not a generic regex.

This uses property-based testing with generated case variations applied
to actual pack matchers.

```go
func TestPropertySQLCaseInsensitivity(t *testing.T) {
    // Test actual pack patterns with random case variations
    patternTests := []struct {
        packID  string
        pattern string
        sqlBase string // base SQL to vary case on
        tool    string
        flag    string
    }{
        {"database.postgresql", "psql-drop-table", "DROP TABLE users", "psql", "-c"},
        {"database.postgresql", "psql-drop-database", "DROP DATABASE myapp", "psql", "-c"},
        {"database.postgresql", "psql-truncate", "TRUNCATE TABLE orders", "psql", "-c"},
        {"database.mysql", "mysql-drop-table", "DROP TABLE users", "mysql", "-e"},
        {"database.mysql", "mysql-drop-database", "DROP DATABASE myapp", "mysql", "-e"},
        {"database.postgresql", "psql-drop-schema", "DROP SCHEMA public CASCADE", "psql", "-c"},
    }

    f := func(seed int64) bool {
        rng := rand.New(rand.NewSource(seed))
        for _, pt := range patternTests {
            varied := randomCase(rng, pt.sqlBase)
            cmd := parse.ExtractedCommand{
                Name:  pt.tool,
                Flags: map[string]string{pt.flag: varied},
            }
            pack := packByID(pt.packID)
            dp := findDestructiveByName(pack, pt.pattern)
            if !dp.Match.Match(cmd) {
                return false
            }
        }
        return true
    }
    quick.Check(f, &quick.Config{MaxCount: 1000})
}

func randomCase(rng *rand.Rand, s string) string {
    result := make([]byte, len(s))
    for i := range s {
        if rng.Intn(2) == 0 {
            result[i] = byte(unicode.ToUpper(rune(s[i])))
        } else {
            result[i] = byte(unicode.ToLower(rune(s[i])))
        }
    }
    return string(result)
}
```

### P5: CheckFlagValues Extension Correctness

**Invariant**: When `CheckFlagValues` is true, `ArgContentMatcher` checks
both `cmd.Args` and `cmd.Flags` values. When false, only `cmd.Args` is
checked.

This tests the cross-plan change to plan 02's `ArgContentMatcher`.

```go
func TestPropertyCheckFlagValuesExtension(t *testing.T) {
    pattern := regexp.MustCompile(`(?i)\bDROP TABLE\b`)

    // With CheckFlagValues=true, should match in flag values
    matcherWithFlags := packs.ArgContentMatcher{
        Regex:           pattern,
        AtIndex:         -1,
        CheckFlagValues: true,
    }

    // With CheckFlagValues=false, should NOT match in flag values
    matcherWithoutFlags := packs.ArgContentMatcher{
        Regex:           pattern,
        AtIndex:         -1,
        CheckFlagValues: false,
    }

    // SQL in flag value only
    cmdFlagOnly := parse.ExtractedCommand{
        Name:  "psql",
        Args:  []string{},
        Flags: map[string]string{"-c": "DROP TABLE users"},
    }

    // SQL in args only
    cmdArgOnly := parse.ExtractedCommand{
        Name:  "psql",
        Args:  []string{"DROP TABLE users"},
        Flags: nil,
    }

    // With flags: matches both locations
    assert.True(t, matcherWithFlags.Match(cmdFlagOnly))
    assert.True(t, matcherWithFlags.Match(cmdArgOnly))

    // Without flags: only matches args
    assert.False(t, matcherWithoutFlags.Match(cmdFlagOnly))
    assert.True(t, matcherWithoutFlags.Match(cmdArgOnly))
}
```

### P6: No Destructive Pattern Matches Empty Command

**Invariant**: An empty `ExtractedCommand` matches no destructive pattern
in any database pack.

```go
func TestPropertyDbEmptyCommandMatchesNothing(t *testing.T) {
    empty := parse.ExtractedCommand{}
    dbPacks := []packs.Pack{pgPack, mysqlPack, sqlitePack, mongoPack, redisPack}
    for _, pack := range dbPacks {
        for _, dp := range pack.Destructive {
            assert.False(t, dp.Match.Match(empty),
                "pattern %s/%s matches empty command", pack.ID, dp.Name)
        }
    }
}
```

### P7: Cross-Pack Pattern Isolation

**Invariant**: A PostgreSQL-specific command does not trigger MySQL,
SQLite, MongoDB, or Redis patterns, and vice versa.

```go
func TestPropertyCrossPackIsolation(t *testing.T) {
    packCommands := map[string]parse.ExtractedCommand{
        "database.postgresql": cmd("psql", nil, m("-c", "DROP TABLE users")),
        "database.mysql":      cmd("mysql", nil, m("-e", "DROP TABLE users")),
        "database.sqlite":     cmd("sqlite3", []string{"test.db", "DROP TABLE users"}, nil),
        "database.mongodb":    cmd("mongosh", nil, m("--eval", "db.users.drop()")),
        "database.redis":      cmd("redis-cli", []string{"FLUSHALL"}, nil),
    }

    dbPacks := map[string]packs.Pack{
        "database.postgresql": pgPack,
        "database.mysql":      mysqlPack,
        "database.sqlite":     sqlitePack,
        "database.mongodb":    mongoPack,
        "database.redis":      redisPack,
    }

    for cmdPackID, testCmd := range packCommands {
        for packID, pack := range dbPacks {
            if packID == cmdPackID {
                continue // Same pack — expected to match
            }
            for _, dp := range pack.Destructive {
                assert.False(t, dp.Match.Match(testCmd),
                    "%s command triggers %s/%s",
                    cmdPackID, packID, dp.Name)
            }
        }
    }
}
```

---

## E: Deterministic Examples

### E1: database.postgresql Pattern Matrix (25 cases)

Complete test matrix using `InteractivePolicy` for decision column.
See plan doc §6.1 for the full golden file entries.

```
# Destructive — HIGH
psql -c "DROP DATABASE myapp"                   → Deny/High (psql-drop-database)
psql -h localhost -c "DROP DATABASE production"  → Deny/High (psql-drop-database)
dropdb myapp_production                          → Deny/High (dropdb)
psql -c "DROP TABLE users"                       → Deny/High (psql-drop-table)
psql -c "DROP TABLE IF EXISTS sessions"          → Deny/High (psql-drop-table)
psql -c "TRUNCATE TABLE orders"                  → Deny/High (psql-truncate)
psql -c "drop table users"                       → Deny/High (psql-drop-table, case insensitive)
psql -c "DROP SCHEMA public CASCADE"             → Deny/High (psql-drop-schema)

# Destructive — MEDIUM
psql -c "DELETE FROM users"                      → Ask/Medium (psql-delete-no-where)
psql -c "UPDATE users SET active=false"          → Ask/Medium (psql-update-no-where)
pg_dump --clean mydb > backup.sql                → Ask/Medium (pg-dump-clean)
pg_dump -c mydb > backup.sql                     → Ask/Medium (pg-dump-clean, short flag)
pg_restore --clean backup.dump                   → Ask/Medium (pg-restore-clean)
psql -c "ALTER TABLE users DROP COLUMN email"    → Ask/Medium (psql-alter-drop)

# Safe / Edge
psql -c "SELECT * FROM users"                    → Allow (psql-select-safe)
psql -c "\dt"                                    → Allow (psql-select-safe, meta-command)
psql -c "\d+ users"                              → Allow (psql-select-safe, meta-command)
pg_dump mydb > backup.sql                        → Allow (pg-dump-safe)
createdb myapp_test                              → Allow (createdb-safe)
psql -h localhost mydb                           → Allow (psql-interactive-safe)
pg_restore backup.dump                           → Allow (pg-restore-safe)
psql -c "DELETE FROM users WHERE id = 1"         → Allow (no destructive match)
psql -c "UPDATE users SET active=false WHERE id=1" → Allow (no destructive match)
psql -f migrate.sql                              → Allow (no match — file execution gap)
```

### E2: database.mysql Pattern Matrix (14 cases)

```
# Destructive — HIGH
mysql -e "DROP DATABASE myapp"                   → Deny/High (mysql-drop-database)
mysqladmin drop myapp_production                 → Deny/High (mysqladmin-drop)
mysql -e "DROP TABLE users"                      → Deny/High (mysql-drop-table)
mysql -e "TRUNCATE TABLE orders"                 → Deny/High (mysql-truncate)

# Destructive — MEDIUM
mysql -e "DELETE FROM users"                     → Ask/Medium (mysql-delete-no-where)
mysql -e "UPDATE users SET active=false"         → Ask/Medium (mysql-update-no-where)
mysql -e "ALTER TABLE users DROP COLUMN email"   → Ask/Medium (mysql-alter-drop)
mysqladmin flush-tables                          → Ask/Medium (mysqladmin-flush)

# Safe / Edge
mysql -e "SELECT * FROM users"                   → Allow (mysql-select-safe)
mysqldump mydb > backup.sql                      → Allow (mysqldump-safe)
mysql -h localhost mydb                          → Allow (mysql-interactive-safe)
mysqladmin status                                → Allow (mysqladmin-readonly-safe)
mysql -e "DELETE FROM users WHERE id = 1"        → Allow (no destructive match)
mysql -e "UPDATE users SET active=false WHERE id=1" → Allow (no destructive match)
```

### E3: database.sqlite Pattern Matrix (9 cases)

```
# Destructive — HIGH
sqlite3 test.db "DROP TABLE users"               → Deny/High (sqlite3-drop-table)
sqlite3 test.db ".drop trigger update_timestamp"  → Deny/High (sqlite3-dot-drop)

# Destructive — MEDIUM
sqlite3 test.db "DELETE FROM users"              → Ask/Medium (sqlite3-delete-no-where)
sqlite3 test.db "UPDATE config SET value='x'"    → Ask/Medium (sqlite3-update-no-where)

# Safe / Edge
sqlite3 test.db "SELECT * FROM users"            → Allow (sqlite3-readonly-safe)
sqlite3 test.db ".tables"                        → Allow (sqlite3-readonly-safe)
sqlite3 test.db                                  → Allow (sqlite3-non-destructive-safe)
sqlite3 test.db "DELETE FROM users WHERE id = 1" → Allow (no destructive match)
sqlite3 test.db "UPDATE config SET value='x' WHERE key='debug'" → Allow (no destructive match)
```

### E4: database.mongodb Pattern Matrix (11 cases)

```
# Destructive — HIGH
mongosh --eval "db.dropDatabase()"               → Deny/High (mongo-drop-database)
mongo --eval "db.dropDatabase()"                 → Deny/High (mongo-drop-database)
mongosh --eval "db.users.drop()"                 → Deny/High (mongo-collection-drop)

# Destructive — MEDIUM
mongosh --eval "db.users.deleteMany({})"         → Ask/Medium (mongo-delete-many-all)
mongosh --eval "db.users.remove({})"             → Ask/Medium (mongo-remove-all)
mongorestore --drop dump/                        → Ask/Medium (mongorestore-drop)
mongosh --eval "db.users.deleteMany({status: 'inactive'})" → Ask/Medium (mongo-delete-many)

# Safe / Edge
mongodump --out /backup/                         → Allow (mongodump-safe)
mongosh --eval "db.users.find()"                 → Allow (mongosh-readonly-safe)
mongosh mongodb://localhost:27017/mydb           → Allow (mongosh-interactive-safe)
mongosh --eval "show dbs"                        → Allow (mongosh-readonly-safe)
```

### E5: database.redis Pattern Matrix (16 cases)

```
# Destructive — HIGH
redis-cli FLUSHALL                               → Deny/High (redis-flushall)
redis-cli flushall                               → Deny/High (redis-flushall)
redis-cli FlushAll                               → Deny/High (redis-flushall, ArgAtFold case-insensitive)
redis-cli -h prod.redis.example.com FLUSHALL     → Deny/High (redis-flushall)
redis-cli FLUSHDB                                → Deny/High (redis-flushdb)

# Destructive — MEDIUM
redis-cli DEL mykey                              → Ask/Medium (redis-key-delete)
redis-cli UNLINK session:12345                   → Ask/Medium (redis-key-delete)
redis-cli CONFIG SET maxmemory 100mb             → Ask/Medium (redis-config-set)
redis-cli CONFIG GET maxmemory                   → Allow (redis-cli-readonly-safe does not include CONFIG; expected no destructive match)
redis-cli SHUTDOWN                               → Ask/Medium (redis-shutdown)
redis-cli DEBUG SEGFAULT                         → Ask/Medium (redis-debug)
redis-cli DEBUG SLEEP 9999                       → Ask/Medium (redis-debug)

# Safe / Edge
redis-cli GET mykey                              → Allow (redis-cli-readonly-safe)
redis-cli INFO                                   → Allow (redis-cli-readonly-safe)
redis-cli KEYS "user:*"                          → Allow (redis-cli-readonly-safe)
redis-cli -h localhost                           → Allow (redis-cli-interactive-safe)
redis-cli PING                                   → Allow (redis-cli-readonly-safe)
```

### E6: Cross-Pack Non-Interference (6 cases)

Verify that database packs from different systems don't interfere:

```
# PostgreSQL command should not trigger MySQL pack
psql -c "DROP TABLE users"                       → Matches database.postgresql only
mysql -e "DROP TABLE users"                      → Matches database.mysql only

# MongoDB command should not trigger SQL packs
mongosh --eval "db.dropDatabase()"               → Matches database.mongodb only

# Redis command should not trigger any other pack
redis-cli FLUSHALL                               → Matches database.redis only

# Similar SQL across different tools
psql -c "DROP DATABASE test" && mysql -e "DROP DATABASE test"
    → Both packs match independently

# sqlite3 should not be env-sensitive even with same SQL
sqlite3 test.db "DROP TABLE users"               → High severity (no escalation)
psql -c "DROP TABLE users"                       → High (escalates to Critical in prod)
```

### E7: SQL Content Edge Cases (14 cases)

SQL-specific edge cases that stress the regex patterns:

```
# Case variations
psql -c "Drop Table users"                      → Deny/High (case insensitive)
psql -c "DROP   TABLE   users"                  → Deny/High (whitespace tolerance)
mysql -e "drop database   myapp"                 → Deny/High (case + whitespace)

# Multi-statement SQL
psql -c "SELECT 1; DROP TABLE users"             → Deny/High (destructive wins)
mysql -e "SELECT 1; TRUNCATE orders"             → Deny/High (destructive wins)

# Dual SQL flag (both -c and --command)
psql -c "SELECT 1" --command "DROP TABLE users"  → Deny/High (DROP in --command flag value)

# DELETE FROM with WHERE variations
psql -c "DELETE FROM users WHERE 1=1"            → Allow (has WHERE — even if trivial)
psql -c "DELETE FROM users\nWHERE id > 100"      → Allow (WHERE on next line)
psql -c "DELETE FROM users; SELECT 1 WHERE true" → Allow (KNOWN FALSE NEGATIVE — WHERE in second statement masks DELETE without WHERE)

# SQL in --command long form
psql --command "DROP TABLE users"                → Deny/High (--command = -c)
mysql --execute "DROP TABLE users"               → Deny/High (--execute = -e)

# Quoted identifiers
psql -c 'DROP TABLE "public"."users"'            → Deny/High (quoted table name)
mysql -e "DROP TABLE \`users\`"                  → Deny/High (backtick-quoted)

# Comment-containing SQL (not a bypass — regex matches keywords anywhere)
psql -c "-- comment\nDROP TABLE users"           → Deny/High (keyword still present)
```

---

## F: Fault Injection

### F1: Nil/Empty Fields in ExtractedCommand

Test that all database pack matchers handle degenerate inputs gracefully.
This is especially important because database patterns use `ArgContentMatcher`
with regex, which could panic on nil inputs.

```go
func TestFaultDbNilFields(t *testing.T) {
    dbPacks := []packs.Pack{pgPack, mysqlPack, sqlitePack, mongoPack, redisPack}
    degenerateCmds := []parse.ExtractedCommand{
        {Name: "psql", Args: nil, Flags: nil},
        {Name: "mysql", Args: nil, Flags: nil},
        {Name: "redis-cli", Args: nil, Flags: nil},
        {Name: "mongosh", Args: nil, Flags: nil},
        {Name: "sqlite3", Args: nil, Flags: nil},
        {Name: "psql", Args: []string{}, Flags: map[string]string{}},
        {Name: "", Args: nil, Flags: nil},
        {Name: "psql", Args: []string{""}, Flags: map[string]string{"": ""}},
        {Name: "psql", Flags: map[string]string{"-c": ""}}, // empty -c value
    }

    for _, pack := range dbPacks {
        for i, c := range degenerateCmds {
            t.Run(fmt.Sprintf("%s/degenerate-%d", pack.ID, i), func(t *testing.T) {
                for _, dp := range pack.Destructive {
                    assert.NotPanics(t, func() { dp.Match.Match(c) })
                }
                for _, sp := range pack.Safe {
                    assert.NotPanics(t, func() { sp.Match.Match(c) })
                }
            })
        }
    }
}
```

### F2: ArgContentMatcher with Pathological Regex Input

Test that `ArgContentMatcher` handles adversarial string inputs without
excessive backtracking or panics. While Go's `regexp` package guarantees
linear-time matching (RE2 semantics), this test verifies no unexpected
behavior with large or unusual inputs.

```go
func TestFaultPathologicalRegexInput(t *testing.T) {
    pattern := packs.SQLContent(`\bDROP\s+TABLE\b`)

    pathological := []string{
        strings.Repeat("DROP ", 10000),                  // Very long repeated keyword
        strings.Repeat("a", 100000),                     // Very long no-match string
        "DROP\x00TABLE users",                           // Null byte in middle
        strings.Repeat("DROP TABLE ", 100) + "WHERE 1=1", // Long multi-keyword
        string(make([]byte, 1000000)),                   // 1MB null bytes
    }

    for i, s := range pathological {
        t.Run(fmt.Sprintf("pathological-%d", i), func(t *testing.T) {
            cmd := parse.ExtractedCommand{
                Name: "psql",
                Flags: map[string]string{"-c": s},
            }
            assert.NotPanics(t, func() { pattern.Match(cmd) })
        })
    }
}
```

### F3: Flag Value with SQL Injection Attempts

Test that patterns handle SQL-injection-style strings without misclassification:

```go
func TestFaultSQLInjectionStrings(t *testing.T) {
    injectionStrings := []struct {
        name      string
        sql       string
        wantMatch bool // Whether DROP TABLE pattern should match
    }{
        {"union select", "' UNION SELECT * FROM users--", false},
        {"comment bypass", "SELECT * FROM users /* DROP TABLE users */", true}, // TP for keyword heuristic
        {"string literal", "SELECT 'DROP TABLE is dangerous'", true}, // FP but acceptable
        {"double dash comment", "SELECT 1 -- DROP TABLE users", true}, // FP but acceptable
        {"nested quotes", `SELECT "DROP TABLE users" FROM dual`, true}, // FP but acceptable
    }

    dropTablePattern := findDestructive(pgPack, "psql-drop-table")
    for _, tt := range injectionStrings {
        t.Run(tt.name, func(t *testing.T) {
            cmd := parse.ExtractedCommand{
                Name:  "psql",
                Flags: map[string]string{"-c": tt.sql},
            }
            got := dropTablePattern.Match.Match(cmd)
            assert.Equal(t, tt.wantMatch, got, "sql: %s", tt.sql)
        })
    }
}
```

**Note**: False positives on commented-out or string-literal SQL keywords
are acceptable — we intentionally over-match rather than under-match. The
regex is a content heuristic, not a SQL parser. These FPs are documented
as known behavior.

---

## O: Comparison Oracle Tests

### O1: Upstream Rust Version Comparison

Compare database pack results against the upstream Rust
`destructive-command-guard` for shared database commands. This test identifies
intentional improvements and potential regressions.

```go
func TestComparisonDbUpstreamRust(t *testing.T) {
    if testing.Short() {
        t.Skip("comparison tests require upstream binary")
    }
    corpus := loadComparisonCorpus(t, "testdata/comparison/database_commands.txt")
    for _, entry := range corpus {
        t.Run(entry.Command, func(t *testing.T) {
            goResult := pipeline.Run(ctx, entry.Command, cfg)
            rustResult := runUpstream(t, entry.Command)

            if goResult.Decision != rustResult.Decision {
                t.Logf("DIVERGENCE: %q go=%v rust=%v category=%s",
                    entry.Command, goResult.Decision, rustResult.Decision,
                    categorizeDivergence(goResult, rustResult))
            }
        })
    }
}
```

**Comparison corpus**: All 75 golden file commands from §6 of the plan doc,
plus additional edge cases:
- Mixed-case SQL across all database tools
- Connection flag combinations (`-h`, `-p`, `-u`, `-a`)
- Long SQL statements with multiple clauses
- All 5 database interactive modes

### O2: Cross-Database Consistency

For equivalent destructive operations across SQL databases (PostgreSQL,
MySQL, SQLite), verify that severity and confidence assignments are
consistent (except explicitly documented divergences like SQLite TRUNCATE):

```go
func TestComparisonCrossDatabaseConsistency(t *testing.T) {
    equivalents := []struct {
        name     string
        commands map[string]parse.ExtractedCommand
    }{
        {
            "DROP TABLE",
            map[string]parse.ExtractedCommand{
                "postgresql": cmd("psql", nil, m("-c", "DROP TABLE users")),
                "mysql":      cmd("mysql", nil, m("-e", "DROP TABLE users")),
                "sqlite":     cmd("sqlite3", []string{"test.db", "DROP TABLE users"}, nil),
            },
        },
        {
            "TRUNCATE",
            map[string]parse.ExtractedCommand{
                "postgresql": cmd("psql", nil, m("-c", "TRUNCATE users")),
                "mysql":      cmd("mysql", nil, m("-e", "TRUNCATE users")),
                // sqlite3 TRUNCATE is ConfidenceLow (not valid SQL) — expected divergence
            },
        },
        {
            "DELETE FROM no WHERE",
            map[string]parse.ExtractedCommand{
                "postgresql": cmd("psql", nil, m("-c", "DELETE FROM users")),
                "mysql":      cmd("mysql", nil, m("-e", "DELETE FROM users")),
                "sqlite":     cmd("sqlite3", []string{"test.db", "DELETE FROM users"}, nil),
            },
        },
    }

    for _, eq := range equivalents {
        t.Run(eq.name, func(t *testing.T) {
            var severities []guard.Severity
            var confidences []guard.Confidence
            for db, testCmd := range eq.commands {
                pack := packForDB(db)
                for _, dp := range pack.Destructive {
                    if dp.Match.Match(testCmd) {
                        severities = append(severities, dp.Severity)
                        confidences = append(confidences, dp.Confidence)
                        t.Logf("%s: severity=%v confidence=%v pattern=%s",
                            db, dp.Severity, dp.Confidence, dp.Name)
                        break
                    }
                }
            }
            // All matching databases should have same severity
            for i := 1; i < len(severities); i++ {
                assert.Equal(t, severities[0], severities[i],
                    "%s severity inconsistency", eq.name)
            }
            // Confidence should also align for equivalent operations.
            // Expected divergence: SQLite TRUNCATE is ConfidenceLow.
            for i := 1; i < len(confidences); i++ {
                assert.Equal(t, confidences[0], confidences[i],
                    "%s confidence inconsistency", eq.name)
            }
        })
    }
}
```

### O3: Policy Monotonicity

For each database golden file entry, verify that stricter policies never
allow what looser policies deny (same as 03a O2, extended to database packs):

```go
func TestComparisonDbPolicyMonotonicity(t *testing.T) {
    entries := golden.LoadCorpus(t, "testdata/golden/database_*.txt")
    restrictiveness := map[guard.Decision]int{guard.Allow: 0, guard.Ask: 1, guard.Deny: 2}

    for _, entry := range entries {
        t.Run(entry.Description, func(t *testing.T) {
            strict := pipeline.Run(ctx, entry.Command, strictCfg)
            inter := pipeline.Run(ctx, entry.Command, interCfg)
            perm := pipeline.Run(ctx, entry.Command, permCfg)

            sr := restrictiveness[strict.Decision]
            ir := restrictiveness[inter.Decision]
            pr := restrictiveness[perm.Decision]

            assert.GreaterOrEqual(t, sr, ir)
            assert.GreaterOrEqual(t, ir, pr)
        })
    }
}
```

---

## B: Benchmarks

### B1: Per-Pack SQL Pattern Matching Throughput

Database packs have higher per-pattern cost than core packs due to regex
evaluation. Benchmark to establish baselines and detect regressions.

```go
func BenchmarkPostgresPackMatch(b *testing.B) {
    commands := map[string]parse.ExtractedCommand{
        "safe-select":     cmd("psql", nil, m("-c", "SELECT * FROM users")),
        "drop-database":   cmd("psql", nil, m("-c", "DROP DATABASE myapp")),
        "drop-table":      cmd("psql", nil, m("-c", "DROP TABLE users")),
        "delete-no-where": cmd("psql", nil, m("-c", "DELETE FROM users")),
        "interactive":     cmd("psql", []string{"mydb"}, m("-h", "localhost")),
        "dropdb":          cmd("dropdb", []string{"myapp"}, nil),
    }
    for name, c := range commands {
        b.Run(name, func(b *testing.B) {
            for i := 0; i < b.N; i++ {
                matchPack(pgPack, c)
            }
        })
    }
}

func BenchmarkMySQLPackMatch(b *testing.B) {
    commands := map[string]parse.ExtractedCommand{
        "safe-select":   cmd("mysql", nil, m("-e", "SELECT * FROM users")),
        "drop-database": cmd("mysql", nil, m("-e", "DROP DATABASE myapp")),
        "mysqladmin":    cmd("mysqladmin", []string{"drop", "myapp"}, nil),
        "interactive":   cmd("mysql", []string{"mydb"}, m("-h", "localhost")),
    }
    for name, c := range commands {
        b.Run(name, func(b *testing.B) {
            for i := 0; i < b.N; i++ {
                matchPack(mysqlPack, c)
            }
        })
    }
}

func BenchmarkMongoPackMatch(b *testing.B) {
    commands := map[string]parse.ExtractedCommand{
        "safe-find":      cmd("mongosh", nil, m("--eval", "db.users.find()")),
        "drop-database":  cmd("mongosh", nil, m("--eval", "db.dropDatabase()")),
        "delete-many":    cmd("mongosh", nil, m("--eval", "db.users.deleteMany({})")),
        "interactive":    cmd("mongosh", []string{"mongodb://localhost:27017"}, nil),
    }
    for name, c := range commands {
        b.Run(name, func(b *testing.B) {
            for i := 0; i < b.N; i++ {
                matchPack(mongoPack, c)
            }
        })
    }
}

func BenchmarkRedisPackMatch(b *testing.B) {
    commands := map[string]parse.ExtractedCommand{
        "safe-get":    cmd("redis-cli", []string{"GET", "mykey"}, nil),
        "flushall":    cmd("redis-cli", []string{"FLUSHALL"}, nil),
        "del":         cmd("redis-cli", []string{"DEL", "mykey"}, nil),
        "interactive": cmd("redis-cli", []string{"-h", "localhost"}, nil),
    }
    for name, c := range commands {
        b.Run(name, func(b *testing.B) {
            for i := 0; i < b.N; i++ {
                matchPack(redisPack, c)
            }
        })
    }
}

func BenchmarkSQLitePackMatch(b *testing.B) {
    commands := map[string]parse.ExtractedCommand{
        "safe-select": cmd("sqlite3", []string{"test.db", "SELECT * FROM users"}, nil),
        "drop-table":  cmd("sqlite3", []string{"test.db", "DROP TABLE users"}, nil),
        "interactive": cmd("sqlite3", []string{"test.db"}, nil),
    }
    for name, c := range commands {
        b.Run(name, func(b *testing.B) {
            for i := 0; i < b.N; i++ {
                matchPack(sqlitePack, c)
            }
        })
    }
}
```

**Targets** (initial — adjust after baseline measurement):
- Safe pattern match (short-circuit, no regex eval): < 150ns per command
- Destructive pattern match (with regex): < 500ns per command
- Full pack evaluation (all patterns): < 1000ns per command

Database packs have ~2-3x higher latency than core packs due to regex
evaluation in `ArgContentMatcher`. The first implementation should establish
actual baselines before freezing targets.

### B2: Database Golden File Corpus Throughput

```go
func BenchmarkDbGoldenCorpus(b *testing.B) {
    entries := golden.LoadCorpus(b, "testdata/golden/database_*.txt")
    pipeline := setupBenchPipeline(b)
    cfg := &evalConfig{policy: InteractivePolicy()}

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        for _, e := range entries {
            pipeline.Run(context.Background(), e.Command, cfg)
        }
    }
}
```

**Target**: Full database corpus (75 entries) < 2ms total.

### B3: Regex Compilation Overhead

Verify that `SQLContent` regex patterns are compiled once at init time,
not per-match:

```go
func BenchmarkSQLContentRegexCompilation(b *testing.B) {
    // This should be near-zero per-call because regex is pre-compiled
    pattern := packs.SQLContent(`\bDROP\s+TABLE\b`)
    cmd := parse.ExtractedCommand{
        Name:  "psql",
        Flags: map[string]string{"-c": "DROP TABLE users"},
    }

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        pattern.Match(cmd)
    }
    // Expect: < 200ns (just regex.MatchString, no compilation)
}
```

---

## S: Stress Tests

### S1: Concurrent Database Pack Matching

Verify all database packs are safe for concurrent use:

```go
func TestStressConcurrentDbMatching(t *testing.T) {
    var wg sync.WaitGroup
    commands := []parse.ExtractedCommand{
        cmd("psql", nil, m("-c", "DROP TABLE users")),
        cmd("mysql", nil, m("-e", "SELECT * FROM users")),
        cmd("redis-cli", []string{"FLUSHALL"}, nil),
        cmd("mongosh", nil, m("--eval", "db.dropDatabase()")),
        cmd("sqlite3", []string{"test.db", "DROP TABLE users"}, nil),
    }

    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(idx int) {
            defer wg.Done()
            c := commands[idx%len(commands)]
            dbPacks := []packs.Pack{pgPack, mysqlPack, sqlitePack, mongoPack, redisPack}
            for j := 0; j < 1000; j++ {
                for _, pack := range dbPacks {
                    for _, dp := range pack.Destructive {
                        dp.Match.Match(c)
                    }
                    for _, sp := range pack.Safe {
                        sp.Match.Match(c)
                    }
                }
            }
        }(i)
    }
    wg.Wait()
}
```

Run with `-race` flag. The regex patterns in `ArgContentMatcher` use
pre-compiled `*regexp.Regexp` which is safe for concurrent use (Go's
regexp package guarantees this).

### S2: High-Volume Mixed Database Commands

Simulate a high-throughput environment with interleaved commands from
all 5 database types:

```go
func TestStressHighVolumeDbCommands(t *testing.T) {
    allCommands := loadAllGoldenCommands(t, "testdata/golden/database_*.txt")

    var wg sync.WaitGroup
    for i := 0; i < 50; i++ {
        wg.Add(1)
        go func(worker int) {
            defer wg.Done()
            for j := 0; j < 100; j++ {
                cmd := allCommands[(worker*100+j)%len(allCommands)]
                result := pipeline.Run(context.Background(), cmd.Command, interCfg)
                _ = result // Just verify no panics/races
            }
        }(i)
    }
    wg.Wait()
}
```

---

## SEC: Security Tests

### SEC1: SQL Pattern Evasion Attempts

Test that known SQL evasion techniques don't bypass pattern detection:

```go
func TestSecuritySQLPatternEvasion(t *testing.T) {
    tests := []struct {
        name     string
        tool     string
        flag     string
        sql      string
        wantDeny bool
        reason   string
    }{
        // Case evasion
        {"mixed case DROP TABLE", "psql", "-c", "DrOp TaBlE users", true,
            "case-insensitive regex should catch mixed case"},

        // Whitespace evasion
        {"extra whitespace", "psql", "-c", "DROP    TABLE    users", true,
            "\\s+ should handle multiple whitespace chars"},
        {"tab in keyword", "psql", "-c", "DROP\tTABLE users", true,
            "\\s+ should match tab characters"},
        {"newline in keyword", "psql", "-c", "DROP\nTABLE users", true,
            "\\s+ should match newline characters"},

        // Quoting
        {"quoted table name", "psql", "-c", `DROP TABLE "public"."users"`, true,
            "quoted identifiers should not affect keyword match"},

        // Schema prefix
        {"schema-qualified drop", "psql", "-c", "DROP TABLE public.users", true,
            "schema qualification should not affect match"},

        // IF EXISTS
        {"IF EXISTS variant", "psql", "-c", "DROP TABLE IF EXISTS users", true,
            "IF EXISTS should not prevent match"},

        // CASCADE
        {"CASCADE variant", "psql", "-c", "DROP TABLE users CASCADE", true,
            "CASCADE should not prevent match"},

        // Multi-statement with safe prefix
        {"safe prefix + destructive", "psql", "-c", "BEGIN; DROP TABLE users; COMMIT;", true,
            "destructive in transaction should still be caught"},

        // MySQL-specific
        {"mysql mixed case", "mysql", "-e", "dRoP dAtAbAsE myapp", true,
            "MySQL case insensitive"},
        {"mysql backtick table", "mysql", "-e", "DROP TABLE `users`", true,
            "backtick-quoted identifiers"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            testCmd := parse.ExtractedCommand{
                Name:  tt.tool,
                Flags: map[string]string{tt.flag: tt.sql},
            }
            matched := false
            pack := packForTool(tt.tool)
            for _, dp := range pack.Destructive {
                if dp.Match.Match(testCmd) {
                    matched = true
                    break
                }
            }
            assert.Equal(t, tt.wantDeny, matched, tt.reason)
        })
    }
}
```

### SEC2: MongoDB Shell Expression Evasion

```go
func TestSecurityMongoEvasion(t *testing.T) {
    tests := []struct {
        name     string
        eval     string
        wantDeny bool
        reason   string
    }{
        // Whitespace around parentheses
        {"extra whitespace in drop", "db.users.drop ()", true,
            "whitespace before ( should still match"},
        {"extra whitespace in dropDatabase", "db.dropDatabase ()", true,
            "whitespace before ( in dropDatabase"},

        // Chained operations
        {"chained after find", "db.users.find().drop()", true,
            "drop after find chain should match"},

        // Variable collection name
        {"variable collection", "db[collName].drop()", true,
            "bracket notation should match .drop("},

        // deleteMany with whitespace in empty filter
        {"deleteMany with spaced filter", "db.users.deleteMany( { } )", true,
            "whitespace in empty filter should match"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            testCmd := parse.ExtractedCommand{
                Name:  "mongosh",
                Flags: map[string]string{"--eval": tt.eval},
            }
            matched := false
            for _, dp := range mongoPack.Destructive {
                if dp.Match.Match(testCmd) {
                    matched = true
                    break
                }
            }
            assert.Equal(t, tt.wantDeny, matched, tt.reason)
        })
    }
}
```

### SEC3: Redis Command Case Evasion

```go
func TestSecurityRedisCaseEvasion(t *testing.T) {
    // Redis commands use ArgAtFold (strings.EqualFold) for case-insensitive matching
    tests := []struct {
        cmd      string
        wantDeny bool
    }{
        {"FLUSHALL", true},
        {"flushall", true},
        {"FlushAll", true},  // ArgAtFold catches all case variations
        {"FLUSHall", true},  // ArgAtFold catches all case variations
        {"FLUSHDB", true},
        {"flushdb", true},
        {"FlushDb", true},   // ArgAtFold catches all case variations
        {"DEL", true},
        {"del", true},
        {"Del", true},       // ArgAtFold catches all case variations
        {"SHUTDOWN", true},
        {"shutdown", true},
        {"Shutdown", true},  // ArgAtFold catches all case variations
        {"DEBUG", true},
        {"debug", true},
        {"Debug", true},     // ArgAtFold catches all case variations
    }

    for _, tt := range tests {
        t.Run(tt.cmd, func(t *testing.T) {
            testCmd := parse.ExtractedCommand{
                Name: "redis-cli",
                Args: []string{tt.cmd},
            }
            matched := false
            for _, dp := range redisPack.Destructive {
                if dp.Match.Match(testCmd) {
                    matched = true
                    break
                }
            }
            assert.Equal(t, tt.wantDeny, matched)
        })
    }
}
```

**Note on Redis case handling**: All Redis patterns use `ArgAtFold()`
with `strings.EqualFold()` for fully case-insensitive matching. This
eliminates the mixed-case evasion vector identified in review.

### SEC4: Environment Sensitivity Preconditions

Verify that env-sensitive patterns correctly escalate severity when
production environment is detected:

```go
func TestSecurityEnvSensitivityPreConditions(t *testing.T) {
    envSensitivePatterns := []struct {
        packID  string
        pattern string
        baseSeverity guard.Severity
    }{
        {"database.postgresql", "psql-drop-database", guard.High},
        {"database.postgresql", "dropdb", guard.High},
        {"database.mysql", "mysql-drop-database", guard.High},
        {"database.mongodb", "mongo-drop-database", guard.High},
        {"database.redis", "redis-flushall", guard.High},
    }

    for _, tt := range envSensitivePatterns {
        t.Run(tt.packID+"/"+tt.pattern, func(t *testing.T) {
            pack := packByID(tt.packID)
            dp := findDestructiveByName(pack, tt.pattern)
            assert.True(t, dp.EnvSensitive,
                "pattern should be env-sensitive")
            assert.Equal(t, tt.baseSeverity, dp.Severity,
                "base severity mismatch")
            // End-to-end escalation behavior is tested in env detection / pipeline plans.
        })
    }
}
```

---

## MQ: Manual QA Plan

### MQ1: Real-World Database Command Evaluation

Test with database commands from actual LLM coding sessions:

1. Collect 20 database-related Bash tool invocations from Claude Code logs:
   - psql queries for debugging (SELECT, \dt, etc.)
   - MySQL migrations (ALTER TABLE, CREATE INDEX)
   - Redis cache operations (GET, SET, KEYS, DEL)
   - MongoDB queries (find, aggregate)
   - SQLite operations for local test databases
2. Run each through the pipeline with `InteractivePolicy`
3. Verify:
   - No false positives on read-only database operations
   - All destructive commands are caught
   - `DELETE FROM` with WHERE is correctly allowed
   - `DELETE FROM` without WHERE is correctly flagged
   - Interactive sessions (bare `psql`, `mysql`, etc.) are allowed

### MQ2: Database Pack Documentation Review

Review each database pack's patterns:
1. For each destructive pattern, verify the `Reason` accurately describes
   the database-specific risk (not generic boilerplate)
2. For each `Remediation`, verify the suggestion is database-specific and
   actionable (e.g., "Use pg_dump" for PostgreSQL, "Use mysqldump" for MySQL)
3. For each severity, verify it matches the severity guidelines considering
   environment sensitivity escalation
4. Verify no important destructive database operations are missing:
   - PostgreSQL: VACUUM FULL, REINDEX, DROP SCHEMA, DROP INDEX
   - MySQL: DROP INDEX, OPTIMIZE TABLE, REPAIR TABLE
   - MongoDB: db.repairDatabase(), compact
   - Redis: SWAPDB, MOVE, SELECT (database switching)
5. Document any missing operations as potential v2 additions

### MQ3: Cross-Database SQL Consistency

Manually verify that equivalent SQL operations get consistent treatment
across PostgreSQL, MySQL, and SQLite:
1. `DROP TABLE` — all three should be High/ConfidenceHigh
2. `DROP DATABASE` — PostgreSQL and MySQL should be High/ConfidenceHigh
   (SQLite has no databases)
3. `TRUNCATE` — PostgreSQL and MySQL High/ConfidenceHigh,
   SQLite Medium/ConfidenceLow (not valid SQLite SQL)
4. `DELETE FROM` without WHERE — all three Medium/ConfidenceMedium

### MQ4: Connection String Environment Detection

Manually test commands with production-looking connection strings to verify
the environment detection integration (plan 04) works correctly:
1. `psql -h prod-db.example.com -c "DROP TABLE users"` — should escalate
2. `mysql -h staging-mysql.internal -e "DROP DATABASE test"` — staging policy
3. `redis-cli -h cache.prod.example.com FLUSHALL` — should escalate
4. `mongosh mongodb://prod-cluster.mongodb.net/mydb --eval "db.dropDatabase()"` — should escalate
5. `sqlite3 test.db "DROP TABLE users"` — should NOT escalate (not env-sensitive)

---

## CI Tier Mapping

| Tier | Tests | Trigger |
|------|-------|---------|
| T1 (Fast, every commit) | P1-P7, E1-E7, F1-F3, SEC1-SEC4 | Every commit |
| T2 (Standard, every PR) | T1 + B1-B3, S1-S2 | PR open/update |
| T3 (Extended, nightly) | T1 + T2 + O1-O3 | Nightly schedule |
| T4 (Manual, pre-release) | MQ1-MQ4 | Before each release |

**T1 time budget**: < 15 seconds (slightly higher than core due to regex)
**T2 time budget**: < 45 seconds
**T3 time budget**: < 5 minutes (includes upstream comparison + cross-db oracle)

---

## Exit Criteria

### Must Pass

1. **All property tests pass** — P1-P7
2. **All deterministic examples pass** — E1-E7
3. **All fault injection tests pass** — F1-F3
4. **All security tests pass** — SEC1-SEC4
5. **Golden file corpus passes** — All 75 database entries
6. **Pattern reachability 100%** — Every destructive pattern reachable across
   all 5 packs
7. **Cross-pack isolation verified** — P7 passes
8. **Environment sensitivity correct** — P3 passes for all 5 packs
9. **CheckFlagValues extension works** — P5 passes
10. **No data races** — S1 passes with -race flag
11. **Zero panics in any test** — Including F1-F3 fault injection

### Should Pass

12. **Benchmarks recorded** — B1-B3 have baseline values
13. **Stress tests pass** — S1-S2 complete without issues
14. **Comparison oracle baseline** — O1 has initial divergence report
15. **Cross-database consistency** — O2 has no unexpected severity differences

### Tracked Metrics

- Pattern count by pack (safe + destructive) — target: 16 safe + 35 destructive
  across 5 packs
- Test count by category (unit, reachability, golden, property, security)
- Golden file entry count — target: 75 entries across 5 packs
- SQL regex match latency per pattern (from B1)
- SQL content matching: percentage of patterns using CheckFlagValues
- Environment sensitivity coverage: 4 of 5 packs env-sensitive
- Upstream comparison divergence count and categorization
- Cross-database severity consistency: 0 unexpected differences

---

## Round 1 Review Disposition

| # | Reviewer | Severity | Summary | Disposition | Notes |
|---|----------|----------|---------|-------------|-------|
| 1 | dcg-reviewer | P3 | P4 test doesn't test actual pack patterns (P3-4) | Incorporated | P4 rewritten to test real pack matchers |
| 2 | dcg-reviewer | P2 | Dual SQL flag test case missing (P2-1) | Incorporated | Added to E7 |
| 3 | dcg-reviewer | P3 | psql meta-command tests missing (P3-3) | Incorporated | Added to E1 |
| 4 | dcg-alt-reviewer | P1 | Redis mixed case should be caught (DB-P1.2) | Incorporated | SEC3 updated for ArgAtFold |
| 5 | dcg-alt-reviewer | P1 | UPDATE without WHERE tests needed (DB-P1.3) | Incorporated | Added to E1, E2, E3 |
| 6 | dcg-alt-reviewer | P1 | DROP SCHEMA tests needed (DB-P1.4) | Incorporated | Added to E1 |
| 7 | N/A | N/A | Golden file counts updated (61 → 75) | Incorporated | O1, B2, exit criteria updated |
| 8 | N/A | N/A | Pattern counts updated (30 → 35 destructive) | Incorporated | Exit criteria updated |

## Round 2 Review Disposition

| # | Reviewer | Severity | Summary | Disposition | Notes |
|---|----------|----------|---------|-------------|-------|
| 1 | domain-packs-r2 | P2 | F3 comment-bypass case wording was misleading | Incorporated | Updated case comment to clarify it is a true positive for keyword heuristic |
| 2 | domain-packs-r2 | P2 | Missing explicit known false-negative multi-statement case | Incorporated | Added `DELETE ...; SELECT ... WHERE ...` known-FN case to E7 |
| 3 | domain-packs-r2 | P2 | B1 benchmark interactive command used args instead of flags | Incorporated | Updated benchmark command shapes to reflect extracted Flags vs Args structure |
| 4 | domain-packs-r2 | P3 | O2 compared severity only, not confidence | Incorporated | Added confidence consistency assertions with divergence note |
| 5 | domain-packs-r2 | P3 | SEC4 name implied escalation behavior but tested preconditions only | Incorporated | Renamed SEC4 and test function to preconditions-focused wording |

## Round 3 Review Disposition

No new findings.

---

## Completion Signoff

- **Status**: Partial
- **Date**: 2026-03-03
- **Branch**: main
- **Verified by**: dcg-reviewer
- **Completed items**:
  - Database-focused test suites are implemented in `internal/eval`: property (`db_property_test.go`), fault (`db_fault_test.go`), security (`db_security_test.go`), oracle (`db_oracle_test.go`), benchmarks/stress (`db_benchmark_test.go`).
  - Database golden corpus and policy-monotonicity tests are present and wired into `go test ./internal/eval`.
  - Verification commands executed:
    - `go test ./guard ./internal/eval ./internal/parse ./internal/packs -count=1` — PASS
    - `go test -race ./guard ./internal/eval -count=1` — PASS
- **Outstanding gaps**:
  - **P0**: Harness assumptions around safe/destructive interaction are not fully satisfiable because runtime evaluation does not execute safe rules.
  - **P1**: Planned test-harness references to `CheckFlagValues` matcher semantics do not match implementation architecture (raw command-string matching, no `ArgContentMatcher` flag-value switch behavior to verify).
  - **P1**: CI-tier mapping in this doc does not match current tier scripts; database P/E/F/SEC/O suite is not explicitly mapped in `scripts/ci_tier1.sh`/`tier2.sh`/`tier3.sh` per this plan.
  - **P2**: Benchmark execution path differs from plan expectations: `make bench` runs `guard` and `cmd/dcg-go` benchmarks, but not `internal/eval` database benchmark suites defined in this harness.
  - **P1**: Full-suite verification command `make test` is currently red due to benchmark instability in `internal/testharness` (`TestBenchmarkStability`), so plan exit criteria are not fully met.

---
## Completion Signoff
- **Status**: Complete
- **Date**: 2026-03-04
- **Branch**: main
- **Commit**: f8621ae
- **Verified by**: dcg-reviewer
- **Test verification**: `make test-integration` — PASS
- **Deviations from plan**: Harness package path moved to `e2etest`; heavy database suites run under `-tags=e2e` integration target.
- **Additions beyond plan**: Database harness now coexists with broader multi-pack black-box suites and CI tier scripts in `e2etest`.
