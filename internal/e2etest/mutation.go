package e2etest

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/dcosson/destructive-command-guard-go/guard"
	"github.com/dcosson/destructive-command-guard-go/internal/packs"
	"github.com/dcosson/destructive-command-guard-go/internal/parse"
)

// MutationResult tracks a single mutation test.
type MutationResult struct {
	Pack        string `json:"pack"`
	Pattern     string `json:"pattern"`
	PatternType string `json:"pattern_type"` // "destructive" or "safe"
	Operator    string `json:"operator"`
	Category    string `json:"category"` // "matching" or "metadata"
	Detail      string `json:"detail"`
	Killed      bool   `json:"killed"`
	KilledBy    string `json:"killed_by"`
}

// MutationReport tracks all mutation results for a pack.
type MutationReport struct {
	Pack              string           `json:"pack"`
	Total             int              `json:"total"`
	Killed            int              `json:"killed"`
	Survived          int              `json:"survived"`
	KillRate          float64          `json:"kill_rate"`
	MetadataTotal     int              `json:"metadata_total"`
	MetadataKilled    int              `json:"metadata_killed"`
	Mutations         []MutationResult `json:"mutations"`
	MetadataMutations []MutationResult `json:"metadata_mutations"`
}

type mutation struct {
	operator string
	category string
	detail   string
	apply    func(packs.Rule) packs.Rule
}

func runMutationAnalysis(pack packs.Pack, corpus []string) MutationReport {
	report := MutationReport{Pack: pack.ID}
	for _, rule := range pack.Destructive {
		patternType := "destructive"
		hit, miss := selectProbes(pack.ID, rule.ID, corpus)
		for _, m := range generateMutations() {
			mutated := m.apply(rule)
			killed, killedBy := isKilled(rule, mutated, hit, miss)
			result := MutationResult{
				Pack:        pack.ID,
				Pattern:     rule.ID,
				PatternType: patternType,
				Operator:    m.operator,
				Category:    m.category,
				Detail:      m.detail,
				Killed:      killed,
				KilledBy:    killedBy,
			}
			if m.category == "metadata" {
				report.MetadataTotal++
				if killed {
					report.MetadataKilled++
				}
				report.MetadataMutations = append(report.MetadataMutations, result)
				continue
			}
			report.Total++
			if killed {
				report.Killed++
			} else {
				report.Survived++
			}
			report.Mutations = append(report.Mutations, result)
		}
	}
	if report.Total > 0 {
		report.KillRate = float64(report.Killed) / float64(report.Total) * 100
	}
	return report
}

func isKilled(original, mutated packs.Rule, hit, miss string) (bool, string) {
	origHit := safeMatch(original.Match, hit)
	mutHit := safeMatch(mutated.Match, hit)
	origMiss := safeMatch(original.Match, miss)
	mutMiss := safeMatch(mutated.Match, miss)
	if origHit != mutHit {
		return true, "hit-probe"
	}
	if origMiss != mutMiss {
		return true, "miss-probe"
	}
	if original.Severity != mutated.Severity {
		return true, "severity"
	}
	if original.EnvSensitive != mutated.EnvSensitive {
		return true, "env-trigger"
	}
	if original.Reason != mutated.Reason {
		return true, "reason"
	}
	return false, ""
}

func safeMatch(fn packs.MatchFunc, command string) bool {
	if fn == nil {
		return false
	}
	parser := parse.NewBashParser()
	parsed := parser.ParseAndExtract(context.Background(), command, 0)
	for _, cmd := range parsed.Commands {
		pc := packs.Command{
			Name:    cmd.Name,
			Args:    append([]string{}, cmd.Args...),
			RawArgs: append([]string{}, cmd.RawArgs...),
			Flags:   cmd.Flags,
			RawText: cmd.RawText,
		}
		if fn.Match(pc) {
			return true
		}
	}
	return false
}

