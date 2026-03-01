# Plan Review: 03c-packs-infra-cloud — Systems Engineer Perspective

**Reviewer**: dcg-reviewer (systems-engineer persona)
**Plan**: `docs/plans/03c-packs-infra-cloud.md`
**Test Harness**: `docs/plans/03c-packs-infra-cloud-test-harness.md`
**Date**: 2026-03-01
**Review Round**: R1

---

## Summary

The plan defines 6 packs (terraform, pulumi, ansible, aws, gcp, azure) with 17
safe and 52 destructive patterns covering infrastructure-as-code tools and major
cloud CLIs. The overall structure is solid — all destructive patterns are
EnvSensitive, auto-approve severity escalation is well-designed for terraform and
pulumi, and the test harness is thorough with property tests, comparison oracles,
and cross-cloud consistency checks.

However, there are critical issues with how safe patterns use substring/regex
matching across all positional args instead of position-specific matching. This
causes both false exclusions (terraform S4) and false allowances (gcloud S1,
ansible S1). Three P0 findings relate to safe pattern matching that could cause
clearly destructive commands to be classified as Allow or Indeterminate.

**Findings**: 3 P0, 5 P1, 5 P2, 4 P3

---

## P0 — Critical

### P0-1: terraform-readonly-safe S4 `ArgContent("rm"/"mv")` substring false exclusion

**Location**: §5.1, lines 296-297

S4 uses `Not(ArgContent("rm"))` and `Not(ArgContent("mv"))` to exclude state-rm
and state-mv from the safe pattern. Per plan 02, `ArgContent` does substring
matching across ALL positional args (AtIndex=-1). Per plan 01, short flags like
`-m` are treated as boolean and subsequent tokens become positional args.

This means any terraform arg containing "rm" or "mv" as a substring falsely
excludes the command from S4:

- `terraform state list aws_instance.webfarm` — "webfarm" contains "rm" →
  Not(true) → S4 fails → falls through to destructive → no destructive match
  → Indeterminate. Should be Allow.
- `terraform output farm_count` — "farm" doesn't contain "rm" (f-a-r-m →
  actually "farm" DOES contain "rm" at positions 2-3) → same false exclusion.
- `terraform fmt ./forms/` — "forms" contains "rm" → false exclusion.

**Impact**: Safe commands with resource names containing "rm" or "mv" substrings
are mis-classified as Indeterminate instead of Allow.

**Fix**: Use `ArgAt(1, "rm")` and `ArgAt(1, "mv")` to check specifically the
state subcommand position (index 1) instead of scanning all args. The state
subcommand is always at arg position 1 (`terraform state <subcommand>`).

### P0-2: ansible-gather-safe S1 "command" module allows arbitrary destructive commands

**Location**: §5.3, line 631

S1 includes `ArgContent("command")` in the safe module list. Per plan 01, for
`ansible all -m command -a 'reboot'`, the parsing yields Args: ["all",
"command", "reboot"] (short flags are boolean, subsequent tokens are positional
args). The Not clause only blocklists `rm`, `dd`, `mkfs`, `rmdir`.

Commands that bypass the blocklist but are clearly destructive:

- `ansible all -m command -a 'reboot'` → S1 matches → Allow (reboots entire fleet)
- `ansible all -m command -a 'shutdown -h now'` → S1 matches → Allow
- `ansible all -m command -a 'kill -9 1234'` → S1 matches → Allow
- `ansible all -m command -a 'userdel admin'` → S1 matches → Allow
- `ansible all -m command -a 'systemctl stop nginx'` → S1 matches → Allow
- `ansible all -m command -a 'wipefs -a /dev/sda'` → S1 matches → Allow

The `command` module is inherently dangerous because it can execute arbitrary
commands across all targeted hosts. A blocklist approach is fundamentally unsound
for this module — it will always miss newly-discovered destructive commands.

**Impact**: Clearly destructive commands targeting entire host fleets are
classified as safe (Allow) and never reach destructive pattern D1.

**Fix**: Remove "command" from S1's safe module list. All `ansible -m command`
invocations should fall through to D1 (if they match destructive patterns) or
be Indeterminate. The "command" module is too dangerous for safe-listing.
Note: "shell" and "raw" modules are correctly NOT in S1's safe list.

