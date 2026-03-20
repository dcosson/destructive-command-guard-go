//go:build integration

package integration

// Database pack benchmarks (B1-B3) and stress tests (S1-S2) from test harness
// plan 03b. Measures per-pack matching throughput, regex overhead,
// and concurrent safety.

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/internal/eval"
	"github.com/dcosson/destructive-command-guard-go/internal/evalcore"
	"github.com/dcosson/destructive-command-guard-go/internal/packs"
	"github.com/dcosson/destructive-command-guard-go/internal/parse"
)

// --- B1: Per-Pack Matching Throughput ---

func BenchmarkPostgresPackMatch(b *testing.B) {
	pack := dbPack("database.postgresql")
	if pack == nil {
		b.Fatal("database.postgresql not found")
	}
	commands := map[string]string{
		"safe-select":     `psql -c "SELECT * FROM users"`,
		"drop-database":   `psql -c "DROP DATABASE myapp"`,
		"drop-table":      `psql -c "DROP TABLE users"`,
		"delete-no-where": `psql -c "DELETE FROM users"`,
		"interactive":     `psql -h localhost mydb`,
		"dropdb":          `dropdb myapp`,
	}
	for name, cmd := range commands {
		cmd := cmd
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				matchPackDestructive(pack, cmd)
			}
		})
	}
}

func BenchmarkMySQLPackMatch(b *testing.B) {
	pack := dbPack("database.mysql")
	if pack == nil {
		b.Fatal("database.mysql not found")
	}
	commands := map[string]string{
		"safe-select":   `mysql -e "SELECT * FROM users"`,
		"drop-database": `mysql -e "DROP DATABASE myapp"`,
		"mysqladmin":    `mysqladmin drop myapp`,
		"interactive":   `mysql -h localhost mydb`,
	}
	for name, cmd := range commands {
		cmd := cmd
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				matchPackDestructive(pack, cmd)
			}
		})
	}
}

func BenchmarkMongoPackMatch(b *testing.B) {
	pack := dbPack("database.mongodb")
	if pack == nil {
		b.Fatal("database.mongodb not found")
	}
	commands := map[string]string{
		"safe-find":     `mongosh --eval "db.users.find()"`,
		"drop-database": `mongosh --eval "db.dropDatabase()"`,
		"delete-many":   `mongosh --eval "db.users.deleteMany({})"`,
		"interactive":   `mongosh mongodb://localhost:27017`,
	}
	for name, cmd := range commands {
		cmd := cmd
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				matchPackDestructive(pack, cmd)
			}
		})
	}
}

func BenchmarkRedisPackMatch(b *testing.B) {
	pack := dbPack("database.redis")
	if pack == nil {
		b.Fatal("database.redis not found")
	}
	commands := map[string]string{
		"safe-get":    `redis-cli GET mykey`,
		"flushall":    `redis-cli FLUSHALL`,
		"del":         `redis-cli DEL mykey`,
		"interactive": `redis-cli -h localhost`,
	}
	for name, cmd := range commands {
		cmd := cmd
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				matchPackDestructive(pack, cmd)
			}
		})
	}
}

func BenchmarkSQLitePackMatch(b *testing.B) {
	pack := dbPack("database.sqlite")
	if pack == nil {
		b.Fatal("database.sqlite not found")
	}
	commands := map[string]string{
		"safe-select": `sqlite3 test.db "SELECT * FROM users"`,
		"drop-table":  `sqlite3 test.db "DROP TABLE users"`,
		"interactive": `sqlite3 test.db`,
	}
	for name, cmd := range commands {
		cmd := cmd
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				matchPackDestructive(pack, cmd)
			}
		})
	}
}

// --- B2: Database Golden File Corpus Throughput ---

