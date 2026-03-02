# Plan 03g: macOS System Pack — Systems Engineer Review

**Reviewer**: dcg-reviewer (systems-engineer persona)
**Date**: 2026-03-02
**Documents reviewed**:
- `docs/plans/03g-packs-macos.md` (~999 lines)

**Focus areas**: AppleScript detection correctness, privacy path coverage,
system command matching, keyword pre-filter completeness, safe pattern
design, and cross-plan consistency with plan 01 (flag parsing) and plan 02
(matcher semantics).

**Cross-references consulted**: plan 01 (classifyArg — short flags are
always boolean, next token is positional arg), plan 02 (ArgContentMatcher
checks cmd.Args only, keyword pre-filter triggers pack candidacy).

---

## Summary Assessment

The macOS pack covers three important threat categories: communication
(osascript/Shortcuts/Automator), privacy (keychain, message/email databases),
and system modification (defaults, launchctl, diskutil, etc.). The AppleScript
regex approach is practical given the lack of a mature tree-sitter grammar.

Main weaknesses: (1) `dscl` pattern uses `ArgContent("-delete")` but the
parser decomposes `-delete` into short flags — the pattern will never match,
(2) privacy pack keywords are incomplete — Safari, Mail, and Calendar path
accesses bypass the pre-filter entirely, and (3) `diskutil apfs` is marked
safe by S3 but has destructive subcommands like `deleteVolume`.

**Finding count**: 0 P0, 3 P1, 4 P2, 2 P3

---

## P1 Findings (High — Pattern will not work as written)

### SE-P1.1: D7 `dscl` pattern uses ArgContent for dash-prefixed subcommands

**Location**: 03g lines 659–673

D7 matches `dscl` with `ArgContent("-delete")` and `ArgContent("-create")`.

Plan 01 flag parser (§6.3, line 618–629) classifies `-delete` as combined
short flags because it starts with `-` and contains only ASCII characters:
```
-delete → {"-d": "", "-e": "", "-l": "", "-t": ""}
```

So `-delete` is consumed by the flag parser and does NOT appear in `cmd.Args`.
`ArgContent("-delete")` searches `cmd.Args` for the substring "-delete" and
will never find it.

For `dscl . -delete /Users/testuser`:
- `cmd.Args = [".", "/Users/testuser"]`
- `cmd.Flags = {"-d": "", "-e": "", "-l": "", "-t": ""}`

Neither `ArgContent("-delete")` nor `ArgContent("delete")` matches because
"delete" is not a substring of "." or "/Users/testuser".

**Impact**: D7 will NEVER match. `dscl . -delete /Users/testuser` and
`dscl . -create /Users/newuser` would be completely undetected.

**Fix options**:
(a) Match on decomposed flags: `packs.Flags("-d", "-e", "-l", "-t")` —
    but fragile and could match unrelated flag combinations
(b) Add `-delete` and `-create` as keywords, then match on
    `ArgContentRegex` of `cmd.RawText` — but ArgContentMatcher doesn't
    check RawText
(c) Add a `RawArgContent` matcher to plan 02 that searches `cmd.RawArgs`
    instead of `cmd.Args`. `RawArgs` preserves all tokens including dash-
    prefixed subcommands before flag decomposition. This is the cleanest fix.
(d) Use `RawText` matching: add the ability for ArgContentMatcher to check
    the entire raw command text. But this may be too broad.

**Recommended**: Option (c) — add `RawArgContent` matcher. This is also
needed for any other command that uses dash-prefixed subcommands (e.g.,
`svn -delete`, though less common). This is a plan 02 enhancement.

### SE-P1.2: Privacy pack keywords missing Safari, Mail, Calendars paths

**Location**: 03g lines 383–387

The Keywords field:
```go
Keywords: []string{
    "security", "mdfind",
    "Messages", "AddressBook", "apple.notes",
},
```

The keyword pre-filter must find at least one keyword in the raw command
string for the pack to be a candidate. Check each private data path against
these keywords:

