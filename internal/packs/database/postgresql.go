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
			{ID: "psql-drop-database", Severity: sevHigh, Confidence: confHigh, Reason: "DROP DATABASE permanently destroys an entire database and all its data", Remediation: "Use pg_dump to create a backup first. Verify the database name.", EnvSensitive: true, Match: func(cmd packs.Command) bool { return hasAll(cmd, "psql") && reDropDatabase.MatchString(cmd.RawText) }},
			{ID: "dropdb", Severity: sevHigh, Confidence: confHigh, Reason: "dropdb permanently destroys an entire PostgreSQL database", Remediation: "Use pg_dump to create a backup first. Verify the database name.", EnvSensitive: true, Match: func(cmd packs.Command) bool { return hasAll(cmd, "dropdb") }},
			{ID: "psql-drop-table", Severity: sevHigh, Confidence: confHigh, Reason: "DROP TABLE permanently destroys a table and all its data", Remediation: "Use pg_dump -t to backup the table first. Consider DROP TABLE IF EXISTS.", EnvSensitive: true, Match: func(cmd packs.Command) bool { return hasAll(cmd, "psql") && reDropTable.MatchString(cmd.RawText) }},
			{ID: "psql-truncate", Severity: sevHigh, Confidence: confHigh, Reason: "TRUNCATE removes all rows from a table instantly without logging individual row deletions", Remediation: "Create a backup first. Consider DELETE with WHERE for selective removal.", EnvSensitive: true, Match: func(cmd packs.Command) bool { return hasAll(cmd, "psql") && reTruncate.MatchString(cmd.RawText) }},
			{ID: "psql-delete-no-where", Severity: sevMedium, Confidence: confMedium, Reason: "DELETE FROM without WHERE clause deletes all rows in the table", Remediation: "Add a WHERE clause to target specific rows, or use TRUNCATE if you intend to remove all rows.", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "psql") && reDeleteFrom.MatchString(cmd.RawText) && !reWhere.MatchString(cmd.RawText)
			}},
			{ID: "pg-dump-clean", Severity: sevMedium, Confidence: confHigh, Reason: "pg_dump --clean generates DROP commands before CREATE — restoring this dump will destroy existing objects", Remediation: "Use pg_dump without --clean for a non-destructive backup.", Match: func(cmd packs.Command) bool { return hasAll(cmd, "pg_dump") && hasAny(cmd, "--clean", " -c ") }},
			{ID: "pg-restore-clean", Severity: sevMedium, Confidence: confHigh, Reason: "pg_restore --clean drops existing database objects before recreating them", Remediation: "Use pg_restore without --clean to restore without dropping existing data.", EnvSensitive: true, Match: func(cmd packs.Command) bool { return hasAll(cmd, "pg_restore") && hasAny(cmd, "--clean", " -c ") }},
			{ID: "psql-alter-drop", Severity: sevMedium, Confidence: confMedium, Reason: "ALTER TABLE ... DROP permanently removes columns, constraints, or indexes", Remediation: "Create a backup first. Verify the column/constraint name.", Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "psql") && reAlterTable.MatchString(cmd.RawText) && reDrop.MatchString(cmd.RawText)
			}},
			{ID: "psql-update-no-where", Severity: sevMedium, Confidence: confMedium, Reason: "UPDATE without WHERE clause modifies all rows in the table", Remediation: "Add a WHERE clause to target specific rows.", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "psql") && reUpdate.MatchString(cmd.RawText) && !reWhere.MatchString(cmd.RawText)
			}},
			{ID: "psql-drop-schema", Severity: sevHigh, Confidence: confHigh, Reason: "DROP SCHEMA destroys all objects in the schema. With CASCADE, this can destroy an entire application's database objects.", Remediation: "Use pg_dump to backup the schema first. Verify the schema name.", EnvSensitive: true, Match: func(cmd packs.Command) bool { return hasAll(cmd, "psql") && reDropSchema.MatchString(cmd.RawText) }},
		},
	}
}
