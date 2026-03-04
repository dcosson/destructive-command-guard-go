package database

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

func postgresqlPack() packs.Pack {
	return packs.Pack{
		ID:          "database.postgresql",
		Name:        "PostgreSQL",
		Description: "PostgreSQL database destructive operations via psql, dropdb, and related tools",
		Keywords:    []string{"psql", "pg_dump", "pg_restore", "dropdb", "createdb"},
		Safe: []packs.Rule{
			{ID: "psql-select-safe"},
			{ID: "pg-dump-safe"},
			{ID: "createdb-safe"},
			{ID: "psql-interactive-safe"},
			{ID: "pg-restore-safe"},
		},
		Destructive: []packs.Rule{
			{ID: "psql-drop-database", Severity: sevHigh, Confidence: confHigh, Reason: "DROP DATABASE permanently removes the database and all contained data", Remediation: "Drop specific schemas or tables instead of dropping the database", EnvSensitive: true, Match: func(cmd packs.Command) bool { return hasAll(cmd, "psql") && reDropDatabase.MatchString(cmd.RawText) }},
			{ID: "dropdb", Severity: sevHigh, Confidence: confHigh, Reason: "dropdb permanently removes the database and all contained data", Remediation: "Drop specific schemas or tables instead of dropping the database", EnvSensitive: true, Match: func(cmd packs.Command) bool { return hasAll(cmd, "dropdb") }},
			{ID: "psql-drop-table", Severity: sevHigh, Confidence: confHigh, Reason: "DROP TABLE permanently removes the table and all rows", Remediation: "Delete only required rows with DELETE ... WHERE", EnvSensitive: true, Match: func(cmd packs.Command) bool { return hasAll(cmd, "psql") && reDropTable.MatchString(cmd.RawText) }},
			{ID: "psql-truncate", Severity: sevHigh, Confidence: confHigh, Reason: "TRUNCATE removes all rows in the table immediately", Remediation: "Delete only required rows with DELETE ... WHERE", EnvSensitive: true, Match: func(cmd packs.Command) bool { return hasAll(cmd, "psql") && reTruncate.MatchString(cmd.RawText) }},
			{ID: "psql-delete-no-where", Severity: sevMedium, Confidence: confMedium, Reason: "DELETE without WHERE removes every row in the target table", Remediation: "Add a WHERE clause to scope row deletion", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "psql") && reDeleteFrom.MatchString(cmd.RawText) && !reWhere.MatchString(cmd.RawText)
			}},
			{ID: "pg-dump-clean", Severity: sevMedium, Confidence: confHigh, Reason: "pg_dump --clean emits DROP statements that remove existing objects on restore", Remediation: "Run pg_dump without --clean", Match: func(cmd packs.Command) bool { return hasAll(cmd, "pg_dump") && hasAny(cmd, "--clean", " -c ") }},
			{ID: "pg-restore-clean", Severity: sevMedium, Confidence: confHigh, Reason: "pg_restore --clean drops existing objects before recreation", Remediation: "Run pg_restore without --clean", EnvSensitive: true, Match: func(cmd packs.Command) bool { return hasAll(cmd, "pg_restore") && hasAny(cmd, "--clean", " -c ") }},
			{ID: "psql-alter-drop", Severity: sevMedium, Confidence: confMedium, Reason: "ALTER TABLE ... DROP permanently removes columns, constraints, or indexes", Remediation: "Use additive ALTER operations instead of DROP operations", Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "psql") && reAlterTable.MatchString(cmd.RawText) && reDrop.MatchString(cmd.RawText)
			}},
			{ID: "psql-update-no-where", Severity: sevMedium, Confidence: confMedium, Reason: "UPDATE without WHERE modifies every row in the target table", Remediation: "Add a WHERE clause to scope row updates", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "psql") && reUpdate.MatchString(cmd.RawText) && !reWhere.MatchString(cmd.RawText)
			}},
			{ID: "psql-drop-schema", Severity: sevHigh, Confidence: confHigh, Reason: "DROP SCHEMA removes all objects in the schema, and CASCADE expands removal to dependencies", Remediation: "Drop specific objects instead of dropping the schema", EnvSensitive: true, Match: func(cmd packs.Command) bool { return hasAll(cmd, "psql") && reDropSchema.MatchString(cmd.RawText) }},
		},
	}
}
