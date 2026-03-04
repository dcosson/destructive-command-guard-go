package database

import (
	"regexp"
	"strings"

	"github.com/dcosson/destructive-command-guard-go/internal/packs"
)

const (
	sevLow      = 1
	sevMedium   = 2
	sevHigh     = 3
	sevCritical = 4

	confLow    = 0
	confMedium = 1
	confHigh   = 2
)

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

func hasAll(cmd packs.Command, terms ...string) bool {
	s := strings.ToLower(cmd.RawText)
	for _, term := range terms {
		if !strings.Contains(s, strings.ToLower(term)) {
			return false
		}
	}
	return true
}

func hasAny(cmd packs.Command, terms ...string) bool {
	s := strings.ToLower(cmd.RawText)
	for _, term := range terms {
		if strings.Contains(s, strings.ToLower(term)) {
			return true
		}
	}
	return false
}
