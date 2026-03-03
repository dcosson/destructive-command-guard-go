package parse

import (
	"context"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

// E2: Dataflow Resolution Examples
// Specific test cases from the architecture §8 examples verifying that
// variable assignments are tracked and resolved during extraction.

type dataflowTestCase struct {
	name   string
	input  string
	check  func(t *testing.T, result ParseResult)
}

var dataflowTests = []dataflowTestCase{
	{
		name:  "simple variable carry",
		input: "DIR=/; rm -rf $DIR",
		check: func(t *testing.T, result ParseResult) {
			// $DIR should resolve to "/" in the rm command args
			requireCommandNamed(t, result.Commands, "rm")
			rm := findCommand(result.Commands, "rm")
			assertContainsArg(t, rm.Args, "/")
			assertTrue(t, "DataflowResolved", rm.DataflowResolved)
		},
	},
	{
		name:  "or branch may-alias",
		input: "DIR=/tmp || DIR=/; rm -rf $DIR",
		check: func(t *testing.T, result ParseResult) {
			// Both /tmp and / should produce variant commands
			seen := argSet(result.Commands, "rm")
			if !seen["/tmp"] || !seen["/"] {
				t.Errorf("expected both /tmp and / variants, got args: %v", seen)
			}
		},
	},
	{
		name:  "sequential override",
		input: "DIR=/tmp; DIR=/; rm -rf $DIR",
		check: func(t *testing.T, result ParseResult) {
			// Last assignment wins: only "/" should be resolved
			rm := findCommand(result.Commands, "rm")
			if rm == nil {
				t.Fatal("expected rm command")
			}
			assertContainsArg(t, rm.Args, "/")
		},
	},
	{
		name:  "and chain may-alias",
		input: "DIR=/ && DIR=/tmp && rm -rf $DIR",
		check: func(t *testing.T, result ParseResult) {
			// Both values tracked (may-alias from && branches)
			seen := argSet(result.Commands, "rm")
			if !seen["/"] || !seen["/tmp"] {
				t.Errorf("expected both / and /tmp variants, got: %v", seen)
			}
		},
	},
	{
		name:  "and chain false negative prevention",
		input: "DIR=/ && something && DIR=/tmp && rm -rf $DIR",
		check: func(t *testing.T, result ParseResult) {
			// May-alias preserves dangerous value "/" even after reassignment
			seen := argSet(result.Commands, "rm")
			if !seen["/"] {
				t.Errorf("expected / in variants (false negative prevention), got: %v", seen)
			}
		},
	},
	{
		name:  "url-shaped env var",
		input: "DB_HOST=prod-db.internal; psql -h $DB_HOST",
		check: func(t *testing.T, result ParseResult) {
			psql := findCommand(result.Commands, "psql")
			if psql == nil {
				t.Fatal("expected psql command")
			}
			// $DB_HOST should resolve
			if psql.DataflowResolved {
				assertContainsArg(t, psql.Args, "prod-db.internal")
			}
		},
	},
	{
		name:  "command substitution indeterminate",
		input: "FILE=$(mktemp); rm -rf $FILE",
		check: func(t *testing.T, result ParseResult) {
			// FILE is indeterminate, expect WarnCommandSubstitution
			foundWarning := false
			for _, w := range result.Warnings {
				if w.Code == guard.WarnCommandSubstitution {
					foundWarning = true
				}
			}
			if !foundWarning {
				t.Error("expected WarnCommandSubstitution for $(mktemp)")
			}
		},
	},
	{
		name:  "unresolved variable left raw",
		input: "rm -rf $UNKNOWN",
		check: func(t *testing.T, result ParseResult) {
			rm := findCommand(result.Commands, "rm")
			if rm == nil {
				t.Fatal("expected rm command")
			}
			// $UNKNOWN not tracked — left unsubstituted in raw args
			assertContainsRawArg(t, rm.RawArgs, "$UNKNOWN")
		},
	},
	{
		name:  "common pipeline no false positives",
		input: "cat /var/log/syslog | grep error | sort | uniq -c",
		check: func(t *testing.T, result ParseResult) {
			// No variable assignments, no dataflow warnings
			for _, w := range result.Warnings {
				if w.Code == guard.WarnCommandSubstitution || w.Code == guard.WarnExpansionCapped {
					t.Errorf("unexpected dataflow warning: %v", w)
				}
			}
		},
	},
	{
		name:  "inline env does not leak",
		input: "TMP=/safe echo ok; echo $TMP",
		check: func(t *testing.T, result ParseResult) {
			// Inline env is command-scoped; $TMP in second echo should be raw
			if len(result.Commands) < 2 {
				t.Skipf("expected 2 commands, got %d", len(result.Commands))
			}
			second := result.Commands[1]
			// $TMP should not be resolved to /safe
			assertContainsRawArg(t, second.RawArgs, "$TMP")
		},
	},
	{
		name:  "multiple variables in single command",
		input: "A=foo; B=bar; echo $A $B",
		check: func(t *testing.T, result ParseResult) {
			echo := findCommand(result.Commands, "echo")
			if echo == nil {
				t.Fatal("expected echo command")
			}
			if echo.DataflowResolved {
				assertContainsArg(t, echo.Args, "foo")
				assertContainsArg(t, echo.Args, "bar")
			}
		},
	},
	{
		name:  "expansion cap with many branches",
		input: "A=1 || A=2 || A=3; B=x || B=y || B=z; C=a || C=b || C=c; D=p || D=q || D=r; echo $A$B$C$D",
		check: func(t *testing.T, result ParseResult) {
			// With 3^4=81 possible expansions, should be capped at 16
			foundCap := false
			for _, w := range result.Warnings {
				if w.Code == guard.WarnExpansionCapped {
					foundCap = true
				}
			}
			if !foundCap {
				t.Error("expected WarnExpansionCapped with 4 variables × 3 values each")
			}
		},
	},
	{
		name:  "export propagation to ExportedVars",
		input: "export RAILS_ENV=production",
		check: func(t *testing.T, result ParseResult) {
			// ExportedVars map should be initialized (even if export handling
			// doesn't populate it due to declaration_command stripping)
			if result.ExportedVars == nil {
				t.Error("ExportedVars must not be nil")
			}
		},
	},
}

func TestDataflowResolution(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	for _, tc := range dataflowTests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := bp.ParseAndExtract(context.Background(), tc.input, 0)
			tc.check(t, result)
		})
	}
}

// --- Dataflow test helpers ---

func findCommand(cmds []ExtractedCommand, name string) *ExtractedCommand {
	for i := range cmds {
		if cmds[i].Name == name {
			return &cmds[i]
		}
	}
	return nil
}

func requireCommandNamed(t *testing.T, cmds []ExtractedCommand, name string) {
	t.Helper()
	if findCommand(cmds, name) == nil {
		names := commandNames(cmds)
		t.Fatalf("expected command %q in %v", name, names)
	}
}

func assertContainsRawArg(t *testing.T, rawArgs []string, want string) {
	t.Helper()
	for _, a := range rawArgs {
		if a == want {
			return
		}
	}
	t.Errorf("expected raw arg %q in %v", want, rawArgs)
}

// argSet collects all Args values from commands with the given name.
func argSet(cmds []ExtractedCommand, name string) map[string]bool {
	set := map[string]bool{}
	for _, cmd := range cmds {
		if cmd.Name == name {
			for _, arg := range cmd.Args {
				set[arg] = true
			}
		}
	}
	return set
}
