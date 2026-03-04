package eval

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"github.com/dcosson/destructive-command-guard-go/internal/packs"
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
	registry *packs.Registry
	parser   *parse.BashParser
}

func NewPipeline(registry *packs.Registry) *Pipeline {
	return &Pipeline{
		registry: registry,
		parser:   parse.NewBashParser(),
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

	isProd := isProduction(command, cfg.CallerEnv)
	parsed := p.parser.ParseAndExtract(context.Background(), command, 0)
	result.Warnings = append(result.Warnings, convertParseWarnings(parsed.Warnings)...)
	if len(parsed.Commands) == 0 {
		result.Decision = DecisionAllow
		return result
	}

	for _, extracted := range parsed.Commands {
		for _, pack := range activePacks {
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
				if rule.EnvSensitive && (isProd || isCommandProduction(extracted)) && sev < int(SeverityCritical) {
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

var prodPattern = regexp.MustCompile(`(?i)\b(prod|production)\b`)

func isProduction(command string, callerEnv []string) bool {
	if prodPattern.MatchString(command) {
		return true
	}
	for _, kv := range callerEnv {
		if !strings.Contains(kv, "=") {
			continue
		}
		parts := strings.SplitN(kv, "=", 2)
		key := strings.ToUpper(parts[0])
		if key != "RAILS_ENV" && key != "NODE_ENV" && key != "ENVIRONMENT" && key != "APP_ENV" {
			continue
		}
		if strings.EqualFold(parts[1], "production") || strings.EqualFold(parts[1], "prod") {
			return true
		}
	}
	return false
}

func isCommandProduction(cmd parse.ExtractedCommand) bool {
	for key, value := range cmd.InlineEnv {
		k := strings.ToUpper(key)
		if k != "RAILS_ENV" && k != "NODE_ENV" && k != "ENVIRONMENT" && k != "APP_ENV" {
			continue
		}
		if strings.EqualFold(value, "production") || strings.EqualFold(value, "prod") {
			return true
		}
	}
	return false
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
