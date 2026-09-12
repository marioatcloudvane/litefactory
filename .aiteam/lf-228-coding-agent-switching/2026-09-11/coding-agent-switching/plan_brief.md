# Plan Brief

- **Feature:** coding-agent switching by label
- **Branch:** lf/228-coding-agent-switching
- **Date:** 2026-09-12 (revised same day — owner tightened the resume-vs-cold
  decision after the first completed iteration)
- **Parent issue:** github:marioatcloudvane/litefactory#228
- **Citation base commit:** `648469350aa216426af09cb84fa71029946e9b42` (HEAD of
  `lf/228-coding-agent-switching` at the first Plan iteration). This revision's
  additional citations (`internal/harness/runner.go:544-559,584-598`,
  `internal/naming/session.go:44-76`, `internal/store/segments.go:19-100`,
  `internal/store/migrations/0003_agent.sql:16-43`,
  `internal/daemon/continuation_dispatch.go:107-133`,
  `internal/turn/continuations.go:28-45`) were confirmed by a targeted research
  pass in this same run, against the current worktree state.

## What changed in this revision

The owner tightened one decision: every agent switch, in either direction, must
now always cold-start the newly selected agent — never resume a conversation
that agent had before an earlier switch away from it, and the system must not
retain or reconstruct "which session to switch back to." This replaces the
prior iteration's plan to resume Claude's own earlier conversation on a
switch-back (that plan's BR-5/D-D). The resolution keeps the owner's earlier
"overwrite the same row in place, no rotation" decision intact: the switch
still overwrites the single existing `segments` row, but now also regenerates
its `session_ref` to a value the row never held before, in the same write that
sets the new `agent` and clears `resume_token`. A fresh, never-before-used
session id plus an unconditional cold decision on any switch turn makes
"switching back never resumes an old conversation" true by construction, with
no schema change, no new column, and no segment rotation. Full detail is in the
story's own architecture section (single story, see Story Index below).

## Shared Decisions

None — this plan ships as a single story, so every scoped decision (store
mutation shape, in-place vs. rotate, the always-cold-on-switch rule, refusal
narrowing, script deletion) lives in that story's own architecture section
rather than here, per the anti-duplication rule: a decision used by exactly one
story does not belong in shared context.

## Workflow

One story, one atomic change — there is no cross-story sequencing. Internally,
the story's own tasks order as: the store accessor first, then the
`prepareDeliverySegment` restructuring that depends on it, then the
force-cold-on-switch resume change (which depends on the delivery
restructuring), independently of the refusal-narrowing, then the tests last,
with the manual script's deletion landing in the same change as the narrowed
refusal — never earlier, so the automatic path and the manual escape hatch
never need to coexist mid-change.

## Cross-Story Constraints

- Ring-fenced for this feature: `internal/daemon` label resolution/trigger
  (already sufficient as-is), `internal/harness`, `internal/codex`,
  `internal/render`, `.codex/AGENT_ROSTER.md`, the CLI, and every config file —
  none of these are touched.
- No new package, interface, config key, goroutine, or polling/reconciliation
  loop. The owner stated the simplicity constraint repeatedly; it governs the
  whole feature, not one part of it.
- No test may launch a real `claude` or `codex` process. Live, end-to-end
  observation of a real coding-agent process picking up a switched label is
  recorded as an owner-operated post-merge check and must never become an
  implementation or CI requirement (`docs/decisions/nested-agent-evidence.md`).

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| switch-agent-by-label | Switch the coding agent in place when the issue label changes | [#252](https://github.com/marioatcloudvane/litefactory/issues/252) |

## Out of Scope

- Rotating to a new segment **row** on a switch (a new `ordinal`/new `id`) —
  the switch still overwrites the single existing row in place. The row's
  *session id value* does change on every switch (a freshly generated value,
  not a new row); that is the mechanism, not scope creep, that makes the
  owner's "always a new session" rule achievable without rotation.
- Killing or cancelling an in-flight subprocess when the label changes.
- Any new label-watching, polling, or reconciliation pass.
- Keeping `switch-agent.sh` as a fallback once the automatic path ships.
- Adding a third coding-agent tool, a CLI flag, or any non-label selection
  surface.
- Migrating or rewriting historical `runs.agent` rows.
- A dedicated "agent switched" issue comment or reply kind.
- Any change to `daemon.resolveAgent`'s label ladder or its existing refusal
  replies.
- The unrelated `.codex/AGENT_ROSTER.md` naming coincidence surfaced during
  Research — explicitly not investigated further.
- The pre-existing stale-`prelaunch`-row-plus-label-change interaction
  (accepted risk, same class as an existing mode-change edge case; not fixed
  here).

## Provenance

- Citations in the story are pinned against the worktree state described
  above. No known-stale areas beyond the accepted pre-existing `prelaunch`
  edge case noted above (that is a recorded risk, not a stale citation).
- Re-pinning requirement: if the branch advances materially before
  implementation starts, re-verify the file/line citations in the story's
  architecture notes against the new HEAD before relying on them.
