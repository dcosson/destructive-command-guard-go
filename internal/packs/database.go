package packs

import "regexp"

// Pre-compiled SQL pattern regexes (case-insensitive).
var (
	reDropDatabase = regexp.MustCompile(`(?i)\bDROP\s+DATABASE\b`)
	reDropTable    = regexp.MustCompile(`(?i)\bDROP\s+TABLE\b`)
	reTruncate     = regexp.MustCompile(`(?i)\bTRUNCATE\b`)
	reDeleteFrom   = regexp.MustCompile(`(?i)\bDELETE\s+FROM\b`)
	reWhere        = regexp.MustCompile(`(?i)\bWHERE\b`)
	reUpdate       = regexp.MustCompile(`(?i)\bUPDATE\b`)
	reAlterTable   = regexp.MustCompile(`(?i)\bALTER\s+TABLE\b`)
	reDrop         = regexp.MustCompile(`(?i)\bDROP\b`)
	reDropSchema   = regexp.MustCompile(`(?i)\bDROP\s+SCHEMA\b`)

	// MongoDB patterns
	reMongoDropDB     = regexp.MustCompile(`(?i)dropDatabase\s*\(`)
	reMongoCollDrop   = regexp.MustCompile(`(?i)\.drop\s*\(`)
	reMongoDeleteMany = regexp.MustCompile(`(?i)deleteMany\s*\(`)
	reMongoDelManyAll = regexp.MustCompile(`(?i)deleteMany\s*\(\s*(?:\{\s*\})?\s*\)`)
	reMongoRemoveAll  = regexp.MustCompile(`(?i)\.remove\s*\(\s*\{\s*\}\s*\)`)
	reMongoDotDrop    = regexp.MustCompile(`(?i)\.drop`)
)

