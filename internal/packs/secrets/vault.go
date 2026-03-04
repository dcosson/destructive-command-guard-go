package secrets

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

const (
	sevLow      = 1
	sevMedium   = 2
	sevHigh     = 3
	sevCritical = 4

	confLow    = 0
	confMedium = 1
	confHigh   = 2
)

func vaultPack() packs.Pack {
	return packs.Pack{
		ID:          "secrets.vault",
		Name:        "Vault",
		Description: "HashiCorp Vault destructive operations",
		Keywords:    []string{"vault"},
		Safe: []packs.Rule{
			{ID: "vault-status-safe", Match: packs.And(packs.Name("vault"), packs.ArgAt(0, "status"))},
			{ID: "vault-auth-safe", Match: packs.And(packs.Name("vault"), packs.ArgAt(0, "auth"), packs.Or(packs.ArgAt(1, "list"), packs.ArgAt(1, "enable")))},
			{ID: "vault-token-safe", Match: packs.And(packs.Name("vault"), packs.ArgAt(0, "token"), packs.Or(packs.ArgAt(1, "lookup"), packs.ArgAt(1, "create")))},
			{ID: "vault-policy-safe", Match: packs.And(packs.Name("vault"), packs.ArgAt(0, "policy"), packs.Or(packs.ArgAt(1, "read"), packs.ArgAt(1, "list")))},
			{ID: "vault-audit-safe", Match: packs.And(packs.Name("vault"), packs.ArgAt(0, "audit"), packs.ArgAt(1, "list"))},
		},
		Destructive: []packs.Rule{
			{ID: "vault-secrets-disable", Severity: sevCritical, Confidence: confHigh, EnvSensitive: true, Reason: "vault secrets disable removes a mounted secrets engine and all underlying secret data", Remediation: "Export and backup secret data before disabling engines", Match: packs.And(packs.Name("vault"), packs.ArgAt(0, "secrets"), packs.ArgAt(1, "disable"))},
			{ID: "vault-auth-disable", Severity: sevHigh, Confidence: confHigh, EnvSensitive: true, Reason: "vault auth disable removes an authentication method", Remediation: "Verify replacement auth method and access recovery plan before disabling", Match: packs.And(packs.Name("vault"), packs.ArgAt(0, "auth"), packs.ArgAt(1, "disable"))},
			{ID: "vault-token-revoke", Severity: sevHigh, Confidence: confHigh, EnvSensitive: true, Reason: "vault token revoke invalidates access tokens and can immediately disrupt automation", Remediation: "Scope revocation to intended entities and verify service token dependencies", Match: packs.And(packs.Name("vault"), packs.ArgAt(0, "token"), packs.ArgAt(1, "revoke"))},
			{ID: "vault-policy-delete", Severity: sevHigh, Confidence: confHigh, EnvSensitive: true, Reason: "vault policy delete removes access policy definitions", Remediation: "Back up policy definitions and audit role-policy mappings before deletion", Match: packs.And(packs.Name("vault"), packs.ArgAt(0, "policy"), packs.ArgAt(1, "delete"))},
			{ID: "vault-audit-disable", Severity: sevHigh, Confidence: confHigh, EnvSensitive: true, Reason: "vault audit disable turns off audit logging sink", Remediation: "Ensure alternate audit sink exists before disabling", Match: packs.And(packs.Name("vault"), packs.ArgAt(0, "audit"), packs.ArgAt(1, "disable"))},
		},
	}
}
