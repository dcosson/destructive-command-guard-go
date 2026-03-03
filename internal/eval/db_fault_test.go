package eval

// Database pack fault injection tests (F1-F3) from test harness plan 03b.
// F1: Nil/empty fields, F2: pathological regex input, F3: SQL injection strings.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/internal/packs"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/database"
	"github.com/dcosson/destructive-command-guard-go/internal/parse"
)

// --- F1: Nil/Empty Fields in ExtractedCommand ---

func TestDbFaultNilFields(t *testing.T) {
	t.Parallel()

	degenerateCmds := []parse.ExtractedCommand{
		{Name: "psql", Args: nil, Flags: nil},
		{Name: "mysql", Args: nil, Flags: nil},
		{Name: "redis-cli", Args: nil, Flags: nil},
		{Name: "mongosh", Args: nil, Flags: nil},
		{Name: "sqlite3", Args: nil, Flags: nil},
		{Name: "psql", Args: []string{}, Flags: map[string]string{}},
		{Name: "", Args: nil, Flags: nil},
		{Name: "psql", Args: []string{""}, Flags: map[string]string{"": ""}},
		{Name: "psql", Flags: map[string]string{"-c": ""}},
		{Name: "mysql", Flags: map[string]string{"-e": ""}},
		{Name: "redis-cli", Args: []string{""}, Flags: nil},
		{Name: "mongosh", Flags: map[string]string{"--eval": ""}},
		{Name: "sqlite3", Args: []string{""}, Flags: nil},
	}

	for _, id := range dbPackIDs {
		pack := dbPack(id)
		if pack == nil {
			t.Fatalf("pack %s not found", id)
		}
		for i, c := range degenerateCmds {
			c := c
			t.Run(fmt.Sprintf("%s/degenerate-%d", id, i), func(t *testing.T) {
				for _, dp := range pack.Destructive {
					dp := dp
					func() {
						defer func() {
							if r := recover(); r != nil {
								t.Errorf("destructive %s panicked: %v", dp.Name, r)
							}
						}()
						dp.Match.Match(c)
					}()
				}
				for _, sp := range pack.Safe {
					sp := sp
					func() {
						defer func() {
							if r := recover(); r != nil {
								t.Errorf("safe %s panicked: %v", sp.Name, r)
							}
						}()
						sp.Match.Match(c)
					}()
				}
			})
		}
	}
}

// --- F2: ArgContentMatcher with Pathological Regex Input ---

func TestDbFaultPathologicalRegexInput(t *testing.T) {
	t.Parallel()

	dropTableMatcher := packs.SQLContent(`\bDROP\s+TABLE\b`)

	pathological := []string{
		strings.Repeat("DROP ", 10000),
		strings.Repeat("a", 100000),
		"DROP\x00TABLE users",
		strings.Repeat("DROP TABLE ", 100) + "WHERE 1=1",
		string(make([]byte, 1000000)),
	}

	for i, s := range pathological {
		s := s
		t.Run(fmt.Sprintf("pathological-%d", i), func(t *testing.T) {
			cmd := parse.ExtractedCommand{
				Name:  "psql",
				Flags: map[string]string{"-c": s},
			}
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("panicked on pathological input %d: %v", i, r)
					}
				}()
				dropTableMatcher.Match(cmd)
			}()
		})
	}
}

// --- F3: Flag Value with SQL Injection Attempts ---

func TestDbFaultSQLInjectionStrings(t *testing.T) {
	t.Parallel()

	pgPack := dbPack("database.postgresql")
	if pgPack == nil {
		t.Fatal("database.postgresql not found")
	}
	dropTablePattern := findDestructiveByName(pgPack, "psql-drop-table")
	if dropTablePattern == nil {
		t.Fatal("psql-drop-table pattern not found")
	}

	tests := []struct {
		name      string
		sql       string
		wantMatch bool
	}{
		{"union select", "' UNION SELECT * FROM users--", false},
		{"comment bypass", "SELECT * FROM users /* DROP TABLE users */", true},
		{"string literal", "SELECT 'DROP TABLE is dangerous'", true},
		{"double dash comment", "SELECT 1 -- DROP TABLE users", true},
		{"nested quotes", `SELECT "DROP TABLE users" FROM dual`, true},
		{"no sql keywords", "hello world 12345", false},
		{"empty string", "", false},
		{"just whitespace", "   \t\n   ", false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			cmd := parse.ExtractedCommand{
				Name:  "psql",
				Flags: map[string]string{"-c": tt.sql},
			}
			got := dropTablePattern.Match.Match(cmd)
			if got != tt.wantMatch {
				t.Errorf("match = %v, want %v for sql: %q", got, tt.wantMatch, tt.sql)
			}
		})
	}
}
