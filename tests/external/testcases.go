// Package external contains black-box tests exercising the public guard API
// and the compiled dcg-go binary. Shared test case data lives here so both
// the library and binary test files can use it.
package external

// CommandExpectation defines expected evaluation results for a command.
type CommandExpectation struct {
	Name    string
	Command string
	// Expected decision string: "Allow", "Deny", or "Ask".
	WantDecision string
}

// SafeCommands are commands that should always be allowed.
var SafeCommands = []string{
	"echo hello",
	"git status",
	"ls -la",
	"cat README.md",
	"pwd",
}

// DefaultPolicyCases are commands tested with the default Interactive policy.
var DefaultPolicyCases = []CommandExpectation{
	{"critical-deny", "rm -rf /", "Deny"},
	{"high-ask", "git push --force", "Ask"},
	{"safe-allow", "echo hello", "Allow"},
}

// PolicyVariationCommand is the command used to test all policy variations.
const PolicyVariationCommand = "git push --force"

// PolicyExpectation maps a policy name to the expected decision for
// PolicyVariationCommand (High severity, Destructive category).
type PolicyExpectation struct {
	Policy       string
	WantDecision string
}

// PolicyVariations defines expected decisions for each policy against
// a High-severity destructive command.
var PolicyVariations = []PolicyExpectation{
	{"allow-all", "Allow"},
	{"permissive", "Allow"},
	{"moderate", "Deny"},
	{"strict", "Deny"},
	{"block-all", "Deny"},
	{"interactive", "Ask"},
}
