package parse

import (
	"context"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

func TestInlineDetection(t *testing.T) {
	t.Parallel()

	parser := NewBashParser()

	tests := []struct {
		name     string
		input    string
		contains []string
	}{
		{
			name:     "python -c os.system",
			input:    `python -c "import os; os.system('rm -rf /')"`,
			contains: []string{"python", "rm"},
		},
		{
			name:     "bash -c simple",
			input:    `bash -c "rm -rf /tmp/foo"`,
			contains: []string{"bash", "rm"},
		},
		{
			name:     "eval unquoted",
			input:    `eval rm -rf /`,
			contains: []string{"eval", "rm"},
		},
		{
			name:     "ruby -e system",
			input:    `ruby -e "system('git push --force')"`,
			contains: []string{"ruby", "git"},
		},
		{
			name:     "node --eval",
			input:    `node --eval "require('child_process').execSync('rm -rf /')"`,
			contains: []string{"node", "rm"},
		},
		{
			name:     "heredoc direct shell",
			input:    "bash <<'EOF'\nrm -rf /\nEOF",
			contains: []string{"bash", "rm"},
		},
		{
			name:     "heredoc pipeline shell",
			input:    "cat <<'EOF' | bash\nrm -rf /\nEOF",
			contains: []string{"cat", "bash", "rm"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := parser.ParseAndExtract(context.Background(), tc.input, 0)
			names := make(map[string]bool)
			for _, c := range res.Commands {
				names[c.Name] = true
			}
			for _, want := range tc.contains {
				if !names[want] {
					t.Fatalf("expected command %q in extraction result, got %#v", want, names)
				}
			}
		})
	}
}

func TestInlineDepthLimit(t *testing.T) {
	t.Parallel()

	parser := NewBashParser()
	detector := NewInlineDetector(parser)
	cmd := ExtractedCommand{
		Name:    "bash",
		RawArgs: []string{"-c", "rm -rf /"},
		Args:    []string{"rm -rf /"},
	}
	_, warns := detector.Detect(cmd, MaxInlineDepth)

	found := false
	for _, w := range warns {
		if w.Code == guard.WarnInlineDepthExceeded {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected WarnInlineDepthExceeded, got warnings %#v", warns)
	}
}
