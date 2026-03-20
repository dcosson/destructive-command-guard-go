package personal

import (
	"path"
	"strings"

	"github.com/dcosson/destructive-command-guard-go/internal/evalcore"
	"github.com/dcosson/destructive-command-guard-go/internal/packs"
)

func sshPack() packs.Pack {
	isSSHDirArg := func(arg string) bool {
		a := strings.TrimSpace(arg)
		if a == "" {
			return false
		}
		if strings.HasPrefix(a, "~/.ssh") || strings.HasPrefix(a, "$HOME/.ssh") || strings.HasPrefix(a, "${HOME}/.ssh") {
			return a == "~/.ssh" || a == "~/.ssh/" || a == "$HOME/.ssh" || a == "$HOME/.ssh/" || a == "${HOME}/.ssh" || a == "${HOME}/.ssh/"
		}
		if strings.Contains(a, "/.ssh") {
			return strings.HasSuffix(a, "/.ssh") || strings.HasSuffix(a, "/.ssh/")
		}
		return false
	}

	isSSHPublicKey := func(arg string) bool {
		a := strings.TrimSpace(arg)
		if !strings.Contains(a, "/.ssh/") && !strings.HasPrefix(a, "~/.ssh/") && !strings.HasPrefix(a, "$HOME/.ssh/") && !strings.HasPrefix(a, "${HOME}/.ssh/") {
			return false
		}
		return strings.HasSuffix(a, ".pub")
	}

	isSSHConfigFile := func(arg string) bool {
		a := strings.TrimSpace(arg)
		if !strings.Contains(a, "/.ssh/") && !strings.HasPrefix(a, "~/.ssh/") && !strings.HasPrefix(a, "$HOME/.ssh/") && !strings.HasPrefix(a, "${HOME}/.ssh/") {
			return false
		}
		base := path.Base(a)
		switch base {
		case "config", "known_hosts", "known_hosts.old", "environment", "rc", "agent.sock":
			return true
		default:
			return false
		}
	}

	isSSHPrivateKey := func(arg string) bool {
		a := strings.TrimSpace(arg)
		if !strings.Contains(a, "/.ssh/") && !strings.HasPrefix(a, "~/.ssh/") && !strings.HasPrefix(a, "$HOME/.ssh/") && !strings.HasPrefix(a, "${HOME}/.ssh/") {
			return false
		}
		base := path.Base(a)
		if base == "" || base == "." || strings.HasSuffix(base, ".pub") {
			return false
		}
		return true
	}

	anyArg := func(pred func(string) bool) packs.MatchFunc {
		return packs.MatchFunc(func(cmd packs.Command) bool {
			for _, a := range cmd.Args {
				if pred(a) {
					return true
				}
			}
			for _, a := range cmd.RawArgs {
				if pred(a) {
					return true
				}
			}
			return false
		})
	}

	return packs.Pack{
		ID:          "personal.ssh",
		Name:        "SSH Keys",
		Description: "Protects SSH private keys from unauthorized access",
		Keywords:    []string{".ssh", "id_rsa", "id_ed25519", "id_ecdsa", "id_dsa"},
		Safe: []packs.Rule{
			{ID: "ssh-public-key-access", Match: anyArg(isSSHPublicKey)},
			{ID: "ssh-config-read", Match: packs.And(
				packs.Or(
					packs.Name("cat"), packs.Name("less"), packs.Name("more"), packs.Name("head"),
					packs.Name("tail"), packs.Name("grep"), packs.Name("wc"), packs.Name("file"), packs.Name("stat"),
				),
				anyArg(isSSHConfigFile),
			)},
		},
		Rules: []packs.Rule{
			{ID: "ssh-directory-destructive", Severity: sevCritical, Confidence: confHigh, Reason: "Destructive operation targets the SSH directory", Remediation: "Leave ~/.ssh unchanged and edit only explicit non-key files", Match: packs.And(
				packs.Or(packs.Name("rm"), packs.Name("chmod"), packs.Name("mv")),
				anyArg(isSSHDirArg),
			)},
			{ID: "ssh-private-key-access", Category: evalcore.CategoryPrivacy, Severity: sevHigh, Confidence: confHigh, Reason: "Command accesses a private SSH key", Remediation: "Use public key material instead of private key files", Match: packs.And(
				packs.AnyName(),
				anyArg(isSSHPrivateKey),
				packs.Not(anyArg(isSSHPublicKey)),
			)},
		},
	}
}
