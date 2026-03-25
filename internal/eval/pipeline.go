package eval

import (
	"context"
	"slices"
	"strings"

	"github.com/dcosson/destructive-command-guard-go/internal/envdetect"
	"github.com/dcosson/destructive-command-guard-go/internal/evalcore"
	"github.com/dcosson/destructive-command-guard-go/internal/packs"
	"github.com/dcosson/destructive-command-guard-go/internal/parse"
)

// Config controls a single pipeline evaluation run.
type Config struct {
	DestructivePolicy Policy
	PrivacyPolicy     Policy
	Allowlist         []string
	Blocklist         []string
	EnabledPacks      []string
	DisabledPacks     []string
	CallerEnv         []string
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

	// Blocklist/allowlist/prefilter checked against raw command string.
	if early, ok := p.checkEarlyExit(cmd, cfg, &result); ok {
		return early
	}

	parsed := p.parser.ParseAndExtract(context.Background(), command, 0)
	result.Warnings = append(result.Warnings, convertParseWarnings(parsed.Warnings)...)

	// Parse error with no extracted commands: let policy decide via Indeterminate.
	if len(parsed.Commands) == 0 {
		if !parsed.HasError {
			result.Decision = DecisionAllow
			return result
		}
		return p.indeterminateResult(result, cfg)
	}

	// Convert parsed commands to packs.Command.
	var commands []packs.Command
	for _, extracted := range parsed.Commands {
		commands = append(commands, toPackCommand(extracted))
	}

	// Environment detection: global (process env + exported vars).
	globalEnvResult := p.envDet.DetectProcess(cfg.CallerEnv)
	exportedEnvResult := p.envDet.DetectExported(parsed.ExportedVars)
	globalProd := globalEnvResult.IsProduction || exportedEnvResult.IsProduction

	// Per-command inline env detection produces per-command isProd flags.
	isProdPerCmd := make([]bool, len(parsed.Commands))
	for i, extracted := range parsed.Commands {
		cmdEnvResult := p.envDet.DetectInline(extracted.InlineEnv)
		isProdPerCmd[i] = globalProd || cmdEnvResult.IsProduction
	}

	p.matchCommands(commands, isProdPerCmd, cfg, &result)

	if len(result.Matches) == 0 && !parsed.HasError {
		result.Decision = DecisionAllow
		return result
	}

	dAgg, pAgg := aggregateByCategory(result.Matches)

	// Partial parse: ensure both assessments are at least Indeterminate.
	if parsed.HasError {
		indeterminate := &Assessment{
			Severity:   SeverityIndeterminate,
			Confidence: ConfidenceLow,
		}
		if dAgg == nil {
			dAgg = indeterminate
		}
		if pAgg == nil {
			pAgg = indeterminate
		}
	}

	result.DestructiveAssessment = dAgg
	result.PrivacyAssessment = pAgg
	result.Decision = p.applyPolicy(dAgg, pAgg, cfg)
	return result
}

// RunCommands evaluates pre-built commands against registered rules.
// Used by EvaluateToolUse for non-Bash tools that skip shell parsing.
// The rawText is used for blocklist/allowlist matching and pre-filter
// keyword scanning.
func (p *Pipeline) RunCommands(commands []packs.Command, rawText string, cfg Config) Result {
	var result Result

	if len(commands) == 0 {
		result.Decision = DecisionAllow
		return result
	}

	// Blocklist/allowlist/prefilter checked against synthetic rawText.
	if early, ok := p.checkEarlyExit(rawText, cfg, &result); ok {
		return early
	}

	// Environment detection: process env only (no inline env for tool use).
	globalEnvResult := p.envDet.DetectProcess(cfg.CallerEnv)
	globalProd := globalEnvResult.IsProduction

	isProdPerCmd := make([]bool, len(commands))
	for i := range isProdPerCmd {
		isProdPerCmd[i] = globalProd
	}

	p.matchCommands(commands, isProdPerCmd, cfg, &result)

	if len(result.Matches) == 0 {
		result.Decision = DecisionAllow
		return result
	}

	dAgg, pAgg := aggregateByCategory(result.Matches)
	result.DestructiveAssessment = dAgg
	result.PrivacyAssessment = pAgg
	result.Decision = p.applyPolicy(dAgg, pAgg, cfg)
	return result
}

