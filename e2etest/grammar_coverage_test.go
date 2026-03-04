package e2etest

import (
	"fmt"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

func TestGrammarCoverage(t *testing.T) {
	SkipIfPackMissing(t, "core.git")

	templates := map[string]string{
		"program":              "git push --force",
		"pipeline":             "echo start | git push --force",
		"list_sequential":      "echo start; git push --force",
		"list_and":             "echo start && git push --force",
		"list_or":              "echo start || git push --force",
		"compound_statement":   "{ echo start; git push --force; }",
		"subshell":             "(git push --force)",
		"if_condition":         "if git push --force; then echo done; fi",
		"if_body":              "if true; then git push --force; fi",
		"elif_body":            "if false; then true; elif true; then git push --force; fi",
		"while_condition":      "while git push --force; do echo retry; done",
		"while_body":           "while true; do git push --force; done",
		"until_condition":      "until git push --force; do echo retry; done",
		"until_body":           "until false; do git push --force; done",
		"for_body":             "for x in 1 2 3; do git push --force; done",
		"case_body":            "case $x in y) git push --force;; esac",
		"function_body":        "fn() { git push --force; }; fn",
		"command_substitution": "echo $(git push --force)",
		"process_substitution": "diff <(git push --force) /dev/null",
		"redirected_command":   "git push --force > /dev/null 2>&1",
		"negated":              "! git push --force",
		"backtick":             "echo `git push --force`",
	}

	if len(templates) != 22 {
		t.Fatalf("template count = %d, want 22", len(templates))
	}

	report := NewGrammarCoverageReport(keys(templates))
	for context, cmd := range templates {
		context := context
		cmd := cmd
		t.Run(context, func(t *testing.T) {
			result := guard.Evaluate(cmd, guard.WithPolicy(guard.InteractivePolicy()))
			if result.Decision == guard.Allow {
				t.Fatalf("extractor missed git match in context %s", context)
			}
			found := false
			for _, m := range result.Matches {
				if m.Pack == "core.git" {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("no core.git match in context %s", context)
			}
			report.MarkCovered(context)
		})
	}

	if report.CoveredContexts != report.TotalContexts || report.CoveragePercent != 100 {
		t.Fatalf("coverage incomplete: %+v", report)
	}
}

func TestGrammarCoverageAllPacks(t *testing.T) {
	packCommands := map[string]string{
		"core.git":        "git push --force",
		"core.filesystem": "rm -rf /tmp/grammar-coverage",
		"frameworks":      "RAILS_ENV=production rails db:reset",
	}

	contexts := []string{
		"%s",
		"echo start | %s",
		"echo start; %s",
		"echo start && %s",
		"echo start || %s",
		"{ echo start; %s; }",
		"(%s)",
		"if %s; then echo done; fi",
		"if true; then %s; fi",
		"if false; then true; elif true; then %s; fi",
		"while true; do %s; done",
		"until false; do %s; done",
		"for x in 1 2 3; do %s; done",
		"case $x in y) %s;; esac",
		"fn() { %s; }; fn",
		"echo $(%s)",
		"diff <(%s) /dev/null",
		"%s > /dev/null 2>&1",
		"! %s",
		"echo `%s`",
	}

	if len(contexts) != 20 {
		t.Fatalf("context count = %d, want 20", len(contexts))
	}

	for packID, cmd := range packCommands {
		if !HasRegisteredPack(packID) {
			continue
		}
		for i, ctx := range contexts {
			fullCmd := fmt.Sprintf(ctx, cmd)
			t.Run(fmt.Sprintf("%s/context-%02d", packID, i), func(t *testing.T) {
				result := guard.Evaluate(fullCmd, guard.WithPolicy(guard.InteractivePolicy()))
				if result.Decision == guard.Allow {
					t.Fatalf("missed %s in context %q command=%q", packID, ctx, fullCmd)
				}
				found := false
				for _, m := range result.Matches {
					if m.Pack == packID {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("no %s match in context %q", packID, ctx)
				}
			})
		}
	}
}

func keys[K comparable, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
