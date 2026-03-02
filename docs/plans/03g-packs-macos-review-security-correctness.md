# 03g: macOS System Pack — Security/Correctness Review

**Reviewer**: dcg-alt-reviewer (independent review)
**Plan**: [03g-packs-macos.md](./03g-packs-macos.md)
**Focus**: AppleScript detection coverage, keychain/privacy path correctness,
system command safe/destructive classification, build tag implications for
testing, cross-pack interactions with personal.files.

---

## Findings Summary

| ID | Severity | Title |
|----|----------|-------|
| MO-P0.1 | P0 | S2 osascript-finder-benign has no Not clauses — shadows D1/D2/D3 for multi-tell scripts |
| MO-P0.2 | P0 | S3 diskutil-info `ArgAt(0, "apfs")` matches `diskutil apfs deleteContainer` as safe |
| MO-P1.1 | P1 | D5 nvram-write uses flag exclusion — `nvram variable=value` (no flags) matches, but `nvram -d variable` (delete) also matches correctly; however `nvram -c` (clear ALL) is only caught by absence of -p/-x/--print |
| MO-P1.2 | P1 | D7 dscl-delete uses ArgContent not ArgAt — matches `-delete` anywhere in arguments, could false-positive on paths containing "delete" |
| MO-P1.3 | P1 | JXA bypass: `osascript -l JavaScript -e 'Application("Messages").send(...)'` evades all communication patterns |
| MO-P2.1 | P2 | D6 automator-run matches bare command name with no subcommand — `automator --help` is flagged High |
| MO-P2.2 | P2 | macosPrivateDataRe missing Photos Library path listed in §4.2 table |
| MO-P2.3 | P2 | macos.privacy Keywords list doesn't include `sqlite3` despite §1 table saying it does |
| MO-P2.4 | P2 | D5 mdfind at Medium with ConfidenceLow — but mdfind can search personal file CONTENTS |
| MO-P2.5 | P2 | Build tag test isolation means no CI coverage on Linux — test strategy underspecified |
| MO-P3.1 | P3 | Golden file entries for macos.system are sparse — 4 entries for 14 patterns |
| MO-P3.2 | P3 | `open -a` command deferred (Q3) — but `open -a Terminal` could escalate privileges |
| MO-P3.3 | P3 | No test for osascript with multiple -e flags combining safe and dangerous scripts |
| MO-P3.4 | P3 | launchctl `kickstart` and `kill` subcommands missing from D3 |

---

## P0 — Security-Critical

### MO-P0.1: S2 osascript-finder-benign has no Not clauses — shadows D1/D2/D3 for multi-tell scripts

**Location**: §5.1 (S2, lines 239-245)

**Issue**: The S2 safe pattern matches osascript commands containing
`tell application "Finder" to (get|open folder|reveal)`. But it has
NO Not clauses to exclude scripts that ALSO contain Messages, Mail,
or System Events tell blocks.

A multi-tell AppleScript can target both Finder and Messages:
```
osascript -e 'tell application "Finder" to reveal folder "Documents"' \
  -e 'tell application "Messages" to send "found it" to buddy "John"'
```

This command:
- S2 matches: contains `tell application "Finder" to reveal`
- D1 matches: contains `tell application "Messages"`
- **S2 is evaluated first** (safe-before-destructive ordering)
- S2 triggers → pack short-circuits → D1 is never checked
- **Result: iMessage sending command is classified as SAFE**

This is the same class of safe-pattern-shadowing bug found in vault S2
(03e-P0.1), kubectl S4 (03d), and helm S2 (03d).

**Recommendation**: Add Not clauses to S2 excluding dangerous app targets:

```go
{
    Name: "osascript-finder-benign",
    Match: packs.And(
        packs.Name("osascript"),
        packs.ArgContentRegex(osascriptFinderBenignRe.String()),
        packs.Not(packs.ArgContentRegex(osascriptMessagesRe.String())),
        packs.Not(packs.ArgContentRegex(osascriptMailRe.String())),
        packs.Not(packs.ArgContentRegex(osascriptSystemEventsRe.String())),
        packs.Not(packs.ArgContentRegex(osascriptSensitiveAppsRe.String())),
    ),
},
```

Note: S1 (osascript-display) already has these Not clauses for Messages,
Mail, and System Events — but is missing the sensitive apps exclusion.
S1 should also add `Not(ArgContentRegex(osascriptSensitiveAppsRe))`.

### MO-P0.2: S3 diskutil-info `ArgAt(0, "apfs")` matches destructive apfs subcommands as safe

