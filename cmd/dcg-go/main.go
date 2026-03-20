package main

import (
	"fmt"
	"io"
	"os"
)

var (
	stdin     io.Reader = os.Stdin
	stdout    io.Writer = os.Stdout
	stderr    io.Writer = os.Stderr
	exitFn              = os.Exit
	environFn           = os.Environ
)

const usage = `dcg-go - Destructive Command Guard

Usage:
    dcg-go              Hook mode: read JSON from stdin, evaluate, write JSON
    dcg-go test "cmd"   Evaluate a command and print the result
    dcg-go list packs   List available pattern packs
    dcg-go list rules   List all rules grouped by category
    dcg-go version      Print version information
    dcg-go help         Print this help message

Hook Mode:
    Reads a Claude Code PreToolUse hook event from stdin (JSON),
    evaluates the command, and writes the result to stdout (JSON).
    Always exits 0 on success (decision is in the JSON output).

Test Mode:
    dcg-go test "git push --force"
    dcg-go test --explain "git push --force"
    dcg-go test --json "git push --force"
    dcg-go test --policy strict "RAILS_ENV=production rails db:drop"
    dcg-go test --destructive-policy permissive --privacy-policy strict "cat ~/.ssh/id_rsa"
    dcg-go test --env "rails db:reset"

    Exit codes: 0=Allow, 1=Error, 2=Deny, 3=Ask

List Mode:
    dcg-go list packs
    dcg-go list packs --json
    dcg-go list rules
    dcg-go list rules --json

Config:
    dcg-go looks for config at ~/.config/dcg-go/config.yaml
    Override with DCG_CONFIG environment variable.

    Config fields:
      destructive_policy: strict|interactive|permissive
      privacy_policy: strict|interactive|permissive
`

// Version is set at build time via -ldflags.
var Version = "dev"

func main() {
	if len(os.Args) < 2 {
		if err := runHookMode(); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			exitFn(1)
		}
		return
	}

	switch os.Args[1] {
	case "test":
		if err := runTestMode(os.Args[2:]); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			exitFn(1)
		}
	case "list":
		if err := runListMode(os.Args[2:]); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			exitFn(1)
		}
	case "version":
		fmt.Fprintf(stdout, "dcg-go %s\n", Version)
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usage)
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", os.Args[1])
		fmt.Fprint(stdout, usage)
		exitFn(1)
	}
}
