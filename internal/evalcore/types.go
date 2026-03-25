// Package evalcore defines the shared types for destructive command evaluation.
// Both the public guard package and internal eval pipeline import from here,
// eliminating type duplication and adapter layers.
package evalcore

import (
	"fmt"
	"strings"
)

// Severity levels for destructive command assessments.
type Severity int

const (
	Indeterminate Severity = iota
	Low
	Medium
	High
	Critical
)

func (s Severity) String() string {
	switch s {
	case Indeterminate:
		return "Indeterminate"
	case Low:
		return "Low"
	case Medium:
		return "Medium"
	case High:
		return "High"
	case Critical:
		return "Critical"
	default:
		return "Unknown"
	}
}

// Confidence in pattern match accuracy.
type Confidence int

const (
	ConfidenceLow Confidence = iota
	ConfidenceMedium
	ConfidenceHigh
)

func (c Confidence) String() string {
	switch c {
	case ConfidenceLow:
		return "Low"
	case ConfidenceMedium:
		return "Medium"
	case ConfidenceHigh:
		return "High"
	default:
		return "Unknown"
	}
}

// Decision is the policy output.
type Decision int

const (
	Allow Decision = iota
	Deny
	Ask
)

func (d Decision) String() string {
	switch d {
	case Allow:
		return "Allow"
	case Deny:
		return "Deny"
	case Ask:
		return "Ask"
	default:
		return "Unknown"
	}
}

// Assessment is the raw matching result before policy conversion.
type Assessment struct {
	Severity   Severity
	Confidence Confidence
}

// RuleCategory identifies whether a rule guards against destructive operations,
// privacy violations, or both. Implemented as a bitmask.
type RuleCategory uint8

const (
	CategoryDestructive RuleCategory                            = 1 << iota // 0b01
	CategoryPrivacy                                                         // 0b10
	CategoryBoth        = CategoryDestructive | CategoryPrivacy             // 0b11
)

func (c RuleCategory) String() string {
	switch c {
	case CategoryDestructive:
		return "Destructive"
	case CategoryPrivacy:
		return "Privacy"
	case CategoryBoth:
		return "Both"
	default:
		return "Unknown"
	}
}

func (c RuleCategory) HasDestructive() bool { return c&CategoryDestructive != 0 }
func (c RuleCategory) HasPrivacy() bool     { return c&CategoryPrivacy != 0 }

// Match represents one rule pattern match.
type Match struct {
	Pack         string
	Rule         string
	Category     RuleCategory
	Severity     Severity
	Confidence   Confidence
	Reason       string
	Remediation  string
	EnvEscalated bool
}

// WarningCode identifies warning categories.
type WarningCode int

const (
	WarnPartialParse WarningCode = iota
	WarnInlineDepthExceeded
	WarnInputTruncated
	WarnExpansionCapped
	WarnExtractorPanic
	WarnCommandSubstitution
	WarnMatcherPanic
	WarnUnknownPackID
)

func (w WarningCode) String() string {
	switch w {
	case WarnPartialParse:
		return "PartialParse"
	case WarnInlineDepthExceeded:
		return "InlineDepthExceeded"
	case WarnInputTruncated:
		return "InputTruncated"
	case WarnExpansionCapped:
		return "ExpansionCapped"
	case WarnExtractorPanic:
		return "ExtractorPanic"
	case WarnCommandSubstitution:
		return "CommandSubstitution"
	case WarnMatcherPanic:
		return "MatcherPanic"
	case WarnUnknownPackID:
		return "UnknownPackID"
	default:
		return "Unknown"
	}
}

// Warning describes a non-fatal issue during evaluation.
type Warning struct {
	Code    WarningCode
	Message string
}

// Result contains the full evaluation output.
type Result struct {
	Decision              Decision
	DestructiveAssessment *Assessment
	PrivacyAssessment     *Assessment
	Matches               []Match
	Warnings              []Warning
	Command               string
}

// Reason returns a single-line human-readable summary of the evaluation.
// For matched commands it reports the first match's pack and reason; for
// allowed commands it reports that no patterns matched.
func (r Result) Reason() string {
	if len(r.Matches) == 0 {
		if len(r.Warnings) > 0 {
			codes := make([]string, len(r.Warnings))
			for i, w := range r.Warnings {
				codes[i] = w.Code.String()
			}
			return fmt.Sprintf("No destructive or privacy patterns matched (%d warning(s): %s)",
				len(r.Warnings), strings.Join(codes, ", "))
		}
		return "No destructive or privacy patterns matched"
	}
	m := r.Matches[0]
	reason := fmt.Sprintf("[%s] %s", m.Pack, m.Reason)
	if len(r.Matches) > 1 {
		reason = fmt.Sprintf("%s (and %d more)", reason, len(r.Matches)-1)
	}
	return reason
}

// Remediation returns a single-line remediation suggestion from the first
// matched rule, or an empty string if there are no matches or no remediation.
func (r Result) Remediation() string {
	if len(r.Matches) == 0 {
		return ""
	}
	return r.Matches[0].Remediation
}

// PolicyConfig holds separate policies for each rule category.
type PolicyConfig struct {
	DestructivePolicy Policy
	PrivacyPolicy     Policy
}

// Decide applies the appropriate policy for each category assessment,
// then merges decisions (deny > ask > allow).
func (pc PolicyConfig) Decide(destructive, privacy *Assessment) Decision {
	dDec := Allow
	pDec := Allow
	if destructive != nil {
		dDec = pc.DestructivePolicy.Decide(*destructive)
	}
	if privacy != nil {
		pDec = pc.PrivacyPolicy.Decide(*privacy)
	}
	if dDec == Deny || pDec == Deny {
		return Deny
	}
	if dDec == Ask || pDec == Ask {
		return Ask
	}
	return Allow
}

// Policy converts an Assessment into a Decision.
type Policy interface {
	Decide(Assessment) Decision
}
