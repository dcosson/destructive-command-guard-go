package tooluse

import (
	"testing"
)

func TestNormalize_Bash(t *testing.T) {
	result := Normalize("Bash", map[string]any{"command": "rm -rf /"})
	if !result.UseBashParser {
		t.Fatal("expected UseBashParser=true for Bash tool")
	}
	if result.BashCommand != "rm -rf /" {
		t.Errorf("BashCommand = %q, want %q", result.BashCommand, "rm -rf /")
	}
	if result.CommandSummary != "rm -rf /" {
		t.Errorf("CommandSummary = %q, want %q", result.CommandSummary, "rm -rf /")
	}
}

func TestNormalize_Bash_EmptyCommand(t *testing.T) {
	result := Normalize("Bash", map[string]any{"command": ""})
	if result.NormalizationError {
		t.Fatal("empty Bash command should not be a normalization error")
	}
	if !result.UseBashParser {
		t.Fatal("expected UseBashParser=true for empty Bash command")
	}
	if result.BashCommand != "" {
		t.Errorf("BashCommand = %q, want empty", result.BashCommand)
	}
}

func TestNormalize_Bash_MissingCommand(t *testing.T) {
	result := Normalize("Bash", map[string]any{})
	if !result.NormalizationError {
		t.Fatal("expected NormalizationError for Bash with missing command")
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected warning")
	}
}

func TestNormalize_Bash_WrongType(t *testing.T) {
	result := Normalize("Bash", map[string]any{"command": 123})
	if !result.NormalizationError {
		t.Fatal("expected NormalizationError for Bash with non-string command")
	}
}

func TestNormalize_Bash_NilInput(t *testing.T) {
	result := Normalize("Bash", nil)
	if !result.NormalizationError {
		t.Fatal("expected NormalizationError for Bash with nil input")
	}
}

func TestNormalize_Read(t *testing.T) {
	result := Normalize("Read", map[string]any{"file_path": "/home/user/.ssh/id_rsa"})
	if result.NormalizationError {
		t.Fatal("unexpected NormalizationError")
	}
	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result.Commands))
	}
	cmd := result.Commands[0]
	if cmd.Name != "cat" {
		t.Errorf("Name = %q, want %q", cmd.Name, "cat")
	}
	if len(cmd.Args) != 1 || cmd.Args[0] != "/home/user/.ssh/id_rsa" {
		t.Errorf("Args = %v, want [/home/user/.ssh/id_rsa]", cmd.Args)
	}
	if result.RawText != "cat /home/user/.ssh/id_rsa" {
		t.Errorf("RawText = %q, want %q", result.RawText, "cat /home/user/.ssh/id_rsa")
	}
	if result.CommandSummary != "Read(/home/user/.ssh/id_rsa)" {
		t.Errorf("CommandSummary = %q, want %q", result.CommandSummary, "Read(/home/user/.ssh/id_rsa)")
	}
}

func TestNormalize_Read_MissingPath(t *testing.T) {
	result := Normalize("Read", map[string]any{})
	if !result.NormalizationError {
		t.Fatal("expected NormalizationError for Read with missing file_path")
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected warning")
	}
}

func TestNormalize_Read_WrongType(t *testing.T) {
	result := Normalize("Read", map[string]any{"file_path": 123})
	if !result.NormalizationError {
		t.Fatal("expected NormalizationError for Read with non-string file_path")
	}
}

func TestNormalize_Write(t *testing.T) {
	result := Normalize("Write", map[string]any{"file_path": "/tmp/output.txt", "content": "hello"})
	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result.Commands))
	}
	cmd := result.Commands[0]
	if cmd.Name != "tee" {
		t.Errorf("Name = %q, want %q", cmd.Name, "tee")
	}
	if result.RawText != "tee /tmp/output.txt" {
		t.Errorf("RawText = %q, want %q", result.RawText, "tee /tmp/output.txt")
	}
}

func TestNormalize_Edit(t *testing.T) {
	result := Normalize("Edit", map[string]any{"file_path": "/etc/passwd"})
	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result.Commands))
	}
	cmd := result.Commands[0]
	if cmd.Name != "sed" {
		t.Errorf("Name = %q, want %q", cmd.Name, "sed")
	}
	if cmd.Flags["-i"] != "" {
		t.Errorf("expected -i flag, got flags %v", cmd.Flags)
	}
	if _, ok := cmd.Flags["-i"]; !ok {
		t.Error("expected -i flag to be present")
	}
	if result.RawText != "sed -i /etc/passwd" {
		t.Errorf("RawText = %q, want %q", result.RawText, "sed -i /etc/passwd")
	}
}

