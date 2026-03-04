// Package evalcore defines the shared types for destructive command evaluation.
// Both the public guard package and internal eval pipeline import from here,
// eliminating type duplication and adapter layers.
package evalcore

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

// Match represents one destructive pattern match.
type Match struct {
	Pack         string
	Rule         string
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
	Decision   Decision
	Assessment *Assessment
	Matches    []Match
	Warnings   []Warning
	Command    string
}

// Policy converts an Assessment into a Decision.
type Policy interface {
	Decide(Assessment) Decision
}
