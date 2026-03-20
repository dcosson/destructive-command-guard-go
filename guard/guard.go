package guard

import (
	"sync"

	"github.com/dcosson/destructive-command-guard-go/internal/eval"
	"github.com/dcosson/destructive-command-guard-go/internal/evalcore"
	"github.com/dcosson/destructive-command-guard-go/internal/packs"
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

// PackInfo describes a registered pack.
type PackInfo struct {
	ID               string
	Name             string
	Description      string
	Keywords         []string
	DestructiveCount int
	PrivacyCount     int
	BothCount        int
	HasEnvSensitive  bool
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
			HasEnvSensitive: p.HasEnvSensitive,
		}
		for _, r := range p.Rules {
			cat := r.Category
			if cat == 0 {
				cat = evalcore.CategoryDestructive
			}
			switch cat {
			case evalcore.CategoryBoth:
				info.BothCount++
			case evalcore.CategoryPrivacy:
				info.PrivacyCount++
			default:
				info.DestructiveCount++
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
