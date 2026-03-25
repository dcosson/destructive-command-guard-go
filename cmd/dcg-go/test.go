package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dcosson/destructive-command-guard-go/guard"
	"github.com/spf13/cobra"
)

const policyHelp = `Available policies (from most to least permissive):
  allow-all    Allow everything
  permissive   Allow up to High, deny Critical
  moderate     Allow up to Medium, deny High+ and Indeterminate
  strict       Allow only Low, deny everything else
  very-strict  Deny all matched rules
  interactive  Ask for Indeterminate/Medium/High, deny Critical (default)`

func newTestCmd() *cobra.Command {
	var (
		jsonOut       bool
		destrPolicy   string
		privPolicy    string
		toolName      string
		envFlag       bool
		allowlistFlag []string
		blocklistFlag []string
	)

	cmd := &cobra.Command{
		Use:   "test [flags] <command-or-tool-input>",
		Short: "Evaluate a command or tool use and print the result",
		Long: `Evaluate a shell command or Claude Code tool use against registered rules and print the result.

If only one of --destructive-policy or --privacy-policy is set, the
other defaults to allow-all.

Exit codes: 0=Allow, 1=Error, 2=Deny, 3=Ask

` + policyHelp,
		Example: `  dcg-go test "git push --force"
  dcg-go test --json "git push --force origin main"
  dcg-go test --destructive-policy strict "rm -rf /"
  dcg-go test --destructive-policy permissive --privacy-policy strict "cat ~/.ssh/id_rsa"
  dcg-go test --blocklist "rm *" "rm -rf /tmp"
  dcg-go test --env "RAILS_ENV=production rails db:reset"
  dcg-go test --tool Read '{"file_path":"~/.ssh/id_rsa"}'
  dcg-go test --tool Grep '{"pattern":"password","path":"~/Documents"}'`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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

			toolInput, err := parseTestToolInput(toolName, args[0])
			if err != nil {
				return err
			}

			result := guard.EvaluateToolUse(toolName, toolInput, opts...)
			if jsonOut {
				if err := printTestJSON(result); err != nil {
					return err
				}
			} else {
				if err := printTestHuman(result); err != nil {
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
	cmd.Flags().StringVar(&toolName, "tool", "Bash", "Claude Code tool name to evaluate (default: Bash)")
	cmd.Flags().StringSliceVar(&allowlistFlag, "allowlist", nil, "Glob patterns to always allow")
	cmd.Flags().StringSliceVar(&blocklistFlag, "blocklist", nil, "Glob patterns to always deny")
	cmd.Flags().BoolVar(&envFlag, "env", false, "Include process environment in detection")
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
	case "very-strict":
		return guard.VeryStrictPolicy(), nil
	case "interactive":
		return guard.InteractivePolicy(), nil
	default:
		return nil, fmt.Errorf("unknown policy: %s (valid: allow-all, permissive, moderate, strict, very-strict, interactive)", name)
	}
}

func printTestHuman(result guard.Result) error {
	fmt.Fprintf(stdout, "Command:  %s\n", result.Command)
	fmt.Fprintf(stdout, "Decision: %s\n", result.Decision)
	fmt.Fprintf(stdout, "Reason:   %s\n", result.Reason())
	if rem := result.Remediation(); rem != "" {
		fmt.Fprintf(stdout, "Suggestion: %s\n", rem)
	}
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
			fmt.Fprintf(stdout, "     Reason: %s\n", m.Reason)
			if m.Remediation != "" {
				fmt.Fprintf(stdout, "     Suggestion: %s\n", m.Remediation)
			}
			if m.EnvEscalated {
				fmt.Fprintln(stdout, "     Note: severity escalated (production env detected)")
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

func parseTestToolInput(toolName, arg string) (map[string]any, error) {
	if strings.EqualFold(toolName, "Bash") {
		return map[string]any{"command": arg}, nil
	}

	var toolInput map[string]any
	if err := json.Unmarshal([]byte(arg), &toolInput); err != nil {
		return nil, fmt.Errorf("parsing --tool %s input as JSON object: %w", toolName, err)
	}
	if toolInput == nil {
		return nil, fmt.Errorf("parsing --tool %s input as JSON object: expected object", toolName)
	}
	return toolInput, nil
}

type TestResult struct {
	Command               string              `json:"command"`
	Decision              string              `json:"decision"`
	Reason                string              `json:"reason"`
	Remediation           string              `json:"remediation,omitempty"`
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
		Command:     result.Command,
		Decision:    result.Decision.String(),
		Reason:      result.Reason(),
		Remediation: result.Remediation(),
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
