# Changelog

## Unreleased

### Added
- **BlockAll policy** — denies everything regardless of severity
- **Per-directory personal file rules** — personal.files pack now has separate
  rules for Desktop, Documents, Downloads, Music, Pictures, and Videos with
  per-directory severity tuning
- iCloud rules moved to macos.privacy pack (macOS-only)

### Changed
- **Strict policy** now allows Low severity (previously denied all)
- Downloads, Music, Videos access rules lowered to Low severity
- Personal file delete/overwrite rules lowered from Critical to High

## v0.1.0 — 2026-03-20

Initial release.

### Added
- Tree-sitter-based bash command parsing and structural analysis
- Dual-category rule system: destructive and privacy rules evaluated independently
- 6 built-in policies: Allow All, Permissive, Moderate, Strict, Block All, Interactive
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
