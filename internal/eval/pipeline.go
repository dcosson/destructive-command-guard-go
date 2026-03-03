package eval

import (
	"regexp"
	"slices"
	"strings"

	"github.com/dcosson/destructive-command-guard-go/internal/packs"
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
}

func NewPipeline(registry *packs.Registry) *Pipeline {
	return &Pipeline{registry: registry}
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
	for _, pack := range activePacks {
		for _, rule := range pack.Destructive {
			if rule.Match == nil || !rule.Match(cmd) {
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
