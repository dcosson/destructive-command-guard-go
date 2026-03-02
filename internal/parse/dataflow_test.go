package parse

import "testing"

func TestDataflowDefineAndMerge(t *testing.T) {
	t.Parallel()

	da := NewDataflowAnalyzer()
	da.Define("DIR", "/tmp", false)
	if got := da.Resolve("DIR"); len(got) != 1 || got[0] != "/tmp" {
		t.Fatalf("resolve DIR = %#v, want [/tmp]", got)
	}

	branch := NewDataflowAnalyzer()
	branch.Define("DIR", "/", false)
	da.MergeBranch(branch)

	got := da.Resolve("DIR")
	if len(got) != 2 {
		t.Fatalf("expected merged values, got %#v", got)
	}
}

func TestDataflowIndeterminateSubstitution(t *testing.T) {
	t.Parallel()

	da := NewDataflowAnalyzer()
	da.DefineIndeterminate("FILE", false)
	expansions, capped := da.ResolveString("rm -rf $FILE")
	if capped {
		t.Fatalf("did not expect cap for single unresolved value")
	}
	if len(expansions) != 1 || expansions[0] != "rm -rf $FILE" {
		t.Fatalf("unexpected expansion for indeterminate var: %#v", expansions)
	}
}

func TestDataflowResolveStringCap(t *testing.T) {
	t.Parallel()

	da := NewDataflowAnalyzer()
	for i := 0; i < 5; i++ {
		name := "V" + string(rune('A'+i))
		da.Define(name, "x", false)
		other := NewDataflowAnalyzer()
		other.Define(name, "y", false)
		da.MergeBranch(other)
	}

	expansions, capped := da.ResolveString("$VA $VB $VC $VD $VE")
	if !capped {
		t.Fatalf("expected capped expansion")
	}
	if len(expansions) != maxExpansions {
		t.Fatalf("expected %d expansions, got %d", maxExpansions, len(expansions))
	}
}

func TestExportedVars(t *testing.T) {
	t.Parallel()

	da := NewDataflowAnalyzer()
	da.Define("A", "1", false)
	da.Define("B", "2", true)
	da.DefineIndeterminate("C", true)

	exported := da.ExportedVars()
	if _, ok := exported["A"]; ok {
		t.Fatalf("non-exported var A should not be present")
	}
	if vals := exported["B"]; len(vals) != 1 || vals[0] != "2" {
		t.Fatalf("exported B mismatch: %#v", vals)
	}
	if _, ok := exported["C"]; ok {
		t.Fatalf("indeterminate export C should be omitted")
	}
}
