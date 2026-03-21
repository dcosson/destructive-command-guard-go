package main

import (
	"bytes"
	"testing"
)

// execCmd creates a fresh root command and executes it with the given args.
// Returns stdout, stderr contents.
func execCmd(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	oldOut, oldErr, oldExit, oldEnv := stdout, stderr, exitFn, environFn
	stdout = outBuf
	stderr = errBuf
	exitFn = func(int) {} // don't exit during tests
	t.Cleanup(func() {
		stdout, stderr, exitFn, environFn = oldOut, oldErr, oldExit, oldEnv
	})

	cmd := newRootCmd()
	cmd.SetArgs(args)
	cmd.SetOut(outBuf)
	cmd.SetErr(errBuf)
	err := cmd.Execute()
	return outBuf.String(), errBuf.String(), err
}

func TestMainHelp(t *testing.T) {
	out, _, err := execCmd(t, "help")
	if err != nil {
		t.Fatalf("help error: %v", err)
	}
	if len(out) < 50 {
		t.Fatalf("help output too short: %q", out)
	}
}

func TestMainUnknownCommand(t *testing.T) {
	_, _, err := execCmd(t, "wat")
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
}

// withIO swaps global IO vars for testing. Used by config and integration tests
// that call internal functions directly.
func withIO(t *testing.T) func() {
	t.Helper()
	oldIn, oldOut, oldErr := stdin, stdout, stderr
	oldExit, oldEnv := exitFn, environFn
	stdin = &bytes.Buffer{}
	stdout = &bytes.Buffer{}
	stderr = &bytes.Buffer{}
	return func() {
		stdin, stdout, stderr = oldIn, oldOut, oldErr
		exitFn, environFn = oldExit, oldEnv
	}
}
