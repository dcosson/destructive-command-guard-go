package integration

import (
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

// F1-F3: Fault injection
func TestFaultInfraCloudNilFields(t *testing.T) {
	cmds := []string{
		"terraform",
		"aws",
		"gcloud",
		"az",
		"ansible",
		"pulumi",
		"",
		" ",
		"\t\n",
		"aws \"\"",
	}
	for i, c := range cmds {
		t.Run(fmt.Sprintf("degenerate-%d", i), func(t *testing.T) {
			_ = guard.Evaluate(c, guard.WithDestructivePolicy(guard.InteractivePolicy()))
		})
	}
}

func TestFaultArgAtOutOfBounds(t *testing.T) {
	shortCmds := []string{
		"gcloud",
		"gcloud compute",
		"gcloud compute instances",
		"aws",
		"aws ec2",
		"az",
		"az sql",
		"az sql server",
	}
	for i, c := range shortCmds {
		t.Run(fmt.Sprintf("short-%d", i), func(t *testing.T) {
			_ = guard.Evaluate(c, guard.WithDestructivePolicy(guard.InteractivePolicy()))
		})
	}
}

func TestFaultAnsibleModuleArgs(t *testing.T) {
	cases := []string{
		"ansible all -m file -a ''",
		"ansible all -m file -a 'path=/tmp state=absent recurse=yes'",
		"ansible all -m state=absent -a 'path=/tmp'",
		"ansible all -a 'rm -rf /tmp'",
	}
	for _, c := range cases {
		_ = guard.Evaluate(c, guard.WithDestructivePolicy(guard.InteractivePolicy()))
	}
}

// O1-O3: Oracles / consistency
func TestOracleInfraCloudPolicyMonotonicity(t *testing.T) {
	commands := []string{
		"terraform destroy -auto-approve",
		"pulumi destroy --yes",
		"ansible all -m file -a state=absent",
		"aws ec2 terminate-instances --instance-ids i-123",
		"gcloud projects delete my-proj",
		"az group delete --name rg1",
	}
	restrict := map[guard.Decision]int{guard.Allow: 0, guard.Ask: 1, guard.Deny: 2}
	for _, c := range commands {
		strict := guard.Evaluate(c, guard.WithDestructivePolicy(guard.StrictPolicy()))
		inter := guard.Evaluate(c, guard.WithDestructivePolicy(guard.InteractivePolicy()))
		perm := guard.Evaluate(c, guard.WithDestructivePolicy(guard.PermissivePolicy()))
		sr, ir, pr := restrict[strict.Decision], restrict[inter.Decision], restrict[perm.Decision]
		if sr < ir || ir < pr {
			t.Fatalf("policy monotonicity violated for %q: strict=%s inter=%s perm=%s", c, strict.Decision, inter.Decision, perm.Decision)
		}
	}
}

func TestOracleCrossCloudConsistency(t *testing.T) {
	type eq struct {
		name string
		cmds map[string]string
	}
	equivs := []eq{
		{
			name: "instance termination",
			cmds: map[string]string{
				"cloud.aws":   "aws ec2 terminate-instances --instance-ids i-123",
				"cloud.gcp":   "gcloud compute instances delete vm-1",
				"cloud.azure": "az vm delete --name vm1 --resource-group rg1",
			},
		},
		{
			name: "project/stack deletion",
			cmds: map[string]string{
				"cloud.aws":   "aws cloudformation delete-stack --stack-name app",
				"cloud.gcp":   "gcloud projects delete my-proj",
				"cloud.azure": "az group delete --name rg1",
			},
		},
	}
	for _, e := range equivs {
		t.Run(e.name, func(t *testing.T) {
			var severities []guard.Severity
			for packID, cmd := range e.cmds {
				if !HasRegisteredPack(packID) {
					continue
				}
				r := guard.Evaluate(cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
				if r.DestructiveAssessment != nil {
					severities = append(severities, r.DestructiveAssessment.Severity)
				}
			}
			if len(severities) < 2 {
				t.Skip("insufficient cloud packs registered for consistency check")
			}
			for i := 1; i < len(severities); i++ {
				if severities[i] != severities[0] {
					t.Fatalf("cross-cloud severity mismatch: %v", severities)
				}
			}
		})
	}
}

func TestOracleIaCConsistency(t *testing.T) {
	if !HasRegisteredPack("infrastructure.terraform") || !HasRegisteredPack("infrastructure.pulumi") {
		t.Skip("terraform/pulumi packs not both registered")
	}
	cases := []struct {
		name string
		tf   string
		pl   string
	}{
		{"destroy_auto", "terraform destroy -auto-approve", "pulumi destroy --yes"},
		{"destroy", "terraform destroy", "pulumi destroy"},
		{"apply_up_auto", "terraform apply -auto-approve", "pulumi up --yes"},
		{"apply_up", "terraform apply", "pulumi up"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := guard.Evaluate(tc.tf, guard.WithDestructivePolicy(guard.InteractivePolicy()))
			rp := guard.Evaluate(tc.pl, guard.WithDestructivePolicy(guard.InteractivePolicy()))
			if rt.DestructiveAssessment == nil || rp.DestructiveAssessment == nil {
				t.Fatalf("expected both commands to be assessed")
			}
			if rt.DestructiveAssessment.Severity != rp.DestructiveAssessment.Severity {
				t.Fatalf("severity mismatch tf=%s pulumi=%s", rt.DestructiveAssessment.Severity, rp.DestructiveAssessment.Severity)
			}
		})
	}
}

// S1-S2: Stress
func TestStressConcurrentInfraCloudMatching(t *testing.T) {
	commands := []string{
		"terraform destroy -auto-approve",
		"aws ec2 terminate-instances --instance-ids i-123",
		"gcloud projects delete my-proj",
		"az group delete --name rg1",
		"ansible all -m file -a state=absent",
		"pulumi destroy --yes",
	}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cmd := commands[idx%len(commands)]
			for j := 0; j < 300; j++ {
				_ = guard.Evaluate(cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
			}
		}(i)
	}
	wg.Wait()
}