| Path | Contains keyword? | Result |
|------|------------------|--------|
| `~/Library/Messages/chat.db` | "Messages" ✓ | Detected |
| `~/Library/Mail/V10/MailData/...` | None | **UNDETECTED** |
| `~/Library/Safari/History.db` | None | **UNDETECTED** |
| `~/Library/Application Support/AddressBook/` | "AddressBook" ✓ | Detected |
| `~/Library/Group Containers/group.com.apple.notes/` | "apple.notes" ✓ | Detected |
| `~/Library/Calendars/` | None | **UNDETECTED** |

Three of six private data paths have no matching keyword. Commands like
`sqlite3 ~/Library/Safari/History.db` would bypass the pre-filter entirely
and never reach D4 (`private-data-access`).

**Fix**: Add keywords for the missing paths:
```go
Keywords: []string{
    "security", "mdfind",
    "Messages", "AddressBook", "apple.notes",
    "Safari", "Mail", "Calendars",
},
```

Also: the summary table (line 45) claims "sqlite3" is a keyword, but it's
not in the actual Keywords list. Either add it or fix the table.

### SE-P1.3: S3 `diskutil apfs` safe pattern shadows destructive operations

**Location**: 03g lines 539–553

S3 marks `diskutil` with ArgAt(0) matching "info", "list", or "apfs" as safe:
```go
packs.Or(
    packs.ArgAt(0, "info"),
    packs.ArgAt(0, "list"),
    packs.ArgAt(0, "apfs"),  // apfs list is safe
),
```

But `diskutil apfs` has destructive subcommands:
- `diskutil apfs deleteVolume disk2s1` — deletes an APFS volume
- `diskutil apfs deleteContainer disk2` — deletes an APFS container
- `diskutil apfs resizeContainer` — resizes, potential data loss

For these commands, ArgAt(0) is "apfs" → S3 matches → safe → all destructive
patterns (including D2 diskutil-erase) are skipped. The commands are classified
as **safe** despite being destructive.

The `Not(Or(eraseDisk, eraseVolume, partitionDisk))` clause in S3 doesn't
help because it checks ArgAt(0), and "apfs" ≠ "eraseDisk".

**Fix**: Remove "apfs" from S3's Or clause. Or require a secondary check:
```go
packs.And(
    packs.Name("diskutil"),
    packs.Or(
        packs.ArgAt(0, "info"),
        packs.ArgAt(0, "list"),
    ),
),
```

If `diskutil apfs list` should be safe, add it as a separate safe pattern
with a secondary subcommand check (requires checking ArgAt(1)).

---

## P2 Findings (Medium — Weakens detection or has design issues)

### SE-P2.1: nvram D5 matches read operations at Critical severity

**Location**: 03g lines 626–639

D5 matches any `nvram` invocation that doesn't use `-p`, `-x`, or `--print`.
But `nvram boot-args` (without `=`) is a **read** operation that prints the
current value. Only `nvram boot-args="-v"` (with `=`) is a write.

Current behavior:
- `nvram boot-args` (read) → Critical ✗ (should be no match or Medium)
- `nvram boot-args="-v"` (write) → Critical ✓

**Fix**: Add an ArgContentRegex check for `=` in the arguments to distinguish
reads from writes:
```go
Match: packs.And(
    packs.Name("nvram"),
    packs.ArgContentRegex(`=`),  // only match writes (key=value)
    packs.Not(packs.Or(
        packs.Flags("-p"),
        packs.Flags("-x"),
        packs.Flags("--print"),
    )),
),
```

Or: add a safe pattern for nvram reads (no `=` in args).

### SE-P2.2: Finder destructive osascript operations undetected

**Location**: 03g lines 238–245 (S2), component diagram

S2 marks `tell application "Finder" to get/open folder/reveal` as safe. But
"Finder" is not in any destructive pattern's target app list (D1=Messages,
D2=Mail, D3=System Events, D4=Contacts/Calendar/Reminders/Notes/Safari).