func BenchmarkDbGoldenCorpus(b *testing.B) {
	pipeline := eval.NewPipeline(packs.DefaultRegistry)
	cfg := eval.Config{DestructivePolicy: evalcore.InteractivePolicy(), PrivacyPolicy: evalcore.InteractivePolicy()}

	entries := LoadCorpus(b, filepath.Join("..", "eval", "testdata", "golden"))
	var dbEntries []GoldenEntry
	for _, e := range entries {
		if strings.HasPrefix(e.File, "testdata/golden/database_") ||
			strings.Contains(e.File, "/database_") {
			dbEntries = append(dbEntries, e)
		}
	}
	if len(dbEntries) == 0 {
		b.Skip("no database golden entries")
	}
	b.Logf("corpus size: %d database entries", len(dbEntries))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, e := range dbEntries {
			pipeline.Run(e.Command, cfg)
		}
	}
}

// --- B3: Regex Match vs No-Match Overhead ---

func BenchmarkSQLRegexMatch(b *testing.B) {
	pack := dbPack("database.postgresql")
	if pack == nil {
		b.Fatal("database.postgresql not found")
	}
	dropTableRule := findRuleByID(pack, "psql-drop-table")
	if dropTableRule == nil {
		b.Fatal("psql-drop-table not found")
	}

	b.Run("match", func(b *testing.B) {
		cmd := `psql -c "DROP TABLE users"`
		for i := 0; i < b.N; i++ {
			matchRuleCommand(dropTableRule, cmd)
		}
	})

	b.Run("no-match", func(b *testing.B) {
		cmd := `psql -c "SELECT * FROM users WHERE id = 1"`
		for i := 0; i < b.N; i++ {
			matchRuleCommand(dropTableRule, cmd)
		}
	})
}

// --- S1: Concurrent Database Pack Matching ---

func TestStressConcurrentDbMatching(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test")
	}

	commands := []string{
		`psql -c "DROP TABLE users"`,
		`mysql -e "SELECT * FROM users"`,
		`redis-cli FLUSHALL`,
		`mongosh --eval "db.dropDatabase()"`,
		`sqlite3 test.db "DROP TABLE users"`,
		`psql -c "SELECT * FROM users"`,
		`redis-cli GET mykey`,
		`mysql -e "DELETE FROM users"`,
	}

	var wg sync.WaitGroup
	const goroutines = 16
	const iterations = 50

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			p := parse.NewBashParser()
			cmd := commands[idx%len(commands)]
			for j := 0; j < iterations; j++ {
				for _, id := range dbPackIDs {
					pack := dbPack(id)
					if pack == nil {
						continue
					}
					for _, dp := range pack.Rules {
						if dp.Match != nil {
							matchRuleCommand(dp, cmd, p)
						}
					}
				}
			}
		}(i)
	}
	wg.Wait()
}

// --- S2: High-Volume Mixed Database Commands ---

func TestStressHighVolumeDbCommands(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test")
	}

	pipeline := eval.NewPipeline(packs.DefaultRegistry)
	cfg := eval.Config{DestructivePolicy: evalcore.InteractivePolicy(), PrivacyPolicy: evalcore.InteractivePolicy()}

	commands := []string{
		`psql -c "DROP TABLE users"`,
		`psql -c "SELECT * FROM users"`,
		`mysql -e "DROP DATABASE myapp"`,
		`mysql -e "SELECT 1"`,
		`redis-cli FLUSHALL`,
		`redis-cli GET mykey`,
		`mongosh --eval "db.dropDatabase()"`,
		`mongosh --eval "db.users.find()"`,
		`sqlite3 test.db "DROP TABLE users"`,
		`sqlite3 test.db "SELECT * FROM users"`,
	}

	var wg sync.WaitGroup
	const workers = 50
	const iterationsPerWorker = 100

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < iterationsPerWorker; j++ {
				cmd := commands[(worker*iterationsPerWorker+j)%len(commands)]
				result := pipeline.Run(cmd, cfg)
				_ = result // verify no panics/races
			}
		}(i)
	}
	wg.Wait()
}
