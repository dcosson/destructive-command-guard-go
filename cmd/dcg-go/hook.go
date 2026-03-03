package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

type HookInput struct {
	SessionID      string    `json:"session_id"`
	TranscriptPath string    `json:"transcript_path"`
	Cwd            string    `json:"cwd"`
	HookEventName  string    `json:"hook_event_name"`
	ToolName       string    `json:"tool_name"`
	ToolInput      ToolInput `json:"tool_input"`
}

type ToolInput struct {
	Command     string `json:"command"`
	Description string `json:"description,omitempty"`
	Timeout     int    `json:"timeout,omitempty"`
}

type HookOutput struct {
	HookSpecificOutput HookSpecificOutput `json:"hookSpecificOutput"`
}

type HookSpecificOutput struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
}

const maxHookInputSize = 1 << 20 // 1MB

func runHookMode() error {
	input, err := io.ReadAll(io.LimitReader(stdin, maxHookInputSize))
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}

	var hookInput HookInput
	if err := json.Unmarshal(input, &hookInput); err != nil {
		return fmt.Errorf("parsing hook input: %w", err)
	}
	output := processHookInput(hookInput)
	return writeHookOutput(output.HookSpecificOutput.PermissionDecision, output.HookSpecificOutput.PermissionDecisionReason)
}

func processHookInput(hookInput HookInput) HookOutput {
	if hookInput.HookEventName != "" && hookInput.HookEventName != "PreToolUse" {
		fmt.Fprintf(stderr, "warning: unsupported hook event: %s\n", hookInput.HookEventName)
		return HookOutput{
			HookSpecificOutput: HookSpecificOutput{
				HookEventName:      "PreToolUse",
				PermissionDecision: "allow",
			},
		}
	}

	if hookInput.ToolName != "Bash" {
		return HookOutput{
			HookSpecificOutput: HookSpecificOutput{
				HookEventName:      "PreToolUse",
				PermissionDecision: "allow",
			},
		}
	}

	command := hookInput.ToolInput.Command
	if command == "" {
		return HookOutput{
			HookSpecificOutput: HookSpecificOutput{
				HookEventName:      "PreToolUse",
				PermissionDecision: "allow",
			},
		}
	}

	cfg := loadConfig()
	opts := cfg.toOptions()
	opts = append(opts, guard.WithEnv(environFn()))

	result := guard.Evaluate(command, opts...)
	decision := decisionToHookDecision(result.Decision)
	reason := buildReason(result)
	return HookOutput{
		HookSpecificOutput: HookSpecificOutput{
			HookEventName:            "PreToolUse",
			PermissionDecision:       decision,
			PermissionDecisionReason: reason,
		},
	}
}

func decisionToHookDecision(d guard.Decision) string {
	switch d {
	case guard.Deny:
		return "deny"
	case guard.Ask:
		return "ask"
	default:
		return "allow"
	}
}

func buildReason(result guard.Result) string {
	if len(result.Matches) == 0 {
		return ""
	}
	best := result.Matches[0]
	for _, m := range result.Matches[1:] {
		if m.Severity > best.Severity {
			best = m
		}
	}

	reason := best.Reason
	if best.Remediation != "" {
		reason += ". Suggestion: " + best.Remediation
	}
	if best.EnvEscalated {
		reason += " [severity escalated: production environment detected]"
	}
	if extra := len(result.Matches) - 1; extra > 0 {
		reason += fmt.Sprintf(" (+%d more match", extra)
		if extra > 1 {
			reason += "es"
		}
		reason += ")"
	}
	return reason
}

func writeHookOutput(decision, reason string) error {
	output := HookOutput{
		HookSpecificOutput: HookSpecificOutput{
			HookEventName:            "PreToolUse",
			PermissionDecision:       decision,
			PermissionDecisionReason: reason,
		},
	}
	enc := json.NewEncoder(stdout)
	return enc.Encode(output)
}
