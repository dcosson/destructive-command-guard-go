package main

import (
	"io"
	"os"

	"github.com/spf13/cobra"
)

var (
	stdin     io.Reader = os.Stdin
	stdout    io.Writer = os.Stdout
	stderr    io.Writer = os.Stderr
	exitFn              = os.Exit
	environFn           = os.Environ
)

// Version is set at build time via -ldflags.
var Version = "dev"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "dcg-go",
		Short: "Destructive Command Guard",
		Long: `Analyzes shell commands for destructive and privacy-sensitive operations.

When invoked with no subcommand, runs in hook mode: reads a Claude Code
PreToolUse JSON event from stdin and writes a JSON response to stdout.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHookMode()
		},
	}

	root.AddCommand(newTestCmd())
	root.AddCommand(newListCmd())
	root.AddCommand(newVersionCmd())

	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Printf("dcg-go %s\n", Version)
		},
	}
}

func main() {
	cmd := newRootCmd()
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetIn(stdin)
	if err := cmd.Execute(); err != nil {
		exitFn(1)
	}
}
