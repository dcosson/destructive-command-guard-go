package eval

import (
	"context"
	"slices"
	"strings"

	"github.com/dcosson/destructive-command-guard-go/internal/envdetect"
	"github.com/dcosson/destructive-command-guard-go/internal/packs"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/containers"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/core"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/database"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/frameworks"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/kubernetes"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/platform"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/remote"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/secrets"
	"github.com/dcosson/destructive-command-guard-go/internal/parse"
)

// Config controls a single pipeline evaluation run.
type Config struct {
	Policy        Policy
	Allowlist     []string
	Blocklist     []string
	EnabledPacks  []string
	DisabledPacks []string
	CallerEnv     []string
}

// Pipeline evaluates commands against registered rules.
type Pipeline struct {
	registry  *packs.Registry
	parser    *parse.BashParser
	prefilter *PreFilter
	envDet    *envdetect.Detector
}

func NewPipeline(registry *packs.Registry) *Pipeline {
	return &Pipeline{
		registry:  registry,
		parser:    parse.NewBashParser(),
		prefilter: NewPreFilter(registry),
		envDet:    envdetect.NewDetector(),
	}
}

func (p *Pipeline) Run(command string, cfg Config) Result {
	cmd := strings.TrimSpace(command)
	result := Result{
		Command: command,
	}
	if cmd == "" {
		result.Decision = DecisionAllow
		return result
	}

	// Blocklist has highest precedence.
	for _, pattern := range cfg.Blocklist {
		if globMatch(pattern, cmd) {
			result.Decision = DecisionDeny
			result.Assessment = &Assessment{
				Severity:   SeverityCritical,
				Confidence: ConfidenceHigh,
			}
			result.Matches = []Match{
				{
					Pack:        "_blocklist",
					Rule:        pattern,
					Severity:    SeverityCritical,
					Confidence:  ConfidenceHigh,
					Reason:      "Matched blocklist pattern",
					Remediation: "Remove command from blocklist or use safer command",
				},
			}
			return result
		}
	}

	// Allowlist bypasses pack evaluation if no blocklist match happened.
	for _, pattern := range cfg.Allowlist {
		if globMatch(pattern, cmd) {
			result.Decision = DecisionAllow
			return result
		}
	}

	policy := cfg.Policy
	if policy == nil {
		policy = interactivePolicy{}
	}

	activePacks, warnings := p.activePacks(cfg)
	result.Warnings = append(result.Warnings, warnings...)
	if len(activePacks) == 0 {
		result.Decision = DecisionAllow
		return result
	}

	// Pre-filter: fast keyword scan to short-circuit non-destructive commands.
	filterResult := p.prefilter.Contains(command)
	if !filterResult.Matched {
		result.Decision = DecisionAllow
		return result
	}

	// Narrow packs to those whose keywords matched the pre-filter.
	candidateIDs := p.prefilter.CandidatePacks(filterResult.Keywords, cfg.EnabledPacks)
	if len(candidateIDs) == 0 {
		result.Decision = DecisionAllow
		return result
	}
	candidateSet := make(map[string]struct{}, len(candidateIDs))
	for _, id := range candidateIDs {
		candidateSet[id] = struct{}{}
	}

	parsed := p.parser.ParseAndExtract(context.Background(), command, 0)
	result.Warnings = append(result.Warnings, convertParseWarnings(parsed.Warnings)...)
	if len(parsed.Commands) == 0 {
		result.Decision = DecisionAllow
		return result
	}

	// Environment detection: global (process env + exported vars).
	globalEnvResult := p.envDet.DetectProcess(cfg.CallerEnv)
	exportedEnvResult := p.envDet.DetectExported(parsed.ExportedVars)
	globalProd := globalEnvResult.IsProduction || exportedEnvResult.IsProduction

	for _, extracted := range parsed.Commands {
		// Per-command inline environment detection.
		cmdEnvResult := p.envDet.DetectInline(extracted.InlineEnv)
		isProd := globalProd || cmdEnvResult.IsProduction

		for _, pack := range activePacks {
			// Skip packs not in candidate set.
			if _, ok := candidateSet[pack.ID]; !ok {
				continue
			}

			// Safe patterns short-circuit destructive evaluation within a pack.
			safeMatched := false
			for _, safe := range pack.Safe {
				if safe.Match != nil && safe.Match.Match(toPackCommand(extracted)) {
					safeMatched = true
					break
				}
			}
			if safeMatched {
				continue
			}

			for _, rule := range pack.Destructive {
				if rule.Match == nil || !rule.Match.Match(toPackCommand(extracted)) {
					continue
				}
				sev := rule.Severity
				envEscalated := false
				if rule.EnvSensitive && isProd && sev < int(SeverityCritical) {
					sev = sev + 1
					if sev > int(SeverityCritical) {
						sev = int(SeverityCritical)
					}
					envEscalated = true
				}
				result.Matches = append(result.Matches, Match{
					Pack:         pack.ID,
					Rule:         rule.ID,
					Severity:     Severity(sev),
					Confidence:   Confidence(rule.Confidence),
					Reason:       rule.Reason,
					Remediation:  rule.Remediation,
					EnvEscalated: envEscalated,
				})
			}
		}
	}

	if len(result.Matches) == 0 {
		result.Decision = DecisionAllow
		return result
	}

	agg := aggregate(result.Matches)
	result.Assessment = &agg
	result.Decision = policy.Decide(agg)
	return result
}

