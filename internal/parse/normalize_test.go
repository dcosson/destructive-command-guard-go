package parse

import "testing"

func TestNormalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{in: "git", want: "git"},
		{in: "/usr/bin/git", want: "git"},
		{in: "/usr/local/bin/rm", want: "rm"},
		{in: "./script.sh", want: "script.sh"},
		{in: "/", want: ""},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := Normalize(tc.in); got != tc.want {
				t.Fatalf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestUnwrapEnvPrefix(t *testing.T) {
	t.Parallel()

	cmd := ExtractedCommand{
		Name:    "env",
		RawName: "env",
		RawArgs: []string{"RAILS_ENV=production", "/usr/bin/rails", "db:reset", "--trace"},
		Flags:   map[string]string{},
	}

	unwrapEnvPrefix(&cmd)

	if cmd.Name != "rails" {
		t.Fatalf("expected unwrapped name rails, got %q", cmd.Name)
	}
	if cmd.RawName != "/usr/bin/rails" {
		t.Fatalf("expected raw name /usr/bin/rails, got %q", cmd.RawName)
	}
	if cmd.InlineEnv["RAILS_ENV"] != "production" {
		t.Fatalf("expected inline env to include RAILS_ENV")
	}
	if len(cmd.RawArgs) != 2 || cmd.RawArgs[0] != "db:reset" || cmd.RawArgs[1] != "--trace" {
		t.Fatalf("unexpected args after unwrap: %#v", cmd.RawArgs)
	}
}
