package eval

import (
	"sort"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/internal/packs"
)

func testRegistry() *packs.Registry {
	return packs.NewRegistry(
		packs.Pack{
			ID:       "core.git",
			Keywords: []string{"git", "push", "reset", "clean"},
		},
		packs.Pack{
			ID:       "core.filesystem",
			Keywords: []string{"rm", "dd", "mkfs", "shred", "truncate"},
		},
		packs.Pack{
			ID:       "database.postgresql",
			Keywords: []string{"psql", "pg_dump", "pg_restore", "dropdb", "createdb"},
		},
		packs.Pack{
			ID:       "database.redis",
			Keywords: []string{"redis-cli"},
		},
	)
}

func TestPreFilter_Contains_WordBoundary(t *testing.T) {
	pf := NewPreFilter(testRegistry())

	tests := []struct {
		name    string
		command string
		want    bool
		wantKW  []string
	}{
		{
			name:    "git command matches",
			command: "git push --force",
			want:    true,
			wantKW:  []string{"git", "push"},
		},
		{
			name:    "git inside github does not match",
			command: "curl https://github.com/foo/bar",
			want:    false,
			wantKW:  nil,
		},
		{
			name:    "rm matches as standalone",
			command: "rm -rf /tmp",
			want:    true,
			wantKW:  []string{"rm"},
		},
		{
			name:    "ls does not match",
			command: "ls -la",
			want:    false,
			wantKW:  nil,
		},
		{
			name:    "psql matches",
			command: "psql -c 'DROP TABLE foo'",
			want:    true,
			wantKW:  []string{"psql"},
		},
		{
			name:    "redis-cli matches",
			command: "redis-cli FLUSHALL",
			want:    true,
			wantKW:  []string{"redis-cli"},
		},
		{
			name:    "empty command",
			command: "",
			want:    false,
			wantKW:  nil,
		},
		{
			name:    "git at end of line",
			command: "which git",
			want:    true,
			wantKW:  []string{"git"},
		},
		{
			name:    "git at start",
			command: "git status",
			want:    true,
			wantKW:  []string{"git"},
		},
		{
			name:    "gitignore should not match git",
			command: "cat .gitignore",
			want:    false,
			wantKW:  nil,
		},
		{
			name:    "pipe with rm",
			command: "find . -name '*.tmp' | xargs rm",
			want:    true,
			wantKW:  []string{"rm"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := pf.Contains(tt.command)
			if r.Matched != tt.want {
				t.Errorf("Contains(%q).Matched = %v, want %v", tt.command, r.Matched, tt.want)
			}
			if tt.wantKW != nil {
				sort.Strings(r.Keywords)
				sort.Strings(tt.wantKW)
				if len(r.Keywords) != len(tt.wantKW) {
					t.Fatalf("Keywords = %v, want %v", r.Keywords, tt.wantKW)
				}
				for i := range tt.wantKW {
					if r.Keywords[i] != tt.wantKW[i] {
						t.Errorf("Keywords[%d] = %q, want %q", i, r.Keywords[i], tt.wantKW[i])
					}
				}
			}
		})
	}
}

func TestPreFilter_MayContainDestructive_BackCompat(t *testing.T) {
	pf := NewPreFilter(testRegistry())
	if !pf.MayContainDestructive("git push") {
		t.Error("expected true for 'git push'")
	}
	if pf.MayContainDestructive("ls -la") {
		t.Error("expected false for 'ls -la'")
	}
}

func TestPreFilter_NilRegistry(t *testing.T) {
	pf := NewPreFilter(nil)
	r := pf.Contains("git push")
	if r.Matched {
		t.Error("nil registry should never match")
	}
}

func TestPreFilter_NilPreFilter(t *testing.T) {
	var pf *PreFilter
	r := pf.Contains("git push")
	if r.Matched {
		t.Error("nil prefilter should never match")
	}
	if pf.MayContainDestructive("git push") {
		t.Error("nil prefilter MayContainDestructive should be false")
	}
}

func TestPreFilter_CandidatePacks(t *testing.T) {
	pf := NewPreFilter(testRegistry())

	tests := []struct {
		name     string
		keywords []string
		enabled  []string
		want     []string
	}{
		{
			name:     "git keyword maps to core.git",
			keywords: []string{"git"},
			enabled:  nil,
			want:     []string{"core.git"},
		},
		{
			name:     "push keyword maps to core.git",
			keywords: []string{"push"},
			enabled:  nil,
			want:     []string{"core.git"},
		},
		{
			name:     "rm maps to core.filesystem",
			keywords: []string{"rm"},
			enabled:  nil,
			want:     []string{"core.filesystem"},
		},
		{
			name:     "multiple keywords from different packs",
			keywords: []string{"git", "rm"},
			enabled:  nil,
			want:     []string{"core.git", "core.filesystem"},
		},
		{
			name:     "filtered by enabledPacks",
			keywords: []string{"git", "rm"},
			enabled:  []string{"core.git"},
			want:     []string{"core.git"},
		},
		{
			name:     "enabled pack not in keywords",
			keywords: []string{"rm"},
			enabled:  []string{"core.git"},
			want:     nil,
		},
		{
			name:     "empty keywords",
			keywords: nil,
			enabled:  nil,
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pf.CandidatePacks(tt.keywords, tt.enabled)
			sort.Strings(got)
			sort.Strings(tt.want)
			if len(got) != len(tt.want) {
				t.Fatalf("CandidatePacks() = %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("got[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestWordBoundary(t *testing.T) {
	tests := []struct {
		text  string
		start int
		end   int
		want  bool
	}{
		{"git push", 0, 3, true},    // "git" at start
		{"git push", 4, 8, true},    // "push" at end
		{" git ", 1, 4, true},       // "git" surrounded by spaces
		{"agit", 1, 4, false},       // "git" preceded by 'a'
		{"gito", 0, 3, false},       // "git" followed by 'o'
		{".gitignore", 1, 4, false}, // "git" followed by 'i'
		{"x;git;y", 2, 5, true},     // "git" bounded by semicolons
		{"(git)", 1, 4, true},       // "git" bounded by parens
		{"git", 0, 3, true},         // whole string
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got := isWordBoundary(tt.text, tt.start, tt.end)
			if got != tt.want {
				t.Errorf("isWordBoundary(%q, %d, %d) = %v, want %v",
					tt.text, tt.start, tt.end, got, tt.want)
			}
		})
	}
}
