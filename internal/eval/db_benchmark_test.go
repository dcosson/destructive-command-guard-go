package eval

// Database pack benchmarks (B1-B3) and stress tests (S1-S2) from test harness
// plan 03b. Measures per-pack SQL pattern matching throughput, regex overhead,
// and concurrent safety.

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
	"github.com/dcosson/destructive-command-guard-go/internal/packs"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/database"
	"github.com/dcosson/destructive-command-guard-go/internal/parse"
)

// --- B1: Per-Pack SQL Pattern Matching Throughput ---

func BenchmarkPostgresPackMatch(b *testing.B) {
	pack := dbPack("database.postgresql")
	if pack == nil {
		b.Fatal("database.postgresql not found")
	}
	commands := map[string]parse.ExtractedCommand{
		"safe-select":     ecmd("psql", nil, flagMap("-c", "SELECT * FROM users")),
		"drop-database":   ecmd("psql", nil, flagMap("-c", "DROP DATABASE myapp")),
		"drop-table":      ecmd("psql", nil, flagMap("-c", "DROP TABLE users")),
		"delete-no-where": ecmd("psql", nil, flagMap("-c", "DELETE FROM users")),
		"interactive":     ecmd("psql", []string{"mydb"}, flagMap("-h", "localhost")),
		"dropdb":          ecmd("dropdb", []string{"myapp"}, nil),
	}
	for name, cmd := range commands {
		cmd := cmd
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				matchPack(pack, cmd)
			}
		})
	}
}

func BenchmarkMySQLPackMatch(b *testing.B) {
	pack := dbPack("database.mysql")
	if pack == nil {
		b.Fatal("database.mysql not found")
	}
	commands := map[string]parse.ExtractedCommand{
		"safe-select":   ecmd("mysql", nil, flagMap("-e", "SELECT * FROM users")),
		"drop-database": ecmd("mysql", nil, flagMap("-e", "DROP DATABASE myapp")),
		"mysqladmin":    ecmd("mysqladmin", []string{"drop", "myapp"}, nil),
		"interactive":   ecmd("mysql", []string{"mydb"}, flagMap("-h", "localhost")),
	}
	for name, cmd := range commands {
		cmd := cmd
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				matchPack(pack, cmd)
			}
		})
	}
}

func BenchmarkMongoPackMatch(b *testing.B) {
	pack := dbPack("database.mongodb")
	if pack == nil {
		b.Fatal("database.mongodb not found")
	}
	commands := map[string]parse.ExtractedCommand{
		"safe-find":     ecmd("mongosh", nil, flagMap("--eval", "db.users.find()")),
		"drop-database": ecmd("mongosh", nil, flagMap("--eval", "db.dropDatabase()")),
		"delete-many":   ecmd("mongosh", nil, flagMap("--eval", "db.users.deleteMany({})")),
		"interactive":   ecmd("mongosh", []string{"mongodb://localhost:27017"}, nil),
	}
	for name, cmd := range commands {
		cmd := cmd
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				matchPack(pack, cmd)
			}
		})
	}
}

func BenchmarkRedisPackMatch(b *testing.B) {
	pack := dbPack("database.redis")
	if pack == nil {
		b.Fatal("database.redis not found")
	}
	commands := map[string]parse.ExtractedCommand{
		"safe-get":    ecmd("redis-cli", []string{"GET", "mykey"}, nil),
		"flushall":    ecmd("redis-cli", []string{"FLUSHALL"}, nil),
		"del":         ecmd("redis-cli", []string{"DEL", "mykey"}, nil),
		"interactive": ecmd("redis-cli", nil, flagMap("-h", "localhost")),
	}
	for name, cmd := range commands {
		cmd := cmd
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				matchPack(pack, cmd)
			}
		})
	}
}

func BenchmarkSQLitePackMatch(b *testing.B) {
	pack := dbPack("database.sqlite")
	if pack == nil {
		b.Fatal("database.sqlite not found")
	}
	commands := map[string]parse.ExtractedCommand{
		"safe-select": ecmd("sqlite3", []string{"test.db", "SELECT * FROM users"}, nil),
		"drop-table":  ecmd("sqlite3", []string{"test.db", "DROP TABLE users"}, nil),
		"interactive": ecmd("sqlite3", []string{"test.db"}, nil),
	}
	for name, cmd := range commands {
		cmd := cmd
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				matchPack(pack, cmd)
			}
		})
	}
}

// --- B2: Database Golden File Corpus Throughput ---

func BenchmarkDbGoldenCorpus(b *testing.B) {
	pipeline := NewPipeline(parse.NewBashParser(), packs.DefaultRegistry)
	cfg := &EvalConfig{Policy: guard.InteractivePolicy()}

	entries := LoadCorpus(b, "testdata/golden")
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
			pipeline.Run(context.Background(), e.Command, cfg)
		}
	}
}

// --- B3: Regex Compilation Overhead ---

func BenchmarkSQLContentRegexMatch(b *testing.B) {
	// SQLContent pre-compiles the regex at init time.
	// This benchmark should show near-zero compilation overhead per call.
	pattern := packs.SQLContent(`\bDROP\s+TABLE\b`)
	cmd := parse.ExtractedCommand{
		Name:  "psql",
		Flags: map[string]string{"-c": "DROP TABLE users"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pattern.Match(cmd)
	}
}

func BenchmarkSQLContentRegexNoMatch(b *testing.B) {
	pattern := packs.SQLContent(`\bDROP\s+TABLE\b`)
	cmd := parse.ExtractedCommand{
		Name:  "psql",
		Flags: map[string]string{"-c": "SELECT * FROM users WHERE id = 1"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pattern.Match(cmd)
	}
}

// --- S1: Concurrent Database Pack Matching ---

func TestStressConcurrentDbMatching(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test")
	}

	commands := []parse.ExtractedCommand{
		ecmd("psql", nil, flagMap("-c", "DROP TABLE users")),
		ecmd("mysql", nil, flagMap("-e", "SELECT * FROM users")),
		ecmd("redis-cli", []string{"FLUSHALL"}, nil),
		ecmd("mongosh", nil, flagMap("--eval", "db.dropDatabase()")),
		ecmd("sqlite3", []string{"test.db", "DROP TABLE users"}, nil),
		ecmd("psql", nil, flagMap("-c", "SELECT * FROM users")),
		ecmd("redis-cli", []string{"GET", "mykey"}, nil),
		ecmd("mysql", nil, flagMap("-e", "DELETE FROM users")),
	}

	var wg sync.WaitGroup
	const goroutines = 100
	const iterations = 1000

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cmd := commands[idx%len(commands)]
			for j := 0; j < iterations; j++ {
				for _, id := range dbPackIDs {
					pack := dbPack(id)
					if pack == nil {
						continue
					}
					for _, dp := range pack.Destructive {
						dp.Match.Match(cmd)
					}
					for _, sp := range pack.Safe {
						sp.Match.Match(cmd)
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

	pipeline := NewPipeline(parse.NewBashParser(), packs.DefaultRegistry)
	cfg := &EvalConfig{Policy: guard.InteractivePolicy()}

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
				result := pipeline.Run(context.Background(), cmd, cfg)
				_ = result // verify no panics/races
			}
		}(i)
	}
	wg.Wait()
}
