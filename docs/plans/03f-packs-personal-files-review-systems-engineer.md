# Plan 03f: Personal Files Pack — Systems Engineer Review

**Reviewer**: dcg-reviewer (systems-engineer persona)
**Date**: 2026-03-02
**Documents reviewed**:
- `docs/plans/03f-packs-personal-files.md` (~603 lines)

**Focus areas**: Command-agnostic matching correctness, regex soundness, safe
pattern design, cross-pack interactions, detection completeness, and alignment
with plan 01 (ExtractedCommand) and plan 02 (matching framework).

**Cross-references consulted**: plan 01 (ExtractedCommand struct — fields,
redirect handling), plan 02 (AnyName, ArgContentRegex, safe-before-destructive,
keyword pre-filter word boundaries).

---

## Summary Assessment

The personal files pack introduces the first command-agnostic patterns using
`AnyName()` + `ArgContentRegex()`. The tiered severity approach (Critical for
deletion, High for modification, Medium for catch-all access) is sound. The
path regex covers the major Unix path forms correctly.

Main weaknesses: (1) the overview table claims D4 detects output redirects
to personal paths, but redirect targets are not in ExtractedCommand — this is
architecturally impossible, (2) the SSH S2 safe pattern includes
`authorized_keys`, allowing write operations to skip all destructive detection,
and (3) the SSH private key regex has a subtle over-match on `.pub` files via
its catch-all branch.

**Finding count**: 0 P0, 2 P1, 5 P2, 2 P3

---

## P1 Findings (High — Incorrect or incomplete, will cause detection failures)

### SE-P1.1: SSH S2 safe pattern allows write operations to `authorized_keys`

**Location**: 03f lines 359–372

S2 (`ssh-config-read`) marks `config`, `known_hosts`, and `authorized_keys`
as safe when the command is NOT `rm`, `mv`, `chmod`, `sed`, or `truncate`:

```go
Match: packs.And(
    packs.Not(packs.Or(
        packs.Name("rm"), packs.Name("mv"),
        packs.Name("chmod"), packs.Name("sed"),
        packs.Name("truncate"),
    )),
    packs.ArgContentRegex(sshConfigRe.String()),
),
```

Commands not in the exclusion list but that write/modify files include:
- `tee ~/.ssh/authorized_keys` — S2 matches (tee not excluded), SAFE
- `cp malicious_keys ~/.ssh/authorized_keys` — S2 matches (cp not excluded), SAFE
- `scp remote:key ~/.ssh/authorized_keys` — S2 matches (scp not excluded), SAFE

Writing to `authorized_keys` is a significant security concern — it controls
SSH authentication. S2 classifies these as safe, which means D1 and D2 are
never evaluated for these commands.

**Fix options**:
(a) Remove `authorized_keys` from `sshConfigRe`. It's a write-sensitive file,
    not a "config read" file. Create a separate safe pattern limited to
    `cat`/`less`/`head`/`grep`/`wc` for authorized_keys.
(b) Change S2 from negative matching (exclude destructive commands) to positive
    matching (enumerate read-only commands). This is more robust because new
    write commands won't accidentally pass through. E.g.:
    ```go
    Match: packs.And(
        packs.Or(
            packs.Name("cat"), packs.Name("less"),
            packs.Name("head"), packs.Name("tail"),
            packs.Name("grep"), packs.Name("wc"),
            packs.Name("file"), packs.Name("stat"),
        ),
        packs.ArgContentRegex(sshConfigRe.String()),
    ),
    ```
(c) Both (a) and (b) — recommended. Remove `authorized_keys` from S2 and
    switch to positive matching for the remaining config files.

### SE-P1.2: D4 table claims redirect detection, but redirects are not in ExtractedCommand

**Location**: 03f line 145 (table), lines 264–279 (code)

The overview table (line 145) says:
> D4 | Output redirect to personal path (matched via `ArgContentRegex`) | High

But the actual D4 code matches `sed`/`tee`/`dd` by command name, not output
redirects. More importantly, **redirect targets are architecturally invisible**
to the matching framework:

