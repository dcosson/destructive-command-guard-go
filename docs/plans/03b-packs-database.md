# 03b: Database Packs

**Batch**: 3 (Pattern Packs)
**Depends On**: [02-matching-framework](./02-matching-framework.md), [03a-packs-core](./03a-packs-core.md)
**Blocks**: [05-testing-and-benchmarks](./05-testing-and-benchmarks.md)
**Architecture**: [00-architecture.md](./00-architecture.md) (§3 Layer 3, §5 packs/)
**Plan Index**: [00-plan-index.md](./00-plan-index.md)
**Pack Authoring Guide**: [03a-packs-core.md §4](./03a-packs-core.md)

---

## 1. Summary

This plan defines 5 database packs covering the most common database tools
that LLM coding agents interact with:

1. **`database.postgresql`** — psql, pg_dump, pg_restore, dropdb, createdb
2. **`database.mysql`** — mysql, mysqldump, mysqladmin
3. **`database.sqlite`** — sqlite3
4. **`database.mongodb`** — mongo, mongosh, mongodump, mongorestore
5. **`database.redis`** — redis-cli

**Key design challenges unique to database packs:**

- **SQL content matching**: Destructive SQL operations (`DROP TABLE`,
  `TRUNCATE`, `DELETE FROM`) are passed as argument values to CLI tools
  (`psql -c "DROP TABLE users"`). This requires `ArgContentMatcher` (plan 02
  §5.2.4) with case-insensitive regex matching against argument content.
- **Environment sensitivity**: 4 of 5 packs are `EnvSensitive` — severity
  escalates in production environments. SQLite is the exception (local file
  database, no prod/dev distinction).
- **Shell expression matching**: MongoDB uses JavaScript-like shell
  expressions (`db.dropDatabase()`) passed via `--eval` or piped into
  `mongosh`. This requires matching argument content for method call patterns.
- **Flag-value patterns**: Redis commands are passed as positional arguments
  to `redis-cli` (`redis-cli FLUSHALL`), not as flags. PostgreSQL uses
  `-c` flag with SQL as the value.

**Scope**:
- 5 packs, each with safe + destructive patterns
- All packs follow the pack authoring guide (03a §4)
- 80+ golden file entries across all 5 packs
- Per-pattern unit tests with match and near-miss cases
- Reachability tests for every destructive pattern
- Environment escalation tests for env-sensitive packs

---

## 2. Component Diagram

```mermaid
graph TB
    subgraph "internal/packs/database"
        PG["postgresql.go<br/>database.postgresql"]
        MySQL["mysql.go<br/>database.mysql"]
        SQLite["sqlite.go<br/>database.sqlite"]
        Mongo["mongodb.go<br/>database.mongodb"]
        Redis["redis.go<br/>database.redis"]
    end

    subgraph "internal/packs"
        Registry["Registry<br/>(registry.go)"]
        Builders["Builder DSL<br/>Name(), Flags(), ArgAt(),<br/>ArgContent(), ArgContentRegex()"]
    end

    subgraph "guard"
        Types["Severity, Confidence<br/>(types.go)"]
    end

    PG -->|"init()"| Registry
    MySQL -->|"init()"| Registry
    SQLite -->|"init()"| Registry
    Mongo -->|"init()"| Registry
    Redis -->|"init()"| Registry

    PG --> Builders
    MySQL --> Builders
    SQLite --> Builders
    Mongo --> Builders
    Redis --> Builders

    PG --> Types
    MySQL --> Types
    SQLite --> Types
    Mongo --> Types
    Redis --> Types
```

---

## 3. Import Flow

```mermaid
graph TD
    PG["database/postgresql.go"] --> PACKS["internal/packs"]
    PG --> GUARD["guard"]
    MYSQL["database/mysql.go"] --> PACKS
    MYSQL --> GUARD
    SQLITE["database/sqlite.go"] --> PACKS
    SQLITE --> GUARD
    MONGO["database/mongodb.go"] --> PACKS
    MONGO --> GUARD
    REDIS["database/redis.go"] --> PACKS
    REDIS --> GUARD
```

Each pack file imports only:
- `github.com/dcosson/destructive-command-guard-go/guard`
- `github.com/dcosson/destructive-command-guard-go/internal/packs`

---

## 4. SQL Pattern Matching Strategy

Database packs heavily rely on `ArgContentMatcher` and `ArgContentRegex`
(plan 02 §5.2.4) because destructive operations are SQL statements passed
as argument values.

### 4.1 How SQL Reaches the CLI

| Tool | How SQL is passed | Extraction |
|------|-------------------|------------|
| `psql -c "DROP TABLE users"` | `-c` flag value | `Flags["-c"] = "DROP TABLE users"` or `Args` contains the SQL |
| `psql -f drop.sql` | `-f` flag value (file path) | Cannot inspect file contents — no match |
| `psql` (interactive) | stdin | Cannot inspect — no match |
| `mysql -e "DROP TABLE users"` | `-e` flag value | Same as psql -c |
| `sqlite3 db.sqlite "DROP TABLE users"` | Positional arg | `Args` contains the SQL |
| `mongosh --eval "db.dropDatabase()"` | `--eval` flag value | `Flags["--eval"]` or `Args` |
| `redis-cli FLUSHALL` | Positional args | `Args` = ["FLUSHALL"] |

**Key insight**: We match SQL content within **argument values** using
`ArgContentRegex`. We do NOT attempt to parse SQL itself — we use regex
patterns for well-known destructive keywords. This is a content heuristic,
not a SQL parser.

### 4.2 SQL Regex Patterns

All SQL matching is **case-insensitive** (`(?i)` flag) because SQL keywords
are case-insensitive by specification.

| Pattern | Regex | Matches |
|---------|-------|---------|
| DROP TABLE | `(?i)\bDROP\s+TABLE\b` | `DROP TABLE users`, `drop table "public"."orders"` |
| DROP DATABASE | `(?i)\bDROP\s+DATABASE\b` | `DROP DATABASE mydb` |
| TRUNCATE | `(?i)\bTRUNCATE\b` | `TRUNCATE users`, `TRUNCATE TABLE orders` |
| DELETE no WHERE | `(?i)\bDELETE\s+FROM\b(?!.*\bWHERE\b)` | `DELETE FROM users` but NOT `DELETE FROM users WHERE id=1` |
| ALTER TABLE DROP | `(?i)\bALTER\s+TABLE\b.*\bDROP\b` | `ALTER TABLE users DROP COLUMN email` |

**DELETE FROM heuristic**: The "no WHERE" check uses a negative lookahead
`(?!.*\bWHERE\b)`. This is a **heuristic** — it will false-positive on
multi-statement inputs where WHERE appears in a different statement. We set
`ConfidenceMedium` for this pattern to reflect the heuristic nature.

### 4.3 DataflowResolved Consideration

Per 03a §4.10, when `DataflowResolved` is `false`, argument values may
contain unresolved `$VAR` references. For database packs this means:

- `psql -c "DROP TABLE $TABLE"` — if `$TABLE` is unresolved, the
  `ArgContentRegex` still matches `DROP TABLE` which is sufficient for
  detection. The actual table name doesn't matter for severity.
- `psql -c "$SQL_QUERY"` — if the entire query is a variable, the regex
  won't match. This is acceptable — we can't analyze what we can't see.
  The command will be allowed. This is documented as a known limitation.

### 4.4 Flag Value vs Args Extraction

The tree-sitter extractor (plan 01) handles `-c "SQL"` and `-e "SQL"` as
flag-value pairs: the flag key is `-c` or `-e`, and the value is the SQL
string. For `ArgContentRegex` to work on flag values, we need to check
**both** `cmd.Args` and `cmd.Flags` values.

Plan 02's `ArgContentMatcher` currently only checks `cmd.Args`. For database
packs, we need an `ArgContentRegex` that also checks flag values. Two options:

**(a) Extend ArgContentMatcher** to optionally check flag values (add a
`CheckFlagValues bool` field). This is the cleaner approach.

**(b) Use a custom matcher** that explicitly checks
`cmd.Flags["-c"]` + `cmd.Flags["-e"]` etc. More explicit but repetitive.

**Decision**: Option (a) — add `CheckFlagValues` to `ArgContentMatcher`.
This is a small cross-plan update to plan 02 and benefits all packs that
need content matching in flag values. The field defaults to `false` for
backwards compatibility.