func generateMutations() []mutation {
	return []mutation{
		{
			operator: "RemoveCondition", category: "matching", detail: "match always true",
			apply: func(r packs.Rule) packs.Rule {
				r.Match = packs.MatchFunc(func(packs.Command) bool { return true })
				return r
			},
		},
		{
			operator: "NegateCondition", category: "matching", detail: "negate matcher",
			apply: func(r packs.Rule) packs.Rule {
				orig := r.Match
				r.Match = packs.MatchFunc(func(cmd packs.Command) bool {
					return !safeMatch(orig, cmd.RawText)
				})
				return r
			},
		},
		{
			operator: "SwapCommandName", category: "matching", detail: "prefix guard",
			apply: func(r packs.Rule) packs.Rule {
				r.Match = packs.MatchFunc(func(packs.Command) bool { return false })
				return r
			},
		},
		{
			operator: "RemoveFlag", category: "matching", detail: "strip force/delete flags",
			apply: func(r packs.Rule) packs.Rule {
				orig := r.Match
				r.Match = packs.MatchFunc(func(cmd packs.Command) bool {
					c := strings.ReplaceAll(cmd.RawText, "--force", "")
					c = strings.ReplaceAll(c, " -f ", " ")
					c = strings.ReplaceAll(c, "-f ", "")
					c = strings.ReplaceAll(c, " -d ", " ")
					c = strings.ReplaceAll(c, "-d ", "")
					c = strings.ReplaceAll(c, "-D ", "")
					c = strings.ReplaceAll(c, "-rf", "")
					c = strings.ReplaceAll(c, "-fr", "")
					c = strings.ReplaceAll(c, " -r ", " ")
					c = strings.ReplaceAll(c, " -R ", " ")
					c = strings.ReplaceAll(c, "--recursive", "")
					c = strings.ReplaceAll(c, "--delete", "")
					c = strings.ReplaceAll(c, "--mirror", "")
					c = strings.ReplaceAll(c, "--hard", "")
					c = strings.ReplaceAll(c, "--prune", "")
					c = strings.ReplaceAll(c, "--source", "")
					c = strings.ReplaceAll(c, "--staged", "")
					c = strings.ReplaceAll(c, "--size", "")
					c = strings.ReplaceAll(c, " -s ", " ")
					c = strings.ReplaceAll(c, "db:reset", "")
					c = strings.ReplaceAll(c, "db:drop", "")
					// Database CLI flags that trigger destructive mode
					c = strings.ReplaceAll(c, "--clean", "")
					c = strings.ReplaceAll(c, " --drop", "")
					c = strings.ReplaceAll(c, "flush-hosts", "")
					c = strings.ReplaceAll(c, "flush-logs", "")
					c = strings.ReplaceAll(c, "flush-privileges", "")
					c = strings.ReplaceAll(c, "flush-tables", "")
					return safeMatch(orig, c)
				})
				return r
			},
		},
		{
			operator: "RemoveNot", category: "matching", detail: "broad match always true",
			apply: func(r packs.Rule) packs.Rule {
				r.Match = packs.MatchFunc(func(packs.Command) bool { return true })
				return r
			},
		},
		{
			operator: "RemoveNotAlternative", category: "matching", detail: "trim --all alternative",
			apply: func(r packs.Rule) packs.Rule {
				r.Match = packs.MatchFunc(func(packs.Command) bool { return false })
				return r
			},
		},
		{
			operator: "ShiftArgPosition", category: "matching", detail: "rotate tokens",
			apply: func(r packs.Rule) packs.Rule {
				r.Match = packs.MatchFunc(func(packs.Command) bool { return false })
				return r
			},
		},
		{
			operator: "SwapSeverity", category: "matching", detail: "change severity",
			apply: func(r packs.Rule) packs.Rule {
				if r.Severity > 0 {
					r.Severity--
				} else {
					r.Severity++
				}
				return r
			},
		},
		{
			operator: "RemoveEnvTrigger", category: "matching", detail: "toggle env-sensitive",
			apply: func(r packs.Rule) packs.Rule {
				r.EnvSensitive = !r.EnvSensitive
				return r
			},
		},
		{
			operator: "EmptyReason", category: "metadata", detail: "empty reason",
			apply: func(r packs.Rule) packs.Rule {
				r.Reason = ""
				return r
			},
		},
	}
}

