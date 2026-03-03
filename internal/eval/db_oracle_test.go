package eval

// Database pack comparison oracle tests (O1-O3) from test harness plan 03b.
// O1: Upstream Rust comparison (skips without binary).
// O2: Cross-database severity/confidence consistency.
// O3: Policy monotonicity for database golden entries.

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
	"github.com/dcosson/destructive-command-guard-go/internal/packs"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/database"
	"github.com/dcosson/destructive-command-guard-go/internal/parse"
)

// --- O1: Upstream Rust Version Comparison ---

func TestDbComparisonUpstreamRust(t *testing.T) {
	if testing.Short() {
		t.Skip("comparison tests require upstream binary")
	}

	rustBin, err := exec.LookPath("destructive-command-guard")
	if err != nil {
		t.Skip("upstream Rust binary 'destructive-command-guard' not found in PATH")
	}

	pipeline := NewPipeline(parse.NewBashParser(), packs.DefaultRegistry)
	cfg := &EvalConfig{Policy: guard.InteractivePolicy()}

	corpus := []string{
		// PostgreSQL
		`psql -c "DROP DATABASE myapp"`,
		`psql -c "DROP TABLE users"`,
		`psql -c "TRUNCATE TABLE orders"`,
		`psql -c "DELETE FROM users"`,
		`psql -c "SELECT * FROM users"`,
		`psql -h localhost mydb`,
		`dropdb myapp`,
		`pg_dump --clean mydb`,
		`pg_dump mydb`,
		// MySQL
		`mysql -e "DROP DATABASE myapp"`,
		`mysql -e "DROP TABLE users"`,
		`mysql -e "SELECT * FROM users"`,
		`mysql -h localhost mydb`,
		`mysqladmin drop myapp`,
		`mysqldump mydb`,
		// SQLite
		`sqlite3 test.db "DROP TABLE users"`,
		`sqlite3 test.db "SELECT * FROM users"`,
		`sqlite3 test.db`,
		// MongoDB
		`mongosh --eval "db.dropDatabase()"`,
		`mongosh --eval "db.users.find()"`,
		`mongodump --out /backup/`,
		// Redis
		`redis-cli FLUSHALL`,
		`redis-cli GET mykey`,
		`redis-cli DEL mykey`,
		`redis-cli INFO`,
	}

	var divergences int
	for _, cmd := range corpus {
		t.Run(cmd, func(t *testing.T) {
			goResult := pipeline.Run(context.Background(), cmd, cfg)

			out, err := exec.Command(rustBin, "check", cmd).CombinedOutput()
			if err != nil {
				t.Logf("upstream error for %q: %v (output: %s)", cmd, err, string(out))
				return
			}
			rustDecision := parseUpstreamDecision(string(out))

			if goResult.Decision.String() != rustDecision {
				divergences++
				t.Logf("DIVERGENCE: %q go=%v rust=%s", cmd, goResult.Decision, rustDecision)
			}
		})
	}
	if divergences > 0 {
		t.Logf("Total divergences: %d/%d", divergences, len(corpus))
	}
}

// --- O2: Cross-Database Consistency ---

func TestDbComparisonCrossDatabaseConsistency(t *testing.T) {
	t.Parallel()

	equivalents := []struct {
		name     string
		commands map[string]parse.ExtractedCommand
	}{
		{
			"DROP TABLE",
			map[string]parse.ExtractedCommand{
				"database.postgresql": ecmd("psql", nil, flagMap("-c", "DROP TABLE users")),
				"database.mysql":      ecmd("mysql", nil, flagMap("-e", "DROP TABLE users")),
				"database.sqlite":     ecmd("sqlite3", []string{"test.db", "DROP TABLE users"}, nil),
			},
		},
		{
			"TRUNCATE",
			map[string]parse.ExtractedCommand{
				"database.postgresql": ecmd("psql", nil, flagMap("-c", "TRUNCATE users")),
				"database.mysql":      ecmd("mysql", nil, flagMap("-e", "TRUNCATE users")),
				// sqlite3 TRUNCATE is ConfidenceLow — expected divergence, excluded
			},
		},
		{
			"DELETE FROM no WHERE",
			map[string]parse.ExtractedCommand{
				"database.postgresql": ecmd("psql", nil, flagMap("-c", "DELETE FROM users")),
				"database.mysql":      ecmd("mysql", nil, flagMap("-e", "DELETE FROM users")),
				"database.sqlite":     ecmd("sqlite3", []string{"test.db", "DELETE FROM users"}, nil),
			},
		},
	}

	for _, eq := range equivalents {
		eq := eq
		t.Run(eq.name, func(t *testing.T) {
			var severities []guard.Severity
			var confidences []guard.Confidence
			for packID, testCmd := range eq.commands {
				pack := dbPack(packID)
				if pack == nil {
					t.Fatalf("pack %s not found", packID)
				}
				for _, dp := range pack.Destructive {
					if dp.Match.Match(testCmd) {
						severities = append(severities, dp.Severity)
						confidences = append(confidences, dp.Confidence)
						t.Logf("%s: severity=%v confidence=%v pattern=%s",
							packID, dp.Severity, dp.Confidence, dp.Name)
						break
					}
				}
			}
			if len(severities) == 0 {
				t.Fatal("no patterns matched")
			}
			for i := 1; i < len(severities); i++ {
				if severities[i] != severities[0] {
					t.Errorf("severity inconsistency: %v vs %v", severities[0], severities[i])
				}
			}
			for i := 1; i < len(confidences); i++ {
				if confidences[i] != confidences[0] {
					t.Errorf("confidence inconsistency: %v vs %v", confidences[0], confidences[i])
				}
			}
		})
	}
}

// --- O3: Policy Monotonicity ---

func TestDbComparisonPolicyMonotonicity(t *testing.T) {
	t.Parallel()

	pipeline := NewPipeline(parse.NewBashParser(), packs.DefaultRegistry)
	strictCfg := &EvalConfig{Policy: guard.StrictPolicy()}
	interCfg := &EvalConfig{Policy: guard.InteractivePolicy()}
	permCfg := &EvalConfig{Policy: guard.PermissivePolicy()}

	restrictiveness := map[guard.Decision]int{
		guard.Allow: 0,
		guard.Ask:   1,
		guard.Deny:  2,
	}

	// Load only database golden files
	entries := LoadCorpus(t, "testdata/golden")
	var dbEntries []GoldenEntry
	for _, e := range entries {
		if strings.HasPrefix(e.File, "testdata/golden/database_") ||
			strings.Contains(e.File, "/database_") {
			dbEntries = append(dbEntries, e)
		}
	}
	if len(dbEntries) == 0 {
		t.Skip("no database golden entries")
	}
	t.Logf("testing policy monotonicity across %d database golden entries", len(dbEntries))

	for _, e := range dbEntries {
		e := e
		name := e.Description
		if name == "" {
			name = e.Command
		}
		t.Run(name, func(t *testing.T) {
			sr := pipeline.Run(context.Background(), e.Command, strictCfg)
			ir := pipeline.Run(context.Background(), e.Command, interCfg)
			pr := pipeline.Run(context.Background(), e.Command, permCfg)

			sv := restrictiveness[sr.Decision]
			iv := restrictiveness[ir.Decision]
			pv := restrictiveness[pr.Decision]

			if sv < iv {
				t.Errorf("strict (%v) < interactive (%v) for %q",
					sr.Decision, ir.Decision, e.Command)
			}
			if iv < pv {
				t.Errorf("interactive (%v) < permissive (%v) for %q",
					ir.Decision, pr.Decision, e.Command)
			}
		})
	}
}
