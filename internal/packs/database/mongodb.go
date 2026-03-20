package database

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

func mongodbPack() packs.Pack {
	return packs.Pack{
		ID:          "database.mongodb",
		Name:        "MongoDB",
		Description: "MongoDB destructive operations via mongosh, mongo, mongos, mongodump, mongorestore",
		Keywords:    []string{"mongo", "mongosh", "mongos", "mongodump", "mongorestore"},
		Safe:        []packs.Rule{{ID: "mongodump-safe"}, {ID: "mongosh-readonly-safe"}, {ID: "mongosh-interactive-safe"}},
		Rules: []packs.Rule{
			{ID: "mongo-drop-database", Severity: sevHigh, Confidence: confHigh, Reason: "db.dropDatabase() permanently removes the database and all collections", Remediation: "Drop specific collections instead of dropping the database", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAny(cmd, "mongosh", "mongo", "mongos") && !hasAny(cmd, "mongodump", "mongorestore") && reMongoDropDB.MatchString(cmd.RawText)
			}},
			{ID: "mongo-collection-drop", Severity: sevHigh, Confidence: confHigh, Reason: "collection.drop() permanently removes the collection and all documents", Remediation: "Delete only required documents with deleteMany(filter)", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAny(cmd, "mongosh", "mongo", "mongos") && !hasAny(cmd, "mongodump", "mongorestore") && reMongoCollDrop.MatchString(cmd.RawText) && !reMongoDropDB.MatchString(cmd.RawText)
			}},
			{ID: "mongo-delete-many-all", Severity: sevMedium, Confidence: confHigh, Reason: "deleteMany({}) with an empty filter removes every document in the collection", Remediation: "Use deleteMany with a restrictive filter", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAny(cmd, "mongosh", "mongo", "mongos") && !hasAny(cmd, "mongodump", "mongorestore") && reMongoDelManyAll.MatchString(cmd.RawText)
			}},
			{ID: "mongo-remove-all", Severity: sevMedium, Confidence: confHigh, Reason: "remove({}) with an empty filter removes every document in the collection", Remediation: "Use remove with a restrictive filter", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAny(cmd, "mongosh", "mongo", "mongos") && !hasAny(cmd, "mongodump", "mongorestore") && reMongoRemoveAll.MatchString(cmd.RawText)
			}},
			{ID: "mongorestore-drop", Severity: sevMedium, Confidence: confHigh, Reason: "mongorestore --drop deletes existing collections before restore", Remediation: "Run mongorestore without --drop", EnvSensitive: true, Match: func(cmd packs.Command) bool { return hasAll(cmd, "mongorestore", "--drop") }},
			{ID: "mongo-delete-many", Severity: sevMedium, Confidence: confMedium, Reason: "deleteMany() removes multiple documents that match the filter", Remediation: "Use deleteOne for single-document removal", EnvSensitive: true, Match: func(cmd packs.Command) bool {
				return hasAny(cmd, "mongosh", "mongo", "mongos") && !hasAny(cmd, "mongodump", "mongorestore") && reMongoDeleteMany.MatchString(cmd.RawText) && !reMongoDelManyAll.MatchString(cmd.RawText)
			}},
		},
	}
}
