# Review: 03e-packs-other (domain-packs-r2)

- Source doc: `docs/plans/03e-packs-other.md`
- Reviewed commit: 5dbf12e
- Reviewer: domain-packs-r2
- Round: 2

## Findings

### P2 - Interaction matrix S1/D7 "blocked" is incorrect — different command names cannot shadow

**Problem**
In §8.1 (line ~2003), the frameworks interaction matrix marks the S1/D7 cell as "blocked":

```
| S1 rails-db-migrate | — | — | — | — | — | — | blocked | — | — | — | — |
```

The note below (line ~2016) says: "S1 could shadow D7 (both match `rails db:migrate`) — S1's `Not(--run-syncdb)` prevents this."

This is factually incorrect. S1 `rails-db-migrate-safe` matches `Name("rails") + ArgAt(0, "db:migrate")`. D7 `managepy-migrate-syncdb` matches `Name("manage.py") + ArgAt(0, "migrate") + Flags("--run-syncdb")` (or `Name("python") + ArgAt(0, "manage.py") + ArgAt(1, "migrate") + Flags("--run-syncdb")`).

These are entirely different commands — different tool names (`rails` vs `manage.py`), different subcommand tokens (`db:migrate` vs `migrate`). S1 can never shadow D7 even without its Not clause, because `Name("rails")` never matches a manage.py command.

The S4b/D7 "blocked" entry IS correct — both S4b and D7 match `manage.py migrate`, with S4b's `Not(--run-syncdb)` preventing shadowing. The note should reference only S4b, not S1.

The S1 `Not(--run-syncdb)` clause is purely defensive as documented in §5.1.1 note 5 — `--run-syncdb` is a Django flag that rails never takes. The interaction matrix should show "—" for S1/D7, not "blocked".

**Required fix**
Change the S1/D7 cell from "blocked" to "—". Update the note to only reference S4b:
"S4b could shadow D7 (both match `manage.py migrate`) — S4b's `Not(--run-syncdb)` prevents this."

---

### P3 - Golden entry section headers have stale counts from pre-R1

**Problem**
The golden entry section headers were not updated after R1 incorporation added entries:

| Section | Header says | Summary table (§6.5) says | Actual |
|---------|-------------|---------------------------|--------|
| §6.1 frameworks | 38 entries | 44 | 44 |
| §6.2 rsync | 10 entries | 12 | 12 |
| §6.3 vault | 18 entries | 23 | 23 |
| §6.4 github | 16 entries | 16 | 16 (correct) |

The summary table at §6.5 has the correct counts (total 95). The section headers are stale.

**Required fix**
Update §6.1 header to "44 entries", §6.2 to "12 entries", §6.3 to "23 entries".

---

### P3 - rsync D2 comment says "Higher severity" but actual severity is same Medium as D3

**Problem**
In §5.2 (line ~720), the D2 `rsync-delete-before` inline comment says:

```
// D2: rsync --delete-before (deletes destination files BEFORE transfer)
//     Higher severity than --delete because files are removed before
//     new versions arrive, creating a window where data is missing.
```

But D2 has `Severity: guard.Medium` — the same as D3 `rsync-delete` which also has `Severity: guard.Medium`. The comment claims higher severity but the actual severity is equal. This could confuse implementers who read the comment and expect D2 to have a higher severity than D3.

**Required fix**
Either:
(a) Change D2 to `guard.High` if the reasoning in the comment is correct (delete-before is genuinely more dangerous), or
(b) Update the comment to say "Same severity as --delete. While files are removed before transfer, the impact is equivalent since both delete destination files not in source."

---

## Summary

3 findings: 0 P0, 0 P1, 1 P2, 2 P3

**Verdict**: Approved with revisions

The plan is well-structured with comprehensive coverage across 4 diverse packs. The R1 disposition table shows thorough incorporation of 31 findings — the vault S2 Not clause additions (D7-D11) are a significant strengthening. The frameworks pack's dual-invocation pattern handling (manage.py, artisan) is well-designed. The rsync pack's exhaustive --delete* flag coverage with the correct S1 Not clause chain is clean. The vault interaction matrix correctly documents S2's Not clauses preventing shadowing. The only functional issue is the incorrect S1/D7 "blocked" entry in the frameworks interaction matrix — the remaining findings are documentation accuracy issues.
