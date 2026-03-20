package main

import (
	"encoding/json"
	"flag"
	"fmt"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

func runTestMode(args []string) error {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(stderr)

	explain := fs.Bool("explain", false, "Show detailed reasoning")
	jsonOut := fs.Bool("json", false, "Output as JSON")
	policyName := fs.String("policy", "", "Shorthand: set both destructive and privacy policy")
	destrPolicyName := fs.String("destructive-policy", "", "Policy for destructive rules: allow-all, permissive, moderate, strict, interactive")
	privPolicyName := fs.String("privacy-policy", "", "Policy for privacy rules: allow-all, permissive, moderate, strict, interactive")
	envFlag := fs.Bool("env", false,
		"Include process environment in detection. "+
			"Note: without --env, only inline env vars are detected.")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: dcg-go test [--explain] [--json] [--policy NAME] [--destructive-policy NAME] [--privacy-policy NAME] \"command\"")
	}

	command := fs.Arg(0)
	cfg := loadConfig()
	opts := cfg.toOptions()

	if *policyName != "" {
		p, err := parsePolicy(*policyName)
		if err != nil {
			return err
		}
		opts = append(opts, guard.WithDestructivePolicy(p), guard.WithPrivacyPolicy(p))
	}
	if *destrPolicyName != "" {
		p, err := parsePolicy(*destrPolicyName)
		if err != nil {
			return err
		}
		opts = append(opts, guard.WithDestructivePolicy(p))
	}
	if *privPolicyName != "" {
		p, err := parsePolicy(*privPolicyName)
		if err != nil {
			return err
		}
		opts = append(opts, guard.WithPrivacyPolicy(p))
	}
	if *envFlag {
		opts = append(opts, guard.WithEnv(environFn()))
	}

	result := guard.Evaluate(command, opts...)
	if *jsonOut {
		if err := printTestJSON(result); err != nil {
			return err
		}
	} else {
		if err := printTestHuman(result, *explain); err != nil {
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
	case "block-all":
		return guard.BlockAllPolicy(), nil
	case "interactive":
		return guard.InteractivePolicy(), nil
	default:
		return nil, fmt.Errorf("unknown policy: %s (valid: allow-all, permissive, moderate, strict, block-all, interactive)", name)
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
