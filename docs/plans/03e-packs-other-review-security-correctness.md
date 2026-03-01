# 03e: Other Packs — Security & Correctness Review

**Reviewer**: dcg-alt-reviewer
**Date**: 2026-03-01
**Scope**: [03e-packs-other.md](./03e-packs-other.md), [03e-packs-other-test-harness.md](./03e-packs-other-test-harness.md)
**Focus**: Frameworks severity classifications with/without env escalation, rsync --delete edge cases (--delete-excluded coverage), vault operations that could compromise secrets at rest, gh repo delete vs gh repo archive.

---

## Summary

The plan is well-structured with clear separation of 4 packs. The frameworks pack is a thoughtful addition not present in the upstream Rust version. The rsync pack handles the `--delete*` flag family exhaustively with an excellent 64-combination test (D4). However, the vault pack's S2 safe pattern is dangerously over-broad — it shadows several destructive vault operations beyond the single documented `vault token revoke` gap. Additionally, rsync's safe pattern misses `--remove-source-files`, and a golden file entry references an env var not in the detector's list.

**Finding count**: 1 P0, 5 P1, 5 P2, 5 P3

---

## P0 — Critical (must fix before implementation)

### OT-P0.1: Vault S2 shadows `vault auth disable` — undocumented gap, mass token revocation

**Location**: Plan §5.3, S2 `vault-inspect-safe`

S2 includes `ArgAt(0, "auth")` which classifies ALL `vault auth` subcommands as safe. `vault auth disable <method>` disables an entire auth method and **immediately revokes ALL tokens** issued by that method. This can cause cascading outages across every service that authenticated through that method.

Unlike `vault token revoke` (documented known gap in §5.3.1 note 5), this gap is **not documented** and is arguably more severe — token revocation affects one token, auth disable affects all tokens from an entire method.

**Example bypass**:
```
vault auth disable userpass/
```
ArgAt(0) = "auth" → S2 matches → safe → destructive patterns never evaluated.

**Fix**:
1. Add Not clause to S2: `Not(And(ArgAt(0, "auth"), ArgAt(1, "disable")))`
2. Add destructive pattern `vault-auth-disable` at **Critical** severity with EnvSensitive: true
3. Add reachability command, golden file entry, and unit test
4. Similarly exclude `vault token revoke` (see OT-P1.1) and `vault policy delete` (see OT-P1.2)

The S2 fix should look like:
```go
packs.And(
    packs.Name("vault"),
    packs.Or(
        packs.ArgAt(0, "list"),
        packs.ArgAt(0, "status"),
        packs.ArgAt(0, "audit"),
        packs.ArgAt(0, "path-help"),
        packs.ArgAt(0, "version"),
        packs.ArgAt(0, "token"),
        packs.ArgAt(0, "auth"),
        packs.ArgAt(0, "login"),
        packs.ArgAt(0, "policy"),
    ),
    // Exclude destructive subcommands within safe namespaces
    packs.Not(packs.And(packs.ArgAt(0, "auth"), packs.ArgAt(1, "disable"))),
    packs.Not(packs.And(packs.ArgAt(0, "token"), packs.ArgAt(1, "revoke"))),
    packs.Not(packs.And(packs.ArgAt(0, "policy"), packs.ArgAt(1, "delete"))),
    packs.Not(packs.And(packs.ArgAt(0, "audit"), packs.ArgAt(1, "disable"))),
),
```

---

## P1 — High (should fix)

### OT-P1.1: Vault S2 `vault token revoke` gap should be fixed in v1, not deferred

**Location**: Plan §5.3.1 note 5, Test harness SEC2

The plan documents `vault token revoke` as a "known gap" acceptable for v1 with a v2 fix planned. However:
- Token revocation causes **immediate service outages** for any service using the revoked token
- `vault token revoke -accessor` can target any token by accessor ID (broad attack surface)
- The fix is trivial — one Not clause in S2 plus one new destructive pattern
- If we're already fixing S2 for `vault auth disable` (OT-P0.1), the incremental cost of fixing this gap is near zero

**Recommendation**: Fix in v1 alongside OT-P0.1. Add `vault-token-revoke` destructive pattern at High severity (single token) and `vault-token-revoke-accessor` at High with a note about the broad targeting.

### OT-P1.2: Vault S2 shadows `vault policy delete` — undocumented gap

**Location**: Plan §5.3, S2 `vault-inspect-safe`

S2 includes `ArgAt(0, "policy")` as safe. `vault policy delete <name>` removes an ACL policy. Tokens referencing that policy **immediately lose the permissions** it granted. If the policy provided access to secrets needed by running services, those services lose access.

Not documented as a known gap. Fix: add Not clause (shown in OT-P0.1 fix) and destructive pattern `vault-policy-delete` at High severity with EnvSensitive: true.

### OT-P1.3: Missing `vault kv metadata delete` pattern — permanent all-version deletion

**Location**: Plan §5.3, not covered by any pattern

