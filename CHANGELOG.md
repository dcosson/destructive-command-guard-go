# Changelog

## v0.2.0 — 2026-03-24

### Added
- **Claude Code tool-use evaluation** across non-Bash tools via `guard.EvaluateToolUse()`
- **Tool normalization pipeline** with catalog-driven mappings for Read, Write, Edit,
  Grep, Glob, NotebookEdit, WebFetch, Agent, and WebSearch
- **`Pipeline.RunCommands()`** for evaluating pre-built command structures without
  reparsing shell text
- **`dcg-go test --tool`** for evaluating structured tool inputs from the CLI
- **Hook-mode support for non-Bash tools** using full `tool_input` JSON decoding
- **`dcg-go list tools`** and public `guard.Tools()` metadata for known tool mappings
- **Comprehensive external tests for tool types** covering CLI and hook behavior
- MIT license for open-source distribution

### Changed
- Empty Bash tool input now preserves Bash-path semantics instead of falling into
  normalization-error handling
- Glob normalization now matches the documented synthetic command ordering
- Glob `path` field is now optional (defaults to cwd), matching Claude Code behavior
- `Result` now exposes `Reason()` and `Remediation()` helpers
- `--explain` flag removed from `dcg-go test` (reason/remediation always shown)

## v0.1.1 — 2026-03-21

### Added
- **Very Strict policy** — denies all matched rules regardless of severity
- **Per-directory personal file rules** — personal.files pack now has separate
  rules for Desktop, Documents, Downloads, Music, Pictures, and Videos with
  per-directory severity tuning
- iCloud rules moved to macos.privacy pack (macOS-only)
- `--blocklist` and `--allowlist` flags on `dcg-go test`
- CHANGELOG.md and release instructions in CLAUDE.md/AGENTS.md

### Changed
- **CLI migrated to cobra** for proper subcommand handling and auto-generated help
- **Strict policy** now allows Low severity (previously denied all)
- **--policy flag removed** — use `--destructive-policy` and `--privacy-policy`
  independently; if only one is set, the other defaults to allow-all
- Downloads, Music, Videos access rules lowered to Low severity
- Personal file delete/overwrite rules lowered from Critical to High
- Policy list documented in `dcg-go test --help` output

## v0.1.0 — 2026-03-20

Initial release.

### Added
- Tree-sitter-based bash command parsing and structural analysis
- Dual-category rule system: destructive and privacy rules evaluated independently
- 6 built-in policies: Allow All, Permissive, Moderate, Strict, Very Strict, Interactive
- Per-category policy configuration (separate destructive and privacy policies)
- 170+ rules across 27 packs covering git, filesystem, databases, infrastructure,
  cloud, containers, Kubernetes, frameworks, secrets, personal files, SSH, and macOS
- CLI binary (`dcg-go`) with hook mode, test mode, and list command
- Claude Code hook integration (PreToolUse JSON protocol)
- YAML config file support (`~/.config/dcg-go/config.yaml`)
- Keyword pre-filter for fast rejection of non-matching commands
- Environment detection for production severity escalation
- Indeterminate assessment on parse errors (policy decides outcome)
- Golden file test corpus with per-pack coverage validation
- Property, fault, oracle, stress, security, and mutation test suites
- GitHub Actions CI workflow
- External black-box test suite (binary + library)
