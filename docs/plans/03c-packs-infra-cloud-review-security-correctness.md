# 03c: Infrastructure & Cloud Packs — Security/Correctness Review

**Reviewer**: dcg-alt-reviewer (independent review)
**Plan**: [03c-packs-infra-cloud.md](./03c-packs-infra-cloud.md)
**Test Harness**: [03c-packs-infra-cloud-test-harness.md](./03c-packs-infra-cloud-test-harness.md)
**Date**: 2026-03-01
**Focus Areas**: Severity classifications for production cloud ops, missing
destructive commands, auto-approve severity split correctness, Ansible
argument parsing edge cases, cross-cloud consistency

---

## Summary

16 findings: 2 P0, 4 P1, 6 P2, 4 P3.

The two P0s are both safe-pattern bypass issues: the GCP `gcloud-readonly-safe`
pattern can be triggered by resource names containing "list"/"describe"/"info"
as substrings with word boundaries, and the Ansible `ansible-gather-safe`
pattern includes the `command` module with incomplete Not exclusions relative
to the `ansible-shell-destructive` destructive pattern. Both allow destructive
commands to be classified as safe.

Notable P1 findings include a cross-tool inconsistency where `pulumi up`
(without `--yes`) escapes detection while `terraform apply` (without
`-auto-approve`) is caught at Medium, and a cross-cloud severity
inconsistency for CloudFormation stack deletion vs. Azure resource group /
GCP project deletion.

---

## P0: Critical Security/Correctness Issues

### IC-P0.1: GCP `gcloud-readonly-safe` bypassed by resource names containing "list"/"describe"/"info"

**Location**: Plan §5.5, S1 `gcloud-readonly-safe`

**Issue**: The GCP safe pattern S1 uses `ArgContentRegex(\blist\b)`,
`ArgContentRegex(\bdescribe\b)`, and `ArgContentRegex(\binfo\b)` to match
read-only operations. These regex matchers scan ALL positional args — including
resource names that appear as trailing positional args in gcloud commands.

GCP CLIs place the resource name as a positional arg:
`gcloud compute instances delete INSTANCE_NAME`

If the resource name contains "list", "describe", or "info" with word
boundaries (hyphens count as word boundaries in regex `\b`), S1 matches
and destructive patterns are short-circuited.

**Concrete examples**:
```
gcloud compute instances delete list-replica
  → Args: ["compute", "instances", "delete", "list-replica"]
  → ArgContentRegex(\blist\b) matches "list-replica" (hyphen is \b)
  → S1 matches → SAFE → D2 gcloud-compute-instances-delete SKIPPED

gcloud projects delete my-describe-project
  → Args: ["projects", "delete", "my-describe-project"]
  → ArgContentRegex(\bdescribe\b) matches "my-describe-project"
  → S1 matches → SAFE → D1 gcloud-projects-delete (CRITICAL) SKIPPED

gcloud sql instances delete info-db
  → ArgContentRegex(\binfo\b) matches "info-db"
  → S1 matches → SAFE → D3 SKIPPED
```

**Contrast with Azure**: Azure's S1 pattern includes
`Not(ArgContentRegex(\bdelete\b))` which prevents this class of bypass.
The "delete" subcommand in the args causes the Not to negate the safe
match.

**Fix**: Add `Not(ArgContentRegex(\bdelete\b))` to GCP S1, mirroring the
Azure approach. Or use `ArgAt()`-based matching for the subcommand keywords
instead of `ArgContentRegex()` scanning all args.

**Severity**: P0 — false negative on Critical (gcloud-projects-delete) and
High (gcloud-compute-instances-delete, gcloud-sql-instances-delete, etc.)
severity patterns. Resource names with common English words like "list" are
plausible in production.

---

### IC-P0.2: Ansible S1 safe includes `command` module but Not exclusions miss `> /dev/` from D1

**Location**: Plan §5.3, S1 `ansible-gather-safe` and D1 `ansible-shell-destructive`

**Issue**: S1 includes `packs.ArgContent("command")` in the safe module list.
D1 includes `packs.ArgContent("> /dev/")` as a destructive shell pattern.
But S1's Not exclusion list does NOT include `> /dev/`.

This creates a gap: `ansible all -m command -a '> /dev/sda'` or
`ansible all -m command -a 'cat /dev/urandom > /dev/sda'`:

