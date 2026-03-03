package parse

import "github.com/dcosson/destructive-command-guard-go/guard"

type InlineScript struct {
	Language string
	Content  string
	Source   string
}

// ExtractedCommand contains exactly the cross-plan contract fields consumed by
// downstream matching and API layers.
type ExtractedCommand struct {
	Name             string
	RawName          string
	Args             []string
	RawArgs          []string
	Flags            map[string]string
	InlineEnv        map[string]string
	RawText          string
	InPipeline       bool
	Negated          bool
	DataflowResolved bool
	StartByte        uint32
	EndByte          uint32
}

type ParseResult struct {
	Commands     []ExtractedCommand
	ExportedVars map[string][]string
	Warnings     []guard.Warning
	HasError     bool
}