`vault kv metadata delete <path>` permanently removes **all versions and all metadata** for a secret at the given path. This is more destructive than `vault kv destroy` (D2, which targets specific versions) — metadata delete is irrecoverable and removes the secret entirely from the engine.

This command is not shadowed by a safe pattern (no safe pattern matches `kv` + `metadata`), but it passes through the vault pack completely undetected because no destructive pattern catches it.

**Fix**: Add destructive pattern `vault-kv-metadata-delete` at Critical severity (matches `vault kv metadata delete`). Add reachability command, golden file entry, and unit test.

### OT-P1.4: rsync `--remove-source-files` classified safe by S1

**Location**: Plan §5.2, S1 `rsync-no-delete-safe`

S1 only excludes `--delete*` flags. The `--remove-source-files` flag causes rsync to **delete source files** after successful transfer. This is a different kind of destructive behavior — it deletes at the source, not the destination.

**Example**:
```
rsync --remove-source-files -avz /important-data/ /backup/
```
S1 matches (no --delete* flags) → classified safe. After transfer, all source files in `/important-data/` are deleted.

While `--remove-source-files` only removes successfully transferred files (safer than --delete), it's still destructive and should not be classified as safe.

**Fix**: Add `Not(packs.Flags("--remove-source-files"))` to S1. Add destructive pattern `rsync-remove-source` at Medium severity.

### OT-P1.5: Golden file D6 env escalation uses `DJANGO_SETTINGS_MODULE` — not in env detector list

**Location**: Plan §6.1, line 1230

The golden file entry:
```yaml
command: DJANGO_SETTINGS_MODULE=myapp.settings.production python manage.py flush
decision: Deny
severity: Critical
env_escalated: true
```

But the listed env detector indicators (§5.1) are: `RAILS_ENV`, `RACK_ENV`, `NODE_ENV`, `FLASK_ENV`, `APP_ENV`, `MIX_ENV`, `DATABASE_URL`. `DJANGO_SETTINGS_MODULE` is **not listed**.

If the env detector has a general "production" keyword scanner that checks all inline env vars, this golden entry may work. But if it only checks the enumerated variables, this entry will fail.

**Fix**: Either (a) add `DJANGO_SETTINGS_MODULE` to the env detector's indicator list, or (b) change the golden file entry to use `APP_ENV=production` which is already in the list. Option (a) is preferred for correctness — `DJANGO_SETTINGS_MODULE` is the canonical way to set Django environments.

---

## P2 — Medium (should consider)

### OT-P2.1: Vault S2 shadows `vault audit disable` — audit trail removal

**Location**: Plan §5.3, S2 `vault-inspect-safe`

S2 includes `ArgAt(0, "audit")` as safe. `vault audit disable <path>` disables an audit device. While not data-destructive, disabling audit logging removes the security audit trail, which:
- Violates compliance requirements (SOC2, PCI-DSS)
- Makes it impossible to detect subsequent malicious activity
- Is a common first step in an attack sequence

**Fix**: Include in the S2 Not clauses (shown in OT-P0.1 fix). Add destructive pattern `vault-audit-disable` at Medium severity.

### OT-P2.2: Vault `secrets move` in S3 safe could cause service disruptions

**Location**: Plan §5.3, S3 `vault-write-safe`

S3 includes `packs.And(packs.ArgAt(0, "secrets"), packs.ArgAt(1, "move"))` as safe. `vault secrets move <source> <destination>` changes a secrets engine's mount path. Services configured to access secrets at the old path will **immediately lose access** until reconfigured.

While not data-destructive (secrets are preserved under the new path), this can cause outages equivalent to `vault secrets disable` from the perspective of services using the old path.

**Recommendation**: Consider moving `secrets move` out of S3 and adding it as a Medium destructive pattern with EnvSensitive: true, or add a comment documenting why it's acceptable as safe.

### OT-P2.3: Missing `mix ecto.drop` pattern — common Elixir command

**Location**: Plan §5.1.1 note 4, Open Question 2

`mix ecto.drop` directly drops the database. It's deferred to v2 but:
- It's a common command in Elixir/Phoenix projects
- It passes through undetected (keyword `mix` triggers pre-filter but no pattern matches)
- The fix is one additional destructive pattern (~8 lines of Go)
- `mix ecto.reset` = drop + create + migrate — if reset is caught, the more targeted `drop` should be too

**Recommendation**: Add in v1 alongside D10. Also consider `mix ecto.rollback --all` (rolls back all migrations).

### OT-P2.4: Test harness SEC2 covers only `vault token revoke` — missing SEC tests for other S2 gaps

**Location**: Test harness SEC2

SEC2 documents and tests the `vault token revoke` known gap. But the test harness has no corresponding tests for `vault auth disable`, `vault policy delete`, or `vault audit disable` being classified as safe.

If OT-P0.1 is fixed (S2 Not clauses + new destructive patterns), SEC2 should be updated to verify all four subcommands are correctly caught. If any gaps are intentionally kept for v1, they should each have their own SEC test documenting the gap (like SEC2 does for token revoke).

### OT-P2.5: rsync `--dry-run` with `--delete` still flagged at Medium

