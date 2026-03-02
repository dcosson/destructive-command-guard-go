# Review: 03c-packs-infra-cloud (domain-packs-r2)

- Source doc: `docs/plans/03c-packs-infra-cloud.md`
- Reviewed commit: fb3fa18
- Reviewer: domain-packs-r2
- Round: 2

## Findings

### P1 - Ansible pack patterns (D1-D5, D7, S1) use ArgContent for flag value matching, but ArgContent only checks cmd.Args

**Problem**
In §5.3 (lines 662-843), the Ansible pack's patterns rely on `ArgContent` and `ArgContentRegex` to match module names and module arguments that appear in flag values (`-m`, `-a`, `--extra-vars`). For example:

- D1 `ansible-shell-destructive` (line ~717): Uses `packs.ArgContent("shell")` to detect the shell module and `packs.ArgContentRegex('rm\s+-')` to detect destructive commands
- D2 `ansible-file-absent` (line ~741): Uses `packs.ArgContent("file")` and `packs.ArgContent("state=absent")`
- D4 `ansible-playbook-destructive-vars` (line ~771): Uses `packs.ArgContent("state=absent")` etc.
- S1 `ansible-gather-safe` (line ~669): Uses `packs.ArgContent("ping")`, `packs.ArgContent("stat")`, etc.

For a command like `ansible all -m file -a 'path=/tmp state=absent'`, the extracted command would be:
- Name: "ansible", Args: ["all"], Flags: {"-m": "file", "-a": "path=/tmp state=absent"}

However, plan 02's `ArgContentMatcher.Match()` (02-matching-framework.md lines 606-625) **only iterates over `cmd.Args`** — it does not check flag values:

```go
for _, arg := range cmd.Args {
    if check(arg) {
        return true
    }
}
return false
```

And 03b-packs-database.md §4.4 (line 182) explicitly confirms: "Plan 02's `ArgContentMatcher` currently only checks `cmd.Args`." The database pack solved this by proposing a `CheckFlagValues bool` extension and wrapping it in `SQLContent()`.

Since the Ansible module name (`file`, `shell`, etc.) is in `Flags["-m"]` and the module arguments (`state=absent`, `rm -rf /`, etc.) are in `Flags["-a"]`, `ArgContent` cannot reach these values. This means:

- **D1** (ansible-shell-destructive): ArgContent("shell") won't match → pattern never fires
- **D2** (ansible-file-absent): ArgContent("file") and ArgContent("state=absent") won't match → pattern never fires
- **D3** (ansible-service-stopped): Same issue with ArgContent("service") and ArgContent("state=stopped")
- **D4** (ansible-playbook-destructive-vars): ArgContent("state=absent") won't match Flags["--extra-vars"]
- **D5** (ansible-user-absent): ArgContent("user") and ArgContent("state=absent") won't match
- **D7** (ansible-package-absent): Same issue
- **S1** (ansible-gather-safe): ArgContent("ping"/"debug"/"stat"/"setup") won't match Flags["-m"]

Only **D6** (ansible-playbook-run) and **S2** (ansible-dryrun-safe) work correctly because they use `Name()` and `Flags()` matchers that don't depend on ArgContent.

The golden file entries for Ansible (§6.3) would all produce incorrect results — commands like `ansible all -m shell -a 'rm -rf /' → Deny/Critical` would actually be Indeterminate.

**Required fix**
All Ansible patterns that need to match flag values must use the `CheckFlagValues` extension from 03b §4.4. Options:
1. Use `ArgContentMatcher{Substring: "file", CheckFlagValues: true}` via a new builder (e.g., `ArgContentFlags("file")`)
2. Use the existing `SQLContent()` helper if case-insensitivity is acceptable (it adds `(?i)` prefix)
3. Create an Ansible-specific helper analogous to `SQLContent` that enables `CheckFlagValues: true` without case-insensitivity

Additionally, §5.3.1 (line 870) must be corrected — the note "Both `ArgContent("file")` and `ArgContent("state=absent")` match against all argument and flag values" is factually incorrect per plan 02.

