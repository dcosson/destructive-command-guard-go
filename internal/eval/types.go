package eval

// Severity levels in internal evaluation.
type Severity int

const (
	SeverityIndeterminate Severity = iota
	SeverityLow
	SeverityMedium
	SeverityHigh
	SeverityCritical
)

// Confidence in internal rule matches.
type Confidence int

const (
	ConfidenceLow Confidence = iota
	ConfidenceMedium
	ConfidenceHigh
)

// Decision is the internal policy output.
type Decision int

const (
	DecisionAllow Decision = iota
	DecisionDeny
	DecisionAsk
)

// Assessment is the internal aggregate score.
type Assessment struct {
	Severity   Severity
	Confidence Confidence
}

// Match is one internal rule match.
type Match struct {
	Pack         string
	Rule         string
	Severity     Severity
	Confidence   Confidence
	Reason       string
	Remediation  string
	EnvEscalated bool
}

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

type Warning struct {
	Code    WarningCode
	Message string
}

type Result struct {
	Decision   Decision
	Assessment *Assessment
	Matches    []Match
	Warnings   []Warning
	Command    string
}

// Policy converts an Assessment to a Decision.
type Policy interface {
	Decide(Assessment) Decision
}