func postgresqlPack() Pack {
	return Pack{
		ID:          "database.postgresql",
		Name:        "PostgreSQL",
		Description: "PostgreSQL database destructive operations via psql, dropdb, and related tools",
		Keywords:    []string{"psql", "pg_dump", "pg_restore", "dropdb", "createdb"},
		Safe: []Rule{
			{ID: "psql-select-safe"},
			{ID: "pg-dump-safe"},
			{ID: "createdb-safe"},
			{ID: "psql-interactive-safe"},
			{ID: "pg-restore-safe"},
		},
		Destructive: []Rule{
			{
				ID:         "psql-drop-database",
				Severity:   3, // High
				Confidence: 2, // High
				Reason:     "DROP DATABASE permanently destroys an entire database and all its data",
				Remediation: "Use pg_dump to create a backup first. Verify the database name.",
				EnvSensitive: true,
				Match: func(cmd string) bool {
					return hasAll(cmd, "psql") && reDropDatabase.MatchString(cmd)
				},
			},
			{
				ID:           "dropdb",
				Severity:     3,
				Confidence:   2,
				Reason:       "dropdb permanently destroys an entire PostgreSQL database",
				Remediation:  "Use pg_dump to create a backup first. Verify the database name.",
				EnvSensitive: true,
				Match: func(cmd string) bool {
					return hasAll(cmd, "dropdb")
				},
			},
			{
				ID:           "psql-drop-table",
				Severity:     3,
				Confidence:   2,
				Reason:       "DROP TABLE permanently destroys a table and all its data",
				Remediation:  "Use pg_dump -t to backup the table first. Consider DROP TABLE IF EXISTS.",
				EnvSensitive: true,
				Match: func(cmd string) bool {
					return hasAll(cmd, "psql") && reDropTable.MatchString(cmd)
				},
			},
			{
				ID:           "psql-truncate",
				Severity:     3,
				Confidence:   2,
				Reason:       "TRUNCATE removes all rows from a table instantly without logging individual row deletions",
				Remediation:  "Create a backup first. Consider DELETE with WHERE for selective removal.",
				EnvSensitive: true,
				Match: func(cmd string) bool {
					return hasAll(cmd, "psql") && reTruncate.MatchString(cmd)
				},
			},
			{
				ID:           "psql-delete-no-where",
				Severity:     2, // Medium
				Confidence:   1, // Medium
				Reason:       "DELETE FROM without WHERE clause deletes all rows in the table",
				Remediation:  "Add a WHERE clause to target specific rows, or use TRUNCATE if you intend to remove all rows.",
				EnvSensitive: true,
				Match: func(cmd string) bool {
					return hasAll(cmd, "psql") && reDeleteFrom.MatchString(cmd) && !reWhere.MatchString(cmd)
				},
			},
			{
				ID:          "pg-dump-clean",
				Severity:    2,
				Confidence:  2,
				Reason:      "pg_dump --clean generates DROP commands before CREATE — restoring this dump will destroy existing objects",
				Remediation: "Use pg_dump without --clean for a non-destructive backup.",
				Match: func(cmd string) bool {
					return hasAll(cmd, "pg_dump") && hasAny(cmd, "--clean", " -c ")
				},
			},
			{
				ID:           "pg-restore-clean",
				Severity:     2,
				Confidence:   2,
				Reason:       "pg_restore --clean drops existing database objects before recreating them",
				Remediation:  "Use pg_restore without --clean to restore without dropping existing data.",
				EnvSensitive: true,
				Match: func(cmd string) bool {
					return hasAll(cmd, "pg_restore") && hasAny(cmd, "--clean", " -c ")
				},
			},
			{
				ID:          "psql-alter-drop",
				Severity:    2,
				Confidence:  1,
				Reason:      "ALTER TABLE ... DROP permanently removes columns, constraints, or indexes",
				Remediation: "Create a backup first. Verify the column/constraint name.",
				Match: func(cmd string) bool {
					return hasAll(cmd, "psql") && reAlterTable.MatchString(cmd) && reDrop.MatchString(cmd)
				},
			},
			{
				ID:           "psql-update-no-where",
				Severity:     2,
				Confidence:   1,
				Reason:       "UPDATE without WHERE clause modifies all rows in the table",
				Remediation:  "Add a WHERE clause to target specific rows.",
				EnvSensitive: true,
				Match: func(cmd string) bool {
					return hasAll(cmd, "psql") && reUpdate.MatchString(cmd) && !reWhere.MatchString(cmd)
				},
			},
			{
				ID:           "psql-drop-schema",
				Severity:     3,
				Confidence:   2,
				Reason:       "DROP SCHEMA destroys all objects in the schema. With CASCADE, this can destroy an entire application's database objects.",
				Remediation:  "Use pg_dump to backup the schema first. Verify the schema name.",
				EnvSensitive: true,
				Match: func(cmd string) bool {
					return hasAll(cmd, "psql") && reDropSchema.MatchString(cmd)
				},
			},
		},
	}
}