### P0-3: gcloud-readonly-safe S1 missing `Not(delete)` allows destructive commands

**Location**: §5.5, lines 1156-1166

gcloud S1 uses `ArgContentRegex('\bdescribe\b')`, `ArgContentRegex('\blist\b')`,
`ArgContentRegex('\binfo\b')` to match readonly operations. These scan ALL
positional args. Unlike Azure's S1 which has `Not(ArgContentRegex('\bdelete\b'))`
as a defense, gcloud S1 has NO such exclusion.

A resource name containing "list", "describe", or "info" as a word causes the
safe pattern to match even for destructive commands:

- `gcloud compute instances delete list-all` → `\blist\b` matches in
  "list-all" → S1 matches → Allow. But this deletes an instance!
- `gcloud sql instances delete describe-db` → `\bdescribe\b` matches →
  S1 matches → Allow. But this deletes a database!
- `gcloud compute disks delete info-disk` → `\binfo\b` matches → Allow.

**Impact**: Destructive gcloud commands with certain resource names are
classified as safe. While intentional naming like this is unlikely, LLM agents
could generate resource names containing common words.

**Fix**: Add `Not(ArgContentRegex('\bdelete\b'))` to gcloud S1, matching Azure's
approach. Also consider adding `Not(ArgContentRegex('\bcreate\b'))` and
`Not(ArgContentRegex('\bupdate\b'))` for completeness.

---

## P1 — High

### P1-1: `pulumi up` vs `terraform apply` severity asymmetry

**Location**: §5.2.1 vs §5.1.1

`terraform apply` (without `-auto-approve`) is classified as Medium/ConfidenceHigh.
`pulumi up` (without `--yes`) is NOT classified as destructive at all. Both
commands:
- Can create, modify, or destroy infrastructure resources
- Prompt interactively for confirmation
- Are the primary "make changes" commands for their respective tools

The plan justifies this in §5.2.1: "Not classified as destructive because it
prompts for confirmation interactively." But `terraform apply` ALSO prompts for
confirmation, yet it IS classified as Medium.

**Impact**: Inconsistent treatment of functionally equivalent operations across
IaC tools. An LLM agent running `pulumi up` gets no warning while `terraform
apply` gets Ask/Medium.

**Fix**: Add `pulumi up` (without `--yes`) as Medium/ConfidenceLow, matching
`terraform apply`'s rationale. Add golden file entries for `pulumi up` without
`--yes`.

### P1-2: `terraform state push` not covered

**Location**: §5.1, §13 Q6

`terraform state push` overwrites the remote state file with a local copy. This
can cause:
- State corruption if local state is stale
- Duplicate resource management if multiple states track the same resources
- Resource orphaning if the pushed state is missing resources

This is clearly destructive and not covered by any pattern. The MQ3 manual QA
section acknowledges `terraform state push` needs review (line 973) but no
pattern exists.

**Fix**: Add a destructive pattern for `terraform state push` at Medium severity.
State push is less immediately destructive than state rm (it doesn't orphan
by itself) but can cause cascading issues.

### P1-3: `aws s3 mv` not classified

**Location**: §5.4

`aws s3 mv` moves/renames S3 objects. This is a copy-then-delete operation — the
source object is deleted after copying. Neither safe pattern S2 (which covers
ls, cp, sync, presign, mb) nor any destructive pattern matches `aws s3 mv`.

**Impact**: `aws s3 mv s3://prod-bucket/critical-data s3://other/` → Indeterminate.
This should be at least Medium since it deletes the source object.

**Fix**: Either add `aws s3 mv` to destructive patterns at Medium, or add it to
S2's safe list if the team considers moves non-destructive. Recommend Medium since
it involves deletion.

### P1-4: pulumi-up-yes D4 misplaced in "Medium" section but has High severity

**Location**: §5.2, lines 545-563

D4 `pulumi-up-yes` has `Severity: guard.High` but appears under the
`// ---- Medium ----` section comment. This is misleading for implementers who
follow the section headers.

**Fix**: Move D4 to the High section, between D3 (pulumi-stack-rm) and D5
(pulumi-cancel).

### P1-5: Missing golden entries for common safe terraform operations

**Location**: §6.1