func TestStressHighVolumeInfraCloudCommands(t *testing.T) {
	base := []string{
		"terraform destroy -auto-approve",
		"pulumi destroy --yes",
		"ansible all -m file -a state=absent",
		"aws ec2 terminate-instances --instance-ids i-123",
		"gcloud projects delete my-proj",
		"az group delete --name rg1",
	}
	commands := make([]string, 0, 6000)
	for i := 0; i < 1000; i++ {
		for _, b := range base {
			commands = append(commands, b)
		}
	}
	var wg sync.WaitGroup
	for w := 0; w < 16; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := worker; i < len(commands); i += 16 {
				_ = guard.Evaluate(commands[i], guard.WithDestructivePolicy(guard.InteractivePolicy()))
			}
		}(w)
	}
	wg.Wait()
}

// SEC1-SEC3: Security
func TestSecuritySubcommandEvasion(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		packID   string
		wantDeny bool
	}{
		{"terraform with var-file flag", "terraform destroy -auto-approve -var-file=prod.tfvars", "infrastructure.terraform", true},
		{"aws with profile flag", "aws --profile prod ec2 terminate-instances --instance-ids i-123", "cloud.aws", true},
		{"gcloud with project flag", "gcloud --project prod projects delete my-proj", "cloud.gcp", true},
		{"aws interposed debug flag", "aws ec2 --debug terminate-instances --instance-ids i-123", "cloud.aws", true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if !HasRegisteredPack(tt.packID) {
				t.Skipf("pack %s not registered", tt.packID)
			}
			res := guard.Evaluate(tt.command, guard.WithDestructivePolicy(guard.InteractivePolicy()))
			gotDeny := res.Decision == guard.Deny || res.Decision == guard.Ask
			if gotDeny != tt.wantDeny {
				t.Fatalf("got deny-like=%v want=%v for %q", gotDeny, tt.wantDeny, tt.command)
			}
		})
	}
}

