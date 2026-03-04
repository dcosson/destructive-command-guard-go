package database

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

func init() {
	packs.DefaultRegistry.Register(
		postgresqlPack(),
		mysqlPack(),
		sqlitePack(),
		mongodbPack(),
		redisPack(),
	)
}