1. S1 check: Name("ansible") ✓, Flags("-m") ✓, ArgContent("command") ✓
2. S1 Not exclusions: state=absent ✗, state=stopped ✗, "rm " ✗, "rm\t" ✗,
   rmdir ✗, "dd " ✗, mkfs ✗ — **none trigger**
3. S1 matches → SAFE → D1 never evaluated
4. D1 would have caught this via ArgContent("> /dev/")

**Root cause**: The `command` module is fundamentally a general-purpose
execution module — not a gathering/info module. Including it in S1 requires
the Not exclusion list to be a perfect superset of D1's destructive content
patterns, which is fragile and already incomplete.

**Fix**: Either:
1. Remove `packs.ArgContent("command")` from S1 entirely (preferred — let
   command module commands match D1 or fall through)
2. Add `packs.ArgContent("> /dev/")` to S1's Not list (minimum fix, still
   fragile for future D1 additions)

**Severity**: P0 — false negative on Critical severity pattern. Writing to
raw devices (`> /dev/sda`) across an Ansible fleet can destroy all target
hosts.

---

## P1: High-Priority Issues

### IC-P1.1: `pulumi up` without `--yes` not caught — inconsistent with `terraform apply`

**Location**: Plan §5.2, §5.2.1

**Issue**: `terraform apply` without `-auto-approve` is caught by D4 at
Medium severity. `pulumi up` without `--yes` matches NO pattern — it falls
through as unmatched (allowed). Both commands deploy infrastructure changes
with a confirmation prompt.

In non-interactive LLM agent contexts (which is the primary use case for
dcg), the confirmation prompt may not work correctly — the command might
auto-proceed, fail, or hang depending on the runtime. The risk profile is
the same as `terraform apply`.

The §5.2.1 note says: "Not classified as destructive because it prompts for
confirmation interactively." But this same argument applies to
`terraform apply`, which IS classified as destructive.

**Fix**: Add a `pulumi-up` pattern at Medium/ConfidenceHigh (or ConfidenceLow
like ansible-playbook-run since the up operation could be anything):
```go
{
    Name: "pulumi-up",
    Match: packs.And(
        packs.Name("pulumi"),
        packs.ArgAt(0, "up"),
        packs.Not(packs.Flags("--yes")),
        packs.Not(packs.Flags("-y")),
    ),
    Severity:     guard.Medium,
    Confidence:   guard.ConfidenceHigh,
    ...
}
```

**Severity**: P1 — cross-tool inconsistency for equivalent operations.

---

### IC-P1.2: `aws cloudformation delete-stack` severity (High) inconsistent with cross-cloud equivalents

**Location**: Plan §5.4 D5, §5.5 D1, §5.6 D1

**Issue**: Three operations that delete a "container of resources" have
inconsistent severities:

| Command | What it deletes | Severity |
|---------|----------------|----------|
| `gcloud projects delete` | Entire GCP project + all resources | **Critical** |
| `az group delete` | Resource group + all resources within | **Critical** |
| `aws cloudformation delete-stack` | Stack + all managed resources | **High** |

CloudFormation stacks commonly manage VPCs, RDS instances, EC2 instances,
S3 buckets, and more. Deleting a production stack cascades to ALL managed
resources — the blast radius is comparable to `az group delete`.

The O2 cross-cloud consistency test does NOT compare this tier of operations,
so the inconsistency would not be caught by tests.

**Fix**: Escalate `aws-cfn-delete-stack` to Critical. Or add it to O2
cross-cloud consistency test so the inconsistency is at least documented.

**Severity**: P1 — severity under-classification for a high-blast-radius
operation.

---

### IC-P1.3: Missing `gsutil rsync -d` destructive pattern — inconsistent with `aws s3 sync --delete`

**Location**: Plan §5.5

**Issue**: `aws s3 sync --delete` is caught by D14 (`aws-s3-sync-delete`)
at Medium severity. `gsutil rsync -d` (which deletes files at the
destination not present at the source) has no destructive pattern.

`gsutil rsync -d` is correctly excluded from S2 via `Not(Flags("-d"))`,
but then falls through as unmatched (allowed) since no destructive pattern
catches it.

**Fix**: Add a `gsutil-rsync-delete` destructive pattern:
```go
{
    Name: "gsutil-rsync-delete",
    Match: packs.And(
        packs.Name("gsutil"),
        packs.ArgAt(0, "rsync"),
        packs.Flags("-d"),
    ),
    Severity:     guard.Medium,
    Confidence:   guard.ConfidenceHigh,
    Reason:       "gsutil rsync -d deletes files at destination not in source",
    EnvSensitive: true,
}
```

