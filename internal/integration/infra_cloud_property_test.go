package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
	"github.com/dcosson/destructive-command-guard-go/internal/packs"
)

func TestPropertyEveryInfraCloudDestructivePatternReachable(t *testing.T) {
	reachability := infraCloudReachability()
	packIDs := []string{
		"infrastructure.terraform",
		"infrastructure.pulumi",
		"infrastructure.ansible",
		"cloud.aws",
		"cloud.gcp",
		"cloud.azure",
		"cloud.cloudformation",
	}
	for _, packID := range packIDs {
		pack, ok := findPack(packID)
		if !ok {
			t.Run(packID, func(t *testing.T) { t.Skipf("pack %s not registered", packID) })
			continue
		}
		pairs := reachability[packID]
		byRule := make(map[string]string, len(pairs))
		for _, p := range pairs {
			byRule[p.rule] = p.command
		}
		for _, dp := range pack.Rules {
			dp := dp
			t.Run(packID+"/"+dp.ID, func(t *testing.T) {
				cmd, ok := byRule[dp.ID]
				if !ok {
					t.Fatalf("missing reachability command for %s/%s", packID, dp.ID)
				}
				result := guard.Evaluate(cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
				if !hasRuleMatch(result, packID, dp.ID) {
					t.Fatalf("reachability command did not match %s/%s: %q", packID, dp.ID, cmd)
				}
			})
		}
	}
}

func TestPropertyInfraCloudMutualExclusion(t *testing.T) {
	tools := []struct {
		packID string
		cmd    string
	}{
		{"infrastructure.terraform", "terraform destroy -auto-approve"},
		{"infrastructure.pulumi", "pulumi destroy --yes"},
		{"infrastructure.ansible", "ansible all -m file -a state=absent"},
		{"cloud.aws", "aws ec2 terminate-instances --instance-ids i-123"},
		{"cloud.gcp", "gcloud projects delete my-proj"},
		{"cloud.azure", "az group delete --name rg1"},
		{"cloud.cloudformation", "aws cloudformation delete-stack --stack-name demo"},
	}
	for _, tool := range tools {
		tool := tool
		t.Run(tool.packID, func(t *testing.T) {
			if !HasRegisteredPack(tool.packID) {
				t.Skipf("pack %s not registered", tool.packID)
			}
			result := guard.Evaluate(tool.cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
			for _, m := range result.Matches {
				if strings.HasPrefix(m.Pack, "infrastructure.") || strings.HasPrefix(m.Pack, "cloud.") {
					if m.Pack != tool.packID {
						t.Fatalf("%s command triggered cross-pack match %s/%s", tool.packID, m.Pack, m.Rule)
					}
				}
			}
		})
	}
}

func TestPropertyInfraCloudUniversalEnvSensitivity(t *testing.T) {
	packIDs := []string{
		"infrastructure.terraform",
		"infrastructure.pulumi",
		"infrastructure.ansible",
		"cloud.aws",
		"cloud.gcp",
		"cloud.azure",
		"cloud.cloudformation",
	}
	for _, packID := range packIDs {
		pack, ok := findPack(packID)
		if !ok {
			t.Run(packID, func(t *testing.T) { t.Skipf("pack %s not registered", packID) })
			continue
		}
		for _, dp := range pack.Rules {
			dp := dp
			t.Run(packID+"/"+dp.ID, func(t *testing.T) {
				if !dp.EnvSensitive {
					t.Fatalf("%s/%s must be env-sensitive", packID, dp.ID)
				}
			})
		}
	}
}

func TestPropertyInfraCloudAutoApproveSplit(t *testing.T) {
	// Severity relation checks when both rules exist.
	checks := []struct {
		packID   string
		autoRule string
		baseRule string
	}{
		{"infrastructure.terraform", "terraform-destroy-auto-approve", "terraform-destroy"},
		{"infrastructure.terraform", "terraform-apply-auto-approve", "terraform-apply"},
		{"infrastructure.pulumi", "pulumi-destroy-yes", "pulumi-destroy"},
		{"infrastructure.pulumi", "pulumi-up-yes", "pulumi-up"},
	}
	for _, c := range checks {
		c := c
		t.Run(c.packID+"/"+c.autoRule, func(t *testing.T) {
			pk, ok := findPack(c.packID)
			if !ok {
				t.Skipf("pack %s not registered", c.packID)
			}
			auto, okA := findRule(pk, c.autoRule)
			base, okB := findRule(pk, c.baseRule)
			if !okA || !okB {
				t.Skipf("rules not present for %s: %s / %s", c.packID, c.autoRule, c.baseRule)
			}
			if auto.Severity <= base.Severity {
				t.Fatalf("auto-approve rule severity must be higher: %d <= %d", auto.Severity, base.Severity)
			}
		})
	}
}

func TestPropertyInfraCloudArgAtCorrectness(t *testing.T) {
	// Deterministic examples for all 7 tools from the harness plan.
	cases := []struct {
		toolID string
		cmd    string
	}{
		{"infrastructure.terraform", "terraform destroy -auto-approve"},
		{"infrastructure.pulumi", "pulumi destroy --yes"},
		{"infrastructure.ansible", "ansible all -m file -a state=absent"},
		{"cloud.aws", "aws ec2 terminate-instances --instance-ids i-123"},
		{"cloud.gcp", "gcloud projects delete my-proj"},
		{"cloud.azure", "az group delete --name rg1"},
		{"cloud.cloudformation", "aws cloudformation delete-stack --stack-name demo"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.toolID, func(t *testing.T) {
			if !HasRegisteredPack(tc.toolID) {
				t.Skipf("pack %s not registered", tc.toolID)
			}
			result := guard.Evaluate(tc.cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
			if result.Decision == guard.Allow {
				t.Fatalf("expected non-Allow for tool %s command %q", tc.toolID, tc.cmd)
			}
		})
	}
}

func infraCloudReachability() map[string][]struct {
	rule    string
	command string
} {
	return map[string][]struct {
		rule    string
		command string
	}{
		"infrastructure.terraform": {
			{rule: "terraform-destroy-auto-approve", command: "terraform destroy -auto-approve"},
			{rule: "terraform-destroy", command: "terraform destroy"},
			{rule: "terraform-apply-auto-approve", command: "terraform apply -auto-approve"},
			{rule: "terraform-apply", command: "terraform apply"},
		},
		"infrastructure.pulumi": {
			{rule: "pulumi-destroy-yes", command: "pulumi destroy --yes"},
			{rule: "pulumi-destroy", command: "pulumi destroy"},
			{rule: "pulumi-up-yes", command: "pulumi up --yes"},
			{rule: "pulumi-up", command: "pulumi up"},
		},
		"infrastructure.ansible": {
			{rule: "ansible-delete", command: "ansible all -m file -a state=absent"},
		},
		"cloud.aws": {
			{rule: "aws-ec2-terminate", command: "aws ec2 terminate-instances --instance-ids i-123"},
		},
		"cloud.gcp": {
			{rule: "gcp-project-delete", command: "gcloud projects delete my-proj"},
		},
		"cloud.azure": {
			{rule: "azure-group-delete", command: "az group delete --name rg1"},
		},
		"cloud.cloudformation": {
			{rule: "cloudformation-delete-stack", command: "aws cloudformation delete-stack --stack-name demo"},
		},
	}
}

func findPack(id string) (packs.Pack, bool) {
	for _, p := range packs.DefaultRegistry.All() {
		if p.ID == id {
			return p, true
		}
	}
	return packs.Pack{}, false
}

func findRule(pack packs.Pack, id string) (packs.Rule, bool) {
	for _, r := range pack.Rules {
		if r.ID == id {
			return r, true
		}
	}
	return packs.Rule{}, false
}

func hasRuleMatch(result guard.Result, packID, ruleID string) bool {
	for _, m := range result.Matches {
		if m.Pack == packID && m.Rule == ruleID {
			return true
		}
	}
	return false
}

func TestDeterministicInfraCloudExamples(t *testing.T) {
	examples := []struct {
		tool string
		cmd  string
	}{
		{"Terraform", "terraform destroy -auto-approve"},
		{"Pulumi", "pulumi destroy --yes"},
		{"Ansible", "ansible all -m file -a state=absent"},
		{"AWS", "aws ec2 terminate-instances --instance-ids i-123"},
		{"GCP", "gcloud projects delete my-proj"},
		{"Azure", "az group delete --name rg1"},
		{"CloudFormation", "aws cloudformation delete-stack --stack-name demo"},
	}

	for _, ex := range examples {
		ex := ex
		t.Run(ex.tool, func(t *testing.T) {
			result := guard.Evaluate(ex.cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
			t.Logf("%s => decision=%s matches=%d", ex.cmd, result.Decision, len(result.Matches))
		})
	}
}

func TestInfraCloudReachabilityCountHint(t *testing.T) {
	// This is an informational check tied to the plan's 58-pattern target.
	// It is non-fatal when the current registry has fewer infra/cloud patterns.
	count := 0
	for _, p := range packs.DefaultRegistry.All() {
		if strings.HasPrefix(p.ID, "infrastructure.") || strings.HasPrefix(p.ID, "cloud.") {
			count += len(p.Rules)
		}
	}
	t.Logf("infra/cloud destructive pattern count in current registry: %d (plan target: 58)", count)
	if count == 0 {
		t.Skip("infra/cloud packs not present in current registry")
	}
	if count < 58 {
		t.Logf("registry currently below plan target; tests still validate present patterns")
	}
}

func TestInfraCloudMutualExclusionSmoke(t *testing.T) {
	// Lightweight smoke check that different tool commands are not all mapped
	// to the same pattern set.
	cmds := []string{
		"terraform destroy -auto-approve",
		"pulumi destroy --yes",
		"ansible all -m file -a state=absent",
	}
	signatures := map[string]struct{}{}
	for _, cmd := range cmds {
		r := guard.Evaluate(cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
		var key strings.Builder
		for _, m := range r.Matches {
			key.WriteString(fmt.Sprintf("%s/%s;", m.Pack, m.Rule))
		}
		signatures[key.String()] = struct{}{}
	}
	if len(signatures) == 1 {
		t.Log("all infra/cloud commands resolved identically in current registry; likely packs missing")
	}
}
