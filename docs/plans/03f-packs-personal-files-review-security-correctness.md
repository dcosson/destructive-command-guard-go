# 03f: Personal Files Pack — Security/Correctness Review

**Reviewer**: dcg-alt-reviewer (independent review)
**Plan**: [03f-packs-personal-files.md](./03f-packs-personal-files.md)
**Focus**: Regex correctness, safe pattern coverage, false positive risk,
cross-pack interactions, AnyName matcher security implications.

---

## Findings Summary

| ID | Severity | Title |
|----|----------|-------|
| PF-P0.1 | P0 | sshPrivateKeyRe catch-all `[^/]+` matches .pub files — regex .pub exclusion broken |
| PF-P1.1 | P1 | `personal.files` has zero safe patterns — all personal path commands flagged at Medium minimum |
| PF-P1.2 | P1 | `personal.ssh` S2 excludes only 3 filenames — all other .ssh files treated as private keys |
| PF-P1.3 | P1 | `touch` in D3 at High severity — extremely common build/dev operation, high false positive risk |
| PF-P2.1 | P2 | Case-sensitive regex on case-insensitive filesystem (macOS HFS+) |
| PF-P2.2 | P2 | D2 Not(Flags("-n")) falls through to D5 Medium — no safe pattern for no-clobber operations |
| PF-P2.3 | P2 | osascript script file invocation not handled (only -e inline) |
| PF-P2.4 | P2 | Golden file entries sparse — 9 entries for 7 patterns, below 3-per-pattern target |
| PF-P2.5 | P2 | Cross-pack interaction: personal.files D5 overrides other pack safe patterns |
| PF-P3.1 | P3 | Pack description says "cross-platform" but Windows paths are deferred |
| PF-P3.2 | P3 | Path variant generator missing /root/ for root user on Linux |
| PF-P3.3 | P3 | No test for commands with both personal and non-personal path arguments |

---

## P0 — Security-Critical

### PF-P0.1: sshPrivateKeyRe catch-all `[^/]+` matches .pub files — regex .pub exclusion broken

**Location**: §5.2 (sshPrivateKeyRe, lines 321-326)

**Issue**: The regex intends to match SSH private keys but NOT `.pub` files.
The `.pub` exclusion is implemented as `(?:[^.]|$)` after the filename match.
This works for named key alternatives (`id_rsa`, `id_ed25519`, etc.) but
fails for the catch-all `[^/]+` alternative due to regex backtracking.

Trace for `~/.ssh/id_ed25519.pub`:
1. Path prefix matches: `~/.ssh/`
2. First alternative: `id_(?:rsa|ed25519|...)` matches `id_ed25519`
3. `(?:[^.]|$)` sees `.` next — fails
4. **Regex backtracks to second alternative**: `[^/]+` greedily matches
   `id_ed25519.pub`
5. `(?:[^.]|$)` sees end of string or whitespace — **matches**

Result: `~/.ssh/id_ed25519.pub` matches sshPrivateKeyRe as a "private key."

The D2 matcher compensates with `Not(ArgContentRegex(sshPublicKeyRe))`,
and S1 catches public keys as safe first. So the runtime behavior is
correct today. But the regex alone is semantically wrong, and if either
S1 or the D2 Not clause is removed (e.g., during refactoring), public
key access would be flagged as private key access at High severity.

**Recommendation**: Fix the regex to use a negative lookahead instead of
the broken `(?:[^.]|$)`:

```
`(?:~|...)/\.ssh/(?:id_(?:rsa|ed25519|ecdsa|dsa)|[^/\s]+)(?!\.pub)`
```

Or split into two separate regexes: one for named keys (no catch-all),
one for the catch-all that explicitly excludes `.pub` and known config files.

---

## P1 — Correctness Risks

### PF-P1.1: `personal.files` has zero safe patterns — all personal path commands flagged at Medium minimum

**Location**: §5.1 (lines 192-197)

**Issue**: The `personal.files` pack has NO safe patterns. The S1 and S2
comments explain why they aren't needed, but the result is that EVERY
command touching a personal directory gets flagged at minimum Medium
severity (via D5 catch-all).

This includes benign, common operations:
- `ls ~/Desktop/` → Medium (just listing directory contents)
- `stat ~/Documents/file.txt` → Medium (reading file metadata)
- `file ~/Downloads/archive.zip` → Medium (identifying file type)
- `wc -l ~/Documents/data.csv` → Medium (counting lines)
- `find ~/Documents -name "*.py"` → Medium (searching for files)
- `du -sh ~/Downloads/` → Medium (checking disk usage)