```go
type ArgContentMatcher struct {
    Substring      string
    Regex          *regexp.Regexp
    AtIndex        int
    CheckFlagValues bool  // Also check flag values, not just Args
}

func (m ArgContentMatcher) Match(cmd parse.ExtractedCommand) bool {
    check := func(s string) bool {
        if m.Regex != nil {
            return m.Regex.MatchString(s)
        }
        return strings.Contains(s, m.Substring)
    }
    // Check Args
    if m.AtIndex >= 0 {
        if m.AtIndex < len(cmd.Args) && check(cmd.Args[m.AtIndex]) {
            return true
        }
    } else {
        for _, arg := range cmd.Args {
            if check(arg) {
                return true
            }
        }
    }
    // Check flag values if requested
    if m.CheckFlagValues {
        for _, val := range cmd.Flags {
            if val != "" && check(val) {
                return true
            }
        }
    }
    return false
}
```

**Builder helper**:
```go
// SQLContent creates an ArgContentMatcher that checks both args and flag
// values for a case-insensitive SQL regex pattern.
func SQLContent(pattern string) ArgContentMatcher {
    return ArgContentMatcher{
        Regex:           regexp.MustCompile("(?i)" + pattern),
        AtIndex:         -1,
        CheckFlagValues: true,
    }
}
```

---

## 5. Detailed Design

### 5.1 `database.postgresql` Pack (`internal/packs/database/postgresql.go`)

**Pack ID**: `database.postgresql`
**Keywords**: `["psql", "pg_dump", "pg_restore", "dropdb", "createdb"]`
**Safe Patterns**: 5
**Destructive Patterns**: 8
**EnvSensitive**: Yes (4 of 8 destructive patterns)

```go
package database

import (
    "github.com/dcosson/destructive-command-guard-go/guard"
    "github.com/dcosson/destructive-command-guard-go/internal/packs"
)

func init() {
    packs.DefaultRegistry.Register(pgPack)
}

var pgPack = packs.Pack{
    ID:          "database.postgresql",
    Name:        "PostgreSQL",
    Description: "PostgreSQL database destructive operations via psql, dropdb, and related tools",
    Keywords:    []string{"psql", "pg_dump", "pg_restore", "dropdb", "createdb"},

    Safe: []packs.SafePattern{
        // S1: psql with SELECT/EXPLAIN queries (read-only)
        {
            Name: "psql-select-safe",
            Match: packs.And(
                packs.Name("psql"),
                packs.Or(
                    packs.Flags("-c"),
                    packs.Flags("--command"),
                ),
                packs.Or(
                    packs.SQLContent(`\bSELECT\b`),
                    packs.SQLContent(`\bEXPLAIN\b`),
                    packs.SQLContent(`\b\\d`), // psql meta-commands: \dt, \d+, \dn, etc.
                ),
                packs.Not(packs.Or(
                    packs.SQLContent(`\bDROP\b`),
                    packs.SQLContent(`\bTRUNCATE\b`),
                    packs.SQLContent(`\bDELETE\b`),
                    packs.SQLContent(`\bALTER\b`),
                )),
            ),
        },
        // S2: pg_dump (backup — read-only) without --clean
        {
            Name: "pg-dump-safe",
            Match: packs.And(
                packs.Name("pg_dump"),
                packs.Not(packs.Flags("--clean")),
                packs.Not(packs.Flags("-c")), // pg_dump -c means --clean
            ),
        },
        // S3: createdb (creating databases is safe)
        {
            Name: "createdb-safe",
            Match: packs.Name("createdb"),
        },
        // S4: psql with no -c flag (interactive session — no detectable SQL)
        {
            Name: "psql-interactive-safe",
            Match: packs.And(
                packs.Name("psql"),
                packs.Not(packs.Flags("-c")),
                packs.Not(packs.Flags("--command")),
                packs.Not(packs.Flags("-f")),
                packs.Not(packs.Flags("--file")),
            ),
        },
        // S5: pg_restore without --clean (restoring data, not dropping first)
        {
            Name: "pg-restore-safe",
            Match: packs.And(
                packs.Name("pg_restore"),
                packs.Not(packs.Flags("--clean")),
                packs.Not(packs.Flags("-c")),
            ),
        },
    },

    Destructive: []packs.DestructivePattern{
        // ---- High ----

        // D1: DROP DATABASE via psql
        {
            Name: "psql-drop-database",
            Match: packs.And(
                packs.Name("psql"),
                packs.SQLContent(`\bDROP\s+DATABASE\b`),
            ),
            Severity:     guard.High,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "DROP DATABASE permanently destroys an entire database and all its data",
            Remediation:  "Use pg_dump to create a backup first. Verify the database name.",
            EnvSensitive: true,
        },
        // D2: dropdb CLI tool
        {
            Name: "dropdb",
            Match: packs.Name("dropdb"),
            Severity:     guard.High,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "dropdb permanently destroys an entire PostgreSQL database",
            Remediation:  "Use pg_dump to create a backup first. Verify the database name.",
            EnvSensitive: true,
        },
        // D3: DROP TABLE via psql
        {
            Name: "psql-drop-table",
            Match: packs.And(
                packs.Name("psql"),
                packs.SQLContent(`\bDROP\s+TABLE\b`),
            ),
            Severity:     guard.High,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "DROP TABLE permanently destroys a table and all its data",
            Remediation:  "Use pg_dump -t to backup the table first. Consider DROP TABLE IF EXISTS.",
            EnvSensitive: true,
        },
        // D4: TRUNCATE via psql
        {
            Name: "psql-truncate",
            Match: packs.And(
                packs.Name("psql"),
                packs.SQLContent(`\bTRUNCATE\b`),
            ),
            Severity:     guard.High,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "TRUNCATE removes all rows from a table instantly without logging individual row deletions",
            Remediation:  "Create a backup first. Consider DELETE with WHERE for selective removal.",
            EnvSensitive: true,
        },

        // ---- Medium ----

        // D5: DELETE FROM without WHERE via psql
        {
            Name: "psql-delete-no-where",
            Match: packs.And(
                packs.Name("psql"),
                packs.SQLContent(`\bDELETE\s+FROM\b`),
                packs.Not(packs.SQLContent(`\bWHERE\b`)),
            ),
            Severity:     guard.Medium,
            Confidence:   guard.ConfidenceMedium,
            Reason:       "DELETE FROM without WHERE clause deletes all rows in the table",
            Remediation:  "Add a WHERE clause to target specific rows, or use TRUNCATE if you intend to remove all rows.",
            EnvSensitive: true,
        },
        // D6: pg_dump --clean (includes DROP statements in dump)
        {
            Name: "pg-dump-clean",
            Match: packs.And(
                packs.Name("pg_dump"),
                packs.Or(
                    packs.Flags("--clean"),
                    // Note: pg_dump -c means --clean, but this conflicts with
                    // psql -c which means --command. We only match pg_dump here.
                ),
            ),
            Severity:     guard.Medium,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "pg_dump --clean generates DROP commands before CREATE — restoring this dump will destroy existing objects",
            Remediation:  "Use pg_dump without --clean for a non-destructive backup.",
            EnvSensitive: false,
        },
        // D7: pg_restore --clean (drops objects before restoring)
        {
            Name: "pg-restore-clean",
            Match: packs.And(
                packs.Name("pg_restore"),
                packs.Or(
                    packs.Flags("--clean"),
                    packs.Flags("-c"),
                ),
            ),
            Severity:     guard.Medium,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "pg_restore --clean drops existing database objects before recreating them",
            Remediation:  "Use pg_restore without --clean to restore without dropping existing data.",
            EnvSensitive: false,
        },
        // D8: ALTER TABLE ... DROP via psql
        {
            Name: "psql-alter-drop",
            Match: packs.And(
                packs.Name("psql"),
                packs.SQLContent(`\bALTER\s+TABLE\b`),
                packs.SQLContent(`\bDROP\b`),
            ),
            Severity:     guard.Medium,
            Confidence:   guard.ConfidenceMedium,
            Reason:       "ALTER TABLE ... DROP permanently removes columns, constraints, or indexes",
            Remediation:  "Create a backup first. Verify the column/constraint name.",
            EnvSensitive: false,
        },
    },
}
```

#### 5.1.1 PostgreSQL Pattern Interaction Matrix

| Safe Pattern | Prevents Destructive | Key Distinguishing Condition |
|-------------|---------------------|----------------------------|
| psql-select-safe | psql-drop-database, psql-drop-table, psql-truncate, psql-delete-no-where, psql-alter-drop | `-c` with SELECT/EXPLAIN and Not(DROP\|TRUNCATE\|DELETE\|ALTER) |
| pg-dump-safe | pg-dump-clean | Not(--clean) |
| createdb-safe | (none — no destructive createdb patterns) | Name = createdb |
| psql-interactive-safe | psql-drop-*, psql-truncate, psql-delete-*, psql-alter-drop | Not(-c \| --command \| -f \| --file) |
| pg-restore-safe | pg-restore-clean | Not(--clean \| -c) |

