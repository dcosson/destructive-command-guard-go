package guard

import "github.com/dcosson/destructive-command-guard-go/internal/evalcore"

// Type aliases — re-export evalcore types as the public API.
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

// Severity levels for destructive command assessments.
const (
	Indeterminate = evalcore.Indeterminate
	Low           = evalcore.Low
	Medium        = evalcore.Medium
	High          = evalcore.High
	Critical      = evalcore.Critical
)

// Confidence in pattern match accuracy.
const (
	ConfidenceLow    = evalcore.ConfidenceLow
	ConfidenceMedium = evalcore.ConfidenceMedium
	ConfidenceHigh   = evalcore.ConfidenceHigh
)

// Decision is the policy output.
const (
	Allow = evalcore.Allow
	Deny  = evalcore.Deny
	Ask   = evalcore.Ask
)

// Rule categories.
const (
	CategoryDestructive = evalcore.CategoryDestructive
	CategoryPrivacy     = evalcore.CategoryPrivacy
	CategoryBoth        = evalcore.CategoryBoth
)

// Warning codes.
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
