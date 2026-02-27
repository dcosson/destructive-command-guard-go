---
shaping: true
---

# Destructive Command Guard (Go) — Frame

## Source

> I want to copy a go-native version of [destructive-command-guard] so it can
> be used without cgo

> we should just take inspiration from it, don't need to copy it exactly

> although assume everything it does is there for a reason and make sure we
> understand it before skipping anything

> we also have tree sitter go in a sibling directory... This should be useful
> for heredoc detection, and just inline scripts in general right?

> I'm not so worried about like the nanosecond responses... but I do want some
> sort of budget enforcement of like 100 ms... This isn't that performance
> critical of an integration when the LLM responses take multiple seconds anyway

> [on budget] maybe we just don't need to be that strict about budget? Let's
> just have some benchmarks and make those as fast as we think we can

> super huge strings would just have slower performance marginally, and that
> would be OK

> This should build a simple binary like you're saying, but it should also
> export some public functions or structures that can be used from other go
> programs because the main way I plan to use this is within the H2 Hook
> command that already exists.

---

## Problem

The upstream destructive-command-guard is written in Rust. Using it from Go
requires cgo, which complicates builds, cross-compilation, and distribution.

We have a pure-Go tree-sitter runtime (tree-sitter-go) with bash + 14 other
language grammars at 100% corpus pass rate. This gives us a foundation to build
a structurally-aware command guard that is potentially more robust than the
Rust version's regex-first approach for context disambiguation.

---

## Threat Model

This is a **mistake preventer, not a security boundary**. The "attacker" is an
LLM that accidentally generates `rm -rf /` or `git reset --hard`, not a human
adversary trying to bypass the guard. A determined attacker could trivially
evade static analysis (base64 encoding, writing scripts to disk, eval tricks,
etc.).

We do not need to handle obfuscation or adversarial evasion. We need to catch
the obvious destructive patterns that an LLM would naturally generate. This
also means we accept some risk of interpretation gaps — if Python adds a new
flag for inline execution that we don't know about, we'll miss it. That's an
acceptable trade-off for this use case.

---

## Outcome

A pure-Go library and binary that:

- Exposes a public Go API for evaluating shell commands (primary use: imported into h2's `handle-hook` pipeline)
- Also works as a standalone Claude Code pre-tool-use hook binary
- Uses tree-sitter bash parsing for structural analysis instead of regex heuristics
- Covers the important destructive command categories
- Is fast enough that it's invisible to the user (benchmarked, not budget-enforced)
- Can be extended with new patterns/grammars easily