**Notes**:
- `dropdb` has no safe pattern — all invocations are destructive.
- `psql -f drop.sql` (file execution) matches neither safe nor destructive
  because we cannot inspect file contents. It passes as "no match" → Allow.
  This is a known limitation documented in §13.

---

### 5.2 `database.mysql` Pack (`internal/packs/database/mysql.go`)

**Pack ID**: `database.mysql`
**Keywords**: `["mysql", "mysqldump", "mysqladmin"]`
**Safe Patterns**: 4
**Destructive Patterns**: 7
**EnvSensitive**: Yes (4 of 7 destructive patterns)

```go
var mysqlPack = packs.Pack{
    ID:          "database.mysql",
    Name:        "MySQL",
    Description: "MySQL/MariaDB database destructive operations via mysql, mysqldump, mysqladmin",
    Keywords:    []string{"mysql", "mysqldump", "mysqladmin"},

    Safe: []packs.SafePattern{
        // S1: mysql with SELECT/EXPLAIN/SHOW queries
        {
            Name: "mysql-select-safe",
            Match: packs.And(
                packs.Name("mysql"),
                packs.Or(
                    packs.Flags("-e"),
                    packs.Flags("--execute"),
                ),
                packs.Or(
                    packs.SQLContent(`\bSELECT\b`),
                    packs.SQLContent(`\bEXPLAIN\b`),
                    packs.SQLContent(`\bSHOW\b`),
                    packs.SQLContent(`\bDESCRIBE\b`),
                ),
                packs.Not(packs.Or(
                    packs.SQLContent(`\bDROP\b`),
                    packs.SQLContent(`\bTRUNCATE\b`),
                    packs.SQLContent(`\bDELETE\b`),
                    packs.SQLContent(`\bALTER\b`),
                )),
            ),
        },
        // S2: mysqldump (backup — read-only) without --add-drop-table
        // Note: mysqldump includes DROP TABLE by default. --skip-add-drop-table
        // disables it. We consider default mysqldump safe because it's a backup
        // tool — the risk is at restore time, not dump time.
        {
            Name: "mysqldump-safe",
            Match: packs.Name("mysqldump"),
        },
        // S3: mysql interactive (no -e flag)
        {
            Name: "mysql-interactive-safe",
            Match: packs.And(
                packs.Name("mysql"),
                packs.Not(packs.Flags("-e")),
                packs.Not(packs.Flags("--execute")),
            ),
        },
        // S4: mysqladmin status/ping/version (read-only admin commands)
        {
            Name: "mysqladmin-readonly-safe",
            Match: packs.And(
                packs.Name("mysqladmin"),
                packs.Or(
                    packs.ArgAt(0, "status"),
                    packs.ArgAt(0, "ping"),
                    packs.ArgAt(0, "version"),
                    packs.ArgAt(0, "processlist"),
                    packs.ArgAt(0, "variables"),
                    packs.ArgAt(0, "extended-status"),
                ),
            ),
        },
    },

    Destructive: []packs.DestructivePattern{
        // ---- High ----

        // D1: DROP DATABASE via mysql -e
        {
            Name: "mysql-drop-database",
            Match: packs.And(
                packs.Name("mysql"),
                packs.SQLContent(`\bDROP\s+DATABASE\b`),
            ),
            Severity:     guard.High,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "DROP DATABASE permanently destroys an entire MySQL database and all its tables",
            Remediation:  "Use mysqldump to create a backup first. Verify the database name.",
            EnvSensitive: true,
        },
        // D2: mysqladmin drop (drops a database)
        {
            Name: "mysqladmin-drop",
            Match: packs.And(
                packs.Name("mysqladmin"),
                packs.Or(
                    packs.ArgAt(0, "drop"),
                    packs.Arg("drop"),
                ),
            ),
            Severity:     guard.High,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "mysqladmin drop permanently destroys an entire MySQL database",
            Remediation:  "Use mysqldump to create a backup first. Verify the database name.",
            EnvSensitive: true,
        },
        // D3: DROP TABLE via mysql -e
        {
            Name: "mysql-drop-table",
            Match: packs.And(
                packs.Name("mysql"),
                packs.SQLContent(`\bDROP\s+TABLE\b`),
            ),
            Severity:     guard.High,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "DROP TABLE permanently destroys a table and all its data",
            Remediation:  "Use mysqldump to backup the table first.",
            EnvSensitive: true,
        },
        // D4: TRUNCATE via mysql -e
        {
            Name: "mysql-truncate",
            Match: packs.And(
                packs.Name("mysql"),
                packs.SQLContent(`\bTRUNCATE\b`),
            ),
            Severity:     guard.High,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "TRUNCATE removes all rows from a table instantly",
            Remediation:  "Create a backup first. Consider DELETE with WHERE for selective removal.",
            EnvSensitive: true,
        },

        // ---- Medium ----

        // D5: DELETE FROM without WHERE via mysql -e
        {
            Name: "mysql-delete-no-where",
            Match: packs.And(
                packs.Name("mysql"),
                packs.SQLContent(`\bDELETE\s+FROM\b`),
                packs.Not(packs.SQLContent(`\bWHERE\b`)),
            ),
            Severity:     guard.Medium,
            Confidence:   guard.ConfidenceMedium,
            Reason:       "DELETE FROM without WHERE clause deletes all rows in the table",
            Remediation:  "Add a WHERE clause to target specific rows.",
            EnvSensitive: true,
        },
        // D6: ALTER TABLE DROP via mysql -e
        {
            Name: "mysql-alter-drop",
            Match: packs.And(
                packs.Name("mysql"),
                packs.SQLContent(`\bALTER\s+TABLE\b`),
                packs.SQLContent(`\bDROP\b`),
            ),
            Severity:     guard.Medium,
            Confidence:   guard.ConfidenceMedium,
            Reason:       "ALTER TABLE ... DROP permanently removes columns, constraints, or indexes",
            Remediation:  "Create a backup first. Verify the column/constraint name.",
            EnvSensitive: false,
        },
        // D7: mysqladmin flush-hosts / flush-logs (operational risk)
        {
            Name: "mysqladmin-flush",
            Match: packs.And(
                packs.Name("mysqladmin"),
                packs.Or(
                    packs.Arg("flush-hosts"),
                    packs.Arg("flush-logs"),
                    packs.Arg("flush-privileges"),
                    packs.Arg("flush-tables"),
                ),
            ),
            Severity:     guard.Medium,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "mysqladmin flush operations can disrupt active connections and require careful timing",
            Remediation:  "Schedule flush operations during maintenance windows.",
            EnvSensitive: false,
        },
    },
}
```

#### 5.2.1 MySQL Notes

**mysqldump as safe**: `mysqldump` is classified as safe even though its
output contains `DROP TABLE` statements by default. The dump itself is
read-only — the risk is when someone runs `mysql < dump.sql` to restore.
That `mysql` command won't trigger the safe pattern (it uses stdin, not
`-e`) and will pass as "no match." This is a known gap (see §13).

**mysql vs mysqldump keyword collision**: Both `mysql` and `mysqldump`
contain the substring `mysql`. The Aho-Corasick pre-filter with word-boundary
matching will correctly distinguish them — `mysqldump` is a separate word
that triggers the pack, and the `Name()` matcher in patterns ensures the
right command is matched.

---

### 5.3 `database.sqlite` Pack (`internal/packs/database/sqlite.go`)

**Pack ID**: `database.sqlite`
**Keywords**: `["sqlite3"]`
**Safe Patterns**: 2
**Destructive Patterns**: 4
**EnvSensitive**: No (SQLite is a local file database)

