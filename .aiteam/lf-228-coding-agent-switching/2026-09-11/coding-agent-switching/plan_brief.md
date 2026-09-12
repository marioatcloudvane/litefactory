# Plan Brief

- **Feature:** coding-agent switching by label
- **Branch:** lf/228-coding-agent-switching
- **Date:** 2026-09-12
- **Parent issue:** github:marioatcloudvane/litefactory#228
- **Citation base commit:** `648469350aa216426af09cb84fa71029946e9b42` (HEAD of
  `lf/228-coding-agent-switching` in this worktree at Plan time)

## Shared Decisions

None — this plan ships as a single story, so every scoped decision (store
mutation shape, in-place vs. rotate, resume-vs-cold rule, refusal narrowing,
script deletion) lives in that story's own architecture section rather than
here, per the anti-duplication rule: a decision used by exactly one story does
not belong in shared context.

## Workflow

One story, one atomic change — there is no cross-story sequencing. Internally,
the story's own tasks order as: the store accessor first, then the
`prepareDeliverySegment` restructuring that depends on it, then (independently
of each other) the refusal-narrowing and the resume-decision refinement, then
the tests, with the manual script's deletion landing in the same change as the
narrowed refusal — never earlier, so the automatic path and the manual escape
hatch never need to coexist mid-change.

## Cross-Story Constraints

- Ring-fenced for this feature: `internal/daemon` label resolution/trigger
  (already sufficient as-is), `internal/harness`, `internal/codex`,
  `internal/render`, `.codex/AGENT_ROSTER.md`, the CLI, and every config file —
  none of these are touched.
- No new package, interface, config key, goroutine, or polling/reconciliation
  loop. The owner stated the simplicity constraint twice; it governs the whole
  feature, not one part of it.
- No test may launch a real `claude` or `codex` process. Live, end-to-end
  observation of a real coding-agent process picking up a switched label is
  recorded as an owner-operated post-merge check and must never become an
  implementation or CI requirement (`docs/decisions/nested-agent-evidence.md`).

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| switch-agent-by-label | Switch the coding agent in place when the issue label changes | [#252](https://github.com/marioatcloudvane/litefactory/issues/252) |

## Out of Scope

- Rotating to a new segment on a switch; any new segment-id or session-id
  scheme.
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

- Citations in the story below are pinned against
  `648469350aa216426af09cb84fa71029946e9b42` on `lf/228-coding-agent-switching`,
  as observed in this worktree during this Plan run.
- No known-stale areas beyond the accepted pre-existing `prelaunch` edge case
  noted above (that is a recorded risk, not a stale citation).
- Re-pinning requirement: if the branch advances materially before
  implementation starts, re-verify the file/line citations in the story's
  architecture notes against the new HEAD before relying on them.
