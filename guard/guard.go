package guard

import (
	"sync"

	"github.com/dcosson/destructive-command-guard-go/internal/eval"
	"github.com/dcosson/destructive-command-guard-go/internal/evalcore"
	"github.com/dcosson/destructive-command-guard-go/internal/packs"
	"github.com/dcosson/destructive-command-guard-go/internal/tooluse"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/cloud"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/containers"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/core"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/database"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/frameworks"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/infrastructure"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/kubernetes"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/macos"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/personal"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/platform"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/remote"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/secrets"
)

var (
	pipelineOnce sync.Once
	pipelineInst *eval.Pipeline
)

func getPipeline() *eval.Pipeline {
	pipelineOnce.Do(func() {
		pipelineInst = eval.NewPipeline(packs.DefaultRegistry)
	})
	return pipelineInst
}

// Evaluate analyzes a shell command for destructive and privacy risks.
func Evaluate(command string, opts ...Option) Result {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.destructivePolicy == nil {
		cfg.destructivePolicy = InteractivePolicy()
	}
	if cfg.privacyPolicy == nil {
		cfg.privacyPolicy = InteractivePolicy()
	}
	return getPipeline().Run(command, cfg.toInternal())
}

// EvaluateToolUse analyzes a Claude Code tool use for destructive and privacy
// risks. For Bash, extracts the command string and runs it through the full
// tree-sitter parser. For other known tools, normalizes to synthetic shell
// commands and evaluates those. Unknown tools return Allow.
func EvaluateToolUse(toolName string, toolInput map[string]any, opts ...Option) Result {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.destructivePolicy == nil {
		cfg.destructivePolicy = InteractivePolicy()
	}
	if cfg.privacyPolicy == nil {
		cfg.privacyPolicy = InteractivePolicy()
	}

	norm := tooluse.Normalize(toolName, toolInput)

	if norm.UseBashParser {
		return getPipeline().Run(norm.BashCommand, cfg.toInternal())
	}

	// Known tool with malformed input: indeterminate, let policy decide.
	if norm.NormalizationError {
		indeterminate := &Assessment{
			Severity:   Indeterminate,
			Confidence: ConfidenceLow,
		}
		pc := PolicyConfig{
			DestructivePolicy: cfg.destructivePolicy,
			PrivacyPolicy:     cfg.privacyPolicy,
		}
		var warnings []Warning
		for _, w := range norm.Warnings {
			warnings = append(warnings, Warning{
				Code:    WarnExtractorPanic, // closest existing code for normalization issues
				Message: w.Message,
			})
		}
		return Result{
			Decision:              pc.Decide(indeterminate, indeterminate),
			DestructiveAssessment: indeterminate,
			PrivacyAssessment:     indeterminate,
			Command:               norm.CommandSummary,
			Warnings:              warnings,
		}
	}

	if len(norm.Commands) == 0 {
		// Unknown tool or NoEval — no rules exist.
		return Result{Decision: Allow, Command: norm.CommandSummary}
	}

	result := getPipeline().RunCommands(norm.Commands, norm.RawText, cfg.toInternal())
	result.Command = norm.CommandSummary
	for _, w := range norm.Warnings {
		result.Warnings = append(result.Warnings, Warning{
			Code:    WarnExtractorPanic,
			Message: w.Message,
		})
	}
	return result
}

// ToolInfo describes a known tool from the catalog.
type ToolInfo struct {
	ToolName         string            `json:"tool_name"`
	SyntheticCommand string            `json:"synthetic_command,omitempty"`
	PathField        string            `json:"path_field,omitempty"`
	ExtraFields      []string          `json:"extra_fields,omitempty"`
	Flags            map[string]string `json:"flags,omitempty"`
	NoEval           bool              `json:"no_eval,omitempty"`
}

// Tools returns metadata for all known tools in the catalog.
func Tools() []ToolInfo {
	out := make([]ToolInfo, 0, len(tooluse.Catalog))
	for _, def := range tooluse.Catalog {
		out = append(out, ToolInfo{
			ToolName:         def.ToolName,
			SyntheticCommand: def.SyntheticCommand,
			PathField:        def.PathField,
			ExtraFields:      def.ExtraFields,
			Flags:            def.Flags,
			NoEval:           def.NoEval,
		})
	}
	return out
}

func (c *evalConfig) toInternal() eval.Config {
	return eval.Config{
		DestructivePolicy: c.destructivePolicy,
		PrivacyPolicy:     c.privacyPolicy,
		Allowlist:         c.allowlist,
		Blocklist:         c.blocklist,
		EnabledPacks:      c.enabledPacks,
		DisabledPacks:     c.disabledPacks,
		CallerEnv:         c.callerEnv,
	}
}

// CategoryDetail holds rule count and severity breakdown for a category.
type CategoryDetail struct {
	Count          int
	SeverityCounts map[string]int
}

// PackInfo describes a registered pack.
type PackInfo struct {
	ID              string
	Name            string
	Description     string
	Keywords        []string
	Destructive     CategoryDetail
	Privacy         CategoryDetail
	Both            CategoryDetail
	HasEnvSensitive bool
}

// Packs returns metadata for all registered packs.
func Packs() []PackInfo {
	all := packs.DefaultRegistry.All()
	out := make([]PackInfo, 0, len(all))
	for _, p := range all {
		info := PackInfo{
			ID:              p.ID,
			Name:            p.Name,
			Description:     p.Description,
			Keywords:        append([]string(nil), p.Keywords...),
			Destructive:     CategoryDetail{SeverityCounts: make(map[string]int)},
			Privacy:         CategoryDetail{SeverityCounts: make(map[string]int)},
			Both:            CategoryDetail{SeverityCounts: make(map[string]int)},
			HasEnvSensitive: p.HasEnvSensitive,
		}
		for _, r := range p.Rules {
			cat := r.Category
			if cat == 0 {
				cat = evalcore.CategoryDestructive
			}
			sev := Severity(r.Severity).String()
			switch cat {
			case evalcore.CategoryBoth:
				info.Both.Count++
				info.Both.SeverityCounts[sev]++
			case evalcore.CategoryPrivacy:
				info.Privacy.Count++
				info.Privacy.SeverityCounts[sev]++
			default:
				info.Destructive.Count++
				info.Destructive.SeverityCounts[sev]++
			}
		}
		out = append(out, info)
	}
	return out
}

// RuleInfo describes a single registered rule.
type RuleInfo struct {
	ID          string
	PackID      string
	Category    RuleCategory
	Severity    Severity
	Reason      string
	Remediation string
}

// Rules returns metadata for all registered rules.
func Rules() []RuleInfo {
	all := packs.DefaultRegistry.All()
	var out []RuleInfo
	for _, p := range all {
		for _, r := range p.Rules {
			cat := RuleCategory(r.Category)
			if cat == 0 {
				cat = evalcore.CategoryDestructive
			}
			out = append(out, RuleInfo{
				ID:          r.ID,
				PackID:      p.ID,
				Category:    cat,
				Severity:    Severity(r.Severity),
				Reason:      r.Reason,
				Remediation: r.Remediation,
			})
		}
	}
	return out
}
