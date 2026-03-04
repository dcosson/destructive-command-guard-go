package guard

import (
	"sync"

	"github.com/dcosson/destructive-command-guard-go/internal/eval"
	"github.com/dcosson/destructive-command-guard-go/internal/packs"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/core"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/database"
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

// Evaluate analyzes a shell command for destructive behavior.
func Evaluate(command string, opts ...Option) Result {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.policy == nil {
		cfg.policy = InteractivePolicy()
	}
	internal := getPipeline().Run(command, cfg.toInternal())
	return fromInternalResult(internal)
}

func (c *evalConfig) toInternal() eval.Config {
	return eval.Config{
		Policy:        policyAdapter{policy: c.policy},
		Allowlist:     c.allowlist,
		Blocklist:     c.blocklist,
		EnabledPacks:  c.enabledPacks,
		DisabledPacks: c.disabledPacks,
		CallerEnv:     c.callerEnv,
	}
}

type policyAdapter struct {
	policy Policy
}

func (a policyAdapter) Decide(in eval.Assessment) eval.Decision {
	out := a.policy.Decide(Assessment{
		Severity:   Severity(in.Severity),
		Confidence: Confidence(in.Confidence),
	})
	return eval.Decision(out)
}

func fromInternalResult(in eval.Result) Result {
	out := Result{
		Decision: Decision(in.Decision),
		Command:  in.Command,
	}
	if in.Assessment != nil {
		out.Assessment = &Assessment{
			Severity:   Severity(in.Assessment.Severity),
			Confidence: Confidence(in.Assessment.Confidence),
		}
	}
	if len(in.Matches) > 0 {
		out.Matches = make([]Match, 0, len(in.Matches))
		for _, m := range in.Matches {
			out.Matches = append(out.Matches, Match{
				Pack:         m.Pack,
				Rule:         m.Rule,
				Severity:     Severity(m.Severity),
				Confidence:   Confidence(m.Confidence),
				Reason:       m.Reason,
				Remediation:  m.Remediation,
				EnvEscalated: m.EnvEscalated,
			})
		}
	}
	if len(in.Warnings) > 0 {
		out.Warnings = make([]Warning, 0, len(in.Warnings))
		for _, w := range in.Warnings {
			out.Warnings = append(out.Warnings, Warning{
				Code:    WarningCode(w.Code),
				Message: w.Message,
			})
		}
	}
	return out
}

// PackInfo describes a registered pack.
type PackInfo struct {
	ID              string
	Name            string
	Description     string
	Keywords        []string
	SafeCount       int
	DestrCount      int
	HasEnvSensitive bool
}

// Packs returns metadata for all registered packs.
func Packs() []PackInfo {
	all := packs.DefaultRegistry.All()
	out := make([]PackInfo, 0, len(all))
	for _, p := range all {
		out = append(out, PackInfo{
			ID:              p.ID,
			Name:            p.Name,
			Description:     p.Description,
			Keywords:        append([]string(nil), p.Keywords...),
			SafeCount:       len(p.Safe),
			DestrCount:      len(p.Destructive),
			HasEnvSensitive: p.HasEnvSensitive,
		})
	}
	return out
}
