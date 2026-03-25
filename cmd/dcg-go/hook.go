package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

type HookInput struct {
	SessionID      string          `json:"session_id"`
	TranscriptPath string          `json:"transcript_path"`
	Cwd            string          `json:"cwd"`
	HookEventName  string          `json:"hook_event_name"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`

	toolInputMap map[string]any
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
	toolInputMap, err := decodeHookToolInput(hookInput.ToolInput)
	if err != nil {
		return err
	}
	hookInput.toolInputMap = toolInputMap
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

	toolInputMap := hookInput.toolInputMap
	if toolInputMap == nil {
		var err error
		toolInputMap, err = decodeHookToolInput(hookInput.ToolInput)
		if err != nil {
			toolInputMap = nil
		}
	}

	if strings.EqualFold(hookInput.ToolName, "Bash") && !hasNonEmptyString(toolInputMap, "command") {
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

	result := guard.EvaluateToolUse(hookInput.ToolName, toolInputMap, opts...)
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

func decodeHookToolInput(raw json.RawMessage) (map[string]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}

	var toolInput map[string]any
	if err := json.Unmarshal(raw, &toolInput); err != nil {
		return nil, fmt.Errorf("parsing tool_input: %w", err)
	}
	if toolInput == nil {
		return nil, fmt.Errorf("parsing tool_input: expected object")
	}
	return toolInput, nil
}

func hasNonEmptyString(toolInput map[string]any, key string) bool {
	if toolInput == nil {
		return false
	}
	value, ok := toolInput[key]
	if !ok {
		return false
	}
	s, ok := value.(string)
	return ok && strings.TrimSpace(s) != ""
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

func categoryPrefix(cat guard.RuleCategory) string {
	switch cat {
	case guard.CategoryPrivacy:
		return "[privacy] "
	case guard.CategoryBoth:
		return "[destructive+privacy] "
	default:
		return "[destructive] "
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

	reason := categoryPrefix(best.Category) + best.Reason
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
