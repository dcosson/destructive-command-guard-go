package frameworks

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

const (
	sevLow      = 1
	sevMedium   = 2
	sevHigh     = 3
	sevCritical = 4

	confLow    = 0
	confMedium = 1
	confHigh   = 2
)

func frameworksPack() packs.Pack {
	return packs.Pack{
		ID:          "frameworks",
		Name:        "Frameworks",
		Description: "Framework ORM/migration destructive operations",
		Keywords:    []string{"rails", "rake", "manage.py", "artisan", "mix"},
		Safe: []packs.Rule{
			{ID: "rails-routes-safe", Match: packs.And(packs.Name("rails"), packs.ArgAt(0, "routes"))},
			{ID: "rails-db-migrate-safe", Match: packs.And(packs.Name("rails"), packs.ArgAt(0, "db:migrate"), packs.Not(packs.Flags("--run-syncdb")))},
			{ID: "managepy-safe", Match: packs.Or(
				packs.And(packs.Name("manage.py"), packs.Or(
					packs.ArgAt(0, "runserver"),
					packs.ArgAt(0, "test"),
					packs.ArgAt(0, "shell"),
					packs.ArgAt(0, "showmigrations"),
					packs.And(packs.ArgAt(0, "migrate"), packs.Not(packs.Flags("--run-syncdb"))),
				)),
				packs.And(packs.Name("python"), packs.ArgAt(0, "manage.py"), packs.Or(
					packs.ArgAt(1, "runserver"),
					packs.ArgAt(1, "test"),
					packs.ArgAt(1, "shell"),
					packs.ArgAt(1, "showmigrations"),
					packs.And(packs.ArgAt(1, "migrate"), packs.Not(packs.Flags("--run-syncdb"))),
				)),
			)},
			{ID: "artisan-safe", Match: packs.Or(
				packs.And(packs.Name("artisan"), packs.ArgAt(0, "list")),
				packs.And(packs.Name("php"), packs.ArgAt(0, "artisan"), packs.ArgAt(1, "list")),
			)},
			{ID: "mix-safe", Match: packs.And(packs.Name("mix"), packs.Or(packs.ArgAt(0, "test"), packs.ArgAt(0, "deps.get"), packs.ArgAt(0, "compile"), packs.ArgAt(0, "phx.routes")))},
		},
		Destructive: []packs.Rule{
			{ID: "rails-db-drop", Severity: sevHigh, Confidence: confHigh, EnvSensitive: true, Reason: "rails db:drop destroys the configured database", Remediation: "Verify target environment and back up data before dropping", Match: packs.And(packs.Name("rails"), packs.ArgAt(0, "db:drop"))},
			{ID: "rails-db-reset", Severity: sevHigh, Confidence: confHigh, EnvSensitive: true, Reason: "rails db:reset drops and recreates the database", Remediation: "Use db:migrate when possible and avoid reset on production data", Match: packs.And(packs.Name("rails"), packs.ArgAt(0, "db:reset"))},
			{ID: "rake-db-drop-all", Severity: sevCritical, Confidence: confHigh, EnvSensitive: true, Reason: "rake db:drop:all destroys all configured databases", Remediation: "Confirm environment and backup strategy before running db:drop:all", Match: packs.And(packs.Name("rake"), packs.ArgAt(0, "db:drop:all"))},
			{ID: "managepy-flush", Severity: sevHigh, Confidence: confHigh, EnvSensitive: true, Reason: "manage.py flush removes all data from database tables", Remediation: "Run against non-production environments or restore from backup after validation", Match: packs.Or(
				packs.And(packs.Name("manage.py"), packs.ArgAt(0, "flush")),
				packs.And(packs.Name("python"), packs.ArgAt(0, "manage.py"), packs.ArgAt(1, "flush")),
			)},
			{ID: "managepy-migrate-syncdb", Severity: sevMedium, Confidence: confMedium, EnvSensitive: true, Reason: "manage.py migrate --run-syncdb can create/reset unmanaged schema state", Remediation: "Prefer standard migrations without --run-syncdb unless explicitly required", Match: packs.Or(
				packs.And(packs.Name("manage.py"), packs.ArgAt(0, "migrate"), packs.Flags("--run-syncdb")),
				packs.And(packs.Name("python"), packs.ArgAt(0, "manage.py"), packs.ArgAt(1, "migrate"), packs.Flags("--run-syncdb")),
			)},
			{ID: "artisan-migrate-fresh", Severity: sevHigh, Confidence: confHigh, EnvSensitive: true, Reason: "artisan migrate:fresh drops all tables and re-runs migrations", Remediation: "Use incremental migrations and avoid migrate:fresh in production", Match: packs.Or(
				packs.And(packs.Name("artisan"), packs.Or(packs.ArgAt(0, "migrate:fresh"), packs.ArgAt(0, "migrate:reset"))),
				packs.And(packs.Name("php"), packs.ArgAt(0, "artisan"), packs.Or(packs.ArgAt(1, "migrate:fresh"), packs.ArgAt(1, "migrate:reset"))),
			)},
			{ID: "mix-ecto-reset", Severity: sevHigh, Confidence: confHigh, EnvSensitive: true, Reason: "mix ecto.reset drops and recreates database state", Remediation: "Use ecto.migrate for additive schema changes", Match: packs.And(packs.Name("mix"), packs.Or(packs.ArgAt(0, "ecto.reset"), packs.ArgAt(0, "ecto.drop")))},
		},
	}
}
