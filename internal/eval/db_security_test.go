package eval

// Database pack security tests (SEC1-SEC4) from test harness plan 03b.
// SEC1: SQL pattern evasion attempts.
// SEC2: MongoDB shell expression evasion.
// SEC3: Redis command case evasion.
// SEC4: Environment sensitivity preconditions.

import (
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/database"
	"github.com/dcosson/destructive-command-guard-go/internal/parse"
)

// --- SEC1: SQL Pattern Evasion Attempts ---

func TestDbSecuritySQLPatternEvasion(t *testing.T) {
	t.Parallel()

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

		// Postgres long flag
		{"--command long flag", "psql", "--command", "DROP TABLE users", true,
			"--command should work the same as -c"},

		// MySQL long flag
		{"--execute long flag", "mysql", "--execute", "DROP DATABASE myapp", true,
			"--execute should work the same as -e"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			testCmd := parse.ExtractedCommand{
				Name:  tt.tool,
				Flags: map[string]string{tt.flag: tt.sql},
			}
			matched := false
			packID := toolToPackID(tt.tool)
			pack := dbPack(packID)
			if pack == nil {
				t.Fatalf("pack for tool %s not found", tt.tool)
			}
			for _, dp := range pack.Destructive {
				if dp.Match.Match(testCmd) {
					matched = true
					break
				}
			}
			if matched != tt.wantDeny {
				t.Errorf("match = %v, want %v — %s", matched, tt.wantDeny, tt.reason)
			}
		})
	}
}

// --- SEC2: MongoDB Shell Expression Evasion ---

func TestDbSecurityMongoEvasion(t *testing.T) {
	t.Parallel()

	mongoPack := dbPack("database.mongodb")
	if mongoPack == nil {
		t.Fatal("database.mongodb not found")
	}

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
		tt := tt
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
			if matched != tt.wantDeny {
				t.Errorf("match = %v, want %v — %s", matched, tt.wantDeny, tt.reason)
			}
		})
	}
}

// --- SEC3: Redis Command Case Evasion ---

func TestDbSecurityRedisCaseEvasion(t *testing.T) {
	t.Parallel()

	redisPack := dbPack("database.redis")
	if redisPack == nil {
		t.Fatal("database.redis not found")
	}

	tests := []struct {
		cmd      string
		wantDeny bool
	}{
		{"FLUSHALL", true},
		{"flushall", true},
		{"FlushAll", true},
		{"FLUSHall", true},
		{"FLUSHDB", true},
		{"flushdb", true},
		{"FlushDb", true},
		{"DEL", true},
		{"del", true},
		{"Del", true},
		{"SHUTDOWN", true},
		{"shutdown", true},
		{"Shutdown", true},
		{"DEBUG", true},
		{"debug", true},
		{"Debug", true},
	}

	for _, tt := range tests {
		tt := tt
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
			if matched != tt.wantDeny {
				t.Errorf("redis-cli %s: match = %v, want %v", tt.cmd, matched, tt.wantDeny)
			}
		})
	}
}

// --- SEC4: Environment Sensitivity Preconditions ---

func TestDbSecurityEnvSensitivityPreconditions(t *testing.T) {
	t.Parallel()

	envSensitivePatterns := []struct {
		packID       string
		pattern      string
		baseSeverity guard.Severity
	}{
		{"database.postgresql", "psql-drop-database", guard.High},
		{"database.postgresql", "dropdb", guard.High},
		{"database.postgresql", "psql-drop-table", guard.High},
		{"database.postgresql", "psql-truncate", guard.High},
		{"database.postgresql", "psql-drop-schema", guard.High},
		{"database.mysql", "mysql-drop-database", guard.High},
		{"database.mysql", "mysql-drop-table", guard.High},
		{"database.mysql", "mysql-truncate", guard.High},
		{"database.mongodb", "mongo-drop-database", guard.High},
		{"database.mongodb", "mongo-collection-drop", guard.High},
		{"database.redis", "redis-flushall", guard.High},
		{"database.redis", "redis-flushdb", guard.High},
	}

	for _, tt := range envSensitivePatterns {
		tt := tt
		t.Run(tt.packID+"/"+tt.pattern, func(t *testing.T) {
			pack := dbPack(tt.packID)
			if pack == nil {
				t.Fatalf("pack %s not found", tt.packID)
			}
			dp := findDestructiveByName(pack, tt.pattern)
			if dp == nil {
				t.Fatalf("pattern %s not found", tt.pattern)
			}
			if !dp.EnvSensitive {
				t.Errorf("pattern should be env-sensitive")
			}
			if dp.Severity != tt.baseSeverity {
				t.Errorf("base severity = %v, want %v", dp.Severity, tt.baseSeverity)
			}
		})
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
