package main

import (
	"encoding/json"
	"fmt"

	"github.com/dcosson/destructive-command-guard-go/guard"
	"github.com/spf13/cobra"
)

const policyHelp = `Available policies (from most to least permissive):
  allow-all    Allow everything
  permissive   Allow up to High, deny Critical
  moderate     Allow up to Medium, deny High+ and Indeterminate
  strict       Allow only Low, deny everything else
  block        Deny all matched rules
  interactive  Ask for Indeterminate/Medium/High, deny Critical (default)`

func newTestCmd() *cobra.Command {
	var (
		explain       bool
		jsonOut       bool
		destrPolicy   string
		privPolicy    string
		envFlag       bool
		allowlistFlag []string
		blocklistFlag []string
	)

	cmd := &cobra.Command{
		Use:   "test [flags] \"command\"",
		Short: "Evaluate a command and print the result",
		Long: `Evaluate a shell command against registered rules and print the result.

If only one of --destructive-policy or --privacy-policy is set, the
other defaults to allow-all.

Exit codes: 0=Allow, 1=Error, 2=Deny, 3=Ask

` + policyHelp,
		Example: `  dcg-go test "git push --force"
  dcg-go test --explain "DROP TABLE users;"
  dcg-go test --json "git push --force origin main"
  dcg-go test --destructive-policy strict "rm -rf /"
  dcg-go test --destructive-policy permissive --privacy-policy strict "cat ~/.ssh/id_rsa"
  dcg-go test --blocklist "rm *" "rm -rf /tmp"
  dcg-go test --env "RAILS_ENV=production rails db:reset"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			command := args[0]
			cfg := loadConfig()
			opts := cfg.toOptions()

			if destrPolicy != "" {
				p, err := parsePolicy(destrPolicy)
				if err != nil {
					return err
				}
				opts = append(opts, guard.WithDestructivePolicy(p))
				if privPolicy == "" {
					opts = append(opts, guard.WithPrivacyPolicy(guard.AllowAllPolicy()))
				}
			}
			if privPolicy != "" {
				p, err := parsePolicy(privPolicy)
				if err != nil {
					return err
				}
				opts = append(opts, guard.WithPrivacyPolicy(p))
				if destrPolicy == "" {
					opts = append(opts, guard.WithDestructivePolicy(guard.AllowAllPolicy()))
				}
			}
			if len(allowlistFlag) > 0 {
				opts = append(opts, guard.WithAllowlist(allowlistFlag...))
			}
			if len(blocklistFlag) > 0 {
				opts = append(opts, guard.WithBlocklist(blocklistFlag...))
			}
			if envFlag {
				opts = append(opts, guard.WithEnv(environFn()))
			}

			result := guard.Evaluate(command, opts...)
			if jsonOut {
				if err := printTestJSON(result); err != nil {
					return err
				}
			} else {
				if err := printTestHuman(result, explain); err != nil {
					return err
				}
			}

			switch result.Decision {
			case guard.Deny:
				exitFn(2)
			case guard.Ask:
				exitFn(3)
			}
			return nil
		},
	}

	cmd.Flags().SortFlags = false
	cmd.Flags().StringVar(&destrPolicy, "destructive-policy", "", "Policy for destructive rules (see policies above)")
	cmd.Flags().StringVar(&privPolicy, "privacy-policy", "", "Policy for privacy rules (see policies above)")
	cmd.Flags().StringSliceVar(&allowlistFlag, "allowlist", nil, "Glob patterns to always allow")
	cmd.Flags().StringSliceVar(&blocklistFlag, "blocklist", nil, "Glob patterns to always deny")
	cmd.Flags().BoolVar(&envFlag, "env", false, "Include process environment in detection")
	cmd.Flags().BoolVar(&explain, "explain", false, "Show detailed reasoning")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")

	return cmd
}

func parsePolicy(name string) (guard.Policy, error) {
	switch name {
	case "allow-all":
		return guard.AllowAllPolicy(), nil
	case "permissive":
		return guard.PermissivePolicy(), nil
	case "moderate":
		return guard.ModeratePolicy(), nil
	case "strict":
		return guard.StrictPolicy(), nil
	case "block":
		return guard.BlockPolicy(), nil
	case "interactive":
		return guard.InteractivePolicy(), nil
	default:
		return nil, fmt.Errorf("unknown policy: %s (valid: allow-all, permissive, moderate, strict, block, interactive)", name)
	}
}

func printTestHuman(result guard.Result, explain bool) error {
	fmt.Fprintf(stdout, "Command:  %s\n", result.Command)
	fmt.Fprintf(stdout, "Decision: %s\n", result.Decision)
	if result.DestructiveAssessment != nil {
		fmt.Fprintf(stdout, "Destructive Severity: %s\n", result.DestructiveAssessment.Severity)
		fmt.Fprintf(stdout, "Destructive Confidence: %s\n", result.DestructiveAssessment.Confidence)
	}
	if result.PrivacyAssessment != nil {
		fmt.Fprintf(stdout, "Privacy Severity: %s\n", result.PrivacyAssessment.Severity)
		fmt.Fprintf(stdout, "Privacy Confidence: %s\n", result.PrivacyAssessment.Confidence)
	}
	if len(result.Matches) > 0 {
		fmt.Fprintf(stdout, "\nMatches (%d):\n", len(result.Matches))
		for i, m := range result.Matches {
			fmt.Fprintf(stdout, "  %d. [%s] %s (%s/%s)\n", i+1, m.Pack, m.Rule, m.Severity, m.Confidence)
			if explain {
				fmt.Fprintf(stdout, "     Reason: %s\n", m.Reason)
				if m.Remediation != "" {
					fmt.Fprintf(stdout, "     Suggestion: %s\n", m.Remediation)
				}
				if m.EnvEscalated {
					fmt.Fprintln(stdout, "     Note: severity escalated (production env detected)")
				}
			}
		}
	}
	if len(result.Warnings) > 0 {
		fmt.Fprintf(stdout, "\nWarnings (%d):\n", len(result.Warnings))
		for _, w := range result.Warnings {
			fmt.Fprintf(stdout, "  - [%s] %s\n", w.Code, w.Message)
		}
	}
	return nil
}

type TestResult struct {
	Command               string              `json:"command"`
	Decision              string              `json:"decision"`
	DestructiveSeverity   string              `json:"destructive_severity,omitempty"`
	DestructiveConfidence string              `json:"destructive_confidence,omitempty"`
	PrivacySeverity       string              `json:"privacy_severity,omitempty"`
	PrivacyConfidence     string              `json:"privacy_confidence,omitempty"`
	Matches               []TestMatchResult   `json:"matches,omitempty"`
	Warnings              []TestWarningResult `json:"warnings,omitempty"`
}

type TestMatchResult struct {
	Pack         string `json:"pack"`
	Rule         string `json:"rule"`
	Severity     string `json:"severity"`
	Confidence   string `json:"confidence"`
	Reason       string `json:"reason"`
	Remediation  string `json:"remediation,omitempty"`
	EnvEscalated bool   `json:"env_escalated"`
}

type TestWarningResult struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func printTestJSON(result guard.Result) error {
	tr := TestResult{
		Command:  result.Command,
		Decision: result.Decision.String(),
	}
	if result.DestructiveAssessment != nil {
		tr.DestructiveSeverity = result.DestructiveAssessment.Severity.String()
		tr.DestructiveConfidence = result.DestructiveAssessment.Confidence.String()
	}
	if result.PrivacyAssessment != nil {
		tr.PrivacySeverity = result.PrivacyAssessment.Severity.String()
		tr.PrivacyConfidence = result.PrivacyAssessment.Confidence.String()
	}
	for _, m := range result.Matches {
		tr.Matches = append(tr.Matches, TestMatchResult{
			Pack:         m.Pack,
			Rule:         m.Rule,
			Severity:     m.Severity.String(),
			Confidence:   m.Confidence.String(),
			Reason:       m.Reason,
			Remediation:  m.Remediation,
			EnvEscalated: m.EnvEscalated,
		})
	}
	for _, w := range result.Warnings {
		tr.Warnings = append(tr.Warnings, TestWarningResult{
			Code:    w.Code.String(),
			Message: w.Message,
		})
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(tr)
}
