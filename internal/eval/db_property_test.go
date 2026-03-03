package eval

// Database pack property tests (P3-P7) from test harness plan 03b.
// P1-P2 are covered by reachability + golden file tests.

import (
	"math/rand"
	"testing"
	"testing/quick"
	"unicode"

	"github.com/dcosson/destructive-command-guard-go/internal/packs"
)

// --- P3: Environment Sensitivity Consistency ---

func TestDbPropertyEnvSensitivityConsistency(t *testing.T) {
	t.Parallel()

	envSensitivePacks := map[string]bool{
		"database.postgresql": true,
		"database.mysql":      true,
		"database.mongodb":    true,
		"database.redis":      true,
		"database.sqlite":     false,
	}

	for _, id := range dbPackIDs {
		id := id
		t.Run(id, func(t *testing.T) {
			pack := dbPack(id)
			if pack == nil {
				t.Fatalf("pack %s not found", id)
			}
			hasEnvSensitive := false
			for _, dp := range pack.Destructive {
				if dp.EnvSensitive {
					hasEnvSensitive = true
					break
				}
			}
			expected := envSensitivePacks[id]
			if hasEnvSensitive != expected {
				t.Errorf("pack %s env sensitivity = %v, want %v", id, hasEnvSensitive, expected)
			}
		})
	}

	// SQLite must have zero env-sensitive patterns
	sqlitePack := dbPack("database.sqlite")
	if sqlitePack == nil {
		t.Fatal("database.sqlite not found")
	}
	for _, dp := range sqlitePack.Destructive {
		if dp.EnvSensitive {
			t.Errorf("sqlite3 pattern %s should not be env-sensitive", dp.ID)
		}
	}
}

// --- P4: SQL Regex Case Insensitivity ---

