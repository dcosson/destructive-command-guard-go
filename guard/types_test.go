package guard

import "testing"

func TestStringers(t *testing.T) {
	if got := Indeterminate.String(); got != "Indeterminate" {
		t.Fatalf("Indeterminate.String() = %q", got)
	}
	if got := Low.String(); got != "Low" {
		t.Fatalf("Low.String() = %q", got)
	}
	if got := Medium.String(); got != "Medium" {
		t.Fatalf("Medium.String() = %q", got)
	}
	if got := High.String(); got != "High" {
		t.Fatalf("High.String() = %q", got)
	}
	if got := Critical.String(); got != "Critical" {
		t.Fatalf("Critical.String() = %q", got)
	}
	if got := Severity(99).String(); got != "Unknown" {
		t.Fatalf("Severity(99).String() = %q", got)
	}

	if got := ConfidenceLow.String(); got != "Low" {
		t.Fatalf("ConfidenceLow.String() = %q", got)
	}
	if got := ConfidenceMedium.String(); got != "Medium" {
		t.Fatalf("ConfidenceMedium.String() = %q", got)
	}
	if got := ConfidenceHigh.String(); got != "High" {
		t.Fatalf("ConfidenceHigh.String() = %q", got)
	}
	if got := Confidence(99).String(); got != "Unknown" {
		t.Fatalf("Confidence(99).String() = %q", got)
	}

	if got := Allow.String(); got != "Allow" {
		t.Fatalf("Allow.String() = %q", got)
	}
	if got := Deny.String(); got != "Deny" {
		t.Fatalf("Deny.String() = %q", got)
	}
	if got := Ask.String(); got != "Ask" {
		t.Fatalf("Ask.String() = %q", got)
	}
	if got := Decision(99).String(); got != "Unknown" {
		t.Fatalf("Decision(99).String() = %q", got)
	}

	if got := WarnPartialParse.String(); got != "PartialParse" {
		t.Fatalf("WarnPartialParse.String() = %q", got)
	}
	if got := WarnInlineDepthExceeded.String(); got != "InlineDepthExceeded" {
		t.Fatalf("WarnInlineDepthExceeded.String() = %q", got)
	}
	if got := WarnInputTruncated.String(); got != "InputTruncated" {
		t.Fatalf("WarnInputTruncated.String() = %q", got)
	}
	if got := WarnExpansionCapped.String(); got != "ExpansionCapped" {
		t.Fatalf("WarnExpansionCapped.String() = %q", got)
	}
	if got := WarnExtractorPanic.String(); got != "ExtractorPanic" {
		t.Fatalf("WarnExtractorPanic.String() = %q", got)
	}
	if got := WarnCommandSubstitution.String(); got != "CommandSubstitution" {
		t.Fatalf("WarnCommandSubstitution.String() = %q", got)
	}
	if got := WarnMatcherPanic.String(); got != "MatcherPanic" {
		t.Fatalf("WarnMatcherPanic.String() = %q", got)
	}
	if got := WarnUnknownPackID.String(); got != "UnknownPackID" {
		t.Fatalf("WarnUnknownPackID.String() = %q", got)
	}
	if got := WarningCode(99).String(); got != "Unknown" {
		t.Fatalf("WarningCode(99).String() = %q", got)
	}
}

func TestZeroValueResultIsAllow(t *testing.T) {
	var r Result

	if r.Decision != Allow {
		t.Fatalf("zero-value result decision = %v, want %v", r.Decision, Allow)
	}
	if r.Assessment != nil {
		t.Fatalf("zero-value result assessment = %+v, want nil", r.Assessment)
	}
	if len(r.Matches) != 0 {
		t.Fatalf("zero-value result matches len = %d, want 0", len(r.Matches))
	}
	if len(r.Warnings) != 0 {
		t.Fatalf("zero-value result warnings len = %d, want 0", len(r.Warnings))
	}
	if r.Command != "" {
		t.Fatalf("zero-value result command = %q, want empty", r.Command)
	}
}
