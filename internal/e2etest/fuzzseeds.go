package e2etest

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/dcosson/destructive-command-guard-go/internal/parse"
)

// LoadFuzzSeeds loads seed commands from golden files and edge-case templates.
func LoadFuzzSeeds(goldenDir string) []string {
	seeds := make([]string, 0, 64)
	path := filepath.Join(goldenDir, "commands.txt")
	f, err := os.Open(path)
	if err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			txt := strings.TrimSpace(sc.Text())
			if txt == "" || strings.HasPrefix(txt, "#") {
				continue
			}
			parts := strings.SplitN(txt, "|", 2)
			if len(parts) == 2 {
				seeds = append(seeds, strings.TrimSpace(parts[1]))
			}
		}
	}

	seeds = append(seeds,
		"", " ", "\t", "\n", "\x00",
		"echo hello",
		"git push --force",
		"rm -rf /",
		"RAILS_ENV=production rails db:reset",
		strings.Repeat("a", 1000),
		strings.Repeat("a", parse.MaxInputSize+1),
		"$($($(echo nested)))",
		`"unclosed string`,
		`'unclosed string`,
		"`unclosed backtick",
	)
	return dedupe(seeds)
}

func dedupe(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
