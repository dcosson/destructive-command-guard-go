# Review: 07-tool-use-evaluation (review-2)

- Source doc: `docs/plans/07-tool-use-evaluation.md`
- Reviewed commit: `9c1bec12be479407d8ff5254d42e348bc8783139`
- Reviewer: `fast-dale`

## Findings

### P1 - Unknown tools are still specified to auto-allow, which removes the existing Claude-side safety net for future tool types

**Problem**
The revised plan intentionally keeps unknown tools on an unconditional allow path ([docs/plans/07-tool-use-evaluation.md](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/07-tool-use-evaluation.md#L120), [docs/plans/07-tool-use-evaluation.md](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/07-tool-use-evaluation.md#L123), [docs/plans/07-tool-use-evaluation.md](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/07-tool-use-evaluation.md#L288), [docs/plans/07-tool-use-evaluation.md](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/07-tool-use-evaluation.md#L291)). That is a behavior regression relative to the current system described at the top of the doc, where non-Bash tools fall through to Claude Code's built-in permission handling rather than being explicitly allowed ([docs/plans/07-tool-use-evaluation.md](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/07-tool-use-evaluation.md#L10), [docs/plans/07-tool-use-evaluation.md](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/07-tool-use-evaluation.md#L13)).

That matters for forward compatibility. As soon as Claude introduces a new tool with write, network, or process-launch capability, this design would convert "DCG does not understand it, let Claude decide" into "DCG explicitly allows it." That weakens the overall guard posture in exactly the case where the local rule set is least informed.

**Required fix**
The plan should preserve a safe default for unknown tools. Either treat unknown tool names as indeterminate/policy-driven, or explicitly specify a pass-through mode in hook handling that declines to emit an allow decision for unknown tools so Claude's native guard remains in effect. If the user still wants unconditional allow, that should be called out as an intentional security tradeoff with explicit acceptance criteria and tests.

---

### P2 - The hook input deserialization change is not specified concretely enough to guarantee non-Bash fields survive JSON unmarshalling

**Problem**
Section 4.6 says hook mode will "pass the full `tool_input` JSON as a `map[string]any`" ([docs/plans/07-tool-use-evaluation.md](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/07-tool-use-evaluation.md#L336), [docs/plans/07-tool-use-evaluation.md](/Users/dcosson/h2home/projects/destructive-command-guard-go/docs/plans/07-tool-use-evaluation.md#L340)), but the current CLI code unmarshals `tool_input` into a fixed `ToolInput` struct containing only Bash fields (`command`, `description`, `timeout`) ([hook.go](/Users/dcosson/h2home/projects/destructive-command-guard-go/cmd/dcg-go/hook.go#L11), [hook.go](/Users/dcosson/h2home/projects/destructive-command-guard-go/cmd/dcg-go/hook.go#L24)). With that shape, fields like `file_path`, `path`, `pattern`, and `url` are discarded during JSON unmarshal before evaluation ever sees them.

The plan implies the right end state, but it does not commit to the concrete representation change needed to get there, nor does the test section require a decode-level regression test proving those fields survive unmarshalling.

**Required fix**
Specify the hook input representation explicitly: for example, change `HookInput.ToolInput` to `json.RawMessage` or `map[string]any`, decode it once in `runHookMode`, and pass that decoded map to `EvaluateToolUse()`. Add a hook test that feeds raw JSON for a non-Bash tool such as `{"tool_name":"Read","tool_input":{"file_path":"~/.ssh/id_rsa"}}` and verifies the path is still present after unmarshalling and reaches evaluation.

---

## Summary

2 findings: 0 P0, 1 P1, 1 P2, 0 P3

**Verdict**: Approved with revisions
