# 03e-packs-other — Systems Engineer Review

**Reviewer**: systems-engineer (dcg-reviewer)
**Date**: 2026-03-01
**Plan doc**: `docs/plans/03e-packs-other.md`
**Test harness**: `docs/plans/03e-packs-other-test-harness.md`
**Review scope**: frameworks pack env-sensitivity, rsync --delete variants,
vault secret management operations, gh CLI safe pattern completeness,
frameworks tool coverage (rails, rake, manage.py, artisan, mix)

**Cross-references used**: Plan 01 (flag parsing), Plan 02 (matching framework,
evaluation pipeline, keyword pre-filter)

---

## Summary

4 packs (frameworks, rsync, vault, github) covering 22 destructive and 12 safe
patterns. The frameworks pack is the canonical env-sensitive pack with dual
invocation support for manage.py and artisan. Overall the plan is solid with
well-structured patterns and good use of Or() for dual invocation forms.

Key concern: vault S2 safe pattern is overly broad — it matches entire top-level
subcommands (token, auth, policy) without checking the second-level subcommand,
silently marking genuinely destructive operations like `vault auth disable` as
safe. The `vault token revoke` case is documented as a known gap but `vault auth
disable` is not mentioned despite being comparable in severity to the existing
`vault secrets disable` (D1, Critical).

**Finding count**: 1 P0, 5 P1, 5 P2, 4 P3 (15 total)

---

## Findings

### P0-1: vault S2 shadows `vault auth disable` — undocumented gap

**Location**: §5.3 vault pack, S2 `vault-inspect-safe`, line 790–791

**Issue**: S2 includes `ArgAt(0, "auth")` unconditionally, causing `vault auth
disable <method>` to match the safe pattern and short-circuit destructive
evaluation. `vault auth disable` removes an auth method and immediately revokes
ALL tokens issued by that method. If the disabled method is the primary auth
mechanism (LDAP, OIDC, userpass), all users and services lose Vault access.

This operation is comparable in severity to `vault secrets disable` (D1,
Critical), yet it is actively marked as safe/Allow by S2. The plan documents
`vault token revoke` as a known gap in §5.3.1 note 5 but fails to mention
`vault auth disable`, which is more severe.

**Impact**: A genuinely Critical-severity destructive operation is classified as
safe. Unlike `vault token revoke` (documented gap), this operation is not
mentioned anywhere in the plan.

**Recommendation**: Either (a) add `vault auth disable` as a destructive
pattern now (analogous to D1 `vault secrets disable`), and add
`Not(ArgAt(1, "disable"))` to S2's auth branch; or (b) at minimum, document
this as a known gap alongside `vault token revoke` and restrict S2's auth
matching to safe subcommands:
```go
packs.And(packs.ArgAt(0, "auth"),
    packs.Or(
        packs.ArgAt(1, "list"),
        packs.ArgAt(1, "help"),
        packs.ArgAt(1, "tune"),
    )),
```

---

### P1-1: vault S2 also shadows `vault policy delete`

**Location**: §5.3 vault pack, S2, line 793

**Issue**: S2 includes `ArgAt(0, "policy")` unconditionally. `vault policy
delete <name>` removes a Vault ACL policy. All tokens referencing that policy
lose those permissions. If the deleted policy grants access to secrets needed by
production services, those services lose access.

Not documented as a known gap.

**Recommendation**: Same approach as P0-1 — restrict S2's policy matching to
safe subcommands (list, read, fmt) or document as known gap.

---

### P1-2: rsync `--remove-source-files` undetected

**Location**: §5.2 rsync pack, S1 and D1–D3

**Issue**: rsync's `--remove-source-files` flag deletes source files after
successful transfer. S1 (rsync-no-delete-safe) does not exclude this flag, and
no destructive pattern detects it. `rsync --remove-source-files /src/ /dest/`
matches S1 as safe, but it deletes all successfully-transferred files from the
source directory.

For an LLM agent that might not understand the implications, this could cause
irreversible data loss at the source. The `--delete*` family affects the
destination; `--remove-source-files` affects the source.

**Recommendation**: Either (a) add a D4 destructive pattern for
`--remove-source-files` at Medium severity, and add
`Not(Flags("--remove-source-files"))` to S1; or (b) document as v2 known gap.

---

### P1-3: Missing env-escalated golden entries for D3, D5, D7, D9

**Location**: §6.1 frameworks golden entries

