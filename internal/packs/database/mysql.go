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
			{ID: "mysql-drop-database", Severity: sevHigh, Confidence: confHigh, Reason: "DROP DATABASE permanently removes the database and all tables", Remediation: "Drop specific tables instead of dropping the database", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "mysql") && !hasAny(cmd, "mysqldump", "mysqladmin") && reDropDatabase.MatchString(cmd.RawText)
			}},
			{ID: "mysqladmin-drop", Severity: sevHigh, Confidence: confHigh, Reason: "mysqladmin drop permanently removes the database and all tables", Remediation: "Drop specific tables instead of dropping the database", EnvSensitive: true, Match: func(cmd packs.Command) bool { return hasAll(cmd, "mysqladmin", "drop") }},
			{ID: "mysql-drop-table", Severity: sevHigh, Confidence: confHigh, Reason: "DROP TABLE permanently removes the table and all rows", Remediation: "Delete only required rows with DELETE ... WHERE", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "mysql") && !hasAny(cmd, "mysqldump", "mysqladmin") && reDropTable.MatchString(cmd.RawText)
			}},
			{ID: "mysql-truncate", Severity: sevHigh, Confidence: confHigh, Reason: "TRUNCATE removes all rows in the table immediately", Remediation: "Delete only required rows with DELETE ... WHERE", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "mysql") && !hasAny(cmd, "mysqldump", "mysqladmin") && reTruncate.MatchString(cmd.RawText)
			}},
			{ID: "mysql-delete-no-where", Severity: sevMedium, Confidence: confMedium, Reason: "DELETE without WHERE removes every row in the target table", Remediation: "Add a WHERE clause to scope row deletion", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "mysql") && !hasAny(cmd, "mysqldump", "mysqladmin") && reDeleteFrom.MatchString(cmd.RawText) && !reWhere.MatchString(cmd.RawText)
			}},
			{ID: "mysql-alter-drop", Severity: sevMedium, Confidence: confMedium, Reason: "ALTER TABLE ... DROP permanently removes columns, constraints, or indexes", Remediation: "Use additive ALTER operations instead of DROP operations", Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "mysql") && !hasAny(cmd, "mysqldump", "mysqladmin") && reAlterTable.MatchString(cmd.RawText) && reDrop.MatchString(cmd.RawText)
			}},
			{ID: "mysqladmin-flush", Severity: sevMedium, Confidence: confHigh, Reason: "mysqladmin flush can drop caches and reset connection state", Remediation: "Use read-only inspection commands instead of flush operations", Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "mysqladmin") && hasAny(cmd, "flush-hosts", "flush-logs", "flush-privileges", "flush-tables")
			}},
			{ID: "mysql-update-no-where", Severity: sevMedium, Confidence: confMedium, Reason: "UPDATE without WHERE modifies every row in the target table", Remediation: "Add a WHERE clause to scope row updates", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "mysql") && !hasAny(cmd, "mysqldump", "mysqladmin") && reUpdate.MatchString(cmd.RawText) && !reWhere.MatchString(cmd.RawText)
			}},
		},
	}
}