func TestSecurityAnsibleContentInjection(t *testing.T) {
	if !HasRegisteredPack("infrastructure.ansible") {
		t.Skip("infrastructure.ansible pack not registered")
	}
	tests := []struct {
		cmd      string
		wantDeny bool
	}{
		{"ansible all -m file -a 'path=/tmp/state=absent.txt'", true},
		{"ansible all -m file -a 'state=absent'", true},
		{"ansible all -m file -a 'path=/tmp state=present'", false},
	}
	for _, tt := range tests {
		res := guard.Evaluate(tt.cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
		gotDeny := res.Decision == guard.Deny || res.Decision == guard.Ask
		if gotDeny != tt.wantDeny {
			t.Fatalf("ansible injection case %q got deny-like=%v want=%v", tt.cmd, gotDeny, tt.wantDeny)
		}
	}
}

func TestSecurityEnvSensitivityPreConditions(t *testing.T) {
	samples := []struct {
		packID string
		cmd    string
	}{
		{"infrastructure.terraform", "terraform apply"},
		{"infrastructure.pulumi", "pulumi up"},
		{"infrastructure.ansible", "ansible all -m file -a state=absent"},
		{"cloud.aws", "aws ec2 terminate-instances --instance-ids i-123"},
		{"cloud.gcp", "gcloud projects delete my-proj"},
		{"cloud.azure", "az group delete --name rg1"},
	}
	for _, s := range samples {
		s := s
		t.Run(s.packID, func(t *testing.T) {
			if !HasRegisteredPack(s.packID) {
				t.Skipf("pack %s not registered", s.packID)
			}
			without := guard.Evaluate(s.cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
			with := guard.Evaluate(s.cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()), guard.WithEnv([]string{"ENVIRONMENT=production"}))
			if without.DestructiveAssessment == nil || with.DestructiveAssessment == nil {
				t.Skip("no assessment produced in current registry for this sample")
			}
			if with.DestructiveAssessment.Severity < without.DestructiveAssessment.Severity {
				t.Fatalf("env escalation regressed severity: without=%s with=%s", without.DestructiveAssessment.Severity, with.DestructiveAssessment.Severity)
			}
		})
	}
}

func TestSecurityNoUnexpectedHeapGrowthInInfraCloudBurst(t *testing.T) {
	if testing.Short() {
		t.Skip("skip heap growth check in short mode")
	}
	run := func(n int) uint64 {
		for i := 0; i < n; i++ {
			cmd := "terraform destroy -auto-approve"
			if i%2 == 0 {
				cmd = "aws ec2 terminate-instances --instance-ids i-123"
			}
			_ = guard.Evaluate(cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
		}
		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		return ms.HeapAlloc
	}
	before := run(1000)
	after := run(10000)
	if after > before*3 && after-before > 64*1024*1024 {
		t.Fatalf("heap growth too high: before=%d after=%d", before, after)
	}
}

// B1-B3: Benchmarks
func BenchmarkInfraCloudTerraformPackMatch(b *testing.B) {
	commands := map[string]string{
		"safe-plan":    "terraform plan",
		"destroy":      "terraform destroy",
		"destroy-auto": "terraform destroy -auto-approve",
		"apply":        "terraform apply",
		"state-rm":     "terraform state rm module.vpc",
	}
	benchGuardEvalCommands(b, commands)
}

func BenchmarkInfraCloudPulumiPackMatch(b *testing.B) {
	commands := map[string]string{
		"safe-preview": "pulumi preview",
		"destroy":      "pulumi destroy",
		"destroy-yes":  "pulumi destroy --yes",
		"up":           "pulumi up",
		"up-yes":       "pulumi up --yes",
	}
	benchGuardEvalCommands(b, commands)
}

func BenchmarkInfraCloudAnsiblePackMatch(b *testing.B) {
	commands := map[string]string{
		"safe-ping":         "ansible all -m ping",
		"file-absent":       "ansible all -m file -a state=absent",
		"shell-destructive": "ansible all -m shell -a 'rm -rf /tmp/e2e'",
		"playbook":          "ansible-playbook site.yml",
	}
	benchGuardEvalCommands(b, commands)
}

func BenchmarkInfraCloudAWSPackMatch(b *testing.B) {
	commands := map[string]string{
		"safe-describe": "aws ec2 describe-instances",
		"ec2-terminate": "aws ec2 terminate-instances --instance-ids i-123",
		"s3-rb-force":   "aws s3 rb s3://bucket --force",
		"cfn-delete":    "aws cloudformation delete-stack --stack-name app",
	}
	benchGuardEvalCommands(b, commands)
}

func BenchmarkInfraCloudGCPPackMatch(b *testing.B) {
	commands := map[string]string{
		"safe-list":     "gcloud compute instances list",
		"instances-del": "gcloud compute instances delete vm",
		"projects-del":  "gcloud projects delete my-proj",
		"sql-del":       "gcloud sql instances delete db",
	}
	benchGuardEvalCommands(b, commands)
}

func BenchmarkInfraCloudAzurePackMatch(b *testing.B) {
	commands := map[string]string{
		"safe-list":    "az vm list",
		"group-delete": "az group delete --name rg1",
		"vm-delete":    "az vm delete --name vm1 --resource-group rg1",
		"sql-delete":   "az sql server delete --name db1 --resource-group rg1",
	}
	benchGuardEvalCommands(b, commands)
}

func BenchmarkInfraCloudCloudFormationPackMatch(b *testing.B) {
	commands := map[string]string{
		"delete-stack": "aws cloudformation delete-stack --stack-name app",
		"describe":     "aws cloudformation describe-stacks",
	}
	benchGuardEvalCommands(b, commands)
}

func BenchmarkInfraCloudGoldenCorpusThroughput(b *testing.B) {
	var corpus []string
	for _, pairs := range infraCloudReachability() {
		for _, p := range pairs {
			corpus = append(corpus, p.command)
		}
	}
	if len(corpus) == 0 {
		b.Skip("no infra/cloud reachability corpus in current registry")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, c := range corpus {
			_ = guard.Evaluate(c, guard.WithDestructivePolicy(guard.InteractivePolicy()))
		}
	}
}

func BenchmarkInfraCloudAWSFullPackEvalNoMatchWorstCase(b *testing.B) {
	cmd := "aws dynamodb put-item --table-name tbl --item '{}'"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = guard.Evaluate(cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
	}
}

func benchGuardEvalCommands(b *testing.B, commands map[string]string) {
	for name, cmd := range commands {
		name := name
		cmd := cmd
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = guard.Evaluate(cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
			}
		})
	}
}
