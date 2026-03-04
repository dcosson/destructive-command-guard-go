package parse

import (
	"context"
	"strings"
	"testing"
)

// FZ1: Parse Fuzzing — Go native fuzzing target.
// Verifies parse never panics, warnings are valid, and tree structure invariants hold.
func FuzzParse(f *testing.F) {
	// Seed corpus: representative commands covering different constructs
	seeds := []string{
		// Simple commands
		"git push --force",
		"rm -rf /",
		"echo 'hello world'",
		"ls -la /tmp",

		// Env prefix
		"RAILS_ENV=production rails db:reset",
		"NODE_ENV=production npm run build",

		// Inline scripts
		`python -c "import os"`,
		`bash -c "echo hello"`,
		`ruby -e "puts 'test'"`,

		// Compound commands
		"echo a && echo b",
		"echo a || echo b",
		"echo a; echo b",
		"echo a | grep a",

		// Quoting
		`echo "double quoted"`,
		`echo 'single quoted'`,
		"echo $'escape\\nsequence'",

		// Variable operations
		"DIR=/tmp; echo $DIR",
		"export FOO=bar",
		"A=$(echo hello)",

		// Redirections
		"echo hello > /tmp/out",
		"cat < /tmp/in",
		"echo hello 2>&1",

		// Special constructs
		"(echo subshell)",
		"echo $(command substitution)",
		"! echo negated",

		// Edge cases
		"",
		"   ",
		strings.Repeat("a", 1000),

		// Heredoc
		"bash <<'EOF'\necho hello\nEOF",

		// Path prefixed
		"/usr/bin/git push",
		"./script.sh",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	bp := NewBashParser()

	f.Fuzz(func(t *testing.T, input string) {
		// Must not panic (deferred recover is the actual test)
		tree, warnings := bp.Parse(context.Background(), input)

		// Warning codes must be recognized
		for _, w := range warnings {
			switch w.Code {
			case WarnPartialParse, WarnInputTruncated, WarnExtractorPanic,
				WarnInlineDepthExceeded, WarnExpansionCapped,
				WarnCommandSubstitution, WarnMatcherPanic, WarnUnknownPackID:
				// valid
			default:
				t.Errorf("unrecognized warning code: %d", w.Code)
			}
			if w.Message == "" {
				t.Error("warning has empty message")
			}
		}

		// If input exceeds max, tree must be nil with size warning
		if len(input) > MaxInputSize {
			if tree != nil {
				t.Error("expected nil tree for oversized input")
			}
			if !hasWarning(warnings, WarnInputTruncated) {
				t.Error("expected WarnInputTruncated for oversized input")
			}
			return
		}

		// If tree is non-nil, check structural invariants
		if tree != nil {
			root := tree.RootNode()
			if root.Type() != "program" {
				t.Errorf("root node type is %q, expected 'program'", root.Type())
			}
		}
	})
}

// FZ1b: Parse + Extract fuzzing — verifies full pipeline never panics.
func FuzzParseAndExtract(f *testing.F) {
	seeds := []string{
		"git push --force",
		"rm -rf /",
		"RAILS_ENV=production rails db:reset",
		`python -c "import os; os.system('ls')"`,
		"DIR=/tmp; rm -rf $DIR",
		"echo a && echo b || echo c",
		"cat file | grep pattern | sort",
		"",
		"(subshell command)",
		"! negated command",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	bp := NewBashParser()

	f.Fuzz(func(t *testing.T, input string) {
		// Full pipeline: parse + extract — must not panic
		tree, _ := bp.Parse(context.Background(), input)
		if tree != nil {
			result := NewCommandExtractor(bp).Extract(tree, input)
			for _, cmd := range result.Commands {
				if cmd.Name == "" {
					t.Error("extracted command with empty name")
				}
			}
		}
	})
}

// FZ2: Normalize fuzzing — verifies Normalize is safe for all inputs.
func FuzzNormalize(f *testing.F) {
	seeds := []string{
		"git",
		"/usr/bin/git",
		"/usr/local/bin/rm",
		"./script.sh",
		"/",
		"",
		"a/b/c/d",
		"/bin/",
		"///",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		result := Normalize(input)

		// Idempotent: Normalize(Normalize(x)) == Normalize(x)
		if Normalize(result) != result {
			t.Errorf("Normalize not idempotent: Normalize(%q)=%q, Normalize(%q)=%q",
				input, result, result, Normalize(result))
		}

		// Result should never contain path separator (unless input has no separator)
		if strings.Contains(input, "/") && strings.Contains(result, "/") {
			t.Errorf("Normalize(%q) = %q still contains /", input, result)
		}
	})
}

// FZ3: Dataflow fuzzing — verifies expansion is always bounded.
func FuzzDataflow(f *testing.F) {
	seeds := []string{
		"DIR=/tmp; rm -rf $DIR",
		"A=1 || A=2; echo $A",
		"export FOO=bar && echo $FOO",
		"X=$(cmd); echo $X",
		"A=1; B=2; C=3; echo $A $B $C",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	bp := NewBashParser()

	f.Fuzz(func(t *testing.T, input string) {
		tree, _ := bp.Parse(context.Background(), input)
		if tree == nil {
			return
		}
		result := NewCommandExtractor(bp).Extract(tree, input)

		// Verify expansion cap invariant: no more than maxExpansions (16)
		// command variants should be produced for any single command name.
		// Group extracted commands by (Name, StartByte) to count variants.
		type cmdKey struct {
			name      string
			startByte uint32
		}
		variants := map[cmdKey]int{}
		for _, cmd := range result.Commands {
			k := cmdKey{cmd.Name, cmd.StartByte}
			variants[k]++
		}
		for k, count := range variants {
			if count > maxExpansions {
				t.Errorf("command %q at byte %d produced %d variants, exceeds cap %d",
					k.name, k.startByte, count, maxExpansions)
			}
		}
	})
}
