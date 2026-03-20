//go:build integration

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

func BenchmarkHookJSONRoundtrip(b *testing.B) {
	input := []byte(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git push --force origin main"}}`)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var in HookInput
		if err := json.Unmarshal(input, &in); err != nil {
			b.Fatalf("unmarshal: %v", err)
		}
		out := processHookInput(in)
		if _, err := json.Marshal(out); err != nil {
			b.Fatalf("marshal: %v", err)
		}
	}
}

func BenchmarkProcessHookInput(b *testing.B) {
	cases := []struct {
		name string
		in   HookInput
	}{
		{name: "safe", in: HookInput{ToolName: "Bash", ToolInput: ToolInput{Command: "echo hello"}}},
		{name: "destructive", in: HookInput{ToolName: "Bash", ToolInput: ToolInput{Command: "rm -rf /"}}},
		{name: "nonbash", in: HookInput{ToolName: "Read", ToolInput: ToolInput{Command: "ignored"}}},
	}
	for _, tc := range cases {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = processHookInput(tc.in)
			}
		})
	}
}

func TestStressConcurrentHookProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("skip stress test in short mode")
	}
	commands := []string{"echo hello", "git push --force", "rm -rf /", "git status", ""}
	workers := 64
	iterations := 500
	var wg sync.WaitGroup
	errCh := make(chan error, workers)

	for w := 0; w < workers; w++ {
		w := w
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				cmd := commands[(w+i)%len(commands)]
				out := processHookInput(HookInput{ToolName: "Bash", ToolInput: ToolInput{Command: cmd}})
				d := out.HookSpecificOutput.PermissionDecision
				if d != "allow" && d != "ask" && d != "deny" {
					errCh <- fmt.Errorf("invalid decision %q", d)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}

func TestOracleHookOutputJSONConformance(t *testing.T) {
	reset := withIO(t)
	defer reset()
	in := HookInput{ToolName: "Bash", ToolInput: ToolInput{Command: "rm -rf /"}}
	out := processHookInput(in)
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	if err := enc.Encode(out); err != nil {
		t.Fatalf("encode output: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	hs, ok := parsed["hookSpecificOutput"].(map[string]any)
	if !ok {
		t.Fatalf("missing hookSpecificOutput field")
	}
	if _, ok := hs["permissionDecision"].(string); !ok {
		t.Fatalf("missing permissionDecision string")
	}
	if _, ok := hs["hookEventName"].(string); !ok {
		t.Fatalf("missing hookEventName string")
	}
}
