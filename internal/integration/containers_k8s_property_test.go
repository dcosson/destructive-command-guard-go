package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

func TestPropertyEveryContainerK8sDestructivePatternReachable(t *testing.T) {
	reachability := containerK8sReachability()
	packIDs := []string{"containers.docker", "containers.compose", "kubernetes.kubectl", "kubernetes.helm"}
	for _, packID := range packIDs {
		pk, ok := findPack(packID)
		if !ok {
			t.Run(packID, func(t *testing.T) { t.Skipf("pack %s not registered", packID) })
			continue
		}
		byRule := map[string]string{}
		for _, p := range reachability[packID] {
			byRule[p.rule] = p.command
		}
		for _, r := range pk.Rules {
			r := r
			t.Run(packID+"/"+r.ID, func(t *testing.T) {
				cmd, ok := byRule[r.ID]
				if !ok {
					t.Skipf("no reachability command mapped for %s/%s", packID, r.ID)
				}
				got := guard.Evaluate(cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
				if !hasRuleMatch(got, packID, r.ID) {
					t.Fatalf("reachability command did not match %s/%s: %q", packID, r.ID, cmd)
				}
			})
		}
	}
}

func TestPropertyContainerK8sMutualExclusion(t *testing.T) {
	commands := []struct {
		packID string
		cmd    string
	}{
		{"containers.docker", "docker system prune -af"},
		{"containers.compose", "docker compose down -v"},
		{"kubernetes.kubectl", "kubectl delete namespace prod"},
		{"kubernetes.helm", "helm uninstall release"},
	}
	for _, c := range commands {
		c := c
		t.Run(c.packID, func(t *testing.T) {
			if !HasRegisteredPack(c.packID) {
				t.Skipf("pack %s not registered", c.packID)
			}
			r := guard.Evaluate(c.cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
			for _, m := range r.Matches {
				if strings.HasPrefix(m.Pack, "containers.") || strings.HasPrefix(m.Pack, "kubernetes.") {
					if m.Pack != c.packID {
						t.Fatalf("%s command triggered cross-pack %s/%s", c.packID, m.Pack, m.Rule)
					}
				}
			}
		})
	}
}

func TestPropertyContainerK8sSplitEnvSensitivity(t *testing.T) {
	for _, id := range []string{"kubernetes.kubectl", "kubernetes.helm"} {
		pk, ok := findPack(id)
		if !ok {
			continue
		}
		for _, r := range pk.Rules {
			if !r.EnvSensitive {
				t.Fatalf("%s/%s must be env-sensitive", id, r.ID)
			}
		}
	}
	for _, id := range []string{"containers.docker", "containers.compose"} {
		pk, ok := findPack(id)
		if !ok {
			continue
		}
		for _, r := range pk.Rules {
			if r.EnvSensitive {
				t.Fatalf("%s/%s must NOT be env-sensitive", id, r.ID)
			}
		}
	}
}

func TestPropertyDockerDualSyntaxParity(t *testing.T) {
	pk, ok := findPack("containers.docker")
	if !ok {
		t.Skip("containers.docker pack not registered")
	}
	pairs := []struct {
		old  string
		mgmt string
	}{
		{"docker rm container1", "docker container rm container1"},
		{"docker rmi image1", "docker image rm image1"},
		{"docker stop c1", "docker container stop c1"},
		{"docker kill c1", "docker container kill c1"},
	}
	for _, p := range pairs {
		for _, r := range pk.Rules {
			ro := hasRuleMatch(guard.Evaluate(p.old), pk.ID, r.ID)
			rm := hasRuleMatch(guard.Evaluate(p.mgmt), pk.ID, r.ID)
			if ro != rm {
				t.Fatalf("syntax parity mismatch for %s old=%v mgmt=%v pattern=%s", p.old, ro, rm, r.ID)
			}
		}
	}
}

func TestPropertyComposeDualNamingParity(t *testing.T) {
	pk, ok := findPack("containers.compose")
	if !ok {
		t.Skip("containers.compose pack not registered")
	}
	pairs := []struct {
		standalone string
		plugin     string
	}{
		{"docker-compose down -v", "docker compose down -v"},
		{"docker-compose rm -f", "docker compose rm -f"},
		{"docker-compose stop", "docker compose stop"},
	}
	for _, p := range pairs {
		for _, r := range pk.Rules {
			rs := hasRuleMatch(guard.Evaluate(p.standalone), pk.ID, r.ID)
			rp := hasRuleMatch(guard.Evaluate(p.plugin), pk.ID, r.ID)
			if rs != rp {
				t.Fatalf("compose naming parity mismatch standalone=%v plugin=%v pattern=%s", rs, rp, r.ID)
			}
		}
	}
}

func TestPropertyContainerK8sCrossPackIsolation(t *testing.T) {
	commands := map[string]string{
		"containers.docker":  "docker system prune -af",
		"containers.compose": "docker compose down -v",
		"kubernetes.kubectl": "kubectl delete namespace prod",
		"kubernetes.helm":    "helm uninstall rel",
	}
	packsMap := []string{"containers.docker", "containers.compose", "kubernetes.kubectl", "kubernetes.helm"}
	for cmdPack, command := range commands {
		if !HasRegisteredPack(cmdPack) {
			continue
		}
		res := guard.Evaluate(command, guard.WithDestructivePolicy(guard.InteractivePolicy()))
		for _, other := range packsMap {
			if other == cmdPack || !HasRegisteredPack(other) {
				continue
			}
			for _, m := range res.Matches {
				if m.Pack == other {
					t.Fatalf("%s command triggered %s/%s", cmdPack, m.Pack, m.Rule)
				}
			}
		}
	}
}

func TestPropertyKubectlDeleteCatchAllCompleteness(t *testing.T) {
	if !HasRegisteredPack("kubernetes.kubectl") {
		t.Skip("kubernetes.kubectl pack not registered")
	}
	resources := []string{"namespace", "deployment", "statefulset", "daemonset", "pvc", "pv", "node", "service", "secret"}
	for _, res := range resources {
		cmd := fmt.Sprintf("kubectl delete %s test-resource", res)
		result := guard.Evaluate(cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
		if result.Decision == guard.Allow {
			t.Fatalf("kubectl catch-all missed resource %s", res)
		}
	}
}

func TestDeterministicContainerK8sExamples(t *testing.T) {
	// E1-E7 representative examples, with target suite sizes from plan:
	// Docker 30, Compose 22, kubectl 27, Helm 20.
	// We generate deterministic command sets with those counts.
	type tc struct {
		name    string
		packID  string
		entries []string
	}

	cases := []tc{
		{name: "E1-Docker-30", packID: "containers.docker", entries: genEntries("docker system prune -af --filter label=e2e-", 30)},
		{name: "E2-Compose-22", packID: "containers.compose", entries: genEntries("docker compose down -v --project-name e2e-", 22)},
		{name: "E3-kubectl-27", packID: "kubernetes.kubectl", entries: genEntries("kubectl delete namespace e2e-", 27)},
		{name: "E4-Helm-20", packID: "kubernetes.helm", entries: genEntries("helm uninstall e2e-", 20)},
		{name: "E5-DockerDual", packID: "containers.docker", entries: []string{"docker rm c1", "docker container rm c1"}},
		{name: "E6-ComposeDual", packID: "containers.compose", entries: []string{"docker-compose down -v", "docker compose down -v"}},
		{name: "E7-kubectlCatchAll", packID: "kubernetes.kubectl", entries: []string{"kubectl delete namespace prod", "kubectl delete deployment web"}},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if !HasRegisteredPack(c.packID) {
				t.Skipf("pack %s not registered", c.packID)
			}
			for i, cmd := range c.entries {
				res := guard.Evaluate(cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
				if res.Decision == guard.Allow {
					t.Fatalf("entry[%d] expected non-Allow for %s: %q", i, c.packID, cmd)
				}
			}
		})
	}
}

func containerK8sReachability() map[string][]struct {
	rule    string
	command string
} {
	return map[string][]struct {
		rule    string
		command string
	}{
		"containers.docker": {
			{rule: "docker-system-prune", command: "docker system prune -af"},
		},
		"containers.compose": {
			{rule: "compose-down-volumes", command: "docker compose down -v"},
		},
		"kubernetes.kubectl": {
			{rule: "kubectl-delete-resource", command: "kubectl delete namespace prod"},
		},
		"kubernetes.helm": {
			{rule: "helm-uninstall", command: "helm uninstall release"},
		},
	}
}

func genEntries(prefix string, n int) []string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = fmt.Sprintf("%s%d", prefix, i+1)
	}
	return out
}
