package database

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

func redisPack() packs.Pack {
	return packs.Pack{
		ID:          "database.redis",
		Name:        "Redis",
		Description: "Redis destructive operations via redis-cli",
		Keywords:    []string{"redis-cli"},
		Safe:        []packs.Rule{{ID: "redis-cli-readonly-safe"}, {ID: "redis-cli-interactive-safe"}},
		Rules: []packs.Rule{
			{ID: "redis-flushall", Severity: sevHigh, Confidence: confHigh, Reason: "FLUSHALL removes every key from all Redis databases", Remediation: "Use FLUSHDB to limit deletion to one database", EnvSensitive: true, Match: func(cmd packs.Command) bool { return hasAll(cmd, "redis-cli", "flushall") }},
			{ID: "redis-flushdb", Severity: sevHigh, Confidence: confHigh, Reason: "FLUSHDB removes every key from the current Redis database", Remediation: "Delete only required keys with DEL or UNLINK", EnvSensitive: true, Match: func(cmd packs.Command) bool { return hasAll(cmd, "redis-cli", "flushdb") }},
			{ID: "redis-key-delete", Severity: sevMedium, Confidence: confMedium, Reason: "DEL and UNLINK remove the specified keys", Remediation: "Delete a smaller key set per command", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "redis-cli") && hasAny(cmd, " del ", " del\t", " unlink ", " unlink\t")
			}},
			{ID: "redis-config-set", Severity: sevMedium, Confidence: confHigh, Reason: "CONFIG SET mutates Redis runtime configuration", Remediation: "Use CONFIG GET for read-only inspection", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "redis-cli", "config") && hasAny(cmd, " set ", "resetstat")
			}},
			{ID: "redis-shutdown", Severity: sevMedium, Confidence: confHigh, Reason: "SHUTDOWN stops the Redis server and interrupts traffic", Remediation: "Leave the server running and use read-only commands", Match: func(cmd packs.Command) bool { return hasAll(cmd, "redis-cli", "shutdown") }},
			{ID: "redis-debug", Severity: sevMedium, Confidence: confHigh, Reason: "DEBUG commands can crash, block, or mutate Redis internals", Remediation: "Use INFO and MONITOR for diagnostics", Match: func(cmd packs.Command) bool { return hasAll(cmd, "redis-cli", "debug") }},
		},
	}
}