func (p *Pipeline) activePacks(cfg Config) ([]packs.Pack, []Warning) {
	if p.registry == nil {
		return nil, nil
	}
	var warnings []Warning
	all := p.registry.All()
	byID := make(map[string]packs.Pack, len(all))
	for _, pk := range all {
		byID[pk.ID] = pk
	}

	var selected []packs.Pack
	if cfg.EnabledPacks == nil {
		selected = make([]packs.Pack, 0, len(all))
		for _, pk := range all {
			selected = append(selected, pk)
		}
	} else {
		selected = make([]packs.Pack, 0, len(cfg.EnabledPacks))
		for _, id := range cfg.EnabledPacks {
			pk, ok := byID[id]
			if !ok {
				warnings = append(warnings, Warning{
					Code:    WarnUnknownPackID,
					Message: "unknown pack id: " + id,
				})
				continue
			}
			selected = append(selected, pk)
		}
	}

	if len(cfg.DisabledPacks) == 0 {
		return selected, warnings
	}
	filtered := selected[:0]
	for _, pk := range selected {
		if slices.Contains(cfg.DisabledPacks, pk.ID) {
			continue
		}
		filtered = append(filtered, pk)
	}
	return filtered, warnings
}

func aggregate(matches []Match) Assessment {
	best := Assessment{
		Severity:   SeverityIndeterminate,
		Confidence: ConfidenceLow,
	}
	for i, m := range matches {
		if i == 0 || m.Severity > best.Severity {
			best.Severity = m.Severity
			best.Confidence = m.Confidence
			continue
		}
		if m.Severity == best.Severity && m.Confidence > best.Confidence {
			best.Confidence = m.Confidence
		}
	}
	return best
}

func convertParseWarnings(in []parse.Warning) []Warning {
	if len(in) == 0 {
		return nil
	}
	out := make([]Warning, 0, len(in))
	for _, w := range in {
		out = append(out, Warning{
			Code:    WarningCode(w.Code),
			Message: w.Message,
		})
	}
	return out
}

func toPackCommand(cmd parse.ExtractedCommand) packs.Command {
	flags := make(map[string]string, len(cmd.Flags))
	for k, v := range cmd.Flags {
		flags[k] = v
	}
	return packs.Command{
		Name:    cmd.Name,
		Args:    append([]string{}, cmd.Args...),
		RawArgs: append([]string{}, cmd.RawArgs...),
		Flags:   flags,
		RawText: cmd.RawText,
	}
}
