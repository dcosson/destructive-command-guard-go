package database

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

func sqlitePack() packs.Pack {
	return packs.Pack{
		ID:          "database.sqlite",
		Name:        "SQLite",
		Description: "SQLite database destructive operations via sqlite3 CLI",
		Keywords:    []string{"sqlite3"},
		Safe:        []packs.Rule{{ID: "sqlite3-readonly-safe"}, {ID: "sqlite3-non-destructive-safe"}},
		Rules: []packs.Rule{
			{ID: "sqlite3-drop-table", Severity: sevHigh, Confidence: confHigh, Reason: "DROP TABLE permanently removes the table and all rows", Remediation: "Delete only required rows with DELETE ... WHERE", Match: func(cmd packs.Command) bool { return hasAll(cmd, "sqlite3") && reDropTable.MatchString(cmd.RawText) }},
			{ID: "sqlite3-dot-drop", Severity: sevHigh, Confidence: confMedium, Reason: "sqlite3 .drop removes schema objects such as triggers or views", Remediation: "Use additive schema changes instead of dropping objects", Match: func(cmd packs.Command) bool { return hasAll(cmd, "sqlite3") && reMongoDotDrop.MatchString(cmd.RawText) }},
			{ID: "sqlite3-delete-no-where", Severity: sevMedium, Confidence: confMedium, Reason: "DELETE without WHERE removes every row in the target table", Remediation: "Add a WHERE clause to scope row deletion", Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "sqlite3") && reDeleteFrom.MatchString(cmd.RawText) && !reWhere.MatchString(cmd.RawText)
			}},
			{ID: "sqlite3-truncate", Severity: sevMedium, Confidence: confLow, Reason: "TRUNCATE intent indicates full-table data removal", Remediation: "Add a WHERE clause to scope row deletion", Match: func(cmd packs.Command) bool { return hasAll(cmd, "sqlite3") && reTruncate.MatchString(cmd.RawText) }},
			{ID: "sqlite3-update-no-where", Severity: sevMedium, Confidence: confMedium, Reason: "UPDATE without WHERE modifies every row in the target table", Remediation: "Add a WHERE clause to scope row updates", Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "sqlite3") && reUpdate.MatchString(cmd.RawText) && !reWhere.MatchString(cmd.RawText)
			}},
		},
	}
}
