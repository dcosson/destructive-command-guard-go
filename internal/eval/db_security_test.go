//go:build e2e

package eval

// Database pack security tests (SEC1-SEC4) from test harness plan 03b.
// SEC1: SQL pattern evasion attempts.
// SEC2: MongoDB shell expression evasion.
// SEC3: Redis command case evasion.
// SEC4: Environment sensitivity preconditions.

import (
	"testing"
)

// --- SEC1: SQL Pattern Evasion Attempts ---

func TestDbSecuritySQLPatternEvasion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cmd      string
		wantDeny bool
		reason   string
	}{
		// Case evasion
		{"mixed case DROP TABLE", `psql -c "DrOp TaBlE users"`, true,
			"case-insensitive regex should catch mixed case"},

		// Whitespace evasion
		{"extra whitespace", `psql -c "DROP    TABLE    users"`, true,
			`\s+ should handle multiple whitespace chars`},
		{"tab in keyword", "psql -c \"DROP\tTABLE users\"", true,
			`\s+ should match tab characters`},
		{"newline in keyword", "psql -c \"DROP\nTABLE users\"", true,
			`\s+ should match newline characters`},

		// Quoting
		{"quoted table name", `psql -c "DROP TABLE \"public\".\"users\""`, true,
			"quoted identifiers should not affect keyword match"},

		// Schema prefix
		{"schema-qualified drop", `psql -c "DROP TABLE public.users"`, true,
			"schema qualification should not affect match"},

		// IF EXISTS
		{"IF EXISTS variant", `psql -c "DROP TABLE IF EXISTS users"`, true,
			"IF EXISTS should not prevent match"},

		// CASCADE
		{"CASCADE variant", `psql -c "DROP TABLE users CASCADE"`, true,
			"CASCADE should not prevent match"},

		// Multi-statement with safe prefix
		{"safe prefix + destructive", `psql -c "BEGIN; DROP TABLE users; COMMIT;"`, true,
			"destructive in transaction should still be caught"},

		// MySQL-specific
		{"mysql mixed case", `mysql -e "dRoP dAtAbAsE myapp"`, true,
			"MySQL case insensitive"},

		// Long flag variants
		{"--command long flag", `psql --command "DROP TABLE users"`, true,
			"--command should work (raw string still contains DROP TABLE)"},
		{"--execute long flag", `mysql --execute "DROP DATABASE myapp"`, true,
			"--execute should work (raw string still contains DROP DATABASE)"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			matched := false
			packID := toolToPackID(extractTool(tt.cmd))
			pack := dbPack(packID)
			if pack == nil {
				t.Fatalf("pack for command %q not found", tt.cmd)
			}
			for _, dp := range pack.Destructive {
				if dp.Match != nil && matchRuleCommand(dp, tt.cmd) {
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

// extractTool returns the first word from a command string.
func extractTool(cmd string) string {
	for i, c := range cmd {
		if c == ' ' || c == '\t' {
			return cmd[:i]
		}
	}
	return cmd
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
		cmd      string
		wantDeny bool
		reason   string
	}{
		// Whitespace around parentheses
		{"extra whitespace in drop", `mongosh --eval "db.users.drop ()"`, true,
			`whitespace before ( should still match`},
		{"extra whitespace in dropDatabase", `mongosh --eval "db.dropDatabase ()"`, true,
			`whitespace before ( in dropDatabase`},

		// Chained operations
		{"chained after find", `mongosh --eval "db.users.find().drop()"`, true,
			"drop after find chain should match"},

		// Variable collection name
		{"variable collection", `mongosh --eval "db[collName].drop()"`, true,
			"bracket notation should match .drop("},

		// deleteMany with whitespace in empty filter
		{"deleteMany with spaced filter", `mongosh --eval "db.users.deleteMany( { } )"`, true,
			"whitespace in empty filter should match"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			matched := false
			for _, dp := range mongoPack.Destructive {
				if dp.Match != nil && matchRuleCommand(dp, tt.cmd) {
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
		{"redis-cli FLUSHALL", true},
		{"redis-cli flushall", true},
		{"redis-cli FlushAll", true},
		{"redis-cli FLUSHall", true},
		{"redis-cli FLUSHDB", true},
		{"redis-cli flushdb", true},
		{"redis-cli FlushDb", true},
		{"redis-cli DEL mykey", true},
		{"redis-cli del mykey", true},
		{"redis-cli Del mykey", true},
		{"redis-cli SHUTDOWN", true},
		{"redis-cli shutdown", true},
		{"redis-cli Shutdown", true},
		{"redis-cli DEBUG segfault", true},
		{"redis-cli debug segfault", true},
		{"redis-cli Debug segfault", true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.cmd, func(t *testing.T) {
			matched := false
			for _, dp := range redisPack.Destructive {
				if dp.Match != nil && matchRuleCommand(dp, tt.cmd) {
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
		ruleID       string
		baseSeverity int
	}{
		{"database.postgresql", "psql-drop-database", 3}, // High
		{"database.postgresql", "dropdb", 3},
		{"database.postgresql", "psql-drop-table", 3},
		{"database.postgresql", "psql-truncate", 3},
		{"database.postgresql", "psql-drop-schema", 3},
		{"database.mysql", "mysql-drop-database", 3},
		{"database.mysql", "mysql-drop-table", 3},
		{"database.mysql", "mysql-truncate", 3},
		{"database.mongodb", "mongo-drop-database", 3},
		{"database.mongodb", "mongo-collection-drop", 3},
		{"database.redis", "redis-flushall", 3},
		{"database.redis", "redis-flushdb", 3},
	}

	for _, tt := range envSensitivePatterns {
		tt := tt
		t.Run(tt.packID+"/"+tt.ruleID, func(t *testing.T) {
			pack := dbPack(tt.packID)
			if pack == nil {
				t.Fatalf("pack %s not found", tt.packID)
			}
			dp := findRuleByID(pack, tt.ruleID)
			if dp == nil {
				t.Fatalf("rule %s not found", tt.ruleID)
			}
			if !dp.EnvSensitive {
				t.Errorf("rule should be env-sensitive")
			}
			if dp.Severity != tt.baseSeverity {
				t.Errorf("base severity = %d, want %d", dp.Severity, tt.baseSeverity)
			}
		})
	}
}