// checkEarlyExit handles blocklist, allowlist, and pre-filter checks.
// Returns (result, true) if evaluation should stop early.
func (p *Pipeline) checkEarlyExit(text string, cfg Config, result *Result) (Result, bool) {
	// Blocklist has highest precedence.
	for _, pattern := range cfg.Blocklist {
		if globMatch(pattern, text) {
			result.Decision = DecisionDeny
			blAss := Assessment{
				Severity:   SeverityCritical,
				Confidence: ConfidenceHigh,
			}
			result.DestructiveAssessment = &blAss
			result.Matches = []Match{
				{
					Pack:        "_blocklist",
					Rule:        pattern,
					Category:    evalcore.CategoryDestructive,
					Severity:    SeverityCritical,
					Confidence:  ConfidenceHigh,
					Reason:      "Matched blocklist pattern",
					Remediation: "Remove command from blocklist or use safer command",
				},
			}
			return *result, true
		}
	}

	// Allowlist bypasses pack evaluation.
	for _, pattern := range cfg.Allowlist {
		if globMatch(pattern, text) {
			result.Decision = DecisionAllow
			return *result, true
		}
	}

	activePacks, warnings := p.activePacks(cfg)
	result.Warnings = append(result.Warnings, warnings...)
	if len(activePacks) == 0 {
		result.Decision = DecisionAllow
		return *result, true
	}

	// Pre-filter: fast keyword scan.
	filterResult := p.prefilter.Contains(text)
	if !filterResult.Matched {
		result.Decision = DecisionAllow
		return *result, true
	}

	candidateIDs := p.prefilter.CandidatePacks(filterResult.Keywords, cfg.EnabledPacks)
	if len(candidateIDs) == 0 {
		result.Decision = DecisionAllow
		return *result, true
	}

	return Result{}, false
}

// matchCommands runs pack rules against the given commands and appends
// matches to result.
func (p *Pipeline) matchCommands(commands []packs.Command, isProdPerCmd []bool, cfg Config, result *Result) {
	activePacks, _ := p.activePacks(cfg)

	// Build candidate set from pre-filter. We re-run the pre-filter here
	// against each command's RawText to get the union of candidate packs.
	candidateSet := make(map[string]struct{})
	for _, cmd := range commands {
		fr := p.prefilter.Contains(cmd.RawText)
		if fr.Matched {
			for _, id := range p.prefilter.CandidatePacks(fr.Keywords, cfg.EnabledPacks) {
				candidateSet[id] = struct{}{}
			}
		}
	}

	for i, cmd := range commands {
		isProd := false
		if i < len(isProdPerCmd) {
			isProd = isProdPerCmd[i]
		}

		for _, pack := range activePacks {
			if _, ok := candidateSet[pack.ID]; !ok {
				continue
			}

			safeMatched := false
			for _, safe := range pack.Safe {
				if safe.Match != nil && safe.Match.Match(cmd) {
					safeMatched = true
					break
				}
			}
			if safeMatched {
				continue
			}

			for _, rule := range pack.Rules {
				if rule.Match == nil || !rule.Match.Match(cmd) {
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
				cat := rule.Category
				if cat == 0 {
					cat = evalcore.CategoryDestructive
				}
				result.Matches = append(result.Matches, Match{
					Pack:         pack.ID,
					Rule:         rule.ID,
					Category:     cat,
					Severity:     Severity(sev),
					Confidence:   Confidence(rule.Confidence),
					Reason:       rule.Reason,
					Remediation:  rule.Remediation,
					EnvEscalated: envEscalated,
				})
			}
		}
	}
}

func (p *Pipeline) applyPolicy(dAgg, pAgg *Assessment, cfg Config) Decision {
	dPolicy := cfg.DestructivePolicy
	if dPolicy == nil {
		dPolicy = evalcore.InteractivePolicy()
	}
	pPolicy := cfg.PrivacyPolicy
	if pPolicy == nil {
		pPolicy = evalcore.InteractivePolicy()
	}
	pc := evalcore.PolicyConfig{
		DestructivePolicy: dPolicy,
		PrivacyPolicy:     pPolicy,
	}
	return pc.Decide(dAgg, pAgg)
}

func (p *Pipeline) indeterminateResult(result Result, cfg Config) Result {
	indeterminate := &Assessment{
		Severity:   SeverityIndeterminate,
		Confidence: ConfidenceLow,
	}
	result.DestructiveAssessment = indeterminate
	result.PrivacyAssessment = indeterminate
	result.Decision = p.applyPolicy(indeterminate, indeterminate, cfg)
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
		selected = append(selected, all...)
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

func aggregateByCategory(matches []Match) (destructive, privacy *Assessment) {
	var dMatches, pMatches []Match
	for _, m := range matches {
		if m.Category.HasDestructive() {
			dMatches = append(dMatches, m)
		}
		if m.Category.HasPrivacy() {
			pMatches = append(pMatches, m)
		}
	}
	if len(dMatches) > 0 {
		a := aggregate(dMatches)
		destructive = &a
	}
	if len(pMatches) > 0 {
		a := aggregate(pMatches)
		privacy = &a
	}
	return
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