For a coding agent asked "help me organize my Downloads folder" or
"find all Python files in my Documents," the agent would get flagged on
every single command. Under InteractivePolicy, Medium → Ask, meaning
the user must approve each one individually.

**Recommendation**: Add safe patterns for read-only / metadata operations:

```go
Safe: []packs.SafePattern{
    {
        Name: "personal-files-readonly",
        Match: packs.And(
            packs.Or(
                packs.Name("ls"),
                packs.Name("stat"),
                packs.Name("file"),
                packs.Name("du"),
                packs.Name("find"),
                packs.Name("wc"),
                packs.Name("head"),
                packs.Name("tail"),
                packs.Name("cat"),
                packs.Name("less"),
                packs.Name("more"),
                packs.Name("bat"),
            ),
            packs.ArgContentRegex(personalPathRe.String()),
        ),
    },
},
```

Or alternatively, raise the D5 catch-all to only flag write-capable commands
while leaving read-only commands as Allow.

Note: this is a design choice between safety and usability. If the intent
is to flag ALL access (including reads), document this explicitly and
consider adding an "initial permission grant" UX pattern.

### PF-P1.2: `personal.ssh` S2 excludes only 3 filenames — all other .ssh files treated as private keys

**Location**: §5.2 (sshConfigRe, line 335; D2 catch-all via sshPrivateKeyRe)

**Issue**: S2 (ssh-config-read) only marks `config`, `known_hosts`, and
`authorized_keys` as safe. All other files in `~/.ssh/` that aren't `.pub`
are matched by sshPrivateKeyRe's `[^/]+` catch-all and flagged by D2
(ssh-private-key-access) at High severity.

Files incorrectly classified as private keys:
- `~/.ssh/known_hosts.old` → High (backup of known hosts)
- `~/.ssh/environment` → High (SSH environment file)
- `~/.ssh/rc` → High (SSH login script)
- `~/.ssh/agent.sock` → High (SSH agent socket)
- `~/.ssh/sshd_config` → High (server config, unlikely but possible)

These are non-sensitive configuration/operational files that an agent
might legitimately need to read or modify.

**Recommendation**: Either:
(a) Expand S2 to include common non-key files:
```go
sshConfigRe = regexp.MustCompile(
    `...\.ssh/(?:config|known_hosts(?:\.old)?|authorized_keys|environment|rc|agent\.sock)...`
)
```
Or (b) Change sshPrivateKeyRe to only match known key filenames (remove
`[^/]+` catch-all), accepting the trade-off that custom-named keys won't
be caught.

### PF-P1.3: `touch` in D3 at High severity — extremely common build/dev operation

**Location**: §5.1 D3 (line 254)

**Issue**: `touch` is included in the D3 "personal-files-modify" pattern
at High severity. But `touch` is one of the most commonly used commands
in build systems and development workflows — it's used to update timestamps,
create empty files, and trigger rebuild detection.

Problematic false positive: If an agent creates a build artifact in a
user's Desktop (e.g., `touch ~/Desktop/build-complete.flag`), this gets
flagged at High severity as "modifying personal file attributes." Under
InteractivePolicy (High → Deny), the command is blocked entirely.

While `touch` CAN modify timestamps, it's far less destructive than
`chmod`, `chown`, or `truncate` (the other commands in D3). `truncate`
empties file contents; `chmod`/`chown` can break file access. `touch`
only updates the timestamp, which is effectively a no-op for data integrity.

**Recommendation**: Remove `touch` from D3. If it must be flagged, put it
in D5 (catch-all at Medium) by letting it fall through, or create a
separate pattern for `touch` at Medium severity.

---

## P2 — Coverage Gaps

### PF-P2.1: Case-sensitive regex on case-insensitive filesystem (macOS HFS+)

**Location**: §5.1 (personalPathRe), §6.1 (test cases line 467)

**Issue**: The personalPathRe regex is case-sensitive. The test cases
explicitly verify `~/desktop/file.txt` → no match (line 467). But macOS's
HFS+ and APFS filesystems are case-insensitive by default. An agent
accessing `~/desktop/file.txt` is accessing the same directory as
`~/Desktop/file.txt`.

A malicious or confused agent could bypass detection by using lowercase
directory names. While this isn't a realistic attack vector (the threat
model is mistake prevention, not adversarial bypass), it's a correctness
gap for the stated goal.

**Recommendation**: Make the directory name match case-insensitive:
```
(?i:Desktop|Documents|Downloads|Pictures|Music|Videos)
```
Or document explicitly that case-sensitive matching is a known limitation.

