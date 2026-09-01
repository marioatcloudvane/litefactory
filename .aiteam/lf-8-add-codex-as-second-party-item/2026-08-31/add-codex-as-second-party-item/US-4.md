# US-4 — Store schema: tool-aware session identity

> As a maintainer, I want the store to record which tool owns a segment and to
> carry a tool-reported resume token separately from litefactory's own
> pre-computed identity, so that Codex's discovered-after-the-fact thread id and
> Claude's pre-computed UUID both fit without either being falsified.

Adds an `agent` discriminator column to `segments` and `runs`, splits the
existing `claude_session_id` column into litefactory's own always-computable
identity (`session_ref`) and the tool's own sometimes-absent handle
(`resume_token`), and updates the store's read/write paths accordingly.

## Why This Matters

Right now the database has exactly one idea of "session id," and it is shaped
around how Claude happens to work: litefactory invents the id itself before
anything runs. Codex doesn't work that way — it tells you its own id only after
a run has already started, and there's currently nowhere to put that. Papering
over the difference (forcing Codex's id into the same column, or fabricating
one) would corrupt exactly the data that makes resuming a conversation reliable.
The fix is a real schema change, and every schema change to this table is
one-way once real conversations exist in it — so the shape decided here has to
be right the first time, sized generously enough that a future "resume this
Codex thread" feature has somewhere to write without another migration. None of
this work actually depends on Codex being installed anywhere; it is worth doing
regardless of what the CLI probe finds.

## Architecture Notes

- Migration mechanics (blocking correction from architect review): SQLite
  refuses `ALTER TABLE ... ADD COLUMN x TEXT NOT NULL` without a default on a
  non-empty table. Use `ADD COLUMN agent TEXT NOT NULL DEFAULT 'claude'` for
  both `segments.agent` and `runs.agent` — this single statement satisfies the
  constraint and performs the backfill; do not write a separate `UPDATE`.
- `session_ref` (renamed from `claude_session_id` via `RENAME COLUMN`) keeps its
  full existing constraint set unchanged — write a migration test that snapshots
  the schema before and after and asserts this directly, not by inspection.
- `resume_token TEXT` is nullable. It is written only from a `RunResult`
  produced by a real run — never derived, never guessed, never computed ahead of
  time. `SetSegmentResumeToken` must never overwrite a non-empty token.
- Rename `naming.ClaudeSessionID` to `naming.SessionRef` in this same slice.
  Hard guardrail: the pinned namespace UUID and the
  `"<ref>/segment/<ordinal>"` input string must stay byte-identical across the
  rename — an identifier-only change. Getting this wrong changes every existing
  segment's derived id and makes every existing Claude conversation
  unresumable; test this explicitly, don't just review it.
- `litefactory logs --json` gains `agent` and `session_ref` keys and keeps
  `claude_session_id` as a deprecated alias, populated from `session_ref` when
  `agent == "claude"` and empty otherwise. The human-readable "claude session"
  label becomes "<agent> session".
- Confirm `0002` is actually the next free migration number before writing the
  file.
- None of this story's tasks are Codex-gated — nothing here depends on US-1's
  findings, and it can proceed on its own schedule.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-4.1 | Migration `0002_segment_agent.sql`: `RENAME COLUMN claude_session_id → session_ref` (constraints unchanged); `ADD COLUMN agent TEXT NOT NULL DEFAULT 'claude'` on `segments` and `runs`; `ADD COLUMN resume_token TEXT` (nullable) on `segments` | Schema | confirm 0002 is next free number | M |
| T-4.2 | Store accessors: read/write `session_ref`; `GetOrCreateSegment(ctx, sessionID, ordinal, sessionRef, kind)` gains the `kind` param, stored-row-wins semantics preserved | Code | T-4.1 | M |
| T-4.3 | `SetSegmentResumeToken(ctx, segmentID, token)` — never overwrites a non-empty token | Code | T-4.1 | S |
| T-4.4 | `runs.agent` write path threaded through run recording | Code | T-4.1 | S |
| T-4.5 | Rename `naming.ClaudeSessionID` → `naming.SessionRef`; guardrail test that the pinned namespace UUID and input string are byte-identical | Code | — | S |
| T-4.6 | `litefactory logs` command: `agent` + `session_ref` JSON keys, `claude_session_id` deprecated alias, "<agent> session" human label | Code | T-4.2 | S |
| T-4.7 | Migration tests: schema-constraint snapshot before/after the `RENAME COLUMN`, backfill correctness, a post-migration Claude resume still resumes | Test | T-4.1 | M |
| T-4.8 | Store accessor tests: resume-token overwrite refusal, `GetOrCreateSegment` stored-row-wins with `kind` | Test | T-4.2, T-4.3 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-4.1 | Migration `0002_segment_agent.sql` applies to a database populated by `0001` and preserves every existing `claude_session_id` value in `session_ref` |
| AC-4.2 | Existing rows are backfilled `agent = 'claude'` on both `segments` and `runs`, via the column `DEFAULT`, not a separate `UPDATE` |
| AC-4.3 | A migration test snapshots the schema's constraint set before and after the migration and asserts it is unchanged for `session_ref` (including any `UNIQUE` constraint carried over from `claude_session_id`) |
| AC-4.4 | `resume_token` is `NULL` for every backfilled row, and a post-migration Claude resume still resumes |
| AC-4.5 | `store` exposes `SetSegmentResumeToken`; it is written only after a successful run and a test asserts it never overwrites a non-empty token |
| AC-4.6 | No `claude_session_id` identifier remains anywhere in `internal/store` |
| AC-4.7 | `litefactory logs --json` emits `agent` and `session_ref`; `claude_session_id` still parses for a Claude run |
| AC-4.8 | `naming.SessionRef`'s guardrail test confirms the pinned namespace UUID and `"<ref>/segment/<ordinal>"` input string are byte-identical to `naming.ClaudeSessionID`'s pre-rename values |
