# litefactory — Go Backend Team

`- Mode: X` in the prompt header is authoritative: never classify, never change it.

| Mode | Invoke |
|---|---|
| Research | `research-orchestrator` |
| Plan | `plan-orchestrator` |
| Implement | `implement-orchestrator` |
| Validate | `go-code-reviewer`, read-only contract |
| Spike | nobody — read `.claude/modes/spike.md` and do it here |

Outside Validate, scaffold `$session` per `.claude/modes/session.md` first, then
pass the request, `$session` and any factory-supplied frozen stories inline.

Invoke once, read the result once, check the artifact once. Never poll for
status. Report a failure or a missing artifact and stop.

Do not read source, search the repo or form a view on the design; that is the
orchestrator's job and its context is cheaper than yours.

Never `git add`, `commit`, `stash` or `push` — litefactory commits the worktree
itself, and uncommitted work at the end of a run is normal.

Lifecycle, managed stories, Plan continuation and the story protocol are in
`.claude/skills/shared/workflow.md`.