**Issue**: Framework patterns D1, D2, D6, D8, D10 all have golden file entries
demonstrating env escalation (e.g., `RAILS_ENV=production rails db:drop` →
Critical). But D3 (rails db:schema:load), D5 (rake db:drop/reset/schema:load),
D7 (manage.py migrate --run-syncdb), and D9 (artisan migrate:reset) all have
`EnvSensitive: true` yet lack env-escalated golden entries.

Since frameworks is the canonical env-sensitive pack, every env-sensitive
pattern should have at least one golden file entry demonstrating escalation.

**Missing entries needed**:
- `RAILS_ENV=production rails db:schema:load` → Critical (D3)
- `RAILS_ENV=production rake db:drop` → Critical (D5)
- `DJANGO_SETTINGS_MODULE=myapp.settings.production manage.py migrate --run-syncdb` → High (D7)
- `APP_ENV=production php artisan migrate:reset` → Critical (D9)

---

### P1-4: `manage.py migrate` has no explicit safe pattern

**Location**: §5.1 frameworks pack, S4 `managepy-non-db-safe`

**Issue**: `rails db:migrate` has explicit safe pattern S1 with
`Not(Flags("--run-syncdb"))`. But `manage.py migrate` has no equivalent safe
pattern — S4 lists `runserver`, `test`, `shell`, `createsuperuser`,
`collectstatic`, `makemigrations`, `showmigrations`, `startapp`,
`startproject`, `check` but NOT `migrate`.

`manage.py migrate` is the most commonly run Django command after `runserver`.
It currently passes through as unmatched → Allow, which is correct. But this is
fragile — if a manage.py catch-all destructive is added later, `manage.py
migrate` would be caught without a safe pattern to protect it.

**Recommendation**: Add `migrate` to S4 with `Not(Flags("--run-syncdb"))` to
mirror S1's approach for Rails and provide consistent, explicit safe coverage.

---

### P1-5: Missing golden entry for `rsync --delete-delay`

**Location**: §6.2 rsync golden entries, §9.2 unit tests

**Issue**: D3 matches `--delete-delay` (confirmed in unit test at line 1999),
and S1 has `Not(Flags("--delete-delay"))` exclusion. But the golden file has
no entry for `rsync --delete-delay`. All other D3 variants (`--delete`,
`--delete-after`, `--delete-during`) have golden entries.

**Recommendation**: Add golden entry:
```yaml
command: rsync --delete-delay -avz /src/ /dest/
decision: Ask
severity: Medium
confidence: High
pack: remote.rsync
rule: rsync-delete
env_escalated: false
```

---

### P2-1: Golden file `manage.py migrate` comment misleading

**Location**: §6.1 frameworks golden entries, lines 1345–1350

**Issue**: The comment says `# manage.py migrate WITHOUT --run-syncdb (safe)`
and the entry shows `decision: Allow`. This implies a safe pattern matched, but
no safe pattern covers `manage.py migrate`. The Allow decision comes from no
pack match at all (neither safe nor destructive), not from an explicit safe
classification.

**Recommendation**: Change comment to `# manage.py migrate (no pattern match —
passes through)` to accurately reflect the classification mechanism. Or add the
safe pattern per P1-4.

---

### P2-2: §5.2.1 note 4 incorrect reasoning for rsync D1 precedence

**Location**: §5.2.1 note 4

**Issue**: Note says "A command like `rsync --delete --delete-excluded` will
match D1 (highest severity) because D1 is evaluated first." Per plan 02
§matchCommand, ALL destructive patterns are evaluated and ALL matches are
collected. `aggregateAssessments` then selects the highest severity match.

D1 is selected because it has higher severity (High > Medium), not because
it's evaluated first. The end result is correct but the stated reasoning is
wrong.

**Recommendation**: Change "because D1 is evaluated first" to "because
`aggregateAssessments` selects the highest severity match."

---

### P2-3: `mix ecto.drop` — no negative golden test

**Location**: §14 Q2, §5.1.1 note 4

**Issue**: `mix ecto.drop` is acknowledged as a v2 gap. It should have a
golden file entry showing it is currently unmatched (no pack match → Allow or
Indeterminate), serving as a regression detector for when the pattern is later
added.

**Recommendation**: Add entry:
```yaml
# mix ecto.drop — v2 gap, currently unmatched
command: mix ecto.drop
decision: Allow
```

---

### P2-4: D4 rake-db-drop-all EnvSensitive on Critical is no-op

**Location**: §5.1 frameworks pack, D4

**Issue**: D4 has `Severity: Critical` and `EnvSensitive: true`. Per plan 02
`escalateSeverity`, Critical cannot be escalated further — it's capped at
Critical. The EnvSensitive flag is functionally a no-op for D4.