So S2 is unnecessary — Finder osascripts wouldn't match any destructive
pattern anyway. More importantly, destructive Finder operations are
**completely undetected**:
- `osascript -e 'tell application "Finder" to delete file "x" of desktop'`
- `osascript -e 'tell application "Finder" to empty trash'`
- `osascript -e 'tell application "Finder" to move file "x" to trash'`

**Fix**: Either add "Finder" to D4's sensitive app list with a Not clause
excluding S2's safe operations, or add a separate D4.5 pattern for
destructive Finder operations (delete, move to trash, empty trash).

### SE-P2.3: D6 automator matches all invocations including `--help`

**Location**: 03g lines 321–328

D6 is simply `packs.Name("automator")` with no additional constraints. So
`automator --help`, `automator --version`, and any other non-workflow
invocation would match at High severity.

**Fix**: Add exclusions for informational flags:
```go
Match: packs.And(
    packs.Name("automator"),
    packs.Not(packs.Or(
        packs.Flags("--help"),
        packs.Flags("-h"),
        packs.Flags("--version"),
    )),
),
```

### SE-P2.4: Component diagram D-numbers don't match code order

**Location**: 03g lines 76–92 (diagram) vs lines 556–739 (code)

The diagram numbers patterns D1-D11, but the code organizes by severity
(Critical first, then High), giving different orderings:

| Diagram | Code | Pattern |
|---------|------|---------|
| D1 | D1 | defaults-delete | ← diagram says D1, code D9 |
| D2 | D2 | defaults-write-system | ← diagram says D2, code D10 |
| D3 | D3 | launchctl-remove | ← matches |
| D4 | D4 | diskutil-erase | ← matches |
| ... | ... | ... |

The diagram's D1="defaults-delete" at High, but the code's first pattern is
D1="csrutil-disable" at Critical. This makes cross-referencing unreliable.

**Fix**: Either renumber the diagram to match the code order, or (better)
use pattern names instead of D-numbers in the diagram.

---

## P3 Findings (Low — Polish, minor gaps)

### SE-P3.1: Summary table claims "sqlite3" keyword but it's absent from code

**Location**: 03g line 45 vs lines 383–387

The table says privacy pack keywords include "sqlite3", but the actual
Keywords field doesn't. This is a documentation error (the keyword would
provide additional coverage for `sqlite3 ~/Library/...` commands but isn't
strictly necessary if the path-component keywords like "Messages" cover
those cases — though per SE-P1.2, they don't cover all paths).

### SE-P3.2: osascript content matching documented as "flag value" but actually matches positional arg

**Location**: 03g line 120

> Detection is via ArgContentRegex on the -e flag value or script file content.

Per plan 01 §6.3, short flags are always boolean: `-e` → `Flags["-e"]=""`,
and the script content becomes a positional argument in `cmd.Args[0]`.
`ArgContentRegex` searches `cmd.Args`, so it correctly finds the script
content — but the documentation is misleading.

The pattern works correctly by coincidence of the parser design (short flags
are always boolean), not by matching the flag value.

**Fix**: Update the documentation to say "ArgContentRegex on the script
content argument (which follows the `-e` flag as a positional arg due to
the parser treating short flags as boolean)".

---

## Cross-Plan Consistency Checks

| Check | Result |
|-------|--------|
| Short flag `-e` parsing | **Consistent** — plan 01 decomposes into boolean flag; script content becomes positional arg in cmd.Args; ArgContentRegex works |
| Short flag `-delete` parsing | **Bug** — plan 01 decomposes `-delete` into individual flags; ArgContent("-delete") can never match |
| ArgContentMatcher on cmd.Args | **Consistent** — plan 02 §5.2.4 checks cmd.Args only |
| Keyword pre-filter pack selection | **Gap** — missing keywords for Safari, Mail, Calendars paths |
| AnyName() for privacy patterns | **Consistent** — plan 02 §5.2.8 AnyNameMatcher |
| Build tag `//go:build darwin` | **Consistent** — plan 02 §5.3 documents the build tag pattern |
| Safe-before-destructive | **Bug** — S3 "apfs" safe pattern shadows diskutil apfs destructive operations |