**Location**: §5.3 (S3 diskutil-info, lines 539-553)

**Issue**: S3 marks `diskutil apfs` as safe. The safe pattern matches:
```go
packs.Or(
    packs.ArgAt(0, "info"),
    packs.ArgAt(0, "list"),
    packs.ArgAt(0, "apfs"),  // apfs list is safe
),
```

The comment says "apfs list is safe" but `ArgAt(0, "apfs")` matches ANY
diskutil apfs command, including:
- `diskutil apfs deleteContainer disk3` → **matched as safe** (should be Critical)
- `diskutil apfs deleteVolume disk3s2` → **matched as safe** (should be Critical)
- `diskutil apfs resizeContainer disk3 0b` → **matched as safe** (shrinks to zero)

The S3 Not clause only excludes `eraseDisk`, `eraseVolume`, `partitionDisk`
— these are top-level diskutil subcommands, not apfs subcommands. The
destructive apfs subcommands are at ArgAt(1) depth and are not excluded.

**Recommendation**: Remove `ArgAt(0, "apfs")` from S3. Instead, create
specific safe patterns for safe apfs operations:

```go
// S3a: diskutil apfs list is safe
{
    Name: "diskutil-apfs-list",
    Match: packs.And(
        packs.Name("diskutil"),
        packs.ArgAt(0, "apfs"),
        packs.ArgAt(1, "list"),
    ),
},
```

And add destructive apfs patterns to D2:
```go
packs.Or(
    packs.ArgAt(0, "eraseDisk"),
    packs.ArgAt(0, "eraseVolume"),
    packs.ArgAt(0, "partitionDisk"),
    packs.ArgAt(0, "secureErase"),
    packs.And(packs.ArgAt(0, "apfs"), packs.Or(
        packs.ArgAt(1, "deleteContainer"),
        packs.ArgAt(1, "deleteVolume"),
        packs.ArgAt(1, "resizeContainer"),
    )),
),
```

---

## P1 — Correctness Risks

### MO-P1.1: nvram-write relies on flag exclusion — `nvram -c` (clear ALL NVRAM) caught only indirectly

**Location**: §5.3 D5 nvram-write (lines 628-639)

**Issue**: D5 detects nvram writes by excluding read-only flags (`-p`, `-x`,
`--print`). This means `nvram -c` (which clears ALL firmware variables —
an extremely destructive operation) is caught, but only because `-c` is
not in the exclusion list.

The issue is that the pattern logic is "anything that's NOT a read is a
write," which is fragile:
- If someone adds `-c` to the exclusion list (thinking it's "clear display"
  or similar), NVRAM clearing becomes safe
- `nvram -f file` (restore from file) is caught, but could be misunderstood
- The pattern makes no distinction between `nvram boot-args="-v"` (setting
  one variable, recoverable) and `nvram -c` (clearing everything, potentially
  bricking the system)

**Recommendation**: Add explicit destructive nvram operations at Critical
severity, independent of the flag exclusion:

```go
// Explicit destructive NVRAM operations
{
    Name: "nvram-clear",
    Match: packs.And(
        packs.Name("nvram"),
        packs.Flags("-c"),
    ),
    Severity: guard.Critical,
    Reason: "nvram -c clears ALL firmware variables — can affect boot",
},
```

And keep the current pattern as a catch-all for less-obvious writes.

### MO-P1.2: dscl-delete uses ArgContent not ArgAt — false positive risk on paths containing "delete"

**Location**: §5.3 D7 dscl-delete (lines 659-673)

**Issue**: D7 uses `packs.ArgContent("-delete")` and `packs.ArgContent("delete")`
instead of `packs.ArgAt(position, value)`. ArgContent matches the string
anywhere in any argument.

`dscl` syntax is: `dscl <datasource> <command> <path> [value]`
- `dscl . -delete /Users/testuser` → D7 matches on `-delete` → correct
- `dscl . -read /Users/testuser` → no match → correct
- `dscl . -read /Users/to-delete-later` → D7 matches on `delete` in
  the path argument → **false positive**

The bare `delete` (without dash) in ArgContent is especially problematic
because it can appear in any argument value (usernames, paths, group names).

**Recommendation**: Use `ArgAt(1, "-delete")` and `ArgAt(1, "-create")`
instead of ArgContent. The dscl command is at position 1 (after the
datasource argument at position 0 — wait, actually dscl uses
`dscl <datasource> <command>` so the command is at ArgAt(1) if datasource
is `.` at ArgAt(0)).

