# Plan Brief: Fresh coding agent per mode

- **Feature:** fresh-coding-agent-per-mode
- **Branch:** lf/253-fresh-coding-agent-per-mode
- **Date:** 2026-09-12
- **Parent issue:** #253 ("Fresh coding agent per mode") — supersedes #227 and its
  seven-story plan. This is an independent, from-scratch replacement; nothing
  from #227 is imported or attached.
- **Citation base commit:** `909e284`

## Goal

When the admitted mode changes, the next run starts a fresh coding-agent
conversation instead of continuing the previous mode's transcript, and that
reset run is pointed at the task's existing session artifact directory so
nothing durable is lost. Replies within the same mode keep resuming the
existing conversation exactly as they do today.

## Shared Decisions

None — this plan is a single story. Nothing here binds two or more stories, so
every decision (predicate order, evidence source, where the pointer lives, what
stays untouched) is recorded in the story itself, not duplicated here.

## Workflow

One story, three tasks, no cross-story shipping order:

1. Consistent mode-change resets (segment selection).
2. Existing-artifact pointer in the prompt.
3. Focused regression tests, run last against both of the above.

The first two tasks touch disjoint code (`internal/turn/delivery.go` predicate
ladder vs. a new `internal/turn/session_artifacts.go` plus `internal/harness`
prompt fields) and may proceed in parallel; they meet only inside `prepare`, at
two separate call sites. The test task depends on both.

## Cross-Story Constraints

Not applicable — a single-story plan has no cross-story surface. The relevant
platform, security and test constraints (no schema change, rooted filesystem
reads only, no protected-tree edit, offline-only tests) are recorded in the
story's own architecture section, since they bind only that one story.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| fresh-coding-agent-per-mode | Fresh coding agent per mode | [#254](https://github.com/marioatcloudvane/litefactory/issues/254) |

## Out of Scope

These are feature-wide exclusions named in the triggering issue; they apply
regardless of how the work is decomposed into stories:

- New database schema, migration, table or column.
- An artifact-binding subsystem: no recorded association between a segment and
  the artifacts it read, no verification of artifact contents.
- A generalized handoff or context-transfer system between modes.
- Instruction-tree rollout: no change to `.claude/**`, `.codex/**`,
  `teams/source/**` or root project instructions (this change is entirely Go
  source under `internal/`, so no protected-tree companion story is needed).
- A separate live-agent evidence project, sandbox permission changes, or making
  a live coding-agent capture a CI requirement.
- Carrying unwritten conversation context across the reset. Context that was
  never written to an artifact or a comment is accepted as lost at a mode
  change; this limitation is accepted, not mitigated.
- Changing which mode is admitted, how it is admitted, or the mode
  label/journal machinery in `internal/daemon/mode.go` and
  `internal/store/modes.go`.
- Changing bundle construction, `planDelivery`'s identity inputs, or removing
  the incidental mode component of `bootstrap.md` that already causes an
  accidental reset for bundle-configured repositories today.
- Removing the now-unreachable `PreviousMode` branch in `writeResumeHeader`
  (it stays, with a corrected comment, since deleting it is beyond the three
  named tasks).
- Reviving or reading an earlier segment's transcript.

## Provenance

- **Citation base:** commit `909e284` on `lf/253-fresh-coding-agent-per-mode`.
- **Known-stale areas:** none identified — the architect read the current
  `internal/turn`, `internal/harness`, `internal/store` and `internal/naming`
  sources directly against this commit.
- **Re-pinning requirement:** none. If this story is implemented against a
  materially later commit, re-confirm that `prepareDeliverySegment`,
  `previousRun`, `findSessionDir` and `harness.BuildPrompt` still have the
  shapes cited in the story before starting.

---

Want to change this story? Comment with the requested change and litefactory
will update it. Direct edits are supported, but comments help avoid conflicting
revisions.