**Severity**: P1 — false negative for a destructive GCS operation with
a cross-cloud equivalent that IS caught.

---

### IC-P1.4: P4 auto-approve property test incomplete — missing `pulumi up` pair

**Location**: Test harness §P4

**Issue**: P4 tests auto-approve severity escalation for three pairs:
- terraform-destroy-auto-approve vs terraform-destroy
- terraform-apply-auto-approve vs terraform-apply
- pulumi-destroy-yes vs pulumi-destroy

Missing: `pulumi-up-yes` vs `pulumi-up`. Currently `pulumi-up` doesn't
exist as a pattern (see IC-P1.1). If IC-P1.1 is incorporated, P4 must
also be updated with this pair.

Additionally, the plan's `pulumi-up-yes` pattern (D4) is placed under a
`// ---- Medium ----` comment header but has `Severity: guard.High`.
The section comment is wrong.

**Severity**: P1 — test gap that masks a real pattern coverage issue.

---

## P2: Medium-Priority Issues

### IC-P2.1: AWS S1 `ArgContentRegex` may match flag values — verify with framework

**Location**: Plan §5.4 S1, §5.3.1 (Ansible notes on ArgContent behavior)

**Issue**: If `ArgContentRegex` checks flag values (as §5.3.1 states
ArgContent does), then AWS S1 could false-match flag values:

```
aws iam delete-role --role-name describe-service-role
  → flag value "describe-service-role" starts with "describe-"
  → ArgContentRegex(^describe-) matches → S1 safe → D6 skipped
```

IAM resource names containing "describe-", "list-", or "get-" are
plausible (e.g., roles granting describe permissions).

**Depends on**: Whether `ArgContentRegex` checks flag values by default
or only positional args. The database packs use `CheckFlagValues: true`
to opt into flag value checking, suggesting the default might be
positional-only. Needs verification against 02-matching-framework.

**Fix if confirmed**: Use `ArgAt(1, "describe-...")` positional matching
for the safe pattern instead of regex scanning, or add Not exclusions for
destructive subcommands.

**Severity**: P2 — potentially P0 if ArgContentRegex checks flag values.

---

### IC-P2.2: Missing `gcloud storage rm` destructive pattern

**Location**: Plan §5.5.1

**Issue**: Google is actively migrating users from `gsutil` to
`gcloud storage`. `gcloud storage rm -r gs://bucket` is the recommended
replacement for `gsutil rm -r gs://bucket`. Currently:
- `gsutil rm -r` → caught by D5 (High) ✓
- `gcloud storage rm -r` → no safe match, no destructive match → allowed ✗

The plan acknowledges this gap and defers to v2, but `gcloud storage` is
already the default in recent gcloud SDK versions.

**Severity**: P2 — acknowledged gap but growing in importance as migration
progresses.

---

### IC-P2.3: Azure `--yes`/`-y` auto-approve not severity-escalated

**Location**: Plan §5.6.1

**Issue**: Azure commands with `--yes` skip confirmation. No severity
escalation exists (unlike Terraform/Pulumi). `az group delete --yes`
gets the same Critical as `az group delete`.

For `az group delete`, both variants are already Critical, so escalation
isn't meaningful. But for Medium-severity commands like `az vm stop --yes`,
escalation could be useful.

The plan defers this to v2 citing inconsistent Azure behavior.

**Severity**: P2 — cross-tool inconsistency, partially mitigated by
already-high base severities.

---

### IC-P2.4: `terraform state push` not covered

**Location**: Plan §5.1 S4, MQ3

**Issue**: MQ3 identifies `terraform state push` as uncovered. S4 marks
`state` as safe when not `rm` or `mv`. `terraform state push` would match
S4 (safe).

But `terraform state push` overwrites remote state with local state —
this can orphan or duplicate resources. It should be at least Medium
severity.

**Fix**: Add `packs.Not(packs.ArgContent("push"))` to S4, and add a
`terraform-state-push` destructive pattern at Medium.

**Severity**: P2 — state manipulation with potential for resource orphaning.

---

### IC-P2.5: `bq rm -f` force flag not distinguished from `bq rm`

**Location**: Plan §5.5 D6