Plan 01 test (line 1308):
```
{"redirect", "echo hello > /tmp/out", []ExtractedCommand{{
    Name: "echo", Args: []string{"hello"},
}}}
```

The redirect target (`/tmp/out`) is NOT in `Args`, `RawArgs`, or any other
`ExtractedCommand` field. It lives in the tree-sitter `file_redirect` node
which the extractor does not capture.

This means commands like:
- `echo "overwrite" > ~/Documents/important.txt` — **undetected**
- `cat /dev/null > ~/Desktop/file.txt` — **undetected**
- `python script.py > ~/Downloads/output.csv` — **undetected**

For a "personal files protection" pack, missing redirect-based writes is a
significant gap. This is an architectural limitation of the ExtractedCommand
struct, not fixable within the pack alone.

**Fix**: Two-part:
1. **Immediate (plan 03f)**: Update the table to accurately reflect D4 as
   sed/tee/dd matching, NOT redirect detection. Document the redirect blind
   spot as a known limitation in §9 (Open Questions).
2. **Architectural (plan 01)**: Add a `Redirects []RedirectTarget` field to
   ExtractedCommand, with `RedirectTarget{Path string, Type string}` capturing
   `>`, `>>`, etc. This enables future redirect-aware pattern matching.

---

## P2 Findings (Medium — Weakens assurance or has design issues)

### SE-P2.1: sshPrivateKeyRe catch-all branch matches `.pub` files

**Location**: 03f lines 321–326

```go
var sshPrivateKeyRe = regexp.MustCompile(
    `(?:~|...)/\.ssh/` +
        `(?:id_(?:rsa|ed25519|ecdsa|dsa)` +   // named keys
        `|[^/]+)` +                             // ANY file in .ssh/
        `(?:[^.]|$)`,                           // "NOT ending in .pub"
)
```

Trace for `~/.ssh/id_rsa.pub`:
- Named branch: `id_rsa` matches, then `(?:[^.]|$)` sees `.` → fails ✓
- Catch-all branch: `[^/]+` greedily consumes `id_rsa.pub`, then `$` matches

So the catch-all `[^/]+` branch matches `.pub` files. The "NOT ending in .pub"
check only works for the named-key branch.

**Mitigation**: D2 has a separate `Not(ArgContentRegex(sshPublicKeyRe))` clause
that excludes `.pub` files. So D2 is functionally correct. But the regex
doesn't implement its documented intent, and if reused without the Not clause,
it would be wrong.

**Fix**: Use a negative lookahead or restructure the regex:
```
`(?:~|...)/\.ssh/[^/]+(?<!\.pub)(?:\s|$)`
```
Or simply document that the regex is intentionally over-broad and D2's Not
clause provides the `.pub` exclusion.

### SE-P2.2: D2 `cp` matches source-path personal files at inflated severity

**Location**: 03f lines 229–243

`ArgContentRegex` matches ALL arguments. For `cp ~/Documents/file.txt /tmp/`:
- `~/Documents/file.txt` matches `personalPathRe` → D2 triggers at **High**

But this command copies FROM the personal directory (read operation), not TO it.
The pattern name "personal-files-overwrite" and reason "may overwrite files"
are inaccurate for this case.

Without D2, the command would fall through to D5 (catch-all) at **Medium**,
which is more appropriate for a read operation.

**Fix**: Either (a) accept this as a known false-positive and document it, or
(b) for `cp` specifically, use `ArgAt(-1, regex)` to match only the last
argument (destination) — though this would need ArgAt to support regex matching.

### SE-P2.3: Overview table does not match code for D1 and D3

**Location**: 03f lines 142–146 (table) vs lines 207–262 (code)

| Pattern | Table commands | Code commands | Missing from table |
|---------|---------------|---------------|-------------------|
| D1 | rm, shred, srm | rm, shred, srm, **unlink** | unlink |
| D3 | chmod, chown, truncate | chmod, chown, **chgrp**, truncate, **touch** | chgrp, touch |
| D4 | "Output redirect" | sed, tee, dd | Entirely different |

The table is the primary reference for reviewers and consumers. Code-table
divergence causes confusion and makes review unreliable.

**Fix**: Update the table to match the code exactly.