S4 `terraform-readonly-safe` covers subcommands: validate, fmt, output, graph,
providers, version, workspace, state. The golden file only includes:
- `terraform validate` → Allow
- `terraform fmt` → Allow
- `terraform state list` → Allow

Missing entries that verify S4 coverage:
- `terraform output`
- `terraform state show resource`
- `terraform state pull`
- `terraform graph`
- `terraform providers`
- `terraform version`

Without these, a regression in S4 matching could go undetected for these
subcommands.

**Fix**: Add golden file entries for all S4-covered subcommands.

---

## P2 — Medium

### P2-1: ansible `ArgContent("file")` substring false positive in D2

**Location**: §5.3, lines 696-702

D2 `ansible-file-absent` uses `ArgContent("file")` which does substring matching.
Per plan 01 parsing, for `ansible all -m user -a 'name=profile state=absent'`,
Args would be ["all", "user", "name=profile state=absent"]. "profile" contains
"file" as substring → D2 matches (High) instead of D5 user-absent (Medium).

Other realistic false positives: "logfile", "dockerfile", "configfile" as
resource values.

**Impact**: Over-classification (High instead of Medium), not a false negative.
The command IS flagged as destructive, just by the wrong pattern at a higher
severity.

**Fix**: Use `ArgAt` to check specifically the module name position (position 1
in Args after plan 01 parsing: `ansible <hosts> <module-name>`), or use
`ArgContentRegex('^\bfile\b$')` to avoid substring matching. Note the module
name is the arg FOLLOWING `-m`, which plan 01 puts at a specific Args index.

### P2-2: `terraform workspace delete` coverage gap

**Location**: §13 Q6

`terraform workspace delete <name>` is clearly destructive — it permanently
removes a workspace and its associated state file. This is listed as a v2
item in §13 but represents a meaningful gap for LLM agents that use terraform
workspaces.

**Impact**: `terraform workspace delete staging` → S4 matches (workspace
is in the safe list) → Allow. An actually destructive command is classified
as safe.

**Fix**: Add Not(ArgContent("delete")) to S4's workspace handling, similar
to the Not(ArgContent("rm")) fix for state. Better yet, add a Medium destructive
pattern for `terraform workspace delete`.

### P2-3: AWS S1 `ArgContentRegex` matches all args, not position-specific

**Location**: §5.4, lines 845-852

AWS S1 uses `ArgContentRegex('^describe-')` which scans all positional args.
If a resource argument happened to start with "describe-" (e.g., a resource
named "describe-my-resource"), S1 would match even for non-readonly commands.

Azure S1 defends against this with `Not(ArgContentRegex('\bdelete\b'))`. AWS S1
has no such defense.

**Impact**: Very low practical risk (AWS resource IDs use formats like "i-abc123",
"sg-12345", etc. that never start with "describe-"). Theoretical concern only.

**Fix**: Add `Not(ArgContentRegex('^terminate-|^delete-|^stop-|^remove-'))` as
defense-in-depth, matching Azure's pattern.

### P2-4: `gsutil rsync` without `-d` classified as safe

**Location**: §5.5, lines 1180-1183

gsutil S2 includes `rsync` as safe with `Not(Flags("-d"))` exclusion. Without
`-d`, `gsutil rsync` is additive-only (copies new/changed files to destination
without deleting extra files at destination). This is technically safe but
`gsutil rsync /local gs://bucket/` could still overwrite existing objects at
the destination, which may be unexpected.

**Impact**: Acceptable over-permissiveness. rsync without -d is additive, not
destructive. Documenting as a known accept rather than a bug.

**Fix**: Add a comment in the plan noting that rsync without -d is intentionally
safe because it doesn't delete destination objects.

### P2-5: Ansible D1 redundant regex patterns

**Location**: §5.3, lines 677-682

D1 `ansible-shell-destructive` contains both:
- `ArgContentRegex('rm\s+-(r|rf|fr)')` — matches rm with specific flags
- `ArgContentRegex('rm\s+-')` — matches rm with ANY flags

The second regex is a strict superset of the first. Both produce the same
result (Critical severity). The first regex is redundant and wastes evaluation
time.

**Impact**: No functional impact, but unnecessary regex evaluation per match.

**Fix**: Remove `ArgContentRegex('rm\s+-(r|rf|fr)')` since
`ArgContentRegex('rm\s+-')` covers all cases.