func TestNormalize_Grep(t *testing.T) {
	result := Normalize("Grep", map[string]any{
		"pattern": "password",
		"path":    "/home/user",
	})
	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result.Commands))
	}
	cmd := result.Commands[0]
	if cmd.Name != "grep" {
		t.Errorf("Name = %q, want %q", cmd.Name, "grep")
	}
	// ExtraFields (pattern) come before path
	if len(cmd.Args) != 2 || cmd.Args[0] != "password" || cmd.Args[1] != "/home/user" {
		t.Errorf("Args = %v, want [password /home/user]", cmd.Args)
	}
	if result.RawText != "grep password /home/user" {
		t.Errorf("RawText = %q, want %q", result.RawText, "grep password /home/user")
	}
	if result.CommandSummary != "Grep(password, /home/user)" {
		t.Errorf("CommandSummary = %q, want %q", result.CommandSummary, "Grep(password, /home/user)")
	}
}

func TestNormalize_Grep_MissingPath(t *testing.T) {
	result := Normalize("Grep", map[string]any{"pattern": "foo"})
	if !result.NormalizationError {
		t.Fatal("expected NormalizationError for Grep with missing path")
	}
}

func TestNormalize_Glob(t *testing.T) {
	result := Normalize("Glob", map[string]any{
		"pattern": "*.key",
		"path":    "/home/user/.ssh",
	})
	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result.Commands))
	}
	cmd := result.Commands[0]
	if cmd.Name != "find" {
		t.Errorf("Name = %q, want %q", cmd.Name, "find")
	}
	// find <path> -name <pattern> — path before extras
	wantRaw := "find /home/user/.ssh -name *.key"
	if result.RawText != wantRaw {
		t.Errorf("RawText = %q, want %q", result.RawText, wantRaw)
	}
	// Args should be: path, -name, pattern
	wantArgs := []string{"/home/user/.ssh", "-name", "*.key"}
	if len(cmd.Args) != len(wantArgs) {
		t.Fatalf("Args = %v, want %v", cmd.Args, wantArgs)
	}
	for i, want := range wantArgs {
		if cmd.Args[i] != want {
			t.Errorf("Args[%d] = %q, want %q", i, cmd.Args[i], want)
		}
	}
}

func TestNormalize_Glob_NoPath(t *testing.T) {
	result := Normalize("Glob", map[string]any{"pattern": "*.go"})
	if result.NormalizationError {
		t.Fatal("Glob with missing path should not be a normalization error (path is optional)")
	}
	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result.Commands))
	}
	wantRaw := "find . -name *.go"
	if result.RawText != wantRaw {
		t.Errorf("RawText = %q, want %q", result.RawText, wantRaw)
	}
}

func TestNormalize_WebFetch(t *testing.T) {
	result := Normalize("WebFetch", map[string]any{"url": "https://example.com/api"})
	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result.Commands))
	}
	cmd := result.Commands[0]
	if cmd.Name != "curl" {
		t.Errorf("Name = %q, want %q", cmd.Name, "curl")
	}
	if result.RawText != "curl https://example.com/api" {
		t.Errorf("RawText = %q, want %q", result.RawText, "curl https://example.com/api")
	}
}

func TestNormalize_NotebookEdit(t *testing.T) {
	result := Normalize("NotebookEdit", map[string]any{"file_path": "/home/user/notebook.ipynb"})
	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result.Commands))
	}
	if result.Commands[0].Name != "sed" {
		t.Errorf("Name = %q, want %q", result.Commands[0].Name, "sed")
	}
}

func TestNormalize_Agent_NoEval(t *testing.T) {
	result := Normalize("Agent", map[string]any{"prompt": "do something"})
	if result.NormalizationError {
		t.Fatal("unexpected NormalizationError for Agent")
	}
	if len(result.Commands) != 0 {
		t.Errorf("expected 0 commands for Agent, got %d", len(result.Commands))
	}
	if result.CommandSummary != "Agent" {
		t.Errorf("CommandSummary = %q, want %q", result.CommandSummary, "Agent")
	}
}

