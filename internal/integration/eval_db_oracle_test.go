//go:build integration

package integration

// Database pack comparison oracle tests (O1-O3) from test harness plan 03b.
// O1: Upstream Rust comparison (skips without binary).
// O2: Cross-database severity/confidence consistency.
// O3: Policy monotonicity for database golden entries.

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/internal/eval"
	"github.com/dcosson/destructive-command-guard-go/internal/evalcore"
	"github.com/dcosson/destructive-command-guard-go/internal/packs"
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

	pipeline := eval.NewPipeline(packs.DefaultRegistry)
	cfg := eval.Config{DestructivePolicy: evalcore.InteractivePolicy(), PrivacyPolicy: evalcore.InteractivePolicy()}

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

	decisionStr := func(d eval.Decision) string {
		switch d {
		case eval.DecisionAllow:
			return "Allow"
		case eval.DecisionDeny:
			return "Deny"
		case eval.DecisionAsk:
			return "Ask"
		}
		return "Unknown"
	}

	var divergences int
	for _, cmd := range corpus {
		t.Run(cmd, func(t *testing.T) {
			goResult := pipeline.Run(cmd, cfg)

			out, err := exec.Command(rustBin, "check", cmd).CombinedOutput()
			if err != nil {
				t.Logf("upstream error for %q: %v (output: %s)", cmd, err, string(out))
				return
			}
			rustDecision := parseUpstreamDecision(string(out))

			if decisionStr(goResult.Decision) != rustDecision {
				divergences++
				t.Logf("DIVERGENCE: %q go=%s rust=%s", cmd, decisionStr(goResult.Decision), rustDecision)
			}
		})
	}
	if divergences > 0 {
		t.Logf("Total divergences: %d/%d", divergences, len(corpus))
	}
}

func parseUpstreamDecision(output string) string {
	output = strings.TrimSpace(output)
	switch {
	case strings.Contains(output, "allow"):
		return "Allow"
	case strings.Contains(output, "deny"):
		return "Deny"
	case strings.Contains(output, "ask"):
		return "Ask"
	}
	return output
}

// --- O2: Cross-Database Consistency ---

func TestDbComparisonCrossDatabaseConsistency(t *testing.T) {
	t.Parallel()

	equivalents := []struct {
		name     string
		commands map[string]string // packID → command
	}{
		{
			"DROP TABLE",
			map[string]string{
				"database.postgresql": `psql -c "DROP TABLE users"`,
				"database.mysql":      `mysql -e "DROP TABLE users"`,
				"database.sqlite":     `sqlite3 test.db "DROP TABLE users"`,
			},
		},
		{
			"TRUNCATE",
			map[string]string{
				"database.postgresql": `psql -c "TRUNCATE users"`,
				"database.mysql":      `mysql -e "TRUNCATE users"`,
				// sqlite3 TRUNCATE is ConfidenceLow — expected divergence, excluded
			},
		},
		{
			"DELETE FROM no WHERE",
			map[string]string{
				"database.postgresql": `psql -c "DELETE FROM users"`,
				"database.mysql":      `mysql -e "DELETE FROM users"`,
				"database.sqlite":     `sqlite3 test.db "DELETE FROM users"`,
			},
		},
	}

	for _, eq := range equivalents {
		eq := eq
		t.Run(eq.name, func(t *testing.T) {
			var severities []int
			var confidences []int
			for packID, cmd := range eq.commands {
				pack := dbPack(packID)
				if pack == nil {
					t.Fatalf("pack %s not found", packID)
				}
				for _, dp := range pack.Rules {
					if dp.Match != nil && matchRuleCommand(dp, cmd) {
						severities = append(severities, dp.Severity)
						confidences = append(confidences, dp.Confidence)
						t.Logf("%s: severity=%d confidence=%d rule=%s",
							packID, dp.Severity, dp.Confidence, dp.ID)
						break
					}
				}
			}
			if len(severities) == 0 {
				t.Fatal("no patterns matched")
			}
			for i := 1; i < len(severities); i++ {
				if severities[i] != severities[0] {
					t.Errorf("severity inconsistency: %d vs %d", severities[0], severities[i])
				}
			}
			for i := 1; i < len(confidences); i++ {
				if confidences[i] != confidences[0] {
					t.Errorf("confidence inconsistency: %d vs %d", confidences[0], confidences[i])
				}
			}
		})
	}
}

// --- O3: Policy Monotonicity ---

func TestDbComparisonPolicyMonotonicity(t *testing.T) {
	t.Parallel()

	pipeline := eval.NewPipeline(packs.DefaultRegistry)
	strictCfg := eval.Config{DestructivePolicy: evalcore.StrictPolicy(), PrivacyPolicy: evalcore.StrictPolicy()}
	interCfg := eval.Config{DestructivePolicy: evalcore.InteractivePolicy(), PrivacyPolicy: evalcore.InteractivePolicy()}
	permCfg := eval.Config{DestructivePolicy: evalcore.PermissivePolicy(), PrivacyPolicy: evalcore.PermissivePolicy()}

	restrictiveness := map[eval.Decision]int{
		eval.DecisionAllow: 0,
		eval.DecisionAsk:   1,
		eval.DecisionDeny:  2,
	}

	entries := LoadCorpus(t, filepath.Join("..", "eval", "testdata", "golden"))
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
			sr := pipeline.Run(e.Command, strictCfg)
			ir := pipeline.Run(e.Command, interCfg)
			pr := pipeline.Run(e.Command, permCfg)

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
