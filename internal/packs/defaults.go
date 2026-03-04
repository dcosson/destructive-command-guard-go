package packs

// DefaultRegistry contains built-in command packs.
var DefaultRegistry = NewRegistry(
	coreGitPack(),
	coreFilesystemPack(),
	frameworksPack(),
	postgresqlPack(),
	mysqlPack(),
	sqlitePack(),
	mongodbPack(),
	redisPack(),
)

func coreGitPack() Pack {
	return Pack{
		ID:          "core.git",
		Name:        "Core Git",
		Description: "Potentially destructive git operations",
		Keywords:    []string{"git", "push", "reset", "clean"},
		Safe: []Rule{
			{
				ID:    "git-status",
				Match: And(Name("git"), ArgAt(0, "status")),
			},
			{
				ID: "git-push-force-with-lease",
				Match: And(
					Name("git"),
					ArgAt(0, "push"),
					Or(Flags("--force-with-lease"), Flags("--force-if-includes")),
				),
			},
		},
		Destructive: []Rule{
			{
				ID:          "git-push-force",
				Severity:    3, // High
				Confidence:  2, // High
				Reason:      "git push --force can overwrite remote history",
				Remediation: "Use --force-with-lease or coordinate with collaborators",
				Match: And(
					Name("git"),
					ArgAt(0, "push"),
					Or(
						Flags("--force"),
						Flags("-f"),
						Flags("--mirror"),
						Flags("--delete"),
					),
					Not(Or(Flags("--force-with-lease"), Flags("--force-if-includes"))),
				),
			},
		},
	}
}

func coreFilesystemPack() Pack {
	return Pack{
		ID:          "core.filesystem",
		Name:        "Core Filesystem",
		Description: "Potentially destructive filesystem operations",
		Keywords:    []string{"rm", "dd", "mkfs", "shred", "truncate"},
		Safe: []Rule{
			{
				ID:    "ls",
				Match: Name("ls"),
			},
		},
		Destructive: []Rule{
			{
				ID:          "rm-rf",
				Severity:    4, // Critical
				Confidence:  2, // High
				Reason:      "rm -rf can permanently delete files",
				Remediation: "Use safer paths and verify targets before deletion",
				Match: And(
					Name("rm"),
					Or(
						Flags("--recursive"),
						Flags("--force"),
						Flags("-r", "-f"),
						Flags("-r"),
					),
				),
			},
		},
	}
}

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
