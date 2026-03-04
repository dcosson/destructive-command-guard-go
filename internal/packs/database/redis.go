package database

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

func redisPack() packs.Pack {
	return packs.Pack{
		ID:          "database.redis",
		Name:        "Redis",
		Description: "Redis destructive operations via redis-cli",
		Keywords:    []string{"redis-cli"},
		Safe:        []packs.Rule{{ID: "redis-cli-readonly-safe"}, {ID: "redis-cli-interactive-safe"}},
		Destructive: []packs.Rule{
			{ID: "redis-flushall", Severity: sevHigh, Confidence: confHigh, Reason: "FLUSHALL deletes all keys in all Redis databases", Remediation: "Use redis-cli BGSAVE first to create a backup. Consider FLUSHDB for single-database flush.", EnvSensitive: true, Match: func(cmd packs.Command) bool { return hasAll(cmd, "redis-cli", "flushall") }},
			{ID: "redis-flushdb", Severity: sevHigh, Confidence: confHigh, Reason: "FLUSHDB deletes all keys in the current Redis database", Remediation: "Use redis-cli BGSAVE first. Verify you're connected to the right database.", EnvSensitive: true, Match: func(cmd packs.Command) bool { return hasAll(cmd, "redis-cli", "flushdb") }},
			{ID: "redis-key-delete", Severity: sevMedium, Confidence: confMedium, Reason: "DEL/UNLINK deletes the specified keys from Redis", Remediation: "Verify the key names. Use TTL or OBJECT HELP to inspect keys first.", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "redis-cli") && hasAny(cmd, " del ", " del\t", " unlink ", " unlink\t")
			}},
			{ID: "redis-config-set", Severity: sevMedium, Confidence: confHigh, Reason: "CONFIG SET modifies Redis server configuration at runtime", Remediation: "Verify the configuration parameter and value. Use CONFIG GET to check current value first.", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "redis-cli", "config") && hasAny(cmd, " set ", "resetstat")
			}},
			{ID: "redis-shutdown", Severity: sevMedium, Confidence: confHigh, Reason: "SHUTDOWN stops the Redis server, causing service disruption", Remediation: "Use redis-cli BGSAVE first. Schedule shutdowns during maintenance windows.", Match: func(cmd packs.Command) bool { return hasAll(cmd, "redis-cli", "shutdown") }},
			{ID: "redis-debug", Severity: sevMedium, Confidence: confHigh, Reason: "DEBUG commands can crash the server (SEGFAULT), block it (SLEEP), or modify internal state", Remediation: "DEBUG commands should only be used in development environments for testing purposes.", Match: func(cmd packs.Command) bool { return hasAll(cmd, "redis-cli", "debug") }},
		},
	}
}
