package tooluse

// ToolDef defines how a Claude Code tool maps to a synthetic shell command.
type ToolDef struct {
	// ToolName is the Claude Code tool name (e.g. "Read", "Write").
	ToolName string
	// SyntheticCommand is the shell command name to synthesize (e.g. "cat", "tee").
	SyntheticCommand string
	// PathField is the tool_input field containing the primary file path.
	// Empty if the tool has no file path (e.g. WebSearch).
	PathField string
	// ExtraFields lists additional tool_input fields to include as args
	// (e.g. "pattern" for Grep). Order matters — they become positional args
	// before the path.
	ExtraFields []string
	// Flags are synthetic flags to add to the command (e.g. "-i" for Edit → sed).
	Flags map[string]string
	// ExtraFieldPrefix is a flag inserted before extra field values
	// (e.g. "-name" for find <path> -name <pattern>).
	ExtraFieldPrefix string
	// PathBeforeExtras puts the path arg before extra fields in the synthetic
	// command (e.g. find <path> -name <pattern> instead of find <pattern> <path>).
	PathBeforeExtras bool
	// PathOptional means the path field can be absent without triggering a
	// normalization error (e.g. Glob defaults to cwd when path is omitted).
	PathOptional bool
	// NoEval means this tool is known but has no security-relevant inputs
	// to evaluate (e.g. Agent, WebSearch). Returns Allow with no matching.
	NoEval bool
}

// Catalog is the single source of truth for tool-to-command mappings.
// Adding support for a new Claude Code tool means adding one entry here.
var Catalog = []ToolDef{
	{ToolName: "Read", SyntheticCommand: "cat", PathField: "file_path"},
	{ToolName: "Write", SyntheticCommand: "tee", PathField: "file_path"},
	{ToolName: "Edit", SyntheticCommand: "sed", PathField: "file_path", Flags: map[string]string{"-i": ""}},
	{ToolName: "Grep", SyntheticCommand: "grep", PathField: "path", ExtraFields: []string{"pattern"}},
	{ToolName: "Glob", SyntheticCommand: "find", PathField: "path", ExtraFieldPrefix: "-name", ExtraFields: []string{"pattern"}, PathBeforeExtras: true, PathOptional: true},
	{ToolName: "NotebookEdit", SyntheticCommand: "sed", PathField: "file_path", Flags: map[string]string{"-i": ""}},
	{ToolName: "WebFetch", SyntheticCommand: "curl", PathField: "url"},
	{ToolName: "Agent", NoEval: true},
	{ToolName: "WebSearch", NoEval: true},
}

// catalogIndex is a lookup map built from Catalog at init time.
var catalogIndex map[string]*ToolDef

func init() {
	catalogIndex = make(map[string]*ToolDef, len(Catalog))
	for i := range Catalog {
		catalogIndex[Catalog[i].ToolName] = &Catalog[i]
	}
}

// LookupTool returns the ToolDef for a tool name, or nil if unknown.
func LookupTool(toolName string) *ToolDef {
	return catalogIndex[toolName]
}
