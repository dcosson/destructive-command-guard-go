package e2etest

import (
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

// F1-F3: Fault injection
func TestFaultContainerK8sNilFields(t *testing.T) {
	cmds := []string{
		"docker",
		"docker-compose",
		"kubectl",
		"helm",
		"",
		" ",
		"\t\n",
		"docker \"\"",
		"kubectl \"\"",
	}
	for i, c := range cmds {
		t.Run(fmt.Sprintf("degenerate-%d", i), func(t *testing.T) {
			_ = guard.Evaluate(c, guard.WithDestructivePolicy(guard.InteractivePolicy()))
		})
	}
}

func TestFaultContainerK8sArgAtOutOfBounds(t *testing.T) {
	shortCmds := []string{
		"docker",
		"docker container",
		"docker compose",
		"docker-compose",
		"docker-compose down",
		"kubectl",
		"kubectl delete",
		"helm",
		"helm rollback",
	}
	for i, c := range shortCmds {
		t.Run(fmt.Sprintf("short-%d", i), func(t *testing.T) {
			_ = guard.Evaluate(c, guard.WithDestructivePolicy(guard.InteractivePolicy()))
		})
	}
}

func TestFaultDockerComposeKeywordOverlap(t *testing.T) {
	dockerPack, dockerOK := findPack("containers.docker")
	composePack, composeOK := findPack("containers.compose")
	if !dockerOK || !composeOK {
		t.Skip("containers.docker/containers.compose packs not both registered")
	}

	dockerCmds := []string{
		"docker rm my-container",
		"docker system prune -af",
		"docker volume rm my-vol",
	}
	for i, c := range dockerCmds {
		t.Run(fmt.Sprintf("docker-cmd-%d", i), func(t *testing.T) {
			res := guard.Evaluate(c, guard.WithDestructivePolicy(guard.InteractivePolicy()))
			for _, dp := range composePack.Rules {
				if hasRuleMatch(res, composePack.ID, dp.ID) {
					t.Fatalf("docker command matched compose pattern %s", dp.ID)
				}
			}
		})
	}

	composeCmds := []string{
		"docker compose down -v",
		"docker compose rm -f",
		"docker compose stop",
	}
	for i, c := range composeCmds {
		t.Run(fmt.Sprintf("compose-plugin-%d", i), func(t *testing.T) {
			res := guard.Evaluate(c, guard.WithDestructivePolicy(guard.InteractivePolicy()))
			for _, dp := range dockerPack.Rules {
				if hasRuleMatch(res, dockerPack.ID, dp.ID) {
					t.Fatalf("compose command matched docker pattern %s", dp.ID)
				}
			}
		})
	}
}

// O1-O3: Oracles / consistency
func TestOracleContainerK8sPolicyMonotonicity(t *testing.T) {
	commands := []string{
		"docker system prune -af",
		"docker compose down -v",
		"kubectl delete namespace prod",
		"helm uninstall my-release",
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

func TestOracleContainerK8sOrchestratorConsistency(t *testing.T) {
	type eq struct {
		name       string
		commands   map[string]string
		expectSame bool
	}

	equivs := []eq{
		{
			name: "resource-removal",
			commands: map[string]string{
				"containers.docker":  "docker rm my-container",
				"containers.compose": "docker compose rm -f",
			},
			expectSame: true,
		},
		{
			name: "deployment-removal",
			commands: map[string]string{
				"kubernetes.kubectl": "kubectl delete deployment my-app",
				"kubernetes.helm":    "helm uninstall my-release",
			},
			expectSame: true,
		},
	}

	for _, e := range equivs {
		e := e
		t.Run(e.name, func(t *testing.T) {
			severities := make([]guard.Severity, 0, len(e.commands))
			for packID, cmd := range e.commands {
				if !HasRegisteredPack(packID) {
					continue
				}
				res := guard.Evaluate(cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
				if res.DestructiveAssessment != nil {
					severities = append(severities, res.DestructiveAssessment.Severity)
				}
			}
			if len(severities) < 2 {
				t.Skip("insufficient container/k8s packs registered for consistency check")
			}
			if e.expectSame {
				for i := 1; i < len(severities); i++ {
					if severities[i] != severities[0] {
						t.Fatalf("severity mismatch: %v", severities)
					}
				}
			}
		})
	}
}

func TestOracleContainerK8sCrossPackConsistency(t *testing.T) {
	commands := map[string]string{
		"containers.docker":  "docker system prune -af",
		"containers.compose": "docker compose down -v",
		"kubernetes.kubectl": "kubectl delete namespace prod",
		"kubernetes.helm":    "helm uninstall rel",
	}
	for cmdPack, cmd := range commands {
		cmdPack := cmdPack
		cmd := cmd
		t.Run(cmdPack, func(t *testing.T) {
			if !HasRegisteredPack(cmdPack) {
				t.Skipf("pack %s not registered", cmdPack)
			}
			res := guard.Evaluate(cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
			for _, m := range res.Matches {
				if m.Pack == cmdPack {
					continue
				}
				if m.Pack == "containers.docker" || m.Pack == "containers.compose" || m.Pack == "kubernetes.kubectl" || m.Pack == "kubernetes.helm" {
					t.Fatalf("%s command triggered cross-pack %s/%s", cmdPack, m.Pack, m.Rule)
				}
			}
		})
	}
}

// S1-S2: Stress
func TestStressConcurrentContainerK8sMatching(t *testing.T) {
	commands := []string{
		"docker system prune -af",
		"docker compose down -v",
		"kubectl delete namespace prod",
		"helm uninstall release",
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

func TestStressHighVolumeContainerK8sCommands(t *testing.T) {
	base := []string{
		"docker system prune -af",
		"docker compose down -v",
		"kubectl delete namespace prod",
		"helm uninstall release",
	}
	commands := make([]string, 0, 8000)
	for i := 0; i < 2000; i++ {
		commands = append(commands, base[i%len(base)])
	}

	var wg sync.WaitGroup
	workers := 16
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := worker; i < len(commands); i += workers {
				_ = guard.Evaluate(commands[i], guard.WithDestructivePolicy(guard.InteractivePolicy()))
			}
		}(w)
	}
	wg.Wait()
}

// SEC1-SEC3: Security
func TestSecurityDockerSyntaxEvasion(t *testing.T) {
	tests := []struct {
		name    string
		command string
		packID  string
	}{
		{"docker container rm management syntax", "docker container rm c1", "containers.docker"},
		{"docker image rm management syntax", "docker image rm img1", "containers.docker"},
		{"docker container stop management syntax", "docker container stop c1", "containers.docker"},
		{"docker compose down plugin syntax", "docker compose down -v", "containers.compose"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if !HasRegisteredPack(tt.packID) {
				t.Skipf("pack %s not registered", tt.packID)
			}
			res := guard.Evaluate(tt.command, guard.WithDestructivePolicy(guard.InteractivePolicy()))
			if res.Decision == guard.Allow {
				t.Fatalf("expected deny-like decision for %q, got %s", tt.command, res.Decision)
			}
		})
	}
}

func TestSecurityKubectlDeleteResourceEscalation(t *testing.T) {
	if !HasRegisteredPack("kubernetes.kubectl") {
		t.Skip("kubernetes.kubectl pack not registered")
	}

	pk, ok := findPack("kubernetes.kubectl")
	if !ok {
		t.Skip("kubernetes.kubectl pack missing from registry")
	}

	highImpact := []struct {
		resource    string
		minSeverity guard.Severity
	}{
		{"namespace", guard.Critical},
		{"deployment", guard.High},
		{"statefulset", guard.High},
		{"pvc", guard.High},
		{"node", guard.High},
		{"service", guard.High},
		{"secret", guard.High},
	}
	for _, tt := range highImpact {
		t.Run(tt.resource, func(t *testing.T) {
			cmd := fmt.Sprintf("kubectl delete %s test", tt.resource)
			res := guard.Evaluate(cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
			matched := false
			for _, m := range res.Matches {
				if m.Pack != pk.ID {
					continue
				}
				r, ok := findRule(pk, m.Rule)
				if !ok {
					continue
				}
				if guard.Severity(r.Severity) < tt.minSeverity {
					t.Fatalf("kubectl delete %s matched %s with severity %d < %s", tt.resource, m.Rule, r.Severity, tt.minSeverity)
				}
				matched = true
				break
			}
			if !matched {
				t.Fatalf("kubectl delete %s should match at least one destructive rule", tt.resource)
			}
		})
	}

	generic := []string{"pod", "configmap", "ingress", "job", "cronjob"}
	for _, resource := range generic {
		t.Run("generic-"+resource, func(t *testing.T) {
			cmd := fmt.Sprintf("kubectl delete %s test", resource)
			res := guard.Evaluate(cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
			matched := false
			for _, m := range res.Matches {
				if m.Pack != pk.ID {
					continue
				}
				r, ok := findRule(pk, m.Rule)
				if !ok {
					continue
				}
				if guard.Severity(r.Severity) == guard.Medium {
					matched = true
					break
				}
			}
			if !matched {
				t.Fatalf("kubectl delete %s should match a medium-severity destructive rule", resource)
			}
		})
	}
}

func TestSecurityContainerK8sEnvSensitivityPreConditions(t *testing.T) {
	for _, tt := range []struct {
		packID       string
		pattern      string
		baseSeverity guard.Severity
	}{
		{"kubernetes.kubectl", "kubectl-delete-namespace", guard.Critical},
		{"kubernetes.kubectl", "kubectl-delete-workload", guard.High},
		{"kubernetes.kubectl", "kubectl-delete-resource", guard.Medium},
		{"kubernetes.kubectl", "kubectl-drain", guard.High},
		{"kubernetes.helm", "helm-uninstall", guard.High},
		{"kubernetes.helm", "helm-rollback", guard.Medium},
	} {
		tt := tt
		t.Run(tt.packID+"/"+tt.pattern, func(t *testing.T) {
			pk, ok := findPack(tt.packID)
			if !ok {
				t.Skipf("pack %s not registered", tt.packID)
			}
			r, ok := findRule(pk, tt.pattern)
			if !ok {
				t.Skipf("pattern %s/%s not registered", tt.packID, tt.pattern)
			}
			if !r.EnvSensitive {
				t.Fatalf("%s/%s should be env-sensitive", tt.packID, tt.pattern)
			}
			if guard.Severity(r.Severity) != tt.baseSeverity {
				t.Fatalf("severity mismatch for %s/%s: got=%d want=%s", tt.packID, tt.pattern, r.Severity, tt.baseSeverity)
			}
		})
	}
}

func TestSecurityNoUnexpectedHeapGrowthInContainerK8sBurst(t *testing.T) {
	if testing.Short() {
		t.Skip("skip heap growth check in short mode")
	}

	run := func(n int) uint64 {
		for i := 0; i < n; i++ {
			cmd := "docker system prune -af"
			switch i % 4 {
			case 1:
				cmd = "docker compose down -v"
			case 2:
				cmd = "kubectl delete namespace prod"
			case 3:
				cmd = "helm uninstall release"
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
func BenchmarkContainerK8sDockerPackMatch(b *testing.B) {
	commands := map[string]string{
		"safe-ps":          "docker ps",
		"system-prune-all": "docker system prune -af",
		"volume-rm":        "docker volume rm my-vol",
		"rm":               "docker rm c1",
		"container-rm":     "docker container rm c1",
	}
	benchGuardEvalCommands(b, commands)
}

func BenchmarkContainerK8sComposePackMatch(b *testing.B) {
	commands := map[string]string{
		"safe-ps":        "docker compose ps",
		"down-volumes":   "docker compose down -v",
		"down-rmi":       "docker compose down --rmi all",
		"rm-force":       "docker compose rm -f",
		"stop":           "docker compose stop",
		"standalone-rmf": "docker-compose rm -f",
	}
	benchGuardEvalCommands(b, commands)
}

func BenchmarkContainerK8sKubectlPackMatch(b *testing.B) {
	commands := map[string]string{
		"safe-get":        "kubectl get pods",
		"delete-ns":       "kubectl delete namespace prod",
		"delete-workload": "kubectl delete deployment app",
		"delete-generic":  "kubectl delete pod p1",
		"drain":           "kubectl drain node1",
	}
	benchGuardEvalCommands(b, commands)
}

func BenchmarkContainerK8sHelmPackMatch(b *testing.B) {
	commands := map[string]string{
		"safe-list":    "helm list",
		"uninstall":    "helm uninstall my-release",
		"delete-alias": "helm delete my-release",
		"rollback":     "helm rollback my-release 1",
		"upgrade":      "helm upgrade my-release chart",
	}
	benchGuardEvalCommands(b, commands)
}

func BenchmarkContainerK8sGoldenCorpusThroughput(b *testing.B) {
	var corpus []string
	for _, pairs := range containerK8sReachability() {
		for _, p := range pairs {
			corpus = append(corpus, p.command)
		}
	}
	if len(corpus) == 0 {
		b.Skip("no container/k8s reachability corpus in current registry")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, c := range corpus {
			_ = guard.Evaluate(c, guard.WithDestructivePolicy(guard.InteractivePolicy()))
		}
	}
}

func BenchmarkContainerK8sDockerFullPackEvalNoMatchWorstCase(b *testing.B) {
	cmd := "docker scout cves my-image"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = guard.Evaluate(cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
	}
}