```go
var sqlitePack = packs.Pack{
    ID:          "database.sqlite",
    Name:        "SQLite",
    Description: "SQLite database destructive operations via sqlite3 CLI",
    Keywords:    []string{"sqlite3"},

    Safe: []packs.SafePattern{
        // S1: sqlite3 with SELECT/EXPLAIN/.tables/.schema
        {
            Name: "sqlite3-readonly-safe",
            Match: packs.And(
                packs.Name("sqlite3"),
                packs.Or(
                    packs.SQLContent(`\bSELECT\b`),
                    packs.SQLContent(`\bEXPLAIN\b`),
                    packs.ArgContent("^\\."  ), // sqlite3 dot-commands: .tables, .schema, .dump
                ),
                packs.Not(packs.Or(
                    packs.SQLContent(`\bDROP\b`),
                    packs.SQLContent(`\bTRUNCATE\b`),
                    packs.SQLContent(`\bDELETE\b`),
                )),
            ),
        },
        // S2: sqlite3 interactive (no SQL argument)
        {
            Name: "sqlite3-interactive-safe",
            Match: packs.And(
                packs.Name("sqlite3"),
                // sqlite3 with just a db file path and no SQL arg
                // Heuristic: if there's exactly 1 arg (the db path), it's interactive
                packs.Not(packs.SQLContent(`\b(?:DROP|TRUNCATE|DELETE|ALTER)\b`)),
                packs.Not(packs.ArgContent(`\.drop`)),
            ),
        },
    },

    Destructive: []packs.DestructivePattern{
        // ---- High ----

        // D1: DROP TABLE via sqlite3
        {
            Name: "sqlite3-drop-table",
            Match: packs.And(
                packs.Name("sqlite3"),
                packs.SQLContent(`\bDROP\s+TABLE\b`),
            ),
            Severity:     guard.High,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "DROP TABLE permanently destroys a table and all its data in the SQLite database",
            Remediation:  "Copy the database file as a backup first. Use .dump to export data.",
            EnvSensitive: false,
        },
        // D2: .drop meta-command (sqlite3-specific)
        {
            Name: "sqlite3-dot-drop",
            Match: packs.And(
                packs.Name("sqlite3"),
                packs.ArgContent(`\.drop`),
            ),
            Severity:     guard.High,
            Confidence:   guard.ConfidenceMedium,
            Reason:       "sqlite3 .drop command drops triggers or views",
            Remediation:  "Verify the target. Use .dump to backup first.",
            EnvSensitive: false,
        },

        // ---- Medium ----

        // D3: DELETE FROM without WHERE via sqlite3
        {
            Name: "sqlite3-delete-no-where",
            Match: packs.And(
                packs.Name("sqlite3"),
                packs.SQLContent(`\bDELETE\s+FROM\b`),
                packs.Not(packs.SQLContent(`\bWHERE\b`)),
            ),
            Severity:     guard.Medium,
            Confidence:   guard.ConfidenceMedium,
            Reason:       "DELETE FROM without WHERE clause deletes all rows in the table",
            Remediation:  "Add a WHERE clause to target specific rows.",
            EnvSensitive: false,
        },
        // D4: TRUNCATE (SQLite doesn't have TRUNCATE, but DELETE FROM without
        // WHERE is equivalent. This catches cases where someone writes TRUNCATE
        // thinking it works — sqlite3 will error, but we still flag intent.)
        // Note: This pattern rarely matches for sqlite3 because TRUNCATE isn't
        // valid SQLite SQL. Included for completeness.
        {
            Name: "sqlite3-truncate",
            Match: packs.And(
                packs.Name("sqlite3"),
                packs.SQLContent(`\bTRUNCATE\b`),
            ),
            Severity:     guard.Medium,
            Confidence:   guard.ConfidenceLow,
            Reason:       "TRUNCATE is not valid SQLite SQL but indicates intent to delete all data",
            Remediation:  "SQLite uses DELETE FROM (without WHERE) instead of TRUNCATE.",
            EnvSensitive: false,
        },
    },
}
```

---

### 5.4 `database.mongodb` Pack (`internal/packs/database/mongodb.go`)

**Pack ID**: `database.mongodb`
**Keywords**: `["mongo", "mongosh", "mongodump", "mongorestore"]`
**Safe Patterns**: 3
**Destructive Patterns**: 6
**EnvSensitive**: Yes (5 of 6 destructive patterns)

MongoDB uses JavaScript-like shell expressions rather than SQL. Destructive
operations are method calls like `db.dropDatabase()` or
`db.collection.deleteMany({})`.

