package tooluse

import (
	"fmt"
	"strings"

	"github.com/dcosson/destructive-command-guard-go/internal/packs"
)

// Warning describes a non-fatal issue during normalization.
type Warning struct {
	Message string
}

// NormalizeResult contains the output of normalizing a tool use into
// synthetic shell commands for the evaluation pipeline.
type NormalizeResult struct {
	// Commands to evaluate. Empty means "allow, nothing to check" (for
	// unknown tools or NoEval tools) or "normalization failed" (check
	// NormalizationError).
	Commands []packs.Command
	// RawText is the full synthetic command string (e.g. "cat /path/to/file")
	// used for blocklist/allowlist glob matching, pre-filter keyword scanning,
	// and RawTextContains/RawTextRegex matchers.
	RawText string
	// Warnings from normalization.
	Warnings []Warning
	// NormalizationError is true when a known tool has malformed or
	// incomplete input. The caller should produce an indeterminate
	// assessment and let policy decide.
	NormalizationError bool
	// UseBashParser indicates the Bash tool should go through the full
	// tree-sitter parser instead of the synthesized command path.
	UseBashParser bool
	// BashCommand is the raw command string for Bash tool.
	BashCommand string
	// CommandSummary is a human-readable description of the tool use
	// (e.g. "Read(/Users/me/.ssh/id_rsa)") for Result.Command.
	CommandSummary string
}

// Normalize converts a Claude Code tool use into synthetic shell commands
// for the evaluation pipeline.
//
// For Bash, it sets UseBashParser=true so the caller uses the full
// tree-sitter parser. For other known tools, it builds packs.Command
// structs from the tool catalog. For unknown tools, it returns an empty
// result (allow).
func Normalize(toolName string, toolInput map[string]any) NormalizeResult {
	// Bash special case: delegate to tree-sitter parser.
	if toolName == "Bash" {
		return normalizeBash(toolInput)
	}

	def := LookupTool(toolName)
	if def == nil {
		// Unknown tool — no rules exist.
		return NormalizeResult{
			CommandSummary: toolName,
		}
	}

	if def.NoEval {
		return NormalizeResult{
			CommandSummary: toolName,
		}
	}

	return normalizeFromCatalog(def, toolInput)
}

func normalizeBash(toolInput map[string]any) NormalizeResult {
	if toolInput == nil {
		return NormalizeResult{
			NormalizationError: true,
			CommandSummary:     "Bash",
			Warnings:           []Warning{{Message: "Bash tool missing 'command' field"}},
		}
	}
	val, exists := toolInput["command"]
	if !exists {
		return NormalizeResult{
			NormalizationError: true,
			CommandSummary:     "Bash",
			Warnings:           []Warning{{Message: "Bash tool missing 'command' field"}},
		}
	}
	cmd, ok := val.(string)
	if !ok {
		return NormalizeResult{
			NormalizationError: true,
			CommandSummary:     "Bash",
			Warnings:           []Warning{{Message: "Bash tool 'command' field is not a string"}},
		}
	}
	// Empty command is valid — Pipeline.Run() handles it (returns Allow).
	return NormalizeResult{
		UseBashParser:  true,
		BashCommand:    cmd,
		CommandSummary: cmd,
	}
}

func normalizeFromCatalog(def *ToolDef, toolInput map[string]any) NormalizeResult {
	// Extract the primary path field.
	pathVal, ok := extractString(toolInput, def.PathField)
	if !ok {
		return NormalizeResult{
			NormalizationError: true,
			CommandSummary:     def.ToolName,
			Warnings: []Warning{{
				Message: fmt.Sprintf("%s tool missing or invalid '%s' field", def.ToolName, def.PathField),
			}},
		}
	}

	// Build args with proper ordering.
	var args []string
	var rawParts []string

	rawParts = append(rawParts, def.SyntheticCommand)

	// Flags first (e.g. -i for sed).
	for flag, val := range def.Flags {
		if val == "" {
			args = append(args, flag)
			rawParts = append(rawParts, flag)
		} else {
			args = append(args, flag, val)
			rawParts = append(rawParts, flag, val)
		}
	}

	// Collect extra field values.
	var extraVals []string
	for _, field := range def.ExtraFields {
		val, fieldOK := extractString(toolInput, field)
		if fieldOK {
			extraVals = append(extraVals, val)
		}
	}

	if def.PathBeforeExtras {
		// Path before extras: find <path> -name <pattern>
		args = append(args, pathVal)
		rawParts = append(rawParts, pathVal)
		if def.ExtraFieldPrefix != "" {
			args = append(args, def.ExtraFieldPrefix)
			rawParts = append(rawParts, def.ExtraFieldPrefix)
		}
		args = append(args, extraVals...)
		rawParts = append(rawParts, extraVals...)
	} else {
		// Extras before path: grep <pattern> <path>
		args = append(args, extraVals...)
		rawParts = append(rawParts, extraVals...)
		args = append(args, pathVal)
		rawParts = append(rawParts, pathVal)
	}

	rawText := strings.Join(rawParts, " ")

	cmd := packs.Command{
		Name:    def.SyntheticCommand,
		Args:    args,
		RawArgs: args,
		Flags:   copyFlags(def.Flags),
		RawText: rawText,
	}

	// Build command summary.
	summary := buildSummary(def, toolInput, pathVal)

	return NormalizeResult{
		Commands:       []packs.Command{cmd},
		RawText:        rawText,
		CommandSummary: summary,
	}
}

func extractString(input map[string]any, key string) (string, bool) {
	if input == nil {
		return "", false
	}
	val, exists := input[key]
	if !exists {
		return "", false
	}
	s, ok := val.(string)
	if !ok {
		return "", false
	}
	if s == "" {
		return "", false
	}
	return s, true
}

func copyFlags(flags map[string]string) map[string]string {
	if len(flags) == 0 {
		return nil
	}
	out := make(map[string]string, len(flags))
	for k, v := range flags {
		out[k] = v
	}
	return out
}

func buildSummary(def *ToolDef, toolInput map[string]any, pathVal string) string {
	var parts []string
	for _, field := range def.ExtraFields {
		if val, ok := extractString(toolInput, field); ok {
			parts = append(parts, val)
		}
	}
	parts = append(parts, pathVal)
	return fmt.Sprintf("%s(%s)", def.ToolName, strings.Join(parts, ", "))
}
