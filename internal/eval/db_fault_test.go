package eval

// Database pack fault injection tests (F1-F3) from test harness plan 03b.
// F1: Nil/empty/degenerate commands. F2: Pathological regex input. F3: SQL injection strings.

import (
	"fmt"
	"strings"
	"testing"
)

// --- F1: Degenerate Commands ---

func TestDbFaultDegenerateCommands(t *testing.T) {
	t.Parallel()

	degenerateCmds := []string{
		"",
		" ",
		"\t",
		"\n",
		"psql",
		"mysql",
		"redis-cli",
		"mongosh",
		"sqlite3",
		"psql ",
		"mysql ",
		string(make([]byte, 0)),
		"\x00",
		"psql\x00-c\x00DROP TABLE users",
	}

	for _, id := range dbPackIDs {
		pack := dbPack(id)
		if pack == nil {
			t.Fatalf("pack %s not found", id)
		}
		for i, cmd := range degenerateCmds {
			cmd := cmd
			t.Run(fmt.Sprintf("%s/degenerate-%d", id, i), func(t *testing.T) {
				for _, dp := range pack.Destructive {
					dp := dp
					func() {
						defer func() {
							if r := recover(); r != nil {
								t.Errorf("destructive %s panicked: %v", dp.ID, r)
							}
						}()
						if dp.Match != nil {
							dp.Match(cmd)
						}
					}()
				}
			})
		}
	}
}

// --- F2: Pathological Regex Input ---

func TestDbFaultPathologicalRegexInput(t *testing.T) {
	t.Parallel()

	pathological := []string{
		"psql -c \"" + strings.Repeat("DROP ", 10000) + "\"",
		"psql -c \"" + strings.Repeat("a", 100000) + "\"",
		"psql -c \"DROP\x00TABLE users\"",
		"psql -c \"" + strings.Repeat("DROP TABLE ", 100) + "WHERE 1=1\"",
		"psql -c \"" + string(make([]byte, 1000000)) + "\"",
	}

	pack := dbPack("database.postgresql")
	if pack == nil {
		t.Fatal("database.postgresql not found")
	}
	dropTableRule := findRuleByID(pack, "psql-drop-table")
	if dropTableRule == nil {
		t.Fatal("psql-drop-table not found")
	}

	for i, cmd := range pathological {
		cmd := cmd
		t.Run(fmt.Sprintf("pathological-%d", i), func(t *testing.T) {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("panicked on pathological input %d: %v", i, r)
					}
				}()
				dropTableRule.Match(cmd)
			}()
		})
	}
}

// --- F3: SQL Injection Strings ---

func TestDbFaultSQLInjectionStrings(t *testing.T) {
	t.Parallel()

	pgPack := dbPack("database.postgresql")
	if pgPack == nil {
		t.Fatal("database.postgresql not found")
	}
	dropTableRule := findRuleByID(pgPack, "psql-drop-table")
	if dropTableRule == nil {
		t.Fatal("psql-drop-table not found")
	}

	tests := []struct {
		name      string
		cmd       string
		wantMatch bool
	}{
		{"union select", `psql -c "' UNION SELECT * FROM users--"`, false},
		{"comment bypass", `psql -c "SELECT * FROM users /* DROP TABLE users */"`, true},
		{"string literal", `psql -c "SELECT 'DROP TABLE is dangerous'"`, true},
		{"double dash comment", `psql -c "SELECT 1 -- DROP TABLE users"`, true},
		{"nested quotes", `psql -c "SELECT \"DROP TABLE users\" FROM dual"`, true},
		{"no sql keywords", `psql -c "hello world 12345"`, false},
		{"empty flag value", `psql -c ""`, false},
		{"just whitespace", `psql -c "   	   "`, false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := dropTableRule.Match(tt.cmd)
			if got != tt.wantMatch {
				t.Errorf("match = %v, want %v for cmd: %q", got, tt.wantMatch, tt.cmd)
			}
		})
	}
}