func mysqlPack() Pack {
	return Pack{
		ID:          "database.mysql",
		Name:        "MySQL",
		Description: "MySQL/MariaDB database destructive operations via mysql, mysqldump, mysqladmin",
		Keywords:    []string{"mysql", "mysqldump", "mysqladmin"},
		Safe: []Rule{
			{ID: "mysql-select-safe"},
			{ID: "mysqldump-safe"},
			{ID: "mysql-interactive-safe"},
			{ID: "mysqladmin-readonly-safe"},
		},
		Destructive: []Rule{
			{
				ID:           "mysql-drop-database",
				Severity:     3,
				Confidence:   2,
				Reason:       "DROP DATABASE permanently destroys an entire MySQL database and all its tables",
				Remediation:  "Use mysqldump to create a backup first. Verify the database name.",
				EnvSensitive: true,
				Match: func(cmd string) bool {
					return hasAll(cmd, "mysql") && !hasAny(cmd, "mysqldump", "mysqladmin") &&
						reDropDatabase.MatchString(cmd)
				},
			},
			{
				ID:           "mysqladmin-drop",
				Severity:     3,
				Confidence:   2,
				Reason:       "mysqladmin drop permanently destroys an entire MySQL database",
				Remediation:  "Use mysqldump to create a backup first. Verify the database name.",
				EnvSensitive: true,
				Match: func(cmd string) bool {
					return hasAll(cmd, "mysqladmin", "drop")
				},
			},
			{
				ID:           "mysql-drop-table",
				Severity:     3,
				Confidence:   2,
				Reason:       "DROP TABLE permanently destroys a table and all its data",
				Remediation:  "Use mysqldump to backup the table first.",
				EnvSensitive: true,
				Match: func(cmd string) bool {
					return hasAll(cmd, "mysql") && !hasAny(cmd, "mysqldump", "mysqladmin") &&
						reDropTable.MatchString(cmd)
				},
			},
			{
				ID:           "mysql-truncate",
				Severity:     3,
				Confidence:   2,
				Reason:       "TRUNCATE removes all rows from a table instantly",
				Remediation:  "Create a backup first. Consider DELETE with WHERE for selective removal.",
				EnvSensitive: true,
				Match: func(cmd string) bool {
					return hasAll(cmd, "mysql") && !hasAny(cmd, "mysqldump", "mysqladmin") &&
						reTruncate.MatchString(cmd)
				},
			},
			{
				ID:           "mysql-delete-no-where",
				Severity:     2,
				Confidence:   1,
				Reason:       "DELETE FROM without WHERE clause deletes all rows in the table",
				Remediation:  "Add a WHERE clause to target specific rows.",
				EnvSensitive: true,
				Match: func(cmd string) bool {
					return hasAll(cmd, "mysql") && !hasAny(cmd, "mysqldump", "mysqladmin") &&
						reDeleteFrom.MatchString(cmd) && !reWhere.MatchString(cmd)
				},
			},
			{
				ID:          "mysql-alter-drop",
				Severity:    2,
				Confidence:  1,
				Reason:      "ALTER TABLE ... DROP permanently removes columns, constraints, or indexes",
				Remediation: "Create a backup first. Verify the column/constraint name.",
				Match: func(cmd string) bool {
					return hasAll(cmd, "mysql") && !hasAny(cmd, "mysqldump", "mysqladmin") &&
						reAlterTable.MatchString(cmd) && reDrop.MatchString(cmd)
				},
			},
			{
				ID:          "mysqladmin-flush",
				Severity:    2,
				Confidence:  2,
				Reason:      "mysqladmin flush operations can disrupt active connections and require careful timing",
				Remediation: "Schedule flush operations during maintenance windows.",
				Match: func(cmd string) bool {
					return hasAll(cmd, "mysqladmin") &&
						hasAny(cmd, "flush-hosts", "flush-logs", "flush-privileges", "flush-tables")
				},
			},
			{
				ID:           "mysql-update-no-where",
				Severity:     2,
				Confidence:   1,
				Reason:       "UPDATE without WHERE clause modifies all rows in the table",
				Remediation:  "Add a WHERE clause to target specific rows.",
				EnvSensitive: true,
				Match: func(cmd string) bool {
					return hasAll(cmd, "mysql") && !hasAny(cmd, "mysqldump", "mysqladmin") &&
						reUpdate.MatchString(cmd) && !reWhere.MatchString(cmd)
				},
			},
		},
	}
}

