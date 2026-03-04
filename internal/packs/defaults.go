package packs

// DefaultRegistry contains built-in command packs.
var DefaultRegistry = NewRegistry(
	frameworksPack(),
)

func frameworksPack() Pack {
	return Pack{
		ID:          "frameworks",
		Name:        "Frameworks",
		Description: "Potentially destructive framework/database actions",
		Keywords:    []string{"rails", "db:drop", "db:reset"},
		Safe: []Rule{
			{
				ID:    "rails-routes",
				Match: And(Name("rails"), ArgAt(0, "routes")),
			},
		},
		Destructive: []Rule{
			{
				ID:           "rails-db-reset",
				Severity:     3, // High
				Confidence:   2, // High
				Reason:       "rails db:reset drops and recreates the database",
				Remediation:  "Use non-destructive migrations where possible",
				EnvSensitive: true,
				Match: And(
					Name("rails"),
					Or(Arg("db:reset"), Arg("db:drop"), Arg("db:truncate")),
				),
			},
		},
	}
}