---

## P3 — Low

### P3-1: gcloud S1 `ArgContentRegex` matches resource names for non-destructive commands

**Location**: §5.5, lines 1158-1161

Even after fixing P0-3 (adding Not(delete)), `\bdescribe\b` and `\blist\b` can
still match resource names for non-destructive commands. For example:
- `gcloud compute instances create describe-vm` → S1 matches → Allow

This isn't a safety issue (create is non-destructive), but it means the
"readonly-safe" pattern matches commands that are neither readonly nor safe —
they just happen to have a matching resource name. This could confuse users
reviewing guard decisions.

**Fix**: Consider using positional ArgAt matching for verbs at known positions
instead of scanning all args. For gcloud, the verb is typically at position 2
(e.g., `gcloud compute instances describe`). Add `ArgAt(2, "describe")`,
`ArgAt(2, "list")`, etc.

### P3-2: ansible `--module-name=command` long-flag-with-equals edge case

**Location**: §5.3, lines 618-632

If ansible is invoked with `--module-name=command` (long flag with `=`), plan 01
parsing puts "command" into `Flags["--module-name"]` NOT into Args. Since
`ArgContent("command")` only scans Args (not Flags), the safe pattern S1 would
not match. The command would fall through to destructive matching where the same
issue occurs — D1's `ArgContent("command")` also wouldn't match.

**Impact**: Extremely rare usage pattern. Nobody writes `--module-name=command`
in practice. If they did, the command would be Indeterminate (neither safe nor
destructive), which is conservative and acceptable.

### P3-3: Cross-cloud safe pattern structural inconsistency

**Location**: §5.4-5.6

The three cloud packs (AWS, GCP, Azure) use different matching strategies for
safe readonly operations:
- AWS: `ArgContentRegex('^describe-')` — prefix matching
- GCP: `ArgContentRegex('\bdescribe\b')` — word boundary matching
- Azure: `ArgContentRegex('\bshow\b')` + `Not(delete)` — word + exclusion

This inconsistency makes it harder to reason about correctness across packs and
makes cross-cloud comparison testing (O2 in test harness) less valuable since
the patterns use fundamentally different approaches.

**Fix**: Align all three to use the same matching strategy. Azure's approach
(word boundary + Not exclusion) is the most defensive.

### P3-4: Test harness MQ3 `terraform state push` noted but no conclusion

**Location**: Test harness MQ3, line 973

MQ3 lists `terraform state push` as "depends (not currently covered — review
if needed)". This should have a clear disposition (covered in P1-2 finding
above). The test harness manual QA should document expected behavior for
all listed commands.

**Fix**: Update MQ3 to state the expected behavior: `terraform state push` →
Ask/Medium (destructive, per P1-2).

---

## Cross-Reference Verification

### Shaping doc (§A8) pack scope alignment
All 6 packs match the shaping doc's Pack Scope table. ✓

### Plan 02 matcher DSL compatibility
- ArgAt(), Flags(), Name(), ArgContent(), ArgContentRegex(), And(), Or(), Not()
  — all used correctly per plan 02 definitions. ✓
- No use of matchers not defined in plan 02. ✓
- ArgContent scans cmd.Args only (not Flags) per plan 02 §5.2.4. Ansible patterns
  rely on plan 01's behavior of putting short-flag arg values into Args. ✓

### Plan 01 tree-sitter parsing dependency
- Short flags treated as boolean, next token is positional arg (plan 01
  §classifyArg) — critical for ansible pattern correctness. ✓
- Long flags with `=` parsed as key=value — edge case for `--module-name=command`
  noted in P3-2. ✓

### Plan 03a pack template alignment
- All packs follow the template structure from 03a §4. ✓
- All packs have Keywords, Safe, Destructive sections. ✓
- EnvSensitive on all destructive patterns (03c-specific URP). ✓

### Test harness coverage
- P1-P7 property tests, E1-E8 deterministic examples — adequate. ✓
- O1-O4 comparison/consistency oracles — thorough. ✓
- SEC1-SEC3 security tests — cover evasion, injection, escalation. ✓
- Missing: no property test for "safe pattern never matches a command containing
  a destructive subcommand verb" — would catch P0-3.