This is harmless but semantically confusing. It implies the severity should
change in production when it can't.

**Recommendation**: Either (a) set `EnvSensitive: false` for D4 since it's
already Critical, or (b) add a comment noting escalation is a no-op at
Critical. Prefer (b) for consistency if the convention is "all framework
DB ops are EnvSensitive."

---

### P2-5: §5.1.1 note 7 env var list incomplete

**Location**: §5.1.1 note 7

**Issue**: Note says "The env detector checks `RAILS_ENV`, `RACK_ENV`,
`NODE_ENV`, `FLASK_ENV`, `APP_ENV`, `MIX_ENV` for production values." But the
golden file entry at line 1230 shows `DJANGO_SETTINGS_MODULE=myapp.settings.production`
triggering env escalation. `DJANGO_SETTINGS_MODULE` is not in the listed vars.

This likely works because the env detector has a general "value contains
production" fallback, but the note should mention this or include
`DJANGO_SETTINGS_MODULE` in the list.

**Recommendation**: Add `DJANGO_SETTINGS_MODULE` to the list, or add a note
explaining that the env detector also checks all inline env var values for
production-indicator patterns regardless of the var name.

---

### P3-1: S1 rails-db-migrate-safe unnecessary Not(--run-syncdb)

**Location**: §5.1 frameworks pack, S1, line 244

**Issue**: S1 has `Not(Flags("--run-syncdb"))` but `--run-syncdb` is a Django
flag, not a Rails flag. §5.1.1 note 5 acknowledges this is defensive.

Harmless but adds a Not clause that can never trigger. Very minor clutter.

**Recommendation**: No action needed; note 5 already documents the reasoning.
Optionally remove in a cleanup pass.

---

### P3-2: Vault interaction matrix inconsistency

**Location**: §8.3

**Issue**: The matrix shows "No interactions" between vault safe and destructive
patterns, then the note says "S2 includes `vault token` which would shadow a
hypothetical `vault token revoke` destructive pattern." This is contradictory —
the matrix claims no interactions exist, then the note describes one.

The matrix is technically correct (no interaction with EXISTING destructive
patterns) but the note reveals that S2 WOULD interact with future patterns.

**Recommendation**: Either add a "potential" row to the matrix or restructure
the note as a standalone subsection about safe pattern breadth.

---

### P3-3: `bundle exec rake` pre-filter waste

**Location**: §14 Q5

**Issue**: Keyword `rake` triggers the pre-filter for `bundle exec rake
db:drop`, but the match fails because Name is `bundle`, not `rake`. The
destructive command passes through undetected. The plan documents this as v2
but it represents both a pre-filter efficiency waste and a correctness gap for
real-world Ruby usage.

**Recommendation**: No action needed for v1 — already documented. Note that
`bundle exec rails` has the same issue.

---

### P3-4: Vault S2 breadth scope not fully documented

**Location**: §5.3.1 note 5

**Issue**: Only `vault token revoke` is documented as a known gap from S2
breadth. The full list of potentially destructive operations shadowed by S2
includes `vault auth disable` (P0-1), `vault policy delete` (P1-1), and
`vault token revoke`. These should be documented together for completeness.

**Recommendation**: After addressing P0-1 and P1-1, add a comprehensive
note listing all operations affected by S2 breadth.

---

## Positive Observations

1. **Dual invocation patterns are well-structured**: The Or() wrapping with
   correctly shifted ArgAt indices for `python manage.py` and `php artisan`
   is consistently applied across all safe and destructive patterns.

2. **Keyword pre-filter works for dual invocation**: §10 correctly identifies
   that keyword `manage.py` appears in the raw command string `python manage.py
   flush`, triggering the pre-filter even though `manage.py` isn't the command
   name. Confirmed against plan 02 §Contains.

3. **rsync S1 Not clause coverage is comprehensive**: All 6 `--delete*`
   variants are excluded from S1, preventing safe-pattern shadowing. This is
   better coverage than some earlier packs.

4. **Framework env-sensitivity is consistently applied**: All 10 destructive
   patterns have `EnvSensitive: true`, making this a good canonical reference
   for env-aware pack design.

5. **Vault D3/D4 evaluation order correctly analyzed**: §5.3.1 note 3
   correctly states that pattern ordering doesn't affect correctness because
   `aggregateAssessments` selects the highest severity match. Confirmed against
   plan 02 §matchCommand and §aggregateAssessments.
