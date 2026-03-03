package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestMainHelp(t *testing.T) {
	reset := withIO(t)
	defer reset()

	oldArgs := os.Args
	os.Args = []string{"dcg-go", "help"}
	t.Cleanup(func() { os.Args = oldArgs })

	main()
	if !strings.Contains(stdout.(*bytes.Buffer).String(), "Usage:") {
		t.Fatalf("help output missing usage: %q", stdout.(*bytes.Buffer).String())
	}
}

func TestMainUnknownCommandExits(t *testing.T) {
	reset := withIO(t)
	defer reset()

	oldArgs := os.Args
	os.Args = []string{"dcg-go", "wat"}
	t.Cleanup(func() { os.Args = oldArgs })

	var code int
	exitFn = func(c int) { code = c }
	main()
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.(*bytes.Buffer).String(), "unknown command: wat") {
		t.Fatalf("stderr missing unknown command: %q", stderr.(*bytes.Buffer).String())
	}
}

func withIO(t *testing.T) func() {
	t.Helper()
	oldIn, oldOut, oldErr := stdin, stdout, stderr
	oldExit, oldEnv := exitFn, environFn
	stdin = bytes.NewBuffer(nil)
	stdout = &bytes.Buffer{}
	stderr = &bytes.Buffer{}
	exitFn = oldExit
	environFn = oldEnv
	return func() {
		stdin, stdout, stderr = oldIn, oldOut, oldErr
		exitFn, environFn = oldExit, oldEnv
	}
}