Actually, verify the exact argument positions for dscl. The correct fix
depends on whether the datasource (`.` or `/LDAPv3/...`) is extracted as
ArgAt(0) or as a flag.

### MO-P1.3: JXA bypass — JavaScript for Automation evades all communication patterns

**Location**: §4.1, §9 Q1

**Issue**: All communication pack patterns match AppleScript syntax
(`tell application "..."`) via regex. JavaScript for Automation (JXA)
uses completely different syntax:

```bash
osascript -l JavaScript -e 'var messages = Application("Messages");
messages.send("hello", {to: messages.buddies.byName("John")})'
```

This sends an iMessage but does NOT match any of D1-D4 because:
- D1 checks for `tell\s+application\s+"Messages"` — not present in JXA
- The command name is still `osascript` and the keyword `osascript` triggers
  the pre-filter, but no destructive pattern matches

Q1 acknowledges this as a v2 item. However, this is a real bypass vector
on macOS. An agent using JXA syntax (which LLMs are capable of generating)
completely evades detection for message sending, email sending, and
System Events automation.

**Recommendation**: For v1, add a catch-all pattern for JXA that flags
ANY osascript with `-l JavaScript` at Medium severity:

```go
{
    Name: "osascript-jxa-catchall",
    Match: packs.And(
        packs.Name("osascript"),
        packs.Flags("-l"),
        packs.ArgContent("JavaScript"),
    ),
    Severity:   guard.Medium,
    Confidence: guard.ConfidenceLow,
    Reason:     "JXA scripts can perform arbitrary automation " +
        "including message sending and app control",
},
```

And add specific JXA regexes for v2:
```
(?i)Application\(\s*"(?:Messages|Mail|System Events|Contacts|Calendar)"
```

---

## P2 — Coverage Gaps

### MO-P2.1: D6 automator-run matches bare command name — `automator --help` flagged High

**Location**: §5.1 D6 (lines 322-328)

**Issue**: D6 matches `packs.Name("automator")` with no subcommand or
flag constraints. This means:
- `automator --help` → High
- `automator --version` → High
- `which automator` → potentially triggers if Name extraction includes
  `which` arguments (depends on extraction behavior)

Every invocation of `automator` is flagged, even informational ones.

**Recommendation**: Add a Not clause for informational flags, or add a
subcommand/argument requirement:
```go
packs.And(
    packs.Name("automator"),
    packs.Not(packs.Or(
        packs.Flags("--help"),
        packs.Flags("-h"),
        packs.Flags("--version"),
    )),
),
```

### MO-P2.2: macosPrivateDataRe missing Photos Library path listed in §4.2

**Location**: §5.2 (macosPrivateDataRe, lines 373-377), §4.2 (table)

**Issue**: The §4.2 table lists `~/Pictures/Photos Library.photoslibrary/`
as a private data path. But macosPrivateDataRe only covers paths under
`~/Library/`:
```
.../Library/(?:Messages/|Mail/|Safari/|Application Support/AddressBook/|
Group Containers/group\.com\.apple\.notes/|Calendars/)
```

Photos Library is in `~/Pictures/`, not `~/Library/`. It's not covered
by macosPrivateDataRe. The `personal.files` pack would catch
`~/Pictures/Photos Library.photoslibrary/` via the Pictures keyword, but
only at Medium (D5 catch-all), not High.

**Recommendation**: Either add a separate regex/pattern for the Photos
Library path, or document that Photos Library detection is delegated to
`personal.files` pack at Medium severity.

### MO-P2.3: macos.privacy Keywords list doesn't include `sqlite3`

**Location**: §5.2 (Keywords, lines 383-388), §1 (Pack Summary Table)

**Issue**: The Pack Summary Table (§1 line 46) says `macos.privacy`
keywords include `security, mdfind, sqlite3`. But the actual Keywords
list in the code (lines 383-388) is:

```go
Keywords: []string{
    "security", "mdfind",
    "Messages", "AddressBook", "apple.notes",
},
```

`sqlite3` is NOT in the keyword list. This means `sqlite3 ~/Library/Messages/chat.db` would only trigger the pre-filter via the "Messages"
keyword (from the path argument), not via the command name.

If the path doesn't contain any of the listed keywords (e.g., a renamed
or copied database file like `sqlite3 /tmp/chat-backup.db`), the pre-filter
won't trigger at all and the command will be classified as safe.