### PF-P2.2: D2 Not(Flags("-n")) falls through to D5 Medium — no safe pattern for no-clobber operations

**Location**: §5.1 D2 (line 237)

**Issue**: `cp -n file.txt ~/Documents/` doesn't match D2 (because of
`Not(Flags("-n"))`), but falls through to D5 catch-all at Medium. The
user gets prompted for a no-clobber copy operation that can't overwrite
anything.

Similarly, `mv -n` and `cp --no-clobber` fall through to Medium.

**Recommendation**: Add no-clobber variants to a safe pattern, or
explicitly document that even safe-flagged operations still trigger
at Medium due to the catch-all design.

### PF-P2.3: osascript script file invocation not handled

**Location**: §4.1 (mentioned in osascript variants), not in 03f directly
but relevant to personal.ssh/files interaction

**Issue**: This is noted as a known gap in 03g Q2 but also relevant here:
`osascript ~/Documents/script.scpt` would be caught by personal.files D5
(accessing a personal path) but NOT by the communication pack's specific
pattern (which only checks -e inline script content). The resulting
Medium severity may be inadequate if the script sends messages.

**Recommendation**: Note the cross-pack dependency — personal.files D5
provides a safety net for this gap, but at the wrong severity. Consider
documenting this explicitly.

### PF-P2.4: Golden file entries sparse — 9 entries for 7 patterns, below 3-per-pattern target

**Location**: §8 (Golden File Entries)

**Issue**: Plan 05 requires 3+ golden entries per pattern (match, near-miss,
safe variant). The 03f pack has 9 entries total for 7 patterns (5 files +
2 ssh), averaging 1.3 per pattern. Most patterns have only 1 golden entry.

D3 (personal-files-modify), D4 (personal-files-write), and SSH S2
(ssh-config-read) have zero golden entries.

**Recommendation**: Expand to minimum 21 entries (3 × 7 patterns) before
implementation. Each should include a match, a near-miss (command that
looks similar but shouldn't match), and a safe variant.

### PF-P2.5: Cross-pack interaction: personal.files D5 overrides other pack safe patterns

**Location**: §7.2 (Cross-Pack Interaction Tests)

**Issue**: The plan correctly notes that `rm ~/Desktop/file.txt` matches
both personal.files and core.filesystem, with the highest severity
winning. But it doesn't address the reverse case: a command that another
pack marks as SAFE but personal.files D5 flags as destructive.

Example: `git clone https://github.com/foo/bar ~/Documents/repo`
- `core.git` may have a safe pattern for `git clone`
- `personal.files` D5 flags it at Medium (accesses personal path)
- Pipeline returns Medium (destructive from any pack overrides safe from
  other packs)

The user would be prompted for `git clone` into Documents, which may
or may not be desired. This interaction should be tested and documented.

**Recommendation**: Add specific cross-pack interaction test cases and
document the behavior: "Safe patterns only apply within their own pack.
A command can be safe in one pack but destructive in another, and the
destructive result takes precedence."

---

## P3 — Minor / Improvements

### PF-P3.1: Pack description says "cross-platform" but Windows paths are deferred

**Location**: §1, §9 Q1

**Issue**: Summary says "The pack is **cross-platform** and covers macOS,
Linux, and Windows personal directories." But Q1 defers Windows path
support. The regex only covers Unix-style paths.

**Recommendation**: Update the summary to say "macOS and Linux" or add
Windows regex variants before claiming cross-platform.

### PF-P3.2: Path variant generator missing /root/ for root user on Linux

**Location**: §7.1 (personalPathVariants)

**Issue**: The variant generator produces `~/`, `$HOME/`, `/Users/testuser/`,
`/home/testuser/` variants. On Linux, the root user's home is `/root/`,
not `/home/root/`. The regex uses `/home/[^/]+` which wouldn't match
`/root/Desktop`. While running as root is unlikely for a coding agent,
it's a coverage gap.

**Recommendation**: Add `/root/` to the regex as an alternative:
```
/(?:Users|home)/[^/]+|/root
```

### PF-P3.3: No test for commands with both personal and non-personal path arguments

**Location**: §6.1

**Issue**: All test cases have a single path argument. No test verifies
behavior when a command has both personal and non-personal paths:
`cp /tmp/file.txt ~/Desktop/file.txt` — does ArgContentRegex match on
the second argument? It should, since the regex scans all argument content.

**Recommendation**: Add a test case: `cp /tmp/file.txt ~/Desktop/file.txt`
→ High (D2, personal path in arguments).
