package guard

import "testing"

func TestStrictPolicyDecisions(t *testing.T) {
	p := StrictPolicy()
	cases := []struct {
		name string
		sev  Severity
		want Decision
	}{
		{name: "indeterminate", sev: Indeterminate, want: Deny},
		{name: "low", sev: Low, want: Allow},
		{name: "medium", sev: Medium, want: Deny},
		{name: "high", sev: High, want: Deny},
		{name: "critical", sev: Critical, want: Deny},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := p.Decide(Assessment{Severity: tc.sev})
			if got != tc.want {
				t.Fatalf("Decide(%v) = %v, want %v", tc.sev, got, tc.want)
			}
		})
	}
}

func TestInteractivePolicyDecisions(t *testing.T) {
	p := InteractivePolicy()
	cases := []struct {
		name string
		sev  Severity
		want Decision
	}{
		{name: "indeterminate", sev: Indeterminate, want: Ask},
		{name: "low", sev: Low, want: Allow},
		{name: "medium", sev: Medium, want: Ask},
		{name: "high", sev: High, want: Deny},
		{name: "critical", sev: Critical, want: Deny},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := p.Decide(Assessment{Severity: tc.sev})
			if got != tc.want {
				t.Fatalf("Decide(%v) = %v, want %v", tc.sev, got, tc.want)
			}
		})
	}
}

func TestPermissivePolicyDecisions(t *testing.T) {
	p := PermissivePolicy()
	cases := []struct {
		name string
		sev  Severity
		want Decision
	}{
		{name: "indeterminate", sev: Indeterminate, want: Allow},
		{name: "low", sev: Low, want: Allow},
		{name: "medium", sev: Medium, want: Allow},
		{name: "high", sev: High, want: Ask},
		{name: "critical", sev: Critical, want: Deny},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := p.Decide(Assessment{Severity: tc.sev})
			if got != tc.want {
				t.Fatalf("Decide(%v) = %v, want %v", tc.sev, got, tc.want)
			}
		})
	}
}
