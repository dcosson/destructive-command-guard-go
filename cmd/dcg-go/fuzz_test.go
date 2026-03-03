package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func FuzzConfigParse(f *testing.F) {
	f.Add([]byte("policy: strict\n"))
	f.Add([]byte("policy: 42\n"))
	f.Add([]byte("\x00\x01\x02\xff"))
	f.Add([]byte("a: &a [*a, *a, *a, *a]"))
	f.Add([]byte(strings.Repeat("key: value\n", 10000)))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = parseConfig(data)
	})
}

func FuzzHookInput(f *testing.F) {
	f.Add([]byte(`{"tool_name":"Bash","tool_input":{"command":"ls"}}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`not json`))
	f.Add([]byte(``))

	f.Fuzz(func(t *testing.T, data []byte) {
		var hookInput HookInput
		_ = json.Unmarshal(data, &hookInput)
	})
}

func FuzzHookProcess(f *testing.F) {
	f.Add([]byte(`{"tool_name":"Bash","tool_input":{"command":"ls"}}`))
	f.Add([]byte(`{"tool_name":"Bash","tool_input":{"command":"rm -rf /"}}`))
	f.Add([]byte(`{"tool_name":"Read","tool_input":{"file_path":"/etc/passwd"}}`))
	f.Add([]byte(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"echo hello"}}`))
	f.Add([]byte(`{"tool_name":"Bash","tool_input":{"command":"` + strings.Repeat("a", 10000) + `"}}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var hookInput HookInput
		if err := json.Unmarshal(data, &hookInput); err != nil {
			return
		}
		_ = processHookInput(hookInput)
	})
}
