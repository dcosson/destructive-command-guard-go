package eval

import "github.com/dcosson/destructive-command-guard-go/internal/evalcore"

// Type aliases — these are the same types as evalcore, not new types.
type (
	Severity     = evalcore.Severity
	Confidence   = evalcore.Confidence
	Decision     = evalcore.Decision
	Assessment   = evalcore.Assessment
	RuleCategory = evalcore.RuleCategory
	Match        = evalcore.Match
	WarningCode  = evalcore.WarningCode
	Warning      = evalcore.Warning
	Result       = evalcore.Result
	Policy       = evalcore.Policy
	PolicyConfig = evalcore.PolicyConfig
)

// Severity constants with eval-prefixed names for internal use.
const (
	SeverityIndeterminate = evalcore.Indeterminate
	SeverityLow           = evalcore.Low
	SeverityMedium        = evalcore.Medium
	SeverityHigh          = evalcore.High
	SeverityCritical      = evalcore.Critical
)

// Confidence constants.
const (
	ConfidenceLow    = evalcore.ConfidenceLow
	ConfidenceMedium = evalcore.ConfidenceMedium
	ConfidenceHigh   = evalcore.ConfidenceHigh
)

// Decision constants.
const (
	DecisionAllow = evalcore.Allow
	DecisionDeny  = evalcore.Deny
	DecisionAsk   = evalcore.Ask
)

// Warning code constants.
const (
	WarnPartialParse        = evalcore.WarnPartialParse
	WarnInlineDepthExceeded = evalcore.WarnInlineDepthExceeded
	WarnInputTruncated      = evalcore.WarnInputTruncated
	WarnExpansionCapped     = evalcore.WarnExpansionCapped
	WarnExtractorPanic      = evalcore.WarnExtractorPanic
	WarnCommandSubstitution = evalcore.WarnCommandSubstitution
	WarnMatcherPanic        = evalcore.WarnMatcherPanic
	WarnUnknownPackID       = evalcore.WarnUnknownPackID
)