```go
var mongoPack = packs.Pack{
    ID:          "database.mongodb",
    Name:        "MongoDB",
    Description: "MongoDB destructive operations via mongosh, mongo, mongodump, mongorestore",
    Keywords:    []string{"mongo", "mongosh", "mongodump", "mongorestore"},

    Safe: []packs.SafePattern{
        // S1: mongodump (backup — read-only)
        {
            Name: "mongodump-safe",
            Match: packs.Name("mongodump"),
        },
        // S2: mongosh/mongo with read-only operations
        {
            Name: "mongosh-readonly-safe",
            Match: packs.And(
                packs.Or(
                    packs.Name("mongosh"),
                    packs.Name("mongo"),
                ),
                packs.Or(
                    packs.Flags("--eval"),
                    packs.Flags("-e"),
                ),
                packs.Or(
                    packs.SQLContent(`\.find\(`),
                    packs.SQLContent(`\.count\(`),
                    packs.SQLContent(`\.aggregate\(`),
                    packs.SQLContent(`\.explain\(`),
                    packs.SQLContent(`show\s+dbs`),
                    packs.SQLContent(`show\s+collections`),
                ),
                packs.Not(packs.Or(
                    packs.SQLContent(`\.drop`),
                    packs.SQLContent(`\.delete`),
                    packs.SQLContent(`\.remove`),
                    packs.SQLContent(`dropDatabase`),
                )),
            ),
        },
        // S3: mongosh/mongo interactive (no --eval)
        {
            Name: "mongosh-interactive-safe",
            Match: packs.And(
                packs.Or(
                    packs.Name("mongosh"),
                    packs.Name("mongo"),
                ),
                packs.Not(packs.Flags("--eval")),
                packs.Not(packs.Flags("-e")),
            ),
        },
    },

    Destructive: []packs.DestructivePattern{
        // ---- High ----

        // D1: db.dropDatabase()
        {
            Name: "mongo-drop-database",
            Match: packs.And(
                packs.Or(
                    packs.Name("mongosh"),
                    packs.Name("mongo"),
                ),
                packs.SQLContent(`dropDatabase\s*\(`),
            ),
            Severity:     guard.High,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "db.dropDatabase() permanently destroys an entire MongoDB database",
            Remediation:  "Use mongodump to create a backup first. Verify the database name.",
            EnvSensitive: true,
        },
        // D2: db.collection.drop()
        {
            Name: "mongo-collection-drop",
            Match: packs.And(
                packs.Or(
                    packs.Name("mongosh"),
                    packs.Name("mongo"),
                ),
                packs.SQLContent(`\.drop\s*\(`),
                packs.Not(packs.SQLContent(`dropDatabase`)),
            ),
            Severity:     guard.High,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "collection.drop() permanently destroys a MongoDB collection and all its documents",
            Remediation:  "Use mongodump --collection to backup the collection first.",
            EnvSensitive: true,
        },

        // ---- Medium ----

        // D3: db.collection.deleteMany({}) (empty filter = delete all)
        {
            Name: "mongo-delete-many-all",
            Match: packs.And(
                packs.Or(
                    packs.Name("mongosh"),
                    packs.Name("mongo"),
                ),
                packs.SQLContent(`deleteMany\s*\(\s*\{\s*\}\s*\)`),
            ),
            Severity:     guard.Medium,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "deleteMany({}) with empty filter deletes all documents in the collection",
            Remediation:  "Add a filter to target specific documents: deleteMany({field: value})",
            EnvSensitive: true,
        },
        // D4: db.collection.remove({}) (legacy, empty filter = remove all)
        {
            Name: "mongo-remove-all",
            Match: packs.And(
                packs.Or(
                    packs.Name("mongosh"),
                    packs.Name("mongo"),
                ),
                packs.SQLContent(`\.remove\s*\(\s*\{\s*\}\s*\)`),
            ),
            Severity:     guard.Medium,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "remove({}) with empty filter deletes all documents in the collection",
            Remediation:  "Add a query filter: remove({field: value})",
            EnvSensitive: true,
        },
        // D5: mongorestore --drop (drops collections before restoring)
        {
            Name: "mongorestore-drop",
            Match: packs.And(
                packs.Name("mongorestore"),
                packs.Flags("--drop"),
            ),
            Severity:     guard.Medium,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "mongorestore --drop drops existing collections before restoring, losing any data not in the backup",
            Remediation:  "Use mongorestore without --drop to merge instead of replace.",
            EnvSensitive: true,
        },
        // D6: db.collection.deleteMany with filter (less dangerous but still destructive)
        {
            Name: "mongo-delete-many",
            Match: packs.And(
                packs.Or(
                    packs.Name("mongosh"),
                    packs.Name("mongo"),
                ),
                packs.SQLContent(`deleteMany\s*\(`),
                packs.Not(packs.SQLContent(`deleteMany\s*\(\s*\{\s*\}\s*\)`)), // Not empty filter
            ),
            Severity:     guard.Medium,
            Confidence:   guard.ConfidenceMedium,
            Reason:       "deleteMany() deletes multiple documents matching the filter",
            Remediation:  "Verify the filter matches only intended documents. Use countDocuments() first to check.",
            EnvSensitive: true,
        },
    },
}
```

#### 5.4.1 MongoDB Notes

**mongo vs mongosh**: The legacy `mongo` shell is deprecated in favor of
`mongosh`. We match both command names for backwards compatibility. The
patterns are identical for both.

**Shell expression matching**: MongoDB patterns use `SQLContent()` (which
wraps `ArgContentRegex` with `CheckFlagValues: true`) to match JavaScript
method call patterns. The regex patterns target method names:
- `dropDatabase\s*\(` — matches `db.dropDatabase()`
- `\.drop\s*\(` — matches `db.collection.drop()`
- `deleteMany\s*\(\s*\{\s*\}\s*\)` — matches `deleteMany({})` with empty filter

**Empty filter detection**: The `deleteMany({})` pattern uses a precise regex
that matches empty object literals `{}` with optional whitespace. This is a
ConfidenceHigh match because the empty filter is structurally unambiguous.
`deleteMany` with a non-empty filter gets ConfidenceMedium because we can't
evaluate filter selectivity.

---

### 5.5 `database.redis` Pack (`internal/packs/database/redis.go`)

**Pack ID**: `database.redis`
**Keywords**: `["redis-cli"]`
**Safe Patterns**: 2
**Destructive Patterns**: 5
**EnvSensitive**: Yes (4 of 5 destructive patterns)

Redis is simpler than SQL databases — commands are passed as positional
arguments to `redis-cli` without a flag like `-c` or `-e`.

```go
var redisPack = packs.Pack{
    ID:          "database.redis",
    Name:        "Redis",
    Description: "Redis destructive operations via redis-cli",
    Keywords:    []string{"redis-cli"},

    Safe: []packs.SafePattern{
        // S1: redis-cli read-only commands
        {
            Name: "redis-cli-readonly-safe",
            Match: packs.And(
                packs.Name("redis-cli"),
                packs.Or(
                    packs.ArgAt(0, "GET"),
                    packs.ArgAt(0, "get"),
                    packs.ArgAt(0, "KEYS"),
                    packs.ArgAt(0, "keys"),
                    packs.ArgAt(0, "INFO"),
                    packs.ArgAt(0, "info"),
                    packs.ArgAt(0, "PING"),
                    packs.ArgAt(0, "ping"),
                    packs.ArgAt(0, "TTL"),
                    packs.ArgAt(0, "ttl"),
                    packs.ArgAt(0, "TYPE"),
                    packs.ArgAt(0, "type"),
                    packs.ArgAt(0, "DBSIZE"),
                    packs.ArgAt(0, "dbsize"),
                    packs.ArgAt(0, "SCAN"),
                    packs.ArgAt(0, "scan"),
                    packs.ArgAt(0, "MONITOR"),
                    packs.ArgAt(0, "monitor"),
                ),
            ),
        },
        // S2: redis-cli interactive (no command argument)
        // When redis-cli is invoked with just connection flags, it opens
        // an interactive session
        {
            Name: "redis-cli-interactive-safe",
            Match: packs.And(
                packs.Name("redis-cli"),
                // No positional args = interactive mode
                // This is a heuristic — connection flags (-h, -p, -a) don't
                // count as Redis commands
                packs.Not(packs.Or(
                    packs.ArgAt(0, "FLUSHALL"),
                    packs.ArgAt(0, "flushall"),
                    packs.ArgAt(0, "FLUSHDB"),
                    packs.ArgAt(0, "flushdb"),
                    packs.ArgAt(0, "DEL"),
                    packs.ArgAt(0, "del"),
                    packs.ArgAt(0, "UNLINK"),
                    packs.ArgAt(0, "unlink"),
                    packs.ArgAt(0, "SET"),
                    packs.ArgAt(0, "set"),
                    packs.ArgAt(0, "CONFIG"),
                    packs.ArgAt(0, "config"),
                    packs.ArgAt(0, "DEBUG"),
                    packs.ArgAt(0, "debug"),
                    packs.ArgAt(0, "SHUTDOWN"),
                    packs.ArgAt(0, "shutdown"),
                )),
            ),
        },
    },

    Destructive: []packs.DestructivePattern{
        // ---- High ----

        // D1: FLUSHALL (delete all keys in all databases)
        {
            Name: "redis-flushall",
            Match: packs.And(
                packs.Name("redis-cli"),
                packs.Or(
                    packs.ArgAt(0, "FLUSHALL"),
                    packs.ArgAt(0, "flushall"),
                    packs.Arg("FLUSHALL"),
                    packs.Arg("flushall"),
                ),
            ),
            Severity:     guard.High,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "FLUSHALL deletes all keys in all Redis databases",
            Remediation:  "Use redis-cli BGSAVE first to create a backup. Consider FLUSHDB for single-database flush.",
            EnvSensitive: true,
        },
        // D2: FLUSHDB (delete all keys in current database)
        {
            Name: "redis-flushdb",
            Match: packs.And(
                packs.Name("redis-cli"),
                packs.Or(
                    packs.ArgAt(0, "FLUSHDB"),
                    packs.ArgAt(0, "flushdb"),
                    packs.Arg("FLUSHDB"),
                    packs.Arg("flushdb"),
                ),
            ),
            Severity:     guard.High,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "FLUSHDB deletes all keys in the current Redis database",
            Remediation:  "Use redis-cli BGSAVE first. Verify you're connected to the right database.",
            EnvSensitive: true,
        },

        // ---- Medium ----

        // D3: DEL with wildcard pattern (mass deletion)
        {
            Name: "redis-del-wildcard",
            Match: packs.And(
                packs.Name("redis-cli"),
                packs.Or(
                    packs.ArgAt(0, "DEL"),
                    packs.ArgAt(0, "del"),
                    packs.ArgAt(0, "UNLINK"),
                    packs.ArgAt(0, "unlink"),
                ),
            ),
            Severity:     guard.Medium,
            Confidence:   guard.ConfidenceMedium,
            Reason:       "DEL/UNLINK deletes the specified keys from Redis",
            Remediation:  "Verify the key names. Use TTL or OBJECT HELP to inspect keys first.",
            EnvSensitive: true,
        },
        // D4: CONFIG SET (modifies Redis configuration at runtime)
        {
            Name: "redis-config-set",
            Match: packs.And(
                packs.Name("redis-cli"),
                packs.Or(
                    packs.ArgAt(0, "CONFIG"),
                    packs.ArgAt(0, "config"),
                ),
                packs.Or(
                    packs.Arg("SET"),
                    packs.Arg("set"),
                    packs.Arg("RESETSTAT"),
                    packs.Arg("resetstat"),
                ),
            ),
            Severity:     guard.Medium,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "CONFIG SET modifies Redis server configuration at runtime",
            Remediation:  "Verify the configuration parameter and value. Use CONFIG GET to check current value first.",
            EnvSensitive: true,
        },
        // D5: SHUTDOWN (stops the Redis server)
        {
            Name: "redis-shutdown",
            Match: packs.And(
                packs.Name("redis-cli"),
                packs.Or(
                    packs.ArgAt(0, "SHUTDOWN"),
                    packs.ArgAt(0, "shutdown"),
                    packs.Arg("SHUTDOWN"),
                    packs.Arg("shutdown"),
                ),
            ),
            Severity:     guard.Medium,
            Confidence:   guard.ConfidenceHigh,
            Reason:       "SHUTDOWN stops the Redis server, causing service disruption",
            Remediation:  "Use redis-cli BGSAVE first. Schedule shutdowns during maintenance windows.",
            EnvSensitive: false,
        },
    },
}
```

#### 5.5.1 Redis Notes

**Case sensitivity**: Redis commands are case-insensitive (FLUSHALL =
flushall). We match both forms explicitly with `ArgAt(0, "FLUSHALL")` and
`ArgAt(0, "flushall")`. An alternative would be to use `ArgContentRegex`
with `(?i)`, but explicit matching is simpler and more readable for Redis
since the command set is small.

**DEL with wildcard**: The shaping doc mentions `DEL *` as a pattern. In
practice, `redis-cli DEL *` doesn't actually use shell globbing — the `*`
is passed literally to Redis which doesn't support glob in DEL. Mass deletion
is typically done via `redis-cli KEYS "pattern*" | xargs redis-cli DEL`.
We flag all `DEL`/`UNLINK` commands at Medium severity because they indicate
key deletion intent. The wildcard case (via xargs pipeline) is caught by the
pipeline's compound command evaluation — the inner `redis-cli DEL` matches.

**redis-cli connection flags**: `redis-cli -h host -p 6379 -a password FLUSHALL`
puts `-h`, `-p`, `-a` into the Flags map and `FLUSHALL` into `Args[0]` after
flag extraction. The patterns correctly match `ArgAt(0, "FLUSHALL")` because
connection flags are separated from the Redis command.

---

## 6. Golden File Entries

**Policy**: All golden file entries use `InteractivePolicy` for the decision
column.

### 6.1 `database.postgresql` Golden Entries

```
format: v1
policy: interactive
---
# D1: psql DROP DATABASE — denied
command: psql -c "DROP DATABASE myapp"
decision: Deny
severity: High
confidence: High
pack: database.postgresql
rule: psql-drop-database
---
# D1: psql DROP DATABASE with host — denied
command: psql -h localhost -c "DROP DATABASE production"
decision: Deny
severity: High
confidence: High
pack: database.postgresql
rule: psql-drop-database
---
# D2: dropdb — denied
command: dropdb myapp_production
decision: Deny
severity: High
confidence: High
pack: database.postgresql
rule: dropdb
---
# D3: psql DROP TABLE — denied
command: psql -c "DROP TABLE users"
decision: Deny
severity: High
confidence: High
pack: database.postgresql
rule: psql-drop-table
---
# D3: psql DROP TABLE IF EXISTS — denied
command: psql -c "DROP TABLE IF EXISTS sessions"
decision: Deny
severity: High
confidence: High
pack: database.postgresql
rule: psql-drop-table
---
# D4: psql TRUNCATE — denied
command: psql -c "TRUNCATE TABLE orders"
decision: Deny
severity: High
confidence: High
pack: database.postgresql
rule: psql-truncate
---
# D5: psql DELETE FROM no WHERE — ask
command: psql -c "DELETE FROM users"
decision: Ask
severity: Medium
confidence: Medium
pack: database.postgresql
rule: psql-delete-no-where
---
# Edge: psql DELETE FROM with WHERE — allowed (no pattern match)
command: psql -c "DELETE FROM users WHERE id = 1"
decision: Allow
---
# D6: pg_dump --clean — ask
command: pg_dump --clean mydb > backup.sql
decision: Ask
severity: Medium
confidence: High
pack: database.postgresql
rule: pg-dump-clean
---
# D7: pg_restore --clean — ask
command: pg_restore --clean backup.dump
decision: Ask
severity: Medium
confidence: High
pack: database.postgresql
rule: pg-restore-clean
---
# D8: psql ALTER TABLE DROP COLUMN — ask
command: psql -c "ALTER TABLE users DROP COLUMN email"
decision: Ask
severity: Medium
confidence: Medium
pack: database.postgresql
rule: psql-alter-drop
---
# S1: psql SELECT — allowed
command: psql -c "SELECT * FROM users"
decision: Allow
---
# S2: pg_dump (no --clean) — allowed
command: pg_dump mydb > backup.sql
decision: Allow
---
# S3: createdb — allowed
command: createdb myapp_test
decision: Allow
---
# S4: psql interactive — allowed
command: psql -h localhost mydb
decision: Allow
---
# S5: pg_restore (no --clean) — allowed
command: pg_restore backup.dump
decision: Allow
---
# Edge: psql lowercase drop table — denied (case insensitive)
command: psql -c "drop table users"
decision: Deny
severity: High
confidence: High
pack: database.postgresql
rule: psql-drop-table
```

### 6.2 `database.mysql` Golden Entries

```
format: v1
policy: interactive
---
# D1: mysql DROP DATABASE — denied
command: mysql -e "DROP DATABASE myapp"
decision: Deny
severity: High
confidence: High
pack: database.mysql
rule: mysql-drop-database
---
# D2: mysqladmin drop — denied
command: mysqladmin drop myapp_production
decision: Deny
severity: High
confidence: High
pack: database.mysql
rule: mysqladmin-drop
---
# D3: mysql DROP TABLE — denied
command: mysql -e "DROP TABLE users"
decision: Deny
severity: High
confidence: High
pack: database.mysql
rule: mysql-drop-table
---
# D4: mysql TRUNCATE — denied
command: mysql -e "TRUNCATE TABLE orders"
decision: Deny
severity: High
confidence: High
pack: database.mysql
rule: mysql-truncate
---
# D5: mysql DELETE FROM no WHERE — ask
command: mysql -e "DELETE FROM users"
decision: Ask
severity: Medium
confidence: Medium
pack: database.mysql
rule: mysql-delete-no-where
---
# Edge: mysql DELETE FROM with WHERE — allowed
command: mysql -e "DELETE FROM users WHERE id = 1"
decision: Allow
---
# D6: mysql ALTER TABLE DROP — ask
command: mysql -e "ALTER TABLE users DROP COLUMN email"
decision: Ask
severity: Medium
confidence: Medium
pack: database.mysql
rule: mysql-alter-drop
---
# D7: mysqladmin flush — ask
command: mysqladmin flush-tables
decision: Ask
severity: Medium
confidence: High
pack: database.mysql
rule: mysqladmin-flush
---
# S1: mysql SELECT — allowed
command: mysql -e "SELECT * FROM users"
decision: Allow
---
# S2: mysqldump — allowed
command: mysqldump mydb > backup.sql
decision: Allow
---
# S3: mysql interactive — allowed
command: mysql -h localhost mydb
decision: Allow
---
# S4: mysqladmin status — allowed
command: mysqladmin status
decision: Allow
```

### 6.3 `database.sqlite` Golden Entries

```
format: v1
policy: interactive
---
# D1: sqlite3 DROP TABLE — denied
command: sqlite3 test.db "DROP TABLE users"
decision: Deny
severity: High
confidence: High
pack: database.sqlite
rule: sqlite3-drop-table
---
# D2: sqlite3 .drop — denied
command: sqlite3 test.db ".drop trigger update_timestamp"
decision: Deny
severity: High
confidence: Medium
pack: database.sqlite
rule: sqlite3-dot-drop
---
# D3: sqlite3 DELETE FROM no WHERE — ask
command: sqlite3 test.db "DELETE FROM users"
decision: Ask
severity: Medium
confidence: Medium
pack: database.sqlite
rule: sqlite3-delete-no-where
---
# Edge: sqlite3 DELETE FROM with WHERE — allowed
command: sqlite3 test.db "DELETE FROM users WHERE id = 1"
decision: Allow
---
# S1: sqlite3 SELECT — allowed
command: sqlite3 test.db "SELECT * FROM users"
decision: Allow
---
# S1: sqlite3 .tables — allowed
command: sqlite3 test.db ".tables"
decision: Allow
---
# Edge: sqlite3 interactive — allowed
command: sqlite3 test.db
decision: Allow
```

### 6.4 `database.mongodb` Golden Entries

```
format: v1
policy: interactive
---
# D1: mongosh dropDatabase — denied
command: mongosh --eval "db.dropDatabase()"
decision: Deny
severity: High
confidence: High
pack: database.mongodb
rule: mongo-drop-database
---
# D1: mongo (legacy) dropDatabase — denied
command: mongo --eval "db.dropDatabase()"
decision: Deny
severity: High
confidence: High
pack: database.mongodb
rule: mongo-drop-database
---
# D2: mongosh collection.drop() — denied
command: mongosh --eval "db.users.drop()"
decision: Deny
severity: High
confidence: High
pack: database.mongodb
rule: mongo-collection-drop
---
# D3: mongosh deleteMany({}) — ask
command: mongosh --eval "db.users.deleteMany({})"
decision: Ask
severity: Medium
confidence: High
pack: database.mongodb
rule: mongo-delete-many-all
---
# D4: mongosh remove({}) — ask
command: mongosh --eval "db.users.remove({})"
decision: Ask
severity: Medium
confidence: High
pack: database.mongodb
rule: mongo-remove-all
---
# D5: mongorestore --drop — ask
command: mongorestore --drop dump/
decision: Ask
severity: Medium
confidence: High
pack: database.mongodb
rule: mongorestore-drop
---
# D6: mongosh deleteMany with filter — ask
command: mongosh --eval "db.users.deleteMany({status: 'inactive'})"
decision: Ask
severity: Medium
confidence: Medium
pack: database.mongodb
rule: mongo-delete-many
---
# S1: mongodump — allowed
command: mongodump --out /backup/
decision: Allow
---
# S2: mongosh find — allowed
command: mongosh --eval "db.users.find()"
decision: Allow
---
# S3: mongosh interactive — allowed
command: mongosh mongodb://localhost:27017/mydb
decision: Allow
---
# Edge: mongosh show dbs — allowed
command: mongosh --eval "show dbs"
decision: Allow
```

### 6.5 `database.redis` Golden Entries

```
format: v1
policy: interactive
---
# D1: redis-cli FLUSHALL — denied
command: redis-cli FLUSHALL
decision: Deny
severity: High
confidence: High
pack: database.redis
rule: redis-flushall
---
# D1: redis-cli flushall (lowercase) — denied
command: redis-cli flushall
decision: Deny
severity: High
confidence: High
pack: database.redis
rule: redis-flushall
---
# D1: redis-cli FLUSHALL with connection flags — denied
command: redis-cli -h prod.redis.example.com FLUSHALL
decision: Deny
severity: High
confidence: High
pack: database.redis
rule: redis-flushall
---
# D2: redis-cli FLUSHDB — denied
command: redis-cli FLUSHDB
decision: Deny
severity: High
confidence: High
pack: database.redis
rule: redis-flushdb
---
# D3: redis-cli DEL — ask
command: redis-cli DEL mykey
decision: Ask
severity: Medium
confidence: Medium
pack: database.redis
rule: redis-del-wildcard
---
# D3: redis-cli UNLINK — ask
command: redis-cli UNLINK session:12345
decision: Ask
severity: Medium
confidence: Medium
pack: database.redis
rule: redis-del-wildcard
---
# D4: redis-cli CONFIG SET — ask
command: redis-cli CONFIG SET maxmemory 100mb
decision: Ask
severity: Medium
confidence: High
pack: database.redis
rule: redis-config-set
---
# D5: redis-cli SHUTDOWN — ask
command: redis-cli SHUTDOWN
decision: Ask
severity: Medium
confidence: High
pack: database.redis
rule: redis-shutdown
---
# S1: redis-cli GET — allowed
command: redis-cli GET mykey
decision: Allow
---
# S1: redis-cli INFO — allowed
command: redis-cli INFO
decision: Allow
---
# S1: redis-cli KEYS — allowed
command: redis-cli KEYS "user:*"
decision: Allow
---
# S2: redis-cli interactive — allowed
command: redis-cli -h localhost
decision: Allow
---
# S1: redis-cli PING — allowed
command: redis-cli PING
decision: Allow
```

**Total golden file entries**: 18 (postgresql) + 12 (mysql) + 7 (sqlite) +
11 (mongodb) + 13 (redis) = **61 entries**

---

## 7. Testing Strategy

### 7.1 Unit Tests

Each pack has table-driven tests following the 03a §4.7 template. Key
additions for database packs:

**SQL content matching tests**: Each SQL pattern must be tested with:
- Exact SQL match (uppercase)
- Case variation (lowercase, mixed case)
- SQL with extra whitespace
- SQL within larger string (flag value with other SQL)
- Near-miss (similar SQL that should NOT match)

**Environment escalation tests**: For env-sensitive patterns, verify that
the `EnvSensitive` flag is set correctly.

#### `postgresql_test.go`

```go
package database

import (
    "testing"

    "github.com/dcosson/destructive-command-guard-go/internal/parse"
    "github.com/stretchr/testify/assert"
)

func cmd(name string, args []string, flags map[string]string) parse.ExtractedCommand {
    return parse.ExtractedCommand{Name: name, Args: args, Flags: flags}
}

func m(pairs ...string) map[string]string {
    out := make(map[string]string, len(pairs)/2)
    for i := 0; i < len(pairs); i += 2 {
        if i+1 < len(pairs) {
            out[pairs[i]] = pairs[i+1]
        } else {
            out[pairs[i]] = ""
        }
    }
    return out
}

func TestPsqlDropDatabase(t *testing.T) {
    pattern := pgPack.Destructive[indexOfPgDestructive("psql-drop-database")].Match
    tests := []struct {
        name string
        cmd  parse.ExtractedCommand
        want bool
    }{
        {"DROP DATABASE", cmd("psql", nil, m("-c", "DROP DATABASE myapp")), true},
        {"drop database (lowercase)", cmd("psql", nil, m("-c", "drop database myapp")), true},
        {"DROP DATABASE IF EXISTS", cmd("psql", nil, m("-c", "DROP DATABASE IF EXISTS myapp")), true},
        {"DROP DATABASE in args", cmd("psql", []string{"DROP DATABASE myapp"}, nil), true},
        // Near-miss
        {"SELECT (not drop)", cmd("psql", nil, m("-c", "SELECT * FROM users")), false},
        {"CREATE DATABASE", cmd("psql", nil, m("-c", "CREATE DATABASE myapp")), false},
        {"mysql (wrong tool)", cmd("mysql", nil, m("-c", "DROP DATABASE myapp")), false},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            assert.Equal(t, tt.want, pattern.Match(tt.cmd))
        })
    }
}

func TestPsqlDropTable(t *testing.T) {
    pattern := pgPack.Destructive[indexOfPgDestructive("psql-drop-table")].Match
    tests := []struct {
        name string
        cmd  parse.ExtractedCommand
        want bool
    }{
        {"DROP TABLE", cmd("psql", nil, m("-c", "DROP TABLE users")), true},
        {"DROP TABLE IF EXISTS", cmd("psql", nil, m("-c", "DROP TABLE IF EXISTS users")), true},
        {"drop table (lowercase)", cmd("psql", nil, m("-c", "drop table users")), true},
        // Near-miss
        {"CREATE TABLE", cmd("psql", nil, m("-c", "CREATE TABLE users (id int)")), false},
        {"SELECT", cmd("psql", nil, m("-c", "SELECT * FROM users")), false},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            assert.Equal(t, tt.want, pattern.Match(tt.cmd))
        })
    }
}

func TestPsqlTruncate(t *testing.T) {
    pattern := pgPack.Destructive[indexOfPgDestructive("psql-truncate")].Match
    tests := []struct {
        name string
        cmd  parse.ExtractedCommand
        want bool
    }{
        {"TRUNCATE", cmd("psql", nil, m("-c", "TRUNCATE TABLE orders")), true},
        {"TRUNCATE without TABLE", cmd("psql", nil, m("-c", "TRUNCATE orders")), true},
        {"truncate (lowercase)", cmd("psql", nil, m("-c", "truncate users")), true},
        {"SELECT (not truncate)", cmd("psql", nil, m("-c", "SELECT * FROM users")), false},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            assert.Equal(t, tt.want, pattern.Match(tt.cmd))
        })
    }
}

func TestPsqlDeleteNoWhere(t *testing.T) {
    pattern := pgPack.Destructive[indexOfPgDestructive("psql-delete-no-where")].Match
    tests := []struct {
        name string
        cmd  parse.ExtractedCommand
        want bool
    }{
        {"DELETE FROM no WHERE", cmd("psql", nil, m("-c", "DELETE FROM users")), true},
        {"delete from (lowercase)", cmd("psql", nil, m("-c", "delete from users")), true},
        // Near-miss: has WHERE
        {"DELETE FROM with WHERE", cmd("psql", nil, m("-c", "DELETE FROM users WHERE id = 1")), false},
        {"DELETE FROM with where", cmd("psql", nil, m("-c", "delete from users where id = 1")), false},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            assert.Equal(t, tt.want, pattern.Match(tt.cmd))
        })
    }
}

func TestDropdb(t *testing.T) {
    pattern := pgPack.Destructive[indexOfPgDestructive("dropdb")].Match
    tests := []struct {
        name string
        cmd  parse.ExtractedCommand
        want bool
    }{
        {"dropdb myapp", cmd("dropdb", []string{"myapp"}, nil), true},
        {"dropdb with host", cmd("dropdb", []string{"myapp"}, m("-h", "localhost")), true},
        {"createdb (not dropdb)", cmd("createdb", []string{"myapp"}, nil), false},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            assert.Equal(t, tt.want, pattern.Match(tt.cmd))
        })
    }
}

func TestPsqlSelectSafe(t *testing.T) {
    pattern := pgPack.Safe[indexOfPgSafe("psql-select-safe")].Match
    tests := []struct {
        name string
        cmd  parse.ExtractedCommand
        want bool
    }{
        {"SELECT query", cmd("psql", nil, m("-c", "SELECT * FROM users")), true},
        {"EXPLAIN query", cmd("psql", nil, m("-c", "EXPLAIN SELECT * FROM users")), true},
        // Must NOT match destructive
        {"DROP TABLE (not safe)", cmd("psql", nil, m("-c", "DROP TABLE users")), false},
        {"TRUNCATE (not safe)", cmd("psql", nil, m("-c", "TRUNCATE users")), false},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            assert.Equal(t, tt.want, pattern.Match(tt.cmd))
        })
    }
}

// --- Pattern reachability test ---

func TestPgPatternReachability(t *testing.T) {
    destructiveCmds := map[string]parse.ExtractedCommand{
        "psql-drop-database": cmd("psql", nil, m("-c", "DROP DATABASE myapp")),
        "dropdb":             cmd("dropdb", []string{"myapp"}, nil),
        "psql-drop-table":    cmd("psql", nil, m("-c", "DROP TABLE users")),
        "psql-truncate":      cmd("psql", nil, m("-c", "TRUNCATE users")),
        "psql-delete-no-where": cmd("psql", nil, m("-c", "DELETE FROM users")),
        "pg-dump-clean":      cmd("pg_dump", nil, m("--clean", "")),
        "pg-restore-clean":   cmd("pg_restore", nil, m("--clean", "")),
        "psql-alter-drop":    cmd("psql", nil, m("-c", "ALTER TABLE users DROP COLUMN email")),
    }

    for _, dp := range pgPack.Destructive {
        t.Run(dp.Name+"/reachable", func(t *testing.T) {
            testCmd, ok := destructiveCmds[dp.Name]
            if !ok {
                t.Fatalf("no reachability test command for pattern %s", dp.Name)
            }
            for _, sp := range pgPack.Safe {
                if sp.Match.Match(testCmd) {
                    t.Errorf("safe pattern %s matches reachability command for %s",
                        sp.Name, dp.Name)
                }
            }
            assert.True(t, dp.Match.Match(testCmd),
                "destructive pattern %s does not match its reachability command", dp.Name)
        })
    }
}

// --- Environment sensitivity test ---

func TestPgEnvSensitiveFlags(t *testing.T) {
    envSensitive := map[string]bool{
        "psql-drop-database":  true,
        "dropdb":              true,
        "psql-drop-table":     true,
        "psql-truncate":       true,
        "psql-delete-no-where": true,
        "pg-dump-clean":       false,
        "pg-restore-clean":    false,
        "psql-alter-drop":     false,
    }
    for _, dp := range pgPack.Destructive {
        t.Run(dp.Name+"/env-sensitive", func(t *testing.T) {
            expected, ok := envSensitive[dp.Name]
            if !ok {
                t.Fatalf("no env-sensitive expectation for %s", dp.Name)
            }
            assert.Equal(t, expected, dp.EnvSensitive,
                "EnvSensitive mismatch for %s", dp.Name)
        })
    }
}
```

Similar test files for `mysql_test.go`, `sqlite_test.go`, `mongodb_test.go`,
and `redis_test.go` follow the same pattern with tests for:
- Each destructive pattern (match + near-miss)
- Each safe pattern (match + must-not-match)
- Pattern reachability
- Environment sensitivity flags

### 7.2 Pack Completeness

The shared `TestAllPacksComplete` test from 03a §7.2 covers all database
packs automatically once they're registered via `init()`.

### 7.3 Golden File Tests

All 61 golden file entries (§6) are tested via the golden file infrastructure.

### 7.4 Benchmarks

```go
func BenchmarkPostgresPackMatch(b *testing.B) {
    commands := []parse.ExtractedCommand{
        cmd("psql", nil, m("-c", "DROP TABLE users")),       // Match
        cmd("psql", nil, m("-c", "SELECT * FROM users")),    // Safe
        cmd("pg_dump", []string{"mydb"}, nil),               // Safe
        cmd("dropdb", []string{"myapp"}, nil),               // Match
    }
    for _, c := range commands {
        b.Run(c.Name, func(b *testing.B) {
            for i := 0; i < b.N; i++ {
                matchPack(pgPack, c)
            }
        })
    }
}
```

---

## 8. State Diagram: SQL Content Matching

```mermaid
stateDiagram-v2
    [*] --> ExtractCommand: Tree-sitter parse + extract

    ExtractCommand --> CheckFlags: psql/mysql command
    CheckFlags --> HasSQLArg: -c/-e flag present
    CheckFlags --> Interactive: No SQL flag
    Interactive --> [*]: Allow (safe pattern)

    HasSQLArg --> MatchSQL: Extract SQL from flag value
    MatchSQL --> CheckSafe: Run safe patterns
    CheckSafe --> SafeMatch: SELECT/EXPLAIN/meta-commands
    CheckSafe --> CheckDestructive: Not safe
    SafeMatch --> [*]: Allow

    CheckDestructive --> DropDB: DROP DATABASE regex
    CheckDestructive --> DropTable: DROP TABLE regex
    CheckDestructive --> Truncate: TRUNCATE regex
    CheckDestructive --> DeleteNoWhere: DELETE FROM + no WHERE
    CheckDestructive --> NoMatch: No destructive match

    DropDB --> Assess: High severity
    DropTable --> Assess: High severity
    Truncate --> Assess: High severity
    DeleteNoWhere --> Assess: Medium severity
    NoMatch --> [*]: Allow

    Assess --> EnvCheck: EnvSensitive?
    EnvCheck --> Escalate: Production detected
    EnvCheck --> [*]: Not production
    Escalate --> [*]: Severity + 1
```

---

## 9. Alien Artifacts

Not directly applicable. The SQL regex patterns are straightforward. However,
the `CheckFlagValues` extension to `ArgContentMatcher` (§4.4) is a small
framework improvement that benefits all future packs needing content matching
in flag values.

---

## 10. URP (Unreasonably Robust Programming)

### SQL Pattern Case Insensitivity

All SQL regex patterns use `(?i)` for case-insensitive matching. This
catches `DROP TABLE`, `drop table`, `Drop Table`, and all other case
variations. This is URP because we could have required uppercase only
(the most common form), but case insensitivity eliminates a class of
false negatives for free.

**Measurement**: Unit tests verify case variations for every SQL pattern.

### Environment Sensitivity Coverage

4 of 5 database packs are environment-sensitive. The severity escalation
from Medium → High or High → Critical in production environments provides
defense-in-depth for the most critical failure mode: running destructive
commands against production databases.

**Measurement**: `TestPgEnvSensitiveFlags` (and equivalents for each pack)
verify the `EnvSensitive` flag is correctly set.

### Delete-Without-WHERE Heuristic

The `DELETE FROM` without `WHERE` pattern uses a regex negative lookahead
to detect missing WHERE clauses. While imperfect (it's a heuristic — see
§4.2), it catches the most common and dangerous form of mass deletion.
Setting `ConfidenceMedium` honestly reflects the heuristic nature.

**Measurement**: Golden file entries for both with-WHERE and without-WHERE
cases. Unit tests for case variations.

---

## 11. Extreme Optimization

Not applicable for pattern packs. Database pack matching has slightly higher
per-pattern cost than core packs due to regex evaluation in `ArgContentMatcher`,
but this is still bounded by the number of patterns per pack (< 10) and the
regex engine's O(n) complexity.

---

## 12. Implementation Order

1. **`SQLContent` builder helper** — Add to `internal/packs/matcher.go`.
   Update `ArgContentMatcher` with `CheckFlagValues` field. This is a
   cross-plan update to plan 02's matcher implementation.

2. **`internal/packs/database/postgresql.go`** + `postgresql_test.go` —
   PostgreSQL pack. This establishes the SQL matching pattern for the rest.

3. **`internal/packs/database/mysql.go`** + `mysql_test.go` — MySQL pack.
   Very similar to PostgreSQL.

4. **`internal/packs/database/sqlite.go`** + `sqlite_test.go` — SQLite pack.
   Simplest SQL pack.

5. **`internal/packs/database/mongodb.go`** + `mongodb_test.go` — MongoDB
   pack. Different matching pattern (JS expressions vs SQL).

6. **`internal/packs/database/redis.go`** + `redis_test.go` — Redis pack.
   Simplest overall (positional arg matching).

7. **Golden file entries** — Add all 61 entries to
   `internal/eval/testdata/golden/`.

8. **Run all tests** — Unit, reachability, completeness, golden file.

Steps 2-6 can be partially parallelized (2 must go first, then 3-6 in
parallel). Step 1 must complete before any pack implementation.

---

## 13. Open Questions

1. **`psql -f` file execution**: When SQL is passed via file (`psql -f script.sql`),
   we cannot inspect file contents. These commands pass through as "no match"
   and are Allowed. This is a known limitation of static analysis. We could
   add a Low severity pattern for `psql -f` / `mysql < file` to surface the
   risk, but this would create many false positives for normal backup/migration
   workflows. **Decision**: Accept the gap. Document it.

2. **Piped SQL**: `echo "DROP TABLE users" | psql` pipes SQL via stdin. The
   pipeline evaluator sees both commands separately — `echo` matches nothing,
   `psql` matches the interactive safe pattern (no -c flag). The destructive
   SQL is invisible to pattern matching. Same limitation as file execution.
   **Decision**: Accept the gap. The echo command itself could be detected
   in a future enhancement, but it requires cross-command dataflow analysis
   that's out of scope for v1.

3. **Multi-statement SQL**: `psql -c "SELECT 1; DROP TABLE users"` contains
   both safe and destructive SQL in one argument. The `ArgContentRegex` will
   match `DROP TABLE` even though `SELECT` is also present. The safe pattern
   will NOT match because it checks `Not(DROP)`. This is correct behavior —
   the destructive statement should be caught.

4. **redis-cli `--eval` for Lua scripts**: `redis-cli --eval script.lua` runs
   Lua scripts in Redis. We don't analyze Lua content. This is similar to the
   `psql -f` gap. **Decision**: Accept the gap for v1.
