---
shaping: true
---

# Destructive Command Guard (Go) — Shaping

## Requirements (R)

| ID | Requirement | Status |
|----|-------------|--------|
| R0 | Guard against destructive shell commands; assessment (severity/confidence) decoupled from policy (allow/deny/ask) so callers control risk tolerance | Core goal |
| R1 | Pure Go — no cgo, no C dependencies | Must-have |
| R2 | Public Go API so it can be imported as a library (primary use: h2's `handle-hook` pipeline) | Must-have |
| R3 | Use tree-sitter for structural command analysis (bash parsing, inline script detection) | Must-have |
| R4 | Cover the important destructive command categories (git, filesystem, databases, containers, cloud, etc.) | Must-have |
| R5 | Performance is invisible to user — benchmarked, no hard runtime budget | Must-have |
| R6 | Also work as a standalone Claude Code hook binary (JSON stdin/stdout) | Must-have |
| R7 | 🟡 Standalone binary supports other agent protocols (Copilot, Gemini CLI) in addition to Claude Code | Nice-to-have |
| R8 | 🟡 Library API supports caller-provided allowlists and blocklists (overrides in both directions) | Must-have |
| R9 | 🟡 Standalone binary reads config file for allowlists/blocklists/pack selection | Nice-to-have |
| R10 | CLI: `dcgo test "cmd"` (with `--explain`), `dcgo packs` to list packs | Must-have |
| R11 | 🟡 Environment awareness — detect production indicators from inline env vars in the command (via AST) and from caller-provided process env vars; escalate severity of database/infra commands in production-looking environments | Must-have |

---

## A: Tree-sitter-first structural analysis

Use tree-sitter bash grammar as the primary analysis layer. Parse every
command that passes keyword pre-filter, walk the AST to extract command
invocations, then match extracted commands against pattern packs.

Library-first architecture: core evaluation logic is a public Go package,
standalone binary and h2 integration are thin wrappers around it.

| Part | Mechanism | Flag |
|------|-----------|:----:|
| **A1** | **Public evaluation API** — `guard.Evaluate(command string, opts ...Option) → Result` as the core entry point. Returns a decision (Allow, Deny, Ask) plus match details (pack, rule, severity, confidence, reason, remediation). Stateless, no I/O. Callers pass options for policy, allowlists, blocklists, and pack selection. | |
| **A2** | **Policy layer** — Converts raw assessment (severity + confidence) into a decision (Allow, Deny, Ask). Caller-configurable: `StrictPolicy` (no Ask — uncertain → Deny, for background agents), `InteractivePolicy` (uncertain → Ask, for user-facing agents), or custom thresholds. Ships with sensible defaults. Decoupled from pattern internals. | |
| **A3** | **Keyword pre-filter** — Simple string-contains check for pack keywords (git, rm, docker, etc.). Commands with no matching keywords skip parsing entirely. Internal to the evaluation pipeline. | |
| **A4** | **Tree-sitter bash parse** — Parse command string with tree-sitter-go bash grammar. Produces full AST with command boundaries, pipelines, compound commands, subshells, string literals, heredocs. | |
| **A5** | **AST command extraction** — Walk the bash AST to extract individual command invocations. Each `simple_command` node yields (command_name, args, flags, inline_env_vars). String arguments identified as safe context. Subshells and command substitutions recursed into. Inline env var assignments (e.g. `RAILS_ENV=production` prefix) extracted from AST. | |
| **A6** | **Inline script detection** — For commands like `python -c "..."`, `ruby -e "..."`, `bash -c "..."`, extract the script body and parse with the appropriate tree-sitter grammar. Walk that AST for destructive patterns. | |
| **A7** | **Environment awareness** — Detects production indicators from two sources: (1) inline env vars extracted from the bash AST (e.g. `DATABASE_URL=postgres://prod/... psql`), (2) caller-provided process env vars via option (e.g. `guard.WithEnv(os.Environ())`). Checks for patterns like `RAILS_ENV=production`, `NODE_ENV=production`, `DATABASE_URL` containing "prod", `AWS_PROFILE=production`, etc. When production indicators are detected, escalates severity of database/infra/cloud commands. | |
| **A8** | **Pattern packs** — Modular pattern definitions organized by category. Each pack has: keywords (for pre-filter), safe patterns (whitelist), destructive patterns (blacklist) with severity, confidence, and remediation suggestions. Patterns produce assessments, not decisions — the policy layer (A2) maps these to actions. Some patterns are environment-sensitive (e.g. `rails db:reset` is Medium normally, Critical when `RAILS_ENV=production`). | |
| **A9** | **Standalone hook binary** — Thin `main()` that reads Claude Code JSON from stdin, extracts the command, calls the evaluation API with default policy and `os.Environ()`, outputs JSON with `permissionDecision` of `allow`, `deny`, or `ask`. | |
| **A10** | **Config file loading (standalone binary only)** — Binary reads optional config file for allowlists, blocklists, pack selection, policy choice, then passes them as options to the library API. Not needed for library consumers like h2 who manage their own config. | ⚠️ |
| **A11** | **CLI interface** — Binary is `dcgo`. Subcommands: `dcgo test "cmd"` (run guard, show result; `--explain` for detailed reasoning), `dcgo packs` (list available packs). Hook mode is default when no subcommand (reads stdin JSON). | |

---

## Fit Check: R × A

| Req | Requirement | Status | A |
|-----|-------------|--------|---|
| R0 | Guard against destructive shell commands; assessment (severity/confidence) decoupled from policy (allow/deny/ask) so callers control risk tolerance | Core goal | ✅ |
| R1 | Pure Go — no cgo, no C dependencies | Must-have | ✅ |
| R2 | Public Go API so it can be imported as a library (primary use: h2's `handle-hook` pipeline) | Must-have | ✅ |
| R3 | Use tree-sitter for structural command analysis (bash parsing, inline script detection) | Must-have | ✅ |
| R4 | Cover the important destructive command categories (git, filesystem, databases, containers, cloud, etc.) | Must-have | ✅ |
| R5 | Performance is invisible to user — benchmarked, no hard runtime budget | Must-have | ✅ |
| R6 | Also work as a standalone Claude Code hook binary (JSON stdin/stdout) | Must-have | ✅ |
| R7 | 🟡 Standalone binary supports other agent protocols (Copilot, Gemini CLI) in addition to Claude Code | Nice-to-have | ❌ |
| R8 | 🟡 Library API supports caller-provided allowlists and blocklists (overrides in both directions) | Must-have | ✅ |
| R9 | 🟡 Standalone binary reads config file for allowlists/blocklists/pack selection | Nice-to-have | ❌ |
| R10 | 🟡 CLI: `dcgo test "cmd"` (with `--explain`), `dcgo packs` to list packs | Must-have | ✅ |

| R11 | 🟡 Environment awareness — detect production indicators from inline env vars in the command (via AST) and from caller-provided process env vars; escalate severity of database/infra commands in production-looking environments | Must-have | ✅ |

**Notes:**

- A fails R7: A9 only handles Claude Code protocol for now. Library API is protocol-agnostic (just takes a command string), so adding other protocols later is just A9 work.
- A fails R9: A10 is flagged — config file loading not yet detailed.

---

## A8 Detail: Pack Scope

### Packs (all included in v1)

| Pack | Keywords | Key Destructive Patterns | Env-Sensitive |
|------|----------|--------------------------|:---:|
| **core.git** | git | `git reset --hard`, `git push --force`, `git clean -fd`, `git branch -D`, `git checkout -- .`, `git rebase` on shared branches | |
| **core.filesystem** | rm, dd, shred, chmod, chown, mkfs | `rm -rf`, `dd of=`, `shred`, `chmod -R 777`, `mkfs`, `mv` to /dev/null | |
| **database.postgresql** | psql, pg_dump, pg_restore, dropdb, createdb | `DROP TABLE`, `DROP DATABASE`, `TRUNCATE`, `DELETE FROM` (no WHERE), `dropdb` | ✅ |
| **database.mysql** | mysql, mysqldump, mysqladmin | `DROP TABLE`, `DROP DATABASE`, `TRUNCATE`, `DELETE FROM` (no WHERE), `mysqladmin drop` | ✅ |
| **database.sqlite** | sqlite3 | `DROP TABLE`, `.drop`, `DELETE FROM` (no WHERE) | |
| **containers.docker** | docker | `docker system prune -af`, `docker rm -f`, `docker rmi -f`, `docker volume rm`, `docker network rm` | |
| **containers.compose** | docker-compose, docker compose | `docker compose down -v` (destroys volumes), `docker compose rm -f` | |
| **infrastructure.terraform** | terraform | `terraform destroy`, `terraform apply -auto-approve`, `terraform state rm` | ✅ |
| **infrastructure.pulumi** | pulumi | `pulumi destroy`, `pulumi stack rm` | ✅ |
| **cloud.aws** | aws | `aws ec2 terminate-instances`, `aws rds delete-db-instance`, `aws s3 rb --force`, `aws s3 rm --recursive`, `aws cloudformation delete-stack` | ✅ |
| **cloud.gcp** | gcloud, gsutil | `gcloud compute instances delete`, `gcloud projects delete`, `gsutil rm -r` | ✅ |
| **cloud.azure** | az | `az vm delete`, `az group delete`, `az storage account delete` | ✅ |
| **kubernetes.kubectl** | kubectl | `kubectl delete namespace`, `kubectl delete -f`, `kubectl drain`, `kubectl replace --force` | ✅ |
| **frameworks** | rails, rake, manage.py, artisan, mix | `rails db:reset`, `rails db:drop`, `rake db:drop:all`, `manage.py flush`, `manage.py migrate --run-syncdb`, `artisan migrate:fresh`, `mix ecto.reset` | ✅ |
| **kubernetes.helm** | helm | `helm uninstall`, `helm delete`, `helm rollback` | ✅ |
| **database.mongodb** | mongo, mongosh, mongodump, mongorestore | `db.dropDatabase()`, `db.collection.drop()`, `db.collection.deleteMany({})`, `mongosh --eval "db.dropDatabase()"` | ✅ |
| **database.redis** | redis-cli | `FLUSHALL`, `FLUSHDB`, `DEL *`, `redis-cli FLUSHALL` | ✅ |
| **remote.rsync** | rsync | `rsync --delete`, `rsync --delete-before`, `rsync --delete-after` | |
| **secrets.vault** | vault | `vault secrets disable`, `vault delete`, `vault kv destroy`, `vault lease revoke` | ✅ |
| **platform.github** | gh | `gh repo delete`, `gh release delete`, `gh issue close`, `gh pr close` | |
| **infrastructure.ansible** | ansible, ansible-playbook | `ansible all -m shell -a "rm -rf /"`, `ansible-playbook --extra-vars "state=absent"`, `ansible all -m file -a "state=absent"`, `ansible all -m service -a "state=stopped"` | ✅ |

**Note:** The **frameworks** pack is new — not in the upstream Rust version. It pairs
with environment awareness (A7) to catch the "ran db:reset against prod" class
of mistakes.

### Not planned

DNS, email, payment, search, API gateway, load balancer, monitoring, feature
flags, messaging (Kafka/RabbitMQ/SQS), CI/CD pipeline modification, Podman,
Kustomize, MinIO, 1Password, CircleCI, Jenkins.

These are either too niche for an LLM coding context or the destructive
commands are uncommon LLM mistakes. Can be added if demand arises.
