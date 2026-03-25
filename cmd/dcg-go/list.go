package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/dcosson/destructive-command-guard-go/guard"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List packs, rules, and tools",
	}
	cmd.AddCommand(newListPacksCmd())
	cmd.AddCommand(newListRulesCmd())
	cmd.AddCommand(newListToolsCmd())
	return cmd
}

func newListPacksCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "packs",
		Short: "List available pattern packs",
		RunE: func(cmd *cobra.Command, args []string) error {
			packs := guard.Packs()
			if jsonOut {
				enc := json.NewEncoder(stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(packs)
			}

			fmt.Fprintf(stdout, "Registered packs (%d):\n\n", len(packs))
			for _, p := range packs {
				fmt.Fprintf(stdout, "  %-25s %s\n", p.ID, p.Name)
				if p.Description != "" {
					fmt.Fprintf(stdout, "  %-25s %s\n", "", wrapLine(p.Description, contentCol, wrapWidth))
				}
				fmt.Fprintf(stdout, "  %-25s %s\n", "", formatKeywords(p.Keywords))
				fmt.Fprintf(stdout, "  %-25s Destructive: %s\n", "", formatCategoryDetail(p.Destructive))
				fmt.Fprintf(stdout, "  %-25s Privacy: %s\n", "", formatCategoryDetail(p.Privacy))
				fmt.Fprintf(stdout, "  %-25s Both: %s\n", "", formatCategoryDetail(p.Both))
				fmt.Fprintln(stdout)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	return cmd
}

func newListRulesCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "rules",
		Short: "List all rules sorted by pack",
		RunE: func(cmd *cobra.Command, args []string) error {
			rules := guard.Rules()
			if jsonOut {
				enc := json.NewEncoder(stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(rules)
			}

			sort.Slice(rules, func(i, j int) bool {
				if rules[i].PackID != rules[j].PackID {
					return rules[i].PackID < rules[j].PackID
				}
				return rules[i].ID < rules[j].ID
			})

			const descIndentStr = "      " // 6 spaces for description lines
			const descWidth = 74           // wrap description to this width (80 - 6)

			fmt.Fprintf(stdout, "Rules (%d):\n", len(rules))
			for _, r := range rules {
				fmt.Fprintf(stdout, "  %s (%s) %s\n", r.ID, r.PackID, formatRuleCategory(r))
				wrapped := wrapDesc(r.Reason, descWidth)
				for _, line := range strings.Split(wrapped, "\n") {
					fmt.Fprintf(stdout, "%s%s\n", descIndentStr, line)
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	return cmd
}

type listToolInfo struct {
	ToolName         string            `json:"tool_name"`
	SyntheticCommand string            `json:"synthetic_command,omitempty"`
	PathField        string            `json:"path_field,omitempty"`
	ExtraFields      []string          `json:"extra_fields,omitempty"`
	Flags            map[string]string `json:"flags,omitempty"`
	NoEval           bool              `json:"no_eval,omitempty"`
	UsesParser       bool              `json:"uses_parser,omitempty"`
}

func newListToolsCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "tools",
		Short: "List known Claude Code tools",
		RunE: func(cmd *cobra.Command, args []string) error {
			tools := cliTools()
			if jsonOut {
				enc := json.NewEncoder(stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(tools)
			}

			fmt.Fprintf(stdout, "Known tools (%d):\n\n", len(tools))
			for _, tool := range tools {
				fmt.Fprintf(stdout, "  %-15s -> %s\n", tool.ToolName, formatToolMapping(tool))
			}
			fmt.Fprintln(stdout)
			fmt.Fprintln(stdout, "Unknown tools are allowed (DCG has no rules for them).")
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	return cmd
}

func formatCategoryDetail(d guard.CategoryDetail) string {
	if d.Count == 0 {
		return "0 rules"
	}
	order := []string{"Critical", "High", "Medium", "Low"}
	var parts []string
	for _, sev := range order {
		if n, ok := d.SeverityCounts[sev]; ok && n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, sev))
		}
	}
	return strings.Join(parts, ", ")
}

func formatRuleCategory(r guard.RuleInfo) string {
	sev := r.Severity.String()
	switch r.Category {
	case guard.CategoryBoth:
		return fmt.Sprintf("[Destructive: %s, Privacy: %s]", sev, sev)
	case guard.CategoryPrivacy:
		return fmt.Sprintf("[Privacy: %s]", sev)
	default:
		return fmt.Sprintf("[Destructive: %s]", sev)
	}
}

func cliTools() []listToolInfo {
	out := []listToolInfo{{
		ToolName:   "Bash",
		UsesParser: true,
	}}
	for _, tool := range guard.Tools() {
		out = append(out, listToolInfo{
			ToolName:         tool.ToolName,
			SyntheticCommand: tool.SyntheticCommand,
			PathField:        tool.PathField,
			ExtraFields:      append([]string(nil), tool.ExtraFields...),
			Flags:            copyStringMap(tool.Flags),
			NoEval:           tool.NoEval,
		})
	}
	return out
}

func formatToolMapping(tool listToolInfo) string {
	if tool.UsesParser {
		return "tree-sitter parser (full shell evaluation)"
	}
	if tool.NoEval {
		return "no evaluation (always allow)"
	}

	var parts []string
	parts = append(parts, tool.SyntheticCommand)
	for _, flag := range sortedKeys(tool.Flags) {
		parts = append(parts, flag)
		if value := tool.Flags[flag]; value != "" {
			parts = append(parts, value)
		}
	}
	for _, field := range tool.ExtraFields {
		parts = append(parts, "<"+field+">")
	}
	if tool.PathField != "" {
		parts = append(parts, "<"+tool.PathField+">")
	}
	return strings.Join(parts, " ")
}

func sortedKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

// wrapDesc wraps text to maxWidth characters, breaking on spaces.
func wrapDesc(text string, maxWidth int) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}
	var b strings.Builder
	lineLen := 0
	for i, w := range words {
		if i == 0 {
			b.WriteString(w)
			lineLen = len(w)
			continue
		}
		if lineLen+1+len(w) > maxWidth {
			b.WriteString("\n")
			b.WriteString(w)
			lineLen = len(w)
		} else {
			b.WriteString(" ")
			b.WriteString(w)
			lineLen += 1 + len(w)
		}
	}
	return b.String()
}