func sqlitePack() Pack {
	return Pack{
		ID:          "database.sqlite",
		Name:        "SQLite",
		Description: "SQLite database destructive operations via sqlite3 CLI",
		Keywords:    []string{"sqlite3"},
		Safe: []Rule{
			{ID: "sqlite3-readonly-safe"},
			{ID: "sqlite3-non-destructive-safe"},
		},
		Destructive: []Rule{
			{
				ID:          "sqlite3-drop-table",
				Severity:    3,
				Confidence:  2,
				Reason:      "DROP TABLE permanently destroys a table and all its data in the SQLite database",
				Remediation: "Copy the database file as a backup first. Use .dump to export data.",
				Match: func(cmd string) bool {
					return hasAll(cmd, "sqlite3") && reDropTable.MatchString(cmd)
				},
			},
			{
				ID:          "sqlite3-dot-drop",
				Severity:    3,
				Confidence:  1,
				Reason:      "sqlite3 .drop command drops triggers or views",
				Remediation: "Verify the target. Use .dump to backup first.",
				Match: func(cmd string) bool {
					return hasAll(cmd, "sqlite3") && reMongoDotDrop.MatchString(cmd)
				},
			},
			{
				ID:          "sqlite3-delete-no-where",
				Severity:    2,
				Confidence:  1,
				Reason:      "DELETE FROM without WHERE clause deletes all rows in the table",
				Remediation: "Add a WHERE clause to target specific rows.",
				Match: func(cmd string) bool {
					return hasAll(cmd, "sqlite3") && reDeleteFrom.MatchString(cmd) && !reWhere.MatchString(cmd)
				},
			},
			{
				ID:          "sqlite3-truncate",
				Severity:    2,
				Confidence:  0, // Low — TRUNCATE is not valid SQLite SQL
				Reason:      "TRUNCATE is not valid SQLite SQL but indicates intent to delete all data",
				Remediation: "SQLite uses DELETE FROM (without WHERE) instead of TRUNCATE.",
				Match: func(cmd string) bool {
					return hasAll(cmd, "sqlite3") && reTruncate.MatchString(cmd)
				},
			},
			{
				ID:          "sqlite3-update-no-where",
				Severity:    2,
				Confidence:  1,
				Reason:      "UPDATE without WHERE clause modifies all rows in the table",
				Remediation: "Add a WHERE clause to target specific rows.",
				Match: func(cmd string) bool {
					return hasAll(cmd, "sqlite3") && reUpdate.MatchString(cmd) && !reWhere.MatchString(cmd)
				},
			},
		},
	}
}

func mongodbPack() Pack {
	return Pack{
		ID:          "database.mongodb",
		Name:        "MongoDB",
		Description: "MongoDB destructive operations via mongosh, mongo, mongos, mongodump, mongorestore",
		Keywords:    []string{"mongo", "mongosh", "mongos", "mongodump", "mongorestore"},
		Safe: []Rule{
			{ID: "mongodump-safe"},
			{ID: "mongosh-readonly-safe"},
			{ID: "mongosh-interactive-safe"},
		},
		Destructive: []Rule{
			{
				ID:           "mongo-drop-database",
				Severity:     3,
				Confidence:   2,
				Reason:       "db.dropDatabase() permanently destroys an entire MongoDB database",
				Remediation:  "Use mongodump to create a backup first. Verify the database name.",
				EnvSensitive: true,
				Match: func(cmd string) bool {
					return hasAny(cmd, "mongosh", "mongo") &&
						!hasAny(cmd, "mongodump", "mongorestore") &&
						reMongoDropDB.MatchString(cmd)
				},
			},
			{
				ID:           "mongo-collection-drop",
				Severity:     3,
				Confidence:   2,
				Reason:       "collection.drop() permanently destroys a MongoDB collection and all its documents",
				Remediation:  "Use mongodump --collection to backup the collection first.",
				EnvSensitive: true,
				Match: func(cmd string) bool {
					return hasAny(cmd, "mongosh", "mongo") &&
						!hasAny(cmd, "mongodump", "mongorestore") &&
						reMongoCollDrop.MatchString(cmd) &&
						!reMongoDropDB.MatchString(cmd)
				},
			},
			{
				ID:           "mongo-delete-many-all",
				Severity:     2,
				Confidence:   2,
				Reason:       "deleteMany({}) with empty filter deletes all documents in the collection",
				Remediation:  "Add a filter to target specific documents: deleteMany({field: value})",
				EnvSensitive: true,
				Match: func(cmd string) bool {
					return hasAny(cmd, "mongosh", "mongo") &&
						!hasAny(cmd, "mongodump", "mongorestore") &&
						reMongoDelManyAll.MatchString(cmd)
				},
			},
			{
				ID:           "mongo-remove-all",
				Severity:     2,
				Confidence:   2,
				Reason:       "remove({}) with empty filter deletes all documents in the collection",
				Remediation:  "Add a query filter: remove({field: value})",
				EnvSensitive: true,
				Match: func(cmd string) bool {
					return hasAny(cmd, "mongosh", "mongo") &&
						!hasAny(cmd, "mongodump", "mongorestore") &&
						reMongoRemoveAll.MatchString(cmd)
				},
			},
			{
				ID:           "mongorestore-drop",
				Severity:     2,
				Confidence:   2,
				Reason:       "mongorestore --drop drops existing collections before restoring, losing any data not in the backup",
				Remediation:  "Use mongorestore without --drop to merge instead of replace.",
				EnvSensitive: true,
				Match: func(cmd string) bool {
					return hasAll(cmd, "mongorestore", "--drop")
				},
			},
			{
				ID:           "mongo-delete-many",
				Severity:     2,
				Confidence:   1,
				Reason:       "deleteMany() deletes multiple documents matching the filter",
				Remediation:  "Verify the filter matches only intended documents. Use countDocuments() first to check.",
				EnvSensitive: true,
				Match: func(cmd string) bool {
					return hasAny(cmd, "mongosh", "mongo") &&
						!hasAny(cmd, "mongodump", "mongorestore") &&
						reMongoDeleteMany.MatchString(cmd) &&
						!reMongoDelManyAll.MatchString(cmd)
				},
			},
		},
	}
}