func TestNormalize_WebSearch_NoEval(t *testing.T) {
	result := Normalize("WebSearch", map[string]any{"query": "golang testing"})
	if len(result.Commands) != 0 {
		t.Errorf("expected 0 commands for WebSearch, got %d", len(result.Commands))
	}
}

func TestNormalize_UnknownTool(t *testing.T) {
	result := Normalize("FutureTool", map[string]any{"foo": "bar"})
	if result.NormalizationError {
		t.Fatal("unknown tool should not be a normalization error")
	}
	if len(result.Commands) != 0 {
		t.Errorf("expected 0 commands for unknown tool, got %d", len(result.Commands))
	}
	if result.CommandSummary != "FutureTool" {
		t.Errorf("CommandSummary = %q, want %q", result.CommandSummary, "FutureTool")
	}
}

func TestNormalize_KnownTool_NilInput(t *testing.T) {
	result := Normalize("Read", nil)
	if !result.NormalizationError {
		t.Fatal("expected NormalizationError for known tool with nil input")
	}
}

func TestNormalize_UnknownTool_NilInput(t *testing.T) {
	result := Normalize("FutureTool", nil)
	if result.NormalizationError {
		t.Fatal("unknown tool with nil input should not be a normalization error")
	}
}

func TestNormalize_ExtraFieldsIgnored(t *testing.T) {
	result := Normalize("Read", map[string]any{
		"file_path":   "/tmp/file.txt",
		"extra_field": "should be ignored",
		"another_one": 42,
	})
	if result.NormalizationError {
		t.Fatal("extra fields should not cause normalization error")
	}
	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result.Commands))
	}
}

func TestNormalize_RawTextIsFullCommand(t *testing.T) {
	tests := []struct {
		tool    string
		input   map[string]any
		wantRaw string
	}{
		{"Read", map[string]any{"file_path": "/foo"}, "cat /foo"},
		{"Write", map[string]any{"file_path": "/foo"}, "tee /foo"},
		{"Edit", map[string]any{"file_path": "/foo"}, "sed -i /foo"},
		{"Grep", map[string]any{"pattern": "secret", "path": "/etc"}, "grep secret /etc"},
		{"Glob", map[string]any{"pattern": "*.txt", "path": "/home"}, "find /home -name *.txt"},
		{"NotebookEdit", map[string]any{"file_path": "/nb.ipynb"}, "sed -i /nb.ipynb"},
		{"WebFetch", map[string]any{"url": "https://x.com"}, "curl https://x.com"},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			result := Normalize(tt.tool, tt.input)
			if result.RawText != tt.wantRaw {
				t.Errorf("RawText = %q, want %q", result.RawText, tt.wantRaw)
			}
		})
	}
}

func TestNormalize_ArgOrdering(t *testing.T) {
	tests := []struct {
		tool     string
		input    map[string]any
		wantArgs []string
	}{
		{"Read", map[string]any{"file_path": "/foo"}, []string{"/foo"}},
		{"Write", map[string]any{"file_path": "/foo"}, []string{"/foo"}},
		{"Edit", map[string]any{"file_path": "/foo"}, []string{"-i", "/foo"}},
		{"Grep", map[string]any{"pattern": "x", "path": "/bar"}, []string{"x", "/bar"}},
		{"Glob", map[string]any{"pattern": "*.go", "path": "/src"}, []string{"/src", "-name", "*.go"}},
		{"NotebookEdit", map[string]any{"file_path": "/nb.ipynb"}, []string{"-i", "/nb.ipynb"}},
		{"WebFetch", map[string]any{"url": "https://x.com"}, []string{"https://x.com"}},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			result := Normalize(tt.tool, tt.input)
			if len(result.Commands) != 1 {
				t.Fatalf("expected 1 command, got %d", len(result.Commands))
			}
			args := result.Commands[0].Args
			if len(args) != len(tt.wantArgs) {
				t.Fatalf("Args = %v, want %v", args, tt.wantArgs)
			}
			for i, want := range tt.wantArgs {
				if args[i] != want {
					t.Errorf("Args[%d] = %q, want %q", i, args[i], want)
				}
			}
		})
	}
}

func TestLookupTool(t *testing.T) {
	if def := LookupTool("Read"); def == nil || def.SyntheticCommand != "cat" {
		t.Errorf("LookupTool(Read) = %v, want cat", def)
	}
	if def := LookupTool("NonExistent"); def != nil {
		t.Errorf("LookupTool(NonExistent) = %v, want nil", def)
	}
}
