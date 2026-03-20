package integration

import parsepkg "github.com/dcosson/destructive-command-guard-go/internal/parse"

type (
	DataflowAnalyzer = parsepkg.DataflowAnalyzer
	ExtractedCommand = parsepkg.ExtractedCommand
	ParseResult      = parsepkg.ParseResult
	Warning          = parsepkg.Warning
	WarningCode      = parsepkg.WarningCode
)

var (
	NewBashParser       = parsepkg.NewBashParser
	NewCommandExtractor = parsepkg.NewCommandExtractor
	NewDataflowAnalyzer = parsepkg.NewDataflowAnalyzer
	NewInlineDetector   = parsepkg.NewInlineDetector
	NewTree             = parsepkg.NewTree
	Normalize           = parsepkg.Normalize
)

const (
	MaxInputSize            = parsepkg.MaxInputSize
	maxExpansions           = parsepkg.MaxExpansions
	WarnPartialParse        = parsepkg.WarnPartialParse
	WarnInputTruncated      = parsepkg.WarnInputTruncated
	WarnExtractorPanic      = parsepkg.WarnExtractorPanic
	WarnInlineDepthExceeded = parsepkg.WarnInlineDepthExceeded
	WarnExpansionCapped     = parsepkg.WarnExpansionCapped
	WarnCommandSubstitution = parsepkg.WarnCommandSubstitution
	WarnMatcherPanic        = parsepkg.WarnMatcherPanic
	WarnUnknownPackID       = parsepkg.WarnUnknownPackID
)

func hasWarning(warnings []Warning, code WarningCode) bool {
	for _, w := range warnings {
		if w.Code == code {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
