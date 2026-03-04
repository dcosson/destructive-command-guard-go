package parse

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// --- Command generators ---

// commandFragments provides building blocks for generating random shell-like inputs.
var commandFragments = struct {
	names      []string
	flags      []string
	longFlags  []string
	args       []string
	operators  []string
	prefixes   []string
	envPairs   []string
	quoteChars []string
}{
	names: []string{
		"git", "rm", "mv", "cp", "docker", "kubectl", "terraform",
		"echo", "cat", "grep", "sed", "awk", "ls", "find", "chmod",
		"chown", "mkdir", "rmdir", "kill", "pkill", "dd", "tar",
		"python", "ruby", "node", "bash", "sh", "perl", "lua",
		"psql", "mysql", "redis-cli", "mongo", "curl", "wget",
		"ansible", "pulumi", "aws", "gcloud", "az",
	},
	flags:      []string{"-f", "-r", "-v", "-n", "-i", "-a", "-l", "-d", "-m", "-c", "-e"},
	longFlags:  []string{"--force", "--recursive", "--verbose", "--dry-run", "--yes", "--all", "--delete", "--no-preserve-root", "--auto-approve"},
	args:       []string{"/", "/tmp", "/var", ".", "..", "~", "origin", "main", "production", "staging", "*.log", "foo", "bar"},
	operators:  []string{";", "&&", "||", "|"},
	prefixes:   []string{"/usr/bin/", "/usr/local/bin/", "/bin/", "/sbin/", "./", ""},
	envPairs:   []string{"RAILS_ENV=production", "NODE_ENV=production", "PGHOST=db.internal", "AWS_PROFILE=prod"},
	quoteChars: []string{`"`, `'`, ""},
}

// generateRandomCommand builds a synthetic shell command for property and stress testing.
// The seed ensures reproducibility for debugging failures.
func generateRandomCommand(goroutine, iteration int) string {
	r := rand.New(rand.NewSource(int64(goroutine*100000 + iteration)))
	return generateCommandFromRand(r)
}

// generateCommandFromRand creates a random shell command using the provided random source.
func generateCommandFromRand(r *rand.Rand) string {
	frags := commandFragments
	numCommands := 1 + r.Intn(4) // 1-4 commands chained together
	var parts []string

	for i := 0; i < numCommands; i++ {
		if i > 0 {
			op := frags.operators[r.Intn(len(frags.operators))]
			parts = append(parts, op)
		}

		// Maybe add env var prefix
		if r.Float32() < 0.15 {
			parts = append(parts, frags.envPairs[r.Intn(len(frags.envPairs))])
		}

		// Command name with optional path prefix
		prefix := frags.prefixes[r.Intn(len(frags.prefixes))]
		name := frags.names[r.Intn(len(frags.names))]
		parts = append(parts, prefix+name)

		// Add 0-4 flags
		numFlags := r.Intn(5)
		for j := 0; j < numFlags; j++ {
			if r.Float32() < 0.5 {
				parts = append(parts, frags.flags[r.Intn(len(frags.flags))])
			} else {
				parts = append(parts, frags.longFlags[r.Intn(len(frags.longFlags))])
			}
		}

		// Add 0-3 args
		numArgs := r.Intn(4)
		for j := 0; j < numArgs; j++ {
			arg := frags.args[r.Intn(len(frags.args))]
			q := frags.quoteChars[r.Intn(len(frags.quoteChars))]
			if q != "" {
				arg = q + arg + q
			}
			parts = append(parts, arg)
		}
	}

	return strings.Join(parts, " ")
}

// generateBashLikeInput creates an input string that exercises specific bash constructs.
func generateBashLikeInput(r *rand.Rand) string {
	constructors := []func(*rand.Rand) string{
		genSimpleCommand,
		genPipeline,
		genAndChain,
		genOrChain,
		genSubshell,
		genCommandSubstitution,
		genHeredoc,
		genInlineScript,
		genVariableAssignment,
		genNegatedCommand,
	}
	ctor := constructors[r.Intn(len(constructors))]
	return ctor(r)
}

func genSimpleCommand(r *rand.Rand) string {
	frags := commandFragments
	name := frags.names[r.Intn(len(frags.names))]
	args := frags.args[r.Intn(len(frags.args))]
	return name + " " + args
}

func genPipeline(r *rand.Rand) string {
	return genSimpleCommand(r) + " | " + genSimpleCommand(r)
}

func genAndChain(r *rand.Rand) string {
	return genSimpleCommand(r) + " && " + genSimpleCommand(r)
}

func genOrChain(r *rand.Rand) string {
	return genSimpleCommand(r) + " || " + genSimpleCommand(r)
}

func genSubshell(r *rand.Rand) string {
	return "(" + genSimpleCommand(r) + ")"
}

func genCommandSubstitution(r *rand.Rand) string {
	return "echo $(" + genSimpleCommand(r) + ")"
}

func genHeredoc(r *rand.Rand) string {
	return "bash <<'EOF'\n" + genSimpleCommand(r) + "\nEOF"
}

func genInlineScript(r *rand.Rand) string {
	langs := []struct {
		cmd  string
		flag string
		body string
	}{
		{"python", "-c", "import os; os.system('ls')"},
		{"bash", "-c", "echo hello"},
		{"ruby", "-e", "puts 'hello'"},
		{"perl", "-e", "print 'hello'"},
		{"node", "-e", "console.log('hello')"},
	}
	l := langs[r.Intn(len(langs))]
	return fmt.Sprintf("%s %s '%s'", l.cmd, l.flag, l.body)
}

