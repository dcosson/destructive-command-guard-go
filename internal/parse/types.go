package parse

import ts "github.com/dcosson/treesitter-go"

type WarningCode string

const (
	WarnInputTooLarge WarningCode = "INPUT_TOO_LARGE"
	WarnParseError    WarningCode = "PARSE_ERROR"
	WarnParserPanic   WarningCode = "PARSER_PANIC"
)

type Warning struct {
	Code    WarningCode
	Message string
}

type InlineScript struct {
	Language string
	Content  string
	Source   string
}

type ExtractedCommand struct {
	Name             string
	RawName          string
	Args             []string
	RawArgs          []string
	Flags            map[string]string
	EnvVars          map[string]string
	Subcommand       string
	InlineScripts    []InlineScript
	Stdin            string
	DataflowResolved bool
	SourceNode       ts.Node
	PipelinePosition int
}

type ParseResult struct {
	Commands     []ExtractedCommand
	ExportedVars map[string][]string
	Warnings     []Warning
	HasError     bool
}
