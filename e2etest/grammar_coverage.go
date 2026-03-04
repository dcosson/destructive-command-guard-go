package e2etest

// CommandBearingNodeTypes lists bash AST node types that can contain commands.
var CommandBearingNodeTypes = []string{
	"program",
	"pipeline",
	"list",
	"compound_statement",
	"subshell",
	"if_statement",
	"elif_clause",
	"while_statement",
	"until_statement",
	"for_statement",
	"case_statement",
	"function_definition",
	"command_substitution",
	"process_substitution",
	"redirected_command",
	"negated_command",
	"test_command",
	"heredoc_body",
	"string",
	"backtick_command_substitution",
}

// GrammarCoverageReport tracks which AST contexts are verified.
type GrammarCoverageReport struct {
	TotalContexts   int             `json:"total_contexts"`
	CoveredContexts int             `json:"covered_contexts"`
	CoveragePercent float64         `json:"coverage_percent"`
	Contexts        map[string]bool `json:"contexts"`
}

func NewGrammarCoverageReport(contexts []string) GrammarCoverageReport {
	m := make(map[string]bool, len(contexts))
	for _, c := range contexts {
		m[c] = false
	}
	return GrammarCoverageReport{
		TotalContexts: len(contexts),
		Contexts:      m,
	}
}

func (r *GrammarCoverageReport) MarkCovered(context string) {
	if r.Contexts == nil {
		r.Contexts = map[string]bool{}
	}
	if _, ok := r.Contexts[context]; !ok {
		r.Contexts[context] = true
		r.TotalContexts++
	} else if r.Contexts[context] {
		return
	} else {
		r.Contexts[context] = true
	}
	r.CoveredContexts++
	if r.TotalContexts == 0 {
		r.CoveragePercent = 0
		return
	}
	r.CoveragePercent = float64(r.CoveredContexts) / float64(r.TotalContexts) * 100
}
