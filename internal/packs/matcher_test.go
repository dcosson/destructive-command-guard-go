package packs

import "testing"

func TestArgSubsequence(t *testing.T) {
	t.Parallel()

	m := ArgSubsequence("ec2", "terminate-instances")
	tests := []struct {
		name string
		cmd  Command
		want bool
	}{
		{
			name: "contiguous",
			cmd:  Command{Args: []string{"ec2", "terminate-instances"}},
			want: true,
		},
		{
			name: "with global flag value before subcommand",
			cmd:  Command{Args: []string{"prod", "ec2", "terminate-instances", "i-123"}},
			want: true,
		},
		{
			name: "wrong order",
			cmd:  Command{Args: []string{"terminate-instances", "ec2"}},
			want: false,
		},
		{
			name: "missing",
			cmd:  Command{Args: []string{"ec2", "describe-instances"}},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := m.Match(tt.cmd)
			if got != tt.want {
				t.Fatalf("ArgSubsequence match=%v, want=%v (args=%v)", got, tt.want, tt.cmd.Args)
			}
		})
	}
}

func TestArgSubsequenceInvalidInputs(t *testing.T) {
	t.Parallel()

	if ArgSubsequence().Match(Command{Args: []string{"a"}}) {
		t.Fatal("expected empty matcher input to return false")
	}
	if ArgSubsequence("a", "").Match(Command{Args: []string{"a"}}) {
		t.Fatal("expected empty term matcher input to return false")
	}
}
