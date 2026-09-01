# US-3 — Mid-session tool-switch refusal

> As an issue owner, I want a tool switch on a session that already has history
> to be refused with a clear comment rather than silently resumed or silently
> restarted, so that a Claude conversation is never handed to Codex as a resume
> token it cannot honour.

Makes the cold-vs-resume decision tool-aware, and makes switching the label on a
session that already has a segment produce a clear refusal instead of a broken
resume attempt or a silent restart.

## Why This Matters

This is the story that closes the sharpest risk in the whole feature: if
somebody flips the routing label mid-conversation, litefactory could otherwise
hand a Codex process a Claude session id that Codex has never seen and has no
way to honor — the run doesn't just fail, it fails confusingly, in a way that
looks like a bug in the new feature rather than a limitation the requester
needs to know about. The correct behavior here is intentionally the less clever
one: refuse the switch outright, with a message that says what happened and
why, rather than guess at "start a new conversation instead" — that guess would
need a database feature this codebase doesn't have yet. It costs a small amount
of flexibility today (a label flip does not "just work") in exchange for never
silently corrupting a conversation.

## Architecture Notes

- Cold-vs-resume becomes tool-aware: `prepare.go` decides `resume` only when
  **all** hold — a prior terminal run exists for this segment, that segment's
  `agent` equals the resolved agent, and `resume_token` is non-empty. Otherwise
  cold. Cover the three conditions as separate table-test cases, not one
  combined case.
- A resolved agent that differs from the segment's `agent` refuses the run with
  `ReplyAgentSwitchRefused`, naming both the owning and the requested agent
  (both closed-set values). Nothing is started, no segment is created, no row
  is mutated.
- Clean implementation for "nothing is created" without a check-then-create
  race: call `GetOrCreateSegment(ctx, sessionID, ordinal, sessionRef, kind)`
  once — its stored-row-wins semantics (from US-4) mean it returns the
  *existing* row's `agent` if one exists — then compare the returned `.Agent`
  to the resolved kind and refuse on mismatch. This avoids a separate
  check-then-create pair across two store calls and matches the store's
  single-connection, `BEGIN IMMEDIATE` design.
- A session with no segment yet (first run) adopts the resolved agent; there is
  no switch to refuse in that case.
- This story needs both US-2 (the label decision to compare against) and US-4
  (the segment's `agent` column) — it cannot start before either lands.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-3.1 | Tool-aware cold-vs-resume decision in `prepare.go`: resume only when prior terminal run exists, `segment.agent` matches resolved agent, and `resume_token` is non-empty | Code | US-2, US-4 | M |
| T-3.2 | Switch refusal via a single `GetOrCreateSegment` call, comparing returned `.Agent` to resolved kind — no separate check-then-create | Code | T-3.1 | M |
| T-3.3 | Tests: refusal creates nothing in `segments`/`runs`; first run on an empty session adopts the label; three-way resume-condition table (history exists / agent matches / `resume_token` non-empty) as separate cases | Test | T-3.2 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-3.1 | A session with a `claude` segment and an `lf:agent=codex` label produces `ReplyAgentSwitchRefused`, starts no subprocess, and leaves `segments` and `runs` unchanged |
| AC-3.2 | The refusal reply names both the owning and the requested agent, both closed-set values |
| AC-3.3 | A first run on a session with no segment adopts the labelled agent and is not refused |
| AC-3.4 | A `resume` path is chosen only when prior terminal history exists **and** the segment's agent matches **and** `resume_token` is non-empty; all three conditions are covered by separate table-test cases |
| AC-3.5 | The refusal path is implemented as a single `GetOrCreateSegment` call plus a comparison, not a separate check-then-create pair of store calls |
