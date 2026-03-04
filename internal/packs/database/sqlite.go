package database

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

func sqlitePack() packs.Pack {
	return packs.Pack{
		ID:          "database.sqlite",
		Name:        "SQLite",
		Description: "SQLite database destructive operations via sqlite3 CLI",
		Keywords:    []string{"sqlite3"},
		Safe:        []packs.Rule{{ID: "sqlite3-readonly-safe"}, {ID: "sqlite3-non-destructive-safe"}},
		Destructive: []packs.Rule{
			{ID: "sqlite3-drop-table", Severity: sevHigh, Confidence: confHigh, Reason: "DROP TABLE permanently destroys a table and all its data in the SQLite database", Remediation: "Copy the database file as a backup first. Use .dump to export data.", Match: func(cmd packs.Command) bool { return hasAll(cmd, "sqlite3") && reDropTable.MatchString(cmd.RawText) }},
			{ID: "sqlite3-dot-drop", Severity: sevHigh, Confidence: confMedium, Reason: "sqlite3 .drop command drops triggers or views", Remediation: "Verify the target. Use .dump to backup first.", Match: func(cmd packs.Command) bool { return hasAll(cmd, "sqlite3") && reMongoDotDrop.MatchString(cmd.RawText) }},
			{ID: "sqlite3-delete-no-where", Severity: sevMedium, Confidence: confMedium, Reason: "DELETE FROM without WHERE clause deletes all rows in the table", Remediation: "Add a WHERE clause to target specific rows.", Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "sqlite3") && reDeleteFrom.MatchString(cmd.RawText) && !reWhere.MatchString(cmd.RawText)
			}},
			{ID: "sqlite3-truncate", Severity: sevMedium, Confidence: confLow, Reason: "TRUNCATE is not valid SQLite SQL but indicates intent to delete all data", Remediation: "SQLite uses DELETE FROM (without WHERE) instead of TRUNCATE.", Match: func(cmd packs.Command) bool { return hasAll(cmd, "sqlite3") && reTruncate.MatchString(cmd.RawText) }},
			{ID: "sqlite3-update-no-where", Severity: sevMedium, Confidence: confMedium, Reason: "UPDATE without WHERE clause modifies all rows in the table", Remediation: "Add a WHERE clause to target specific rows.", Match: func(cmd packs.Command) bool {
				return hasAll(cmd, "sqlite3") && reUpdate.MatchString(cmd.RawText) && !reWhere.MatchString(cmd.RawText)
			}},
		},
	}
}
