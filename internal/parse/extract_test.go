package parse

import (
	"context"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

func TestParseAndExtractBasicCases(t *testing.T) {
	t.Parallel()

	parser := NewBashParser()

	tests := []struct {
		name      string
		input     string
		wantCount int
		check     func(t *testing.T, got []ExtractedCommand)
	}{
		{
			name:      "simple command",
			input:     "ls",
			wantCount: 1,
			check: func(t *testing.T, got []ExtractedCommand) {
				if got[0].Name != "ls" {
					t.Fatalf("expected ls, got %q", got[0].Name)
				}
			},
		},
		{
			name:      "path normalization and flags",
			input:     "/usr/bin/git push --force origin main",
			wantCount: 1,
			check: func(t *testing.T, got []ExtractedCommand) {
				cmd := got[0]
				if cmd.Name != "git" {
					t.Fatalf("expected normalized name git, got %q", cmd.Name)
				}
				if _, ok := cmd.Flags["--force"]; !ok {
					t.Fatalf("expected --force flag in %#v", cmd.Flags)
				}
			},
		},
		{
			name:      "inline env assignment",
			input:     "RAILS_ENV=production rails db:reset",
			wantCount: 1,
			check: func(t *testing.T, got []ExtractedCommand) {
				cmd := got[0]
				if cmd.InlineEnv["RAILS_ENV"] != "production" {
					t.Fatalf("expected RAILS_ENV inline env, got %#v", cmd.InlineEnv)
				}
				if cmd.Name != "rails" {
					t.Fatalf("expected rails command, got %q", cmd.Name)
				}
			},
		},
		{
			name:      "pipeline extraction",
			input:     "cat file | grep foo",
			wantCount: 2,
			check: func(t *testing.T, got []ExtractedCommand) {
				if got[0].Name != "cat" || got[1].Name != "grep" {
					t.Fatalf("unexpected pipeline command names: %q, %q", got[0].Name, got[1].Name)
				}
				if !got[0].InPipeline || !got[1].InPipeline {
					t.Fatalf("expected both commands to be marked in-pipeline")
				}
			},
		},
		{
			name:      "compound extraction",
			input:     "echo ok && rm -rf /tmp/a",
			wantCount: 2,
			check: func(t *testing.T, got []ExtractedCommand) {
				if got[1].Name != "rm" {
					t.Fatalf("expected second command rm, got %q", got[1].Name)
				}
				if _, ok := got[1].Flags["-r"]; !ok {
					t.Fatalf("expected -r short flag split")
				}
				if _, ok := got[1].Flags["-f"]; !ok {
					t.Fatalf("expected -f short flag split")
				}
			},
		},
		{
			name:      "env prefix unwrap",
			input:     "env PATH=/tmp /usr/bin/git status",
			wantCount: 1,
			check: func(t *testing.T, got []ExtractedCommand) {
				cmd := got[0]
				if cmd.Name != "git" {
					t.Fatalf("expected env-unwrapped command git, got %q", cmd.Name)
				}
				if cmd.InlineEnv["PATH"] != "/tmp" {
					t.Fatalf("expected PATH inline env from env prefix")
				}
			},
		},
		{
			name:      "sequential assignment dataflow",
			input:     "DIR=/tmp; rm -rf $DIR",
			wantCount: 1,
			check: func(t *testing.T, got []ExtractedCommand) {
				cmd := got[0]
				if len(cmd.Args) == 0 || cmd.Args[len(cmd.Args)-1] != "/tmp" {
					t.Fatalf("expected resolved arg /tmp, got %#v", cmd.Args)
				}
				if !cmd.DataflowResolved {
					t.Fatalf("expected dataflow-resolved command")
				}
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := parser.ParseAndExtract(context.Background(), tc.input, 0)
			if len(result.Commands) != tc.wantCount {
				t.Fatalf("command count = %d, want %d (%#v)", len(result.Commands), tc.wantCount, result.Commands)
			}
			tc.check(t, result.Commands)
		})
	}
}

func TestParseAndExtractDataflowBranching(t *testing.T) {
	t.Parallel()

	parser := NewBashParser()

	t.Run("and chain may alias produces variants", func(t *testing.T) {
		t.Parallel()
		result := parser.ParseAndExtract(context.Background(), "DIR=/ && DIR=/tmp && rm -rf $DIR", 0)
		if len(result.Commands) != 2 {
			t.Fatalf("expected 2 rm variants, got %d: %#v", len(result.Commands), result.Commands)
		}
		seen := map[string]bool{}
		for _, cmd := range result.Commands {
			if cmd.Name != "rm" {
				t.Fatalf("expected rm command, got %q", cmd.Name)
			}
			if len(cmd.Args) == 0 {
				t.Fatalf("expected args on rm variant")
			}
			seen[cmd.Args[len(cmd.Args)-1]] = true
			if !cmd.DataflowResolved {
				t.Fatalf("expected DataflowResolved=true on variant")
			}
		}
		if !seen["/"] || !seen["/tmp"] {
			t.Fatalf("expected both / and /tmp variants, saw %#v", seen)
		}
	})

	t.Run("inline env does not leak", func(t *testing.T) {
		t.Parallel()
		result := parser.ParseAndExtract(context.Background(), "TMP=/safe echo ok; echo $TMP", 0)
		if len(result.Commands) != 2 {
			t.Fatalf("expected 2 commands, got %d", len(result.Commands))
		}
		second := result.Commands[1]
		if len(second.Args) == 0 || second.Args[0] != "$TMP" {
			t.Fatalf("inline env leaked into second command args: %#v", second.Args)
		}
	})

	t.Run("command substitution warning", func(t *testing.T) {
		t.Parallel()
		result := parser.ParseAndExtract(context.Background(), "export FILE=$(mktemp); rm -rf $FILE", 0)
		found := false
		for _, w := range result.Warnings {
			if w.Code == guard.WarnCommandSubstitution {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected WarnCommandSubstitution in warnings: %#v", result.Warnings)
		}
	})
}
