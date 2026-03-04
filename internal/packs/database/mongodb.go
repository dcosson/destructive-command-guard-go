package database

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

func mongodbPack() packs.Pack {
	return packs.Pack{
		ID:          "database.mongodb",
		Name:        "MongoDB",
		Description: "MongoDB destructive operations via mongosh, mongo, mongos, mongodump, mongorestore",
		Keywords:    []string{"mongo", "mongosh", "mongos", "mongodump", "mongorestore"},
		Safe:        []packs.Rule{{ID: "mongodump-safe"}, {ID: "mongosh-readonly-safe"}, {ID: "mongosh-interactive-safe"}},
		Destructive: []packs.Rule{
			{ID: "mongo-drop-database", Severity: sevHigh, Confidence: confHigh, Reason: "db.dropDatabase() permanently destroys an entire MongoDB database", Remediation: "Use mongodump to create a backup first. Verify the database name.", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAny(cmd, "mongosh", "mongo", "mongos") && !hasAny(cmd, "mongodump", "mongorestore") && reMongoDropDB.MatchString(cmd.RawText)
			}},
			{ID: "mongo-collection-drop", Severity: sevHigh, Confidence: confHigh, Reason: "collection.drop() permanently destroys a MongoDB collection and all its documents", Remediation: "Use mongodump --collection to backup the collection first.", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAny(cmd, "mongosh", "mongo", "mongos") && !hasAny(cmd, "mongodump", "mongorestore") && reMongoCollDrop.MatchString(cmd.RawText) && !reMongoDropDB.MatchString(cmd.RawText)
			}},
			{ID: "mongo-delete-many-all", Severity: sevMedium, Confidence: confHigh, Reason: "deleteMany({}) with empty filter deletes all documents in the collection", Remediation: "Add a filter to target specific documents: deleteMany({field: value})", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAny(cmd, "mongosh", "mongo", "mongos") && !hasAny(cmd, "mongodump", "mongorestore") && reMongoDelManyAll.MatchString(cmd.RawText)
			}},
			{ID: "mongo-remove-all", Severity: sevMedium, Confidence: confHigh, Reason: "remove({}) with empty filter deletes all documents in the collection", Remediation: "Add a query filter: remove({field: value})", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAny(cmd, "mongosh", "mongo", "mongos") && !hasAny(cmd, "mongodump", "mongorestore") && reMongoRemoveAll.MatchString(cmd.RawText)
			}},
			{ID: "mongorestore-drop", Severity: sevMedium, Confidence: confHigh, Reason: "mongorestore --drop drops existing collections before restoring, losing any data not in the backup", Remediation: "Use mongorestore without --drop to merge instead of replace.", EnvSensitive: true, Match: func(cmd packs.Command) bool { return hasAll(cmd, "mongorestore", "--drop") }},
			{ID: "mongo-delete-many", Severity: sevMedium, Confidence: confMedium, Reason: "deleteMany() deletes multiple documents matching the filter", Remediation: "Verify the filter matches only intended documents. Use countDocuments() first to check.", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAny(cmd, "mongosh", "mongo", "mongos") && !hasAny(cmd, "mongodump", "mongorestore") && reMongoDeleteMany.MatchString(cmd.RawText) && !reMongoDelManyAll.MatchString(cmd.RawText)
			}},
		},
	}
}