func redisPack() Pack {
	return Pack{
		ID:          "database.redis",
		Name:        "Redis",
		Description: "Redis destructive operations via redis-cli",
		Keywords:    []string{"redis-cli"},
		Safe: []Rule{
			{ID: "redis-cli-readonly-safe"},
			{ID: "redis-cli-interactive-safe"},
		},
		Destructive: []Rule{
			{
				ID:           "redis-flushall",
				Severity:     3,
				Confidence:   2,
				Reason:       "FLUSHALL deletes all keys in all Redis databases",
				Remediation:  "Use redis-cli BGSAVE first to create a backup. Consider FLUSHDB for single-database flush.",
				EnvSensitive: true,
				Match: func(cmd string) bool {
					return hasAll(cmd, "redis-cli", "flushall")
				},
			},
			{
				ID:           "redis-flushdb",
				Severity:     3,
				Confidence:   2,
				Reason:       "FLUSHDB deletes all keys in the current Redis database",
				Remediation:  "Use redis-cli BGSAVE first. Verify you're connected to the right database.",
				EnvSensitive: true,
				Match: func(cmd string) bool {
					return hasAll(cmd, "redis-cli", "flushdb")
				},
			},
			{
				ID:           "redis-key-delete",
				Severity:     2,
				Confidence:   1,
				Reason:       "DEL/UNLINK deletes the specified keys from Redis",
				Remediation:  "Verify the key names. Use TTL or OBJECT HELP to inspect keys first.",
				EnvSensitive: true,
				Match: func(cmd string) bool {
					return hasAll(cmd, "redis-cli") && hasAny(cmd, " del ", " del\t", " unlink ", " unlink\t")
				},
			},
			{
				ID:           "redis-config-set",
				Severity:     2,
				Confidence:   2,
				Reason:       "CONFIG SET modifies Redis server configuration at runtime",
				Remediation:  "Verify the configuration parameter and value. Use CONFIG GET to check current value first.",
				EnvSensitive: true,
				Match: func(cmd string) bool {
					return hasAll(cmd, "redis-cli", "config") && hasAny(cmd, " set ", "resetstat")
				},
			},
			{
				ID:          "redis-shutdown",
				Severity:    2,
				Confidence:  2,
				Reason:      "SHUTDOWN stops the Redis server, causing service disruption",
				Remediation: "Use redis-cli BGSAVE first. Schedule shutdowns during maintenance windows.",
				Match: func(cmd string) bool {
					return hasAll(cmd, "redis-cli", "shutdown")
				},
			},
			{
				ID:          "redis-debug",
				Severity:    2,
				Confidence:  2,
				Reason:      "DEBUG commands can crash the server (SEGFAULT), block it (SLEEP), or modify internal state",
				Remediation: "DEBUG commands should only be used in development environments for testing purposes.",
				Match: func(cmd string) bool {
					return hasAll(cmd, "redis-cli", "debug")
				},
			},
		},
	}
}
