package eval

import (
	"strings"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/internal/packs"
)

func BenchmarkPreFilter(b *testing.B) {
	pf := NewPreFilter(packs.DefaultRegistry)
	commands := []struct {
		name string
		cmd  string
	}{
		{"miss_echo", "echo hello world"},
		{"miss_ls", "ls -la /tmp"},
		{"hit_git", "git push --force"},
		{"hit_rm", "rm -rf /tmp/build"},
		{"hit_docker", "docker system prune -af"},
		{"miss_long", strings.Repeat("echo safe; ", 100)},
	}

	for _, tc := range commands {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = pf.MayContainDestructive(tc.cmd)
			}
		})
	}
}

func BenchmarkMatchCommand(b *testing.B) {
	pipeline := NewPipeline(packs.DefaultRegistry)
	commands := []struct {
		name string
		cmd  string
	}{
		{"destructive_git", "git push --force origin main"},
		{"destructive_rm", "rm -rf /"},
		{"destructive_rails", "RAILS_ENV=production rails db:reset"},
		{"safe_git", "git status"},
		{"safe_echo", "echo hello"},
		{"compound", "echo start && git push --force && rm -rf /tmp/build"},
	}

	cfg := Config{Policy: interactivePolicy{}}
	for _, tc := range commands {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = pipeline.Run(tc.cmd, cfg)
			}
		})
	}
}