### SE-P2.4: personal.files pack has empty Safe slice without design justification

**Location**: 03f lines 192–202

The Safe slice contains only comments but no actual patterns. Comments explain
that the regex limits false positives, but there's no explicit design note
documenting why zero safe patterns is intentional.

This is notable because other packs extensively use safe patterns, and a reader
might assume missing safe patterns is an oversight rather than a deliberate
choice.

**Fix**: Add a prominent design note (e.g., in §4 or §5) explaining:
- No safe patterns are needed because the path regex requires explicit home-
  prefix matching, eliminating most false positives
- Commands like `ls ~/Desktop/` intentionally trigger at Medium (the threat
  model is about flagging scope creep, not just destructive operations)
- If safe patterns are needed in the future, they should be added as
  command-specific exceptions (e.g., `ls` read-only safe pattern)

### SE-P2.5: SSH S2 negative-match approach is fragile beyond `authorized_keys`

**Location**: 03f lines 362–371

Even after fixing SE-P1.1 by removing `authorized_keys`, S2's approach of
excluding specific commands (`rm`, `mv`, `chmod`, `sed`, `truncate`) means
any newly relevant write command passes through as safe. For example:
- `perl -pi -e 's/old/new/' ~/.ssh/config` — safe (perl not excluded)
- `python -c "..." > ~/.ssh/config` — but redirects are invisible anyway

The positive-match approach from SE-P1.1 fix option (b) addresses this for
all config files, not just authorized_keys.

---

## P3 Findings (Low — Polish, minor gaps)

### SE-P3.1: Golden file entries are sparse

**Location**: 03f lines 535–579

9 golden entries for 7 patterns across 2 packs. Missing coverage:
- No `$HOME/Desktop` or `/Users/*/Desktop` path variants for destructive
  patterns (only tilde form tested in golden files)
- No iCloud path (`~/Library/Mobile Documents/...`) with destructive command
- No SSH with `/home/user/.ssh/` path variant
- No cross-pack interaction entry (e.g., `rm ~/Desktop/file.txt` which matches
  both personal.files D1 and core.filesystem)
- No `cp -n` no-clobber safe case for D2 safe exclusion

**Fix**: Expand to ~25–30 golden entries covering all path variants (use the
`personalPathVariants` helper from §7.1), all safe pattern cases, cross-pack
interactions, and false-positive traps (e.g., `cat README.md` containing the
word "Documents" in text).

### SE-P3.2: `touch` in D3 at High severity may be over-aggressive

**Location**: 03f lines 254, 257

`touch ~/Documents/file.txt` triggers at High severity via D3. But `touch`
has two behaviors:
1. Create a new file — potentially concerning (writes to personal dir)
2. Update timestamps on existing file — benign

Most `touch` usage is for timestamp manipulation or creating empty sentinel
files. High severity means Deny under InteractivePolicy, which may be overly
restrictive.

**Recommendation**: Consider moving `touch` to D5 catch-all (Medium → Ask
under InteractivePolicy) or creating a separate D4.5 pattern at Medium for
`touch`. Alternatively, accept the over-flagging as consistent with the
pack's threat model (flag everything, user confirms).

---

## Cross-Plan Consistency Checks

| Check | Result |
|-------|--------|
| AnyName() exists in plan 02 | **Consistent** — §5.2.8 defines AnyNameMatcher, always returns true |
| ArgContentRegex in plan 02 | **Consistent** — §5.2.7 ArgContentMatcher supports regex on args |
| Keyword pre-filter with "Desktop" etc. | **Consistent** — plan 02 Aho-Corasick with word-boundary post-filter; `/` is non-word character so `~/Desktop/file` triggers on `Desktop` |
| "Mobile Documents" multi-word keyword | **Consistent** — Aho-Corasick finds substring "Mobile Documents", word-boundary checks char before "M" and after "s" |
| Safe-before-destructive per pack | **Consistent** — plan 02 §5.4: if safe matches, skip destructives in that pack |
| Redirect targets in ExtractedCommand | **Gap** — plan 01 does NOT include redirect targets in ExtractedCommand fields; ArgContentRegex cannot detect redirect-based file access |