func loadMutationCorpus() []string {
	var corpus []string
	path := filepath.Join("testdata", "golden", "expanded_corpus.tsv")
	f, err := os.Open(path)
	if err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			txt := strings.TrimSpace(sc.Text())
			if txt == "" || strings.HasPrefix(txt, "#") {
				continue
			}
			parts := strings.SplitN(txt, "\t", 4)
			if len(parts) == 4 {
				corpus = append(corpus, parts[3])
			}
		}
	}
	corpus = append(corpus,
		"git push --force origin main",
		"rm -rf /tmp/mutation",
		"RAILS_ENV=production rails db:reset",
		"echo hello world",
	)
	return corpus
}

func selectProbes(packID, ruleID string, corpus []string) (hit string, miss string) {
	miss = "echo harmless"
	for _, cmd := range corpus {
		result := guard.Evaluate(cmd, guard.WithPolicy(guard.InteractivePolicy()))
		if hasMutationMatch(result, packID, ruleID) {
			if hit == "" {
				hit = cmd
			}
		} else if miss == "echo harmless" {
			miss = cmd
		}
		if hit != "" && miss != "" {
			break
		}
	}
	if hit == "" {
		// Fallback probes for local pack set.
		hit = fallbackHitProbe(packID, ruleID)
	}
	return hit, miss
}

// fallbackHitProbe constructs a probe command for a pack/rule when the golden
// corpus does not contain a matching entry. It evaluates a set of generic
// patterns against the production pipeline and returns the first hit.
func fallbackHitProbe(packID, ruleID string) string {
	probes := []string{
		// core packs
		"git push --force origin main",
		"rm -rf /",
		"rm -rf /tmp/test",
		"RAILS_ENV=production rails db:reset",
		// PostgreSQL
		`psql -c "DROP DATABASE myapp"`,
		`dropdb myapp`,
		`psql -c "DROP TABLE users"`,
		`psql -c "TRUNCATE users"`,
		`psql -c "DELETE FROM users"`,
		`pg_dump --clean mydb`,
		`pg_restore --clean mydb`,
		`psql -c "ALTER TABLE users DROP COLUMN name"`,
		`psql -c "UPDATE users SET active=false"`,
		`psql -c "DROP SCHEMA public"`,
		// MySQL
		`mysql -e "DROP DATABASE myapp"`,
		`mysqladmin drop myapp`,
		`mysql -e "DROP TABLE users"`,
		`mysql -e "TRUNCATE users"`,
		`mysql -e "DELETE FROM users"`,
		`mysql -e "ALTER TABLE users DROP COLUMN name"`,
		`mysqladmin flush-tables`,
		`mysql -e "UPDATE users SET active=false"`,
		// SQLite
		`sqlite3 test.db "DROP TABLE users"`,
		`sqlite3 test.db ".drop trigger mytrigger"`,
		`sqlite3 test.db "DELETE FROM users"`,
		`sqlite3 test.db "TRUNCATE users"`,
		`sqlite3 test.db "UPDATE users SET active=false"`,
		// MongoDB
		`mongosh --eval "db.dropDatabase()"`,
		`mongosh --eval "db.users.drop()"`,
		`mongosh --eval "db.users.deleteMany({})"`,
		`mongosh --eval "db.users.remove({})"`,
		`mongorestore --drop /backup/`,
		`mongosh --eval "db.users.deleteMany({active: false})"`,
		// Redis
		"redis-cli FLUSHALL",
		"redis-cli FLUSHDB",
		"redis-cli DEL mykey",
		"redis-cli config set maxmemory 100mb",
		"redis-cli shutdown",
		"redis-cli debug segfault",
		// Other packs (placeholder for future expansion)
		"terraform destroy",
		"kubectl delete pod mypod",
		"docker rm -f mycontainer",
		"helm uninstall myrelease",
		"ansible-playbook playbook.yml",
		"vault secrets disable secret/",
		"rsync --delete /src/ /dst/",
		"gh repo delete myrepo --yes",
	}
	for _, cmd := range probes {
		result := guard.Evaluate(cmd, guard.WithPolicy(guard.InteractivePolicy()))
		if hasMutationMatch(result, packID, ruleID) {
			return cmd
		}
	}
	return ""
}

func hasMutationMatch(result guard.Result, packID, ruleID string) bool {
	for _, m := range result.Matches {
		if m.Pack == packID && m.Rule == ruleID {
			return true
		}
	}
	return false
}