**Location**: Plan §5.2.1 note 3, Golden file §6.2

The golden file includes:
```yaml
command: rsync --delete --dry-run -avz /src/ /dest/
decision: Ask
severity: Medium
```

The plan acknowledges this as expected behavior. However, `rsync --delete --dry-run` is a safety check commonly run before the actual destructive command. Flagging it creates noise and may condition users to ignore warnings.

**Recommendation**: Consider adding a `--dry-run` / `-n` awareness to the rsync pack as a "reduces severity" modifier. E.g., if `--dry-run` or `-n` is present alongside `--delete*`, reduce severity to Low or classify as safe. This would also benefit other tools that support dry-run in future packs.

---

## P3 — Low (nice to have)

### OT-P3.1: S1 `--run-syncdb` Not clause is a cross-framework defensive guard

**Location**: Plan §5.1 S1, §5.1.1 note 5

S1 (rails-db-migrate-safe) includes `Not(Flags("--run-syncdb"))` but `--run-syncdb` is a Django/manage.py flag, not a Rails flag. The plan documents this as "defensive" since Rails doesn't accept this flag. While harmless, it's confusing — a reader might think `rails db:migrate --run-syncdb` is a real command.

**Recommendation**: Remove the Not clause from S1 and add a code comment explaining it was considered but is unnecessary because Rails never accepts --run-syncdb.

### OT-P3.2: Missing `rsync --delete-delay` explicit golden file entry

**Location**: Plan §6.2

The golden file has explicit entries for `--delete`, `--delete-before`, `--delete-after`, `--delete-during`, `--delete-excluded`, but not `--delete-delay`. While D4 in the test harness covers all 64 flag combinations exhaustively, an explicit golden entry improves documentation completeness and serves as a regression anchor.

**Recommendation**: Add golden entry:
```yaml
command: rsync --delete-delay -avz /src/ /dest/
decision: Ask
severity: Medium
confidence: High
pack: remote.rsync
rule: rsync-delete
```

### OT-P3.3: `bundle exec` and `django-admin` v2 deferrals need explicit tracking

**Location**: Plan §14 Open Questions 1, 5

Open Questions 1 (`django-admin`) and 5 (`bundle exec rake/rails`) are deferred to v2. These are real-world gaps — Ruby projects routinely use `bundle exec` and some Django projects use `django-admin`. These should have explicit v2 tracking beads/issues to ensure they don't fall through the cracks.

For `bundle exec`, the keyword `rake`/`rails` still triggers the pre-filter, so the command enters pattern evaluation. But Name matching fails because the command name is `bundle`. This means the pre-filter does unnecessary work with no payoff.

### OT-P3.4: `python3` normalization dependency needs cross-plan verification

**Location**: Plan §5.1.1 note 2

The plan says "The python3 variant is also handled because path normalization strips version suffixes (plan 01 §4.2)." All manage.py destructive patterns use `packs.Name("python")`, so if `python3 manage.py flush` has its command name normalized from `python3` to `python`, the patterns work. If not, `python3 manage.py flush` would pass through undetected.

**Recommendation**: Add explicit test case in §9.1 or P5 that verifies `python3 manage.py flush` matches D6 (managepy-flush) after path normalization. This serves as a cross-plan integration test.

### OT-P3.5: `manage.py dbshell` opens direct database access — not covered

**Location**: Not addressed in plan

`manage.py dbshell` and `python manage.py dbshell` open an interactive database shell with full access. While opening a shell is not itself destructive, it provides unrestricted access to execute arbitrary SQL including DROP, TRUNCATE, etc. This is similar to `docker exec` (noted in 03d review).

**Recommendation**: v2 consideration — add `manage.py dbshell` as a Low or Medium destructive pattern, similar to how 03d handles interactive container access.

---

## Cross-Cutting Observations

1. **Vault S2 is the most significant issue** in this plan. The pattern follows the same over-broad safe pattern problem seen in previous reviews (GCP S1 in 03c, helm S2 in 03d, kubectl S4 in 03d). The vault S2 case is the worst because it shadows 4 distinct destructive operations, of which only 1 is documented.

2. **rsync pack is well-designed** — the exhaustive Not clause approach in S1 covering all 6 --delete variants is the correct pattern. The D4 test harness with 64 flag combinations is excellent. The `--remove-source-files` gap (OT-P1.4) is the only structural issue.

3. **Frameworks pack is solid** — the dual-invocation handling for manage.py and artisan is clean. The colon-delimited matching is simple and correct. The env sensitivity model is appropriate. The main gaps are v2 items (mix ecto.drop, bundle exec, django-admin).

4. **GitHub pack is clean** — straightforward ArgAt matching with no shadowing issues. The archive-as-safe classification is reasonable.

5. **Interaction matrix (§8.3) is misleading for vault** — it shows "no interactions" between safe and destructive patterns, but S2 actually shadows multiple destructive operations that aren't yet defined as patterns. The matrix is technically correct (no *defined* destructive patterns are shadowed) but hides the real gap.