func genVariableAssignment(r *rand.Rand) string {
	vars := []string{"DIR", "FILE", "HOST", "PORT", "ENV"}
	vals := []string{"/tmp", "/", "localhost", "8080", "production"}
	v := vars[r.Intn(len(vars))]
	val := vals[r.Intn(len(vals))]
	return fmt.Sprintf("%s=%s; echo $%s", v, val, v)
}

func genNegatedCommand(r *rand.Rand) string {
	return "! " + genSimpleCommand(r)
}

// --- Assertion helpers ---

// assertNoPanic runs fn and fails the test if it panics.
func assertNoPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s panicked: %v", name, r)
		}
	}()
	fn()
}

// assertValidExtractedCommand checks structural invariants on an ExtractedCommand.
func assertValidExtractedCommand(t *testing.T, cmd ExtractedCommand, input string) {
	t.Helper()
	if cmd.Name == "" {
		t.Error("ExtractedCommand.Name must not be empty")
	}
	for k := range cmd.Flags {
		if !strings.HasPrefix(k, "-") {
			t.Errorf("flag key %q does not start with -", k)
		}
	}
}

// assertValidParseResult checks structural invariants on a ParseResult.
func assertValidParseResult(t *testing.T, result ParseResult, input string) {
	t.Helper()
	for i, cmd := range result.Commands {
		t.Run(fmt.Sprintf("cmd[%d]", i), func(t *testing.T) {
			assertValidExtractedCommand(t, cmd, input)
		})
	}
	for _, w := range result.Warnings {
		if w.Message == "" {
			t.Error("Warning.Message must not be empty")
		}
	}
}

// --- Corpus data ---

// realWorldCommands is a corpus of realistic shell commands for testing.
var realWorldCommands = []string{
	// Git operations
	"git push --force origin main",
	"git push origin :refs/heads/feature",
	"git reset --hard HEAD~5",
	"git clean -fdx",
	"git rebase -i HEAD~3",

	// File operations
	"rm -rf /",
	"rm -rf /tmp/build",
	"rm -rf /*",
	"chmod 000 /etc/passwd",
	"chmod -R 777 /var/www",
	"chown -R root:root /",

	// Docker/K8s
	"docker system prune -af",
	"docker rm $(docker ps -aq)",
	"kubectl delete namespace production",
	"kubectl delete pods --all -n kube-system",

	// Database
	"psql -c 'DROP DATABASE production'",
	"mysql -e 'DROP TABLE users'",
	"redis-cli FLUSHALL",

	// Infrastructure
	"terraform destroy -auto-approve",
	"pulumi destroy --yes",
	"ansible all -m shell -a 'rm -rf /'",

	// Cloud
	"aws ec2 terminate-instances --instance-ids i-1234567890abcdef0",
	"gcloud projects delete my-project --quiet",
	"az group delete --name my-rg --yes",

	// Inline scripts
	`python -c "import os; os.system('rm -rf /')"`,
	`bash -c "rm -rf /tmp/data"`,
	`ruby -e "system('git push --force')"`,
	`node -e "require('child_process').execSync('rm -rf /')"`,
	`perl -e 'system("rm -rf /")'`,

	// Dataflow
	"DIR=/; rm -rf $DIR",
	"export RAILS_ENV=production && rails db:reset",
	"DIR=/tmp || DIR=/; rm -rf $DIR",
	"FILE=$(mktemp); rm -rf $FILE",

	// Compound
	"cd /tmp && rm -rf *",
	"test -d /backup || mkdir /backup; cp -r / /backup",
	"echo hello | tee /dev/null",
	"cat /var/log/syslog | grep error | sort | uniq -c | sort -rn | head -20",

	// Path-prefixed
	"/usr/bin/git push --force",
	"/usr/local/bin/rm -rf /tmp/foo",
	"./script.sh --dangerous",

	// Env prefixed
	"RAILS_ENV=production rails db:drop",
	"NODE_ENV=production npm run migrate:destroy",
	"GIT_AUTHOR_EMAIL=foo@bar.com git push --force origin main",

	// Heredocs
	"bash <<'EOF'\nrm -rf /\nEOF",
	"cat <<EOF | bash\nterraform destroy -auto-approve\nEOF",

	// Edge cases
	"",
	"   ",
	"ls",
	"echo 'hello world'",
	`echo "don't rm -rf /"`,
	"eval 'rm -rf /'",
	"eval rm -rf /",
}

// adversarialInputs are crafted inputs designed to stress parser and extraction.
var adversarialInputs = []string{
	// Deeply nested quoting
	`echo "hello 'world "nested" quotes' end"`,
	`echo "$(echo "$(echo "deep")")"`,

	// Massive repetition (kept moderate to avoid 60s+ parse times)
	strings.Repeat(`"`, 1000),
	strings.Repeat(`'`, 999),
	strings.Repeat("echo hello; ", 100),
	strings.Repeat("a ", 500),

	// Invalid UTF-8
	string([]byte{0xff, 0xfe, 0xfd}),
	string([]byte{0xc0, 0xaf}),

	// Null bytes
	"echo \x00 hidden",
	"\x00\x00\x00\x00",

	// ANSI escapes
	"echo '\x1b[31mred\x1b[0m'",

	// Unicode
	"echo '日本語'",
	"rm -rf /tmp/名前",

	// Unterminated constructs
	`echo "unterminated`,
	`echo 'unterminated`,
	"echo $(unterminated",
	"(unterminated",

	// Triple operators
	"git push &&& rm -rf /",
	"echo hello ||| echo world",
	"echo ;;; echo",

	// Very long single token
	strings.Repeat("a", 10000),

	// Just operators
	"&&",
	"||",
	"|",
	";",
	";;",

	// Just special characters
	"$",
	"$$",
	"${}",
	"${!@#}",
	"$()",
	"`",
	"``",
}
