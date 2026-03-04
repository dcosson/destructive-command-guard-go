package database

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

func mysqlPack() packs.Pack {
	return packs.Pack{
		ID:          "database.mysql",
		Name:        "MySQL",
		Description: "MySQL/MariaDB database destructive operations via mysql, mysqldump, mysqladmin",
		Keywords:    []string{"mysql", "mysqldump", "mysqladmin"},
		Safe: []packs.Rule{
			{ID: "mysql-select-safe"},
			{ID: "mysqldump-safe"},
			{ID: "mysql-interactive-safe"},
			{ID: "mysqladmin-readonly-safe"},
		},
		Destructive: []packs.Rule{
			{ID: "mysql-drop-database", Severity: sevHigh, Confidence: confHigh, Reason: "DROP DATABASE permanently destroys an entire MySQL database and all its tables", Remediation: "Use mysqldump to create a backup first. Verify the database name.", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "mysql") && !hasAny(cmd, "mysqldump", "mysqladmin") && reDropDatabase.MatchString(cmd.RawText)
			}},
			{ID: "mysqladmin-drop", Severity: sevHigh, Confidence: confHigh, Reason: "mysqladmin drop permanently destroys an entire MySQL database", Remediation: "Use mysqldump to create a backup first. Verify the database name.", EnvSensitive: true, Match: func(cmd packs.Command) bool { return hasAll(cmd, "mysqladmin", "drop") }},
			{ID: "mysql-drop-table", Severity: sevHigh, Confidence: confHigh, Reason: "DROP TABLE permanently destroys a table and all its data", Remediation: "Use mysqldump to backup the table first.", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "mysql") && !hasAny(cmd, "mysqldump", "mysqladmin") && reDropTable.MatchString(cmd.RawText)
			}},
			{ID: "mysql-truncate", Severity: sevHigh, Confidence: confHigh, Reason: "TRUNCATE removes all rows from a table instantly", Remediation: "Create a backup first. Consider DELETE with WHERE for selective removal.", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "mysql") && !hasAny(cmd, "mysqldump", "mysqladmin") && reTruncate.MatchString(cmd.RawText)
			}},
			{ID: "mysql-delete-no-where", Severity: sevMedium, Confidence: confMedium, Reason: "DELETE FROM without WHERE clause deletes all rows in the table", Remediation: "Add a WHERE clause to target specific rows.", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "mysql") && !hasAny(cmd, "mysqldump", "mysqladmin") && reDeleteFrom.MatchString(cmd.RawText) && !reWhere.MatchString(cmd.RawText)
			}},
			{ID: "mysql-alter-drop", Severity: sevMedium, Confidence: confMedium, Reason: "ALTER TABLE ... DROP permanently removes columns, constraints, or indexes", Remediation: "Create a backup first. Verify the column/constraint name.", Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "mysql") && !hasAny(cmd, "mysqldump", "mysqladmin") && reAlterTable.MatchString(cmd.RawText) && reDrop.MatchString(cmd.RawText)
			}},
			{ID: "mysqladmin-flush", Severity: sevMedium, Confidence: confHigh, Reason: "mysqladmin flush operations can disrupt active connections and require careful timing", Remediation: "Schedule flush operations during maintenance windows.", Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "mysqladmin") && hasAny(cmd, "flush-hosts", "flush-logs", "flush-privileges", "flush-tables")
			}},
			{ID: "mysql-update-no-where", Severity: sevMedium, Confidence: confMedium, Reason: "UPDATE without WHERE clause modifies all rows in the table", Remediation: "Add a WHERE clause to target specific rows.", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "mysql") && !hasAny(cmd, "mysqldump", "mysqladmin") && reUpdate.MatchString(cmd.RawText) && !reWhere.MatchString(cmd.RawText)
			}},
		},
	}
}