func TestDbPropertySQLCaseInsensitivity(t *testing.T) {
	t.Parallel()

	patternTests := []struct {
		packID  string
		ruleID  string
		sqlBase string
		tool    string
		flag    string
	}{
		{"database.postgresql", "psql-drop-table", "DROP TABLE users", "psql", "-c"},
		{"database.postgresql", "psql-drop-database", "DROP DATABASE myapp", "psql", "-c"},
		{"database.postgresql", "psql-truncate", "TRUNCATE TABLE orders", "psql", "-c"},
		{"database.postgresql", "psql-drop-schema", "DROP SCHEMA public CASCADE", "psql", "-c"},
		{"database.mysql", "mysql-drop-table", "DROP TABLE users", "mysql", "-e"},
		{"database.mysql", "mysql-drop-database", "DROP DATABASE myapp", "mysql", "-e"},
		{"database.mysql", "mysql-truncate", "TRUNCATE TABLE orders", "mysql", "-e"},
	}

	f := func(seed int64) bool {
		rng := rand.New(rand.NewSource(seed))
		for _, pt := range patternTests {
			varied := randomCase(rng, pt.sqlBase)
			cmd := pt.tool + " " + pt.flag + " \"" + varied + "\""
			pack := dbPack(pt.packID)
			if pack == nil {
				return false
			}
			rule := findRuleByID(pack, pt.ruleID)
			if rule == nil {
				return false
			}
			if !rule.Match(cmd) {
				return false
			}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatal(err)
	}
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

// --- P5: Raw String Matching Correctness ---

func TestDbPropertyRawStringMatching(t *testing.T) {
	t.Parallel()

	// SQL clients pass queries in flag values (psql -c, mysql -e, mongosh --eval).
	// With raw string matching, destructive keywords anywhere in the command
	// should be detected.
	tests := []struct {
		name    string
		cmd     string
		packID  string
		ruleID  string
		wantHit bool
	}{
		{
			"psql DROP TABLE in flag value",
			`psql -c "DROP TABLE users"`,
			"database.postgresql", "psql-drop-table", true,
		},
		{
			"mysql DROP DATABASE in flag value",
			`mysql -e "DROP DATABASE myapp"`,
			"database.mysql", "mysql-drop-database", true,
		},
		{
			"mongosh dropDatabase in eval",
			`mongosh --eval "db.dropDatabase()"`,
			"database.mongodb", "mongo-drop-database", true,
		},
		{
			"sqlite3 DROP TABLE in argument",
			`sqlite3 test.db "DROP TABLE users"`,
			"database.sqlite", "sqlite3-drop-table", true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			pack := dbPack(tt.packID)
			if pack == nil {
				t.Fatalf("pack %s not found", tt.packID)
			}
			rule := findRuleByID(pack, tt.ruleID)
			if rule == nil {
				t.Fatalf("rule %s not found", tt.ruleID)
			}
			got := rule.Match(tt.cmd)
			if got != tt.wantHit {
				t.Errorf("match = %v, want %v for cmd: %s", got, tt.wantHit, tt.cmd)
			}
		})
	}
}

// --- P7: Cross-Pack Pattern Isolation ---

func TestDbPropertyCrossPackIsolation(t *testing.T) {
	t.Parallel()

	packCommands := map[string]string{
		"database.postgresql": `psql -c "DROP TABLE users"`,
		"database.mysql":      `mysql -e "DROP TABLE users"`,
		"database.sqlite":     `sqlite3 test.db "DROP TABLE users"`,
		"database.mongodb":    `mongosh --eval "db.users.drop()"`,
		"database.redis":      `redis-cli FLUSHALL`,
	}

	for cmdPackID, cmd := range packCommands {
		cmdPackID, cmd := cmdPackID, cmd
		for _, otherPackID := range dbPackIDs {
			if otherPackID == cmdPackID {
				continue
			}
			otherPackID := otherPackID
			t.Run(cmdPackID+"/not-in/"+otherPackID, func(t *testing.T) {
				pack := dbPack(otherPackID)
				if pack == nil {
					t.Fatalf("pack %s not found", otherPackID)
				}
				for _, dp := range pack.Destructive {
					if dp.Match != nil && dp.Match(cmd) {
						t.Errorf("%s command triggers %s/%s",
							cmdPackID, otherPackID, dp.ID)
					}
				}
			})
		}
	}
}

// --- P8: Every Destructive Rule Has Required Fields ---

func TestDbPropertyDestructiveRuleFields(t *testing.T) {
	t.Parallel()

	for _, id := range dbPackIDs {
		id := id
		pack := dbPack(id)
		if pack == nil {
			t.Fatalf("pack %s not found", id)
		}
		for _, dp := range pack.Destructive {
			dp := dp
			t.Run(id+"/"+dp.ID, func(t *testing.T) {
				if dp.ID == "" {
					t.Error("destructive rule has no ID")
				}
				if dp.Severity < 1 || dp.Severity > 4 {
					t.Errorf("severity %d out of range [1,4]", dp.Severity)
				}
				if dp.Confidence < 0 || dp.Confidence > 2 {
					t.Errorf("confidence %d out of range [0,2]", dp.Confidence)
				}
				if dp.Match == nil {
					t.Error("destructive rule has nil Match function")
				}
				if dp.Reason == "" {
					t.Error("destructive rule has no Reason")
				}
				if dp.Remediation == "" {
					t.Error("destructive rule has no Remediation")
				}
			})
		}
	}
}

// --- Reachability: every destructive rule is reachable by at least one command ---

func TestDbPropertyDestructiveReachability(t *testing.T) {
	t.Parallel()

	reachability := map[string]map[string]string{
		"database.postgresql": {
			"psql-drop-database": `psql -c "DROP DATABASE myapp"`,
			"dropdb":             `dropdb myapp`,
			"psql-drop-table":    `psql -c "DROP TABLE users"`,
			"psql-truncate":      `psql -c "TRUNCATE TABLE orders"`,
			"psql-delete-no-where": `psql -c "DELETE FROM users"`,
			"pg-dump-clean":      `pg_dump --clean mydb`,
			"pg-restore-clean":   `pg_restore --clean backup.dump`,
			"psql-alter-drop":    `psql -c "ALTER TABLE users DROP COLUMN email"`,
			"psql-update-no-where": `psql -c "UPDATE users SET active=false"`,
			"psql-drop-schema":  `psql -c "DROP SCHEMA public CASCADE"`,
		},
		"database.mysql": {
			"mysql-drop-database":  `mysql -e "DROP DATABASE myapp"`,
			"mysqladmin-drop":      `mysqladmin drop myapp`,
			"mysql-drop-table":     `mysql -e "DROP TABLE users"`,
			"mysql-truncate":       `mysql -e "TRUNCATE TABLE orders"`,
			"mysql-delete-no-where": `mysql -e "DELETE FROM users"`,
			"mysql-alter-drop":     `mysql -e "ALTER TABLE users DROP COLUMN email"`,
			"mysqladmin-flush":     `mysqladmin flush-tables`,
			"mysql-update-no-where": `mysql -e "UPDATE users SET active=false"`,
		},
		"database.sqlite": {
			"sqlite3-drop-table":      `sqlite3 test.db "DROP TABLE users"`,
			"sqlite3-dot-drop":        `sqlite3 test.db ".drop trigger update_timestamp"`,
			"sqlite3-delete-no-where": `sqlite3 test.db "DELETE FROM users"`,
			"sqlite3-truncate":        `sqlite3 test.db "TRUNCATE TABLE users"`,
			"sqlite3-update-no-where": `sqlite3 test.db "UPDATE config SET value='x'"`,
		},
		"database.mongodb": {
			"mongo-drop-database":  `mongosh --eval "db.dropDatabase()"`,
			"mongo-collection-drop": `mongosh --eval "db.users.drop()"`,
			"mongo-delete-many-all": `mongosh --eval "db.users.deleteMany({})"`,
			"mongo-remove-all":     `mongosh --eval "db.users.remove({})"`,
			"mongorestore-drop":    `mongorestore --drop dump/`,
			"mongo-delete-many":    `mongosh --eval "db.users.deleteMany({status: 'inactive'})"`,
		},
		"database.redis": {
			"redis-flushall":    `redis-cli FLUSHALL`,
			"redis-flushdb":     `redis-cli FLUSHDB`,
			"redis-key-delete":  `redis-cli DEL mykey`,
			"redis-config-set":  `redis-cli CONFIG SET maxmemory 100mb`,
			"redis-shutdown":    `redis-cli SHUTDOWN`,
			"redis-debug":       `redis-cli DEBUG SEGFAULT`,
		},
	}

	for packID, rules := range reachability {
		pack := dbPack(packID)
		if pack == nil {
			t.Fatalf("pack %s not found", packID)
		}
		for ruleID, cmd := range rules {
			ruleID, cmd := ruleID, cmd
			t.Run(packID+"/"+ruleID, func(t *testing.T) {
				rule := findRuleByID(pack, ruleID)
				if rule == nil {
					t.Fatalf("rule %s not found in pack %s", ruleID, packID)
				}
				if !rule.Match(cmd) {
					t.Errorf("reachability command %q did not match rule %s", cmd, ruleID)
				}
			})
		}
		// Verify every rule has a reachability command
		for _, dp := range pack.Destructive {
			if _, ok := rules[dp.ID]; !ok {
				t.Errorf("pack %s rule %s has no reachability command", packID, dp.ID)
			}
		}
	}
}

// toolToPackID maps CLI tool names to their database pack IDs.
func toolToPackID(tool string) string {
	switch tool {
	case "psql", "pg_dump", "pg_restore", "dropdb", "createdb":
		return "database.postgresql"
	case "mysql", "mysqldump", "mysqladmin":
		return "database.mysql"
	case "sqlite3":
		return "database.sqlite"
	case "mongosh", "mongo", "mongos", "mongodump", "mongorestore":
		return "database.mongodb"
	case "redis-cli":
		return "database.redis"
	}
	return ""
}

// packForTool returns the database pack for a given tool, using dbPack.
func packForTool(tool string) *packs.Pack {
	id := toolToPackID(tool)
	if id == "" {
		return nil
	}
	return dbPack(id)
}