---

### P2 - §5.3.1 note claims ArgContent matches "all argument and flag values" — contradicts plan 02

**Problem**
In §5.3.1 (line ~870), the Ansible notes state:

> Both `ArgContent("file")` and `ArgContent("state=absent")` match against all argument and flag values. This is intentional — we want broad matching rather than requiring exact flag-value position matching.

This directly contradicts plan 02 §5.2.4, where `ArgContentMatcher.Match()` only iterates over `cmd.Args`. It also contradicts 03b §4.4, which explicitly identifies this limitation and proposes the `CheckFlagValues` extension as the solution.

This note likely caused the Ansible patterns to be written with `ArgContent` instead of a flag-value-aware variant — it's the root cause documentation error behind the P1 finding above.

**Required fix**
Correct the note to accurately describe `ArgContent` behavior: "ArgContent matches against cmd.Args only. To match flag values (-m, -a, --extra-vars), the patterns must use an ArgContent variant with CheckFlagValues: true (see 03b §4.4)."

---

### P2 - §5.1.1 Terraform import note misleadingly says "Classified as safe"

**Problem**
In §5.1.1 (line ~446), the note states:

> **`terraform import`**: Classified as safe (S4's state subcommand handling allows `state list` and `state show`). Import adds resources to state — it doesn't destroy anything.

However, no safe pattern matches `terraform import`. S4 (terraform-readonly-safe) matches `ArgAt(0, "state")`, `ArgAt(0, "workspace")`, `ArgAt(0, "validate")`, etc. — but "import" is not in S4's `Or` clause. `terraform import` would produce `ArgAt(0) = "import"`, which doesn't match S1 (plan), S2 (show), S3 (init), or S4 (readonly).

The golden file entries in §6.1 also don't include `terraform import`. So `terraform import` actually falls through as **Indeterminate**, not safe.

The note is misleading — it suggests the pattern system classifies import as safe when it doesn't.

**Required fix**
Either:
(a) Add "import" to S4's `Or` clause (since import is genuinely safe — it doesn't modify infrastructure), OR
(b) Correct the note to say "`terraform import` is Indeterminate (no pattern match). This is acceptable — import doesn't destroy infrastructure, and Indeterminate prompts the user for confirmation which is conservative."

---

### P3 - terraform workspace delete (Medium) vs pulumi stack rm (High) severity asymmetry

**Problem**
In §5.1, `terraform-workspace-delete` (D8, line ~413) is Medium severity. In §5.2, `pulumi-stack-rm` (D3, line ~567) is High severity.

Both operations delete a "container" of infrastructure state:
- `terraform workspace delete` permanently removes a workspace and its state file, orphaning any resources still managed by that workspace
- `pulumi stack rm` permanently deletes a stack and its deployment history, orphaning any resources still in the stack

The consequences are nearly identical (state loss, resource orphaning), but the severities differ by one level. The plan doesn't explain why workspace deletion is less severe than stack removal.

The auto-approve severity split pattern (Terraform destroy: High→Critical, Pulumi destroy: High→Critical) shows the plan intends cross-tool parity for equivalent operations. The workspace/stack deletion asymmetry breaks this parity.

**Required fix**
Either align both to the same severity (High seems appropriate for permanent state deletion that orphans resources) or add a design note explaining the severity difference (e.g., Terraform workspaces are typically less stateful than Pulumi stacks).

---

## Summary

4 findings: 0 P0, 1 P1, 2 P2, 1 P3

**Verdict**: Approved with revisions

The plan is well-structured with strong cross-cloud consistency, comprehensive golden file coverage (132 entries), and thorough R1 incorporation (28 of 33 findings). The P1 finding about Ansible `ArgContent` not reaching flag values is the most critical — it renders 5 of 7 destructive patterns and the sole gathering safe pattern non-functional. The fix is straightforward (use `CheckFlagValues` extension from 03b §4.4) but must be applied before implementation. The remaining patterns for Terraform, Pulumi, AWS, GCP, and Azure are correctly specified using `ArgAt()` positional matching and don't have this issue.
