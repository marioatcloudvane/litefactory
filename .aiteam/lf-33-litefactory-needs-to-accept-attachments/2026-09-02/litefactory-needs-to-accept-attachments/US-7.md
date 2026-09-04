# US-7 — Record an audit manifest for every attachment

> As an operator who needs to know what a run actually saw, I want a written
> record of every attachment litefactory considered — fetched or rejected — so
> that I can audit what the agent was given without digging through logs.

Delivers `attachments.md`, the committed, worktree-resident record of every
attachment ingestion attempt in a turn.

## Why This Matters

The owner's first answer in this whole plan was that litefactory, not the agent,
should be the thing that fetches attachments — specifically so it stays possible
to intercept or log what is happening. This story is that logging promise made
concrete: a plain file, sitting in the run's own worktree next to everything else
the agent touched, listing every attachment litefactory looked at and what
happened to it, accepted or not. It is deliberately just a file, not a database
row — the owner's scope explicitly ruled out adding new persistence for this, so
the audit trail this story delivers is exactly as durable as the rest of the
turn's committed output and no more.

## Architecture Notes

- **Scope correction, adopted:** an earlier task-decomposition pass proposed an
  additive SQLite migration for a per-run attachment record. `feature-spec.md`'s
  Out of Scope list is explicit and owner-approved — "no new table, no new
  migration; the manifest file is the record." That proposal is dropped, not
  deferred; if a future need for a queryable audit trail beyond the manifest
  file ever arises, that is new scope requiring its own spec decision, not
  something this story quietly includes.
- `attachments.md` is a worktree file, git-committed by litefactory alongside the
  rest of the turn's changes — it is not a reply, and per `plan_brief.md`'s
  Cross-Story Constraints it must **not** be routed through `internal/redact`
  (only reply text is).
- Records: source (query string stripped for any signed URL), sniffed type, byte
  size, landed filename, outcome (accepted / rejected + reason from US-6), and a
  timestamp. Never the installation token, never raw bytes.
- Depends on US-3 — there is nothing to record until ingestion exists.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-7.1 | Define the `attachments.md` manifest fields and rendering format | code | — | S |
| T-7.2 | Write `attachments.md` into the worktree as a committed artefact (not redactor-routed) | code | T-7.1 | S |
| T-7.3 | ~~Additive store migration for a per-run attachment record~~ — dropped; out of scope per `feature-spec.md`. Do not implement. | n/a | — | — |
| T-7.4 | Ensure every accepted and every rejected attachment produces exactly one manifest entry | code | T-7.1, T-7.2 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-7.1 | Every attachment ingestion attempt in a turn — accepted or rejected — produces exactly one row in `attachments.md`. |
| AC-7.2 | `attachments.md` never contains a query string, raw bytes, base64, or the installation token; a signed URL is recorded with its query stripped. |
| AC-7.3 | `attachments.md` is committed alongside the rest of the turn's worktree changes, not treated as a reply artefact, and is not passed through `internal/redact`. |
| AC-7.4 | No new SQLite table or migration is introduced by this story. |
