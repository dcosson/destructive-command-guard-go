package guard

import (
	"sync"

	"github.com/dcosson/destructive-command-guard-go/internal/eval"
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
	return getPipeline().Run(command, cfg.toInternal())
}

func (c *evalConfig) toInternal() eval.Config {
	return eval.Config{
		Policy:        c.policy,
		Allowlist:     c.allowlist,
		Blocklist:     c.blocklist,
		EnabledPacks:  c.enabledPacks,
		DisabledPacks: c.disabledPacks,
		CallerEnv:     c.callerEnv,
	}
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