**Issue**: BigQuery's `bq rm -f` skips confirmation prompts. Currently both
`bq rm` and `bq rm -f` match D6 at the same severity (High). For
Terraform and Pulumi, auto-approve flags escalate severity by one level.

The precedent set by auto-approve severity splitting suggests `bq rm -f`
should be Critical and `bq rm` (with confirmation) should be High.

**Severity**: P2 — cross-tool auto-approve inconsistency.

---

### IC-P2.6: Golden file entry count discrepancy

**Location**: Plan §1 vs §6

**Issue**: §1 says "120+ golden file entries across all 6 packs." §6
calculates 118 entries (16+14+16+28+22+22). The test harness exit criteria
also says "118 infra/cloud entries."

**Fix**: Update §1 to say "118 golden file entries."

**Severity**: P2 — documentation inconsistency.

---

## P3: Low-Priority Issues

### IC-P3.1: O2 cross-cloud consistency test doesn't cover project/resource group/stack deletion

**Location**: Test harness §O2

**Issue**: O2 tests VM termination, database deletion, storage deletion,
and cluster deletion across all three clouds. But it doesn't test the
"container of resources" deletion tier:

| Tier | AWS | GCP | Azure |
|------|-----|-----|-------|
| Container deletion | `cloudformation delete-stack` (High) | `projects delete` (Critical) | `group delete` (Critical) |

This is where the most significant cross-cloud severity inconsistency
exists (IC-P1.2). Adding this tier to O2 would surface the inconsistency
automatically.

**Severity**: P3 — test coverage gap for an important consistency property.

---

### IC-P3.2: Pulumi pack section comment "---- Medium ----" for High severity pattern

**Location**: Plan §5.2, D4 `pulumi-up-yes`

**Issue**: The section comment says `// ---- Medium ----` but D4
`pulumi-up-yes` has `Severity: guard.High`. The comment header should be
`// ---- High ----` since `pulumi-up-yes` is High severity (matching
`terraform-apply-auto-approve`).

The golden file entries correctly show High for `pulumi up --yes`, so the
code severity is correct — only the organizational comment is wrong.

**Fix**: Change `// ---- Medium ----` to `// ---- High ----` above D4,
and add a new `// ---- Medium ----` section before D5 (pulumi-cancel).

**Severity**: P3 — misleading comment, no functional impact.

---

### IC-P3.3: `terraform import` may cause destructive drift on next apply

**Location**: Plan §5.1.1

**Issue**: `terraform import` is classified as safe. While the import
itself doesn't modify infrastructure, an incorrect import can cause
Terraform to generate a plan that destroys or recreates the imported
resource on next `terraform apply`. In production environments, this
could be catastrophic.

The plan accepts this as safe for v1, noting the risk. This is a
reasonable v1 decision but worth tracking for v2.

**Severity**: P3 — accepted risk with mitigating argument, but should
be revisited.

---

### IC-P3.4: SEC1 test coverage could include flag-in-subcommand ordering for AWS

**Location**: Test harness §SEC1

**Issue**: SEC1 tests that flags before subcommands don't affect matching:
`aws --profile prod ec2 terminate-instances`. But it doesn't test flags
between subcommand levels: `aws ec2 --debug terminate-instances`. The
tree-sitter parser should correctly separate flags from positional args,
but testing this configuration would increase confidence.

**Severity**: P3 — additional test coverage for an unlikely but possible
edge case.

---

## Cross-Cutting Observations

### Auto-Approve Severity Split
The auto-approve severity split design is sound for Terraform and Pulumi.
The one-level escalation (Medium→High for apply, High→Critical for
destroy) correctly reflects the added risk of removing confirmation
prompts. The gap is in applying this consistently across tools — GCP
`--quiet`, Azure `--yes`, and BigQuery `-f` all suppress confirmations
but don't escalate severity.

### Universal Environment Sensitivity
The blanket `EnvSensitive: true` for all 52 destructive patterns is
appropriate and well-tested via P3. Infrastructure and cloud operations
in production environments carry categorically higher risk.

### Cross-Cloud Consistency
O2 and O3 tests provide good coverage for equivalent operations across
clouds. The main gap is the "container deletion" tier (IC-P3.1) where
severities are inconsistent (IC-P1.2).

### Test Harness Quality
The test harness is thorough. P1-P7 properties cover the key invariants.
F1-F3 fault injection tests handle edge cases well. The cross-cloud
consistency oracles (O2, O3) are a particularly strong design choice.