More importantly: `sqlite3 ~/Library/Safari/History.db` — does "Safari"
appear as a keyword? No. The keyword "Safari" is not in the list. So this
command's pre-filter depends on... nothing in the keyword list matching.
The pre-filter would reject this command and it would never reach pattern
matching.

**Recommendation**: Add `sqlite3`, `Safari`, `Mail`, `Calendars` to
the Keywords list. Or better yet, add all path component keywords that
appear in macosPrivateDataRe.

### MO-P2.4: mdfind at Medium with ConfidenceLow — can search personal file contents

**Location**: §5.2 D5 spotlight-search (lines 475-482)

**Issue**: D5 flags ALL `mdfind` commands at Medium / ConfidenceLow. But
`mdfind` can search personal file CONTENTS, not just filenames:
- `mdfind "social security number"` → searches all indexed content
- `mdfind "password"` → could surface credentials in documents
- `mdfind -name "*.kdbx"` → locates password manager databases

Content-searching `mdfind` (without `-name` or `-onlyin` scope) is more
privacy-invasive than filename-only search. Consider a higher severity
for unscoped content searches.

**Recommendation**: Split into two patterns:
- Scoped mdfind (`-onlyin <dir>` or `-name`) → Medium
- Unscoped content search → High

### MO-P2.5: Build tag test isolation means no CI coverage on Linux

**Location**: §7.1 (Build Tag Test Isolation)

**Issue**: All macOS pack tests require `//go:build darwin`. If CI runs on
Linux (standard for most Go projects), the macOS packs have zero CI
coverage. The plan mentions this but the mitigation is weak:

> Alternatively, test the matching logic without build tags by extracting
> pattern definitions into a separate internal file that is always built,
> with only the `init()` registration gated by build tags.

This is the right approach but it's presented as an afterthought. Without
it, the macOS packs could regress silently on every CI run.

**Recommendation**: Make the pattern extraction approach a requirement,
not an alternative. Define all patterns in always-built files. Gate only
the `init()` registration on `//go:build darwin`. Tests for matching
logic can then run on any platform.

---

## P3 — Minor / Improvements

### MO-P3.1: Golden file entries for macos.system are sparse

**Location**: §8 (Golden File Entries)

**Issue**: The golden file section has ~12 entries total across 3 packs
with 22 patterns. macos.system has 4 entries for 14 patterns.
Per plan 05 requirements, each pattern needs 3+ entries (match,
near-miss, safe variant) = minimum 66 entries.

**Recommendation**: Expand golden files to 3+ per pattern before
implementation.

### MO-P3.2: `open -a` command deferred — but `open -a Terminal` could escalate

**Location**: §9 Q3

**Issue**: `open -a Terminal` opens a new Terminal window. An agent
could use this to spawn a shell that isn't monitored by the Claude Code
hook system — effectively bypassing the destructive command guard entirely.
This is more than "lower risk" as Q3 suggests.

**Recommendation**: Reconsider priority. At minimum, `open -a Terminal`
and `open -a iTerm` should be flagged at High in v1 as a hook bypass vector.

### MO-P3.3: No test for osascript with multiple -e flags combining safe and dangerous scripts

**Location**: §6.1 (test cases)

**Issue**: The S1 test case mentions:
> `osascript -e 'display dialog "Hello"' -e 'tell application "Messages" ...'`
> → NOT safe (D1 matches)

But there's no explicit test for S2 with the same multi-flag pattern.
Given that MO-P0.1 identified S2 as vulnerable to this exact attack,
a dedicated test case is essential.

**Recommendation**: Add explicit test cases for S2 combined with each
of D1-D4 to verify the Not clause fix from P0.1.

### MO-P3.4: launchctl `kickstart` and `kill` subcommands missing from D3

**Location**: §5.3 D3 launchctl-remove (lines 590-606)

**Issue**: D3 covers `remove`, `unload`, `bootout`, `disable`. But:
- `launchctl kickstart -k system/com.apple.service` → force-restarts a
  service (can disrupt system functionality)
- `launchctl kill SIGKILL system/com.apple.service` → kills a service process

These are destructive operations on launch services not covered by D3.

**Recommendation**: Add `kickstart` and `kill` to D3's Or clause:
```go
packs.Or(
    packs.ArgAt(0, "remove"),
    packs.ArgAt(0, "unload"),
    packs.ArgAt(0, "bootout"),
    packs.ArgAt(0, "disable"),
    packs.ArgAt(0, "kickstart"),
    packs.ArgAt(0, "kill"),
),
```
