# litefactory — Go Backend Team

`- Mode: X` in the prompt header is authoritative: never classify, never change it.

Use the factory-supplied role definition when present; do not reopen its local
skill or agent file. The routing below is fallback when no role was supplied.
Dispatch-only rules below do not restrict a supplied worker role.
Read supporting contracts only when relevant and not already supplied in full.

| Mode | Invoke |
|---|---|
| Research | `research-orchestrator` |
| Plan | `plan-orchestrator` |
| Implement | `implement-orchestrator` |
| Validate | `go-code-reviewer`, read-only contract |
| Spike | nobody here — the factory runs the graph in `docs/workflow/spike.md`; never execute it in this session |

Outside Validate, scaffold `$session` per `docs/workflow/session.md` first, then
pass the request, `$session` and any factory-supplied frozen stories inline.

Validate reuses the supplied session and frozen scope read-only. Do not scaffold
a directory or MANIFEST, write artifacts, or require artifact existence to route
or finish Validate. Validate finishes with structured runner findings, not an
artifact; assess the returned result without writing or checking for a result
file.

Invoke once, read the result once, check the artifact once. Never poll for
status. Report a failure or a missing artifact and stop.

Do not read source, search the repo or form a view on the design; that is the
orchestrator's job and its context is cheaper than yours.

Never `git add`, `commit`, `stash` or `push` — litefactory commits the worktree
itself, and uncommitted work at the end of a run is normal.

Research uses `internal/teamsource/research/skills/research-evidence/SKILL.md` only. For other
modes, lifecycle, managed stories, Plan continuation and the story protocol are in
`.claude/skills/shared/workflow.md`.

## Shared completion

Follow the factory-declared final route: shared result helper, permitted communication
operations or structured terminal output. Use the supplied explicit baseline and
revision; prior prose is not editable state. A story checkpoint or input receipt
alone does not complete the run. Input, engineering, acceptance and remote effects
remain independent. Correct preflight diagnostics; preserve request identity on
reply loss and use a new request for corrected content. The factory revalidates
after exit; missing/malformed output blocks nominal success, and failure or
interruption retains its cause. Validate remains reference-only, without shell,
edits, capture, fresh checks or delegation. Legacy scopes keep their declared
compatibility route until supported upgrade/admission. Adoption and retirement are
separate; offline checks do not satisfy authentic #274/#296 host observations.
