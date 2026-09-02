-- 0003_agent — splits the single claude_session_id column into two ideas of
-- "which conversation", and records which coding-agent tool owns each segment
-- and each run.
--
-- Right up to this migration the store has had exactly one idea of "session
-- id", shaped around how Claude happens to work: litefactory derives the id
-- itself, before anything runs, and passes it to `claude --session-id`. A
-- second tool that only reports its own conversation handle *after* a run has
-- started — Codex's own thread id — has nowhere to put that without either
-- forcing it into a column shaped for the other tool's contract or fabricating
-- a value up front. Both are corruption of the one fact this table exists to
-- keep straight.
--
-- The fix is two columns instead of one, on segments:
--
--   - session_ref (renamed from claude_session_id) is litefactory's own
--     pre-computed identity, always computable before any process runs, and
--     stays NOT NULL — internal/naming derives it the same way regardless of
--     which agent owns the segment.
--   - resume_token is the tool's own reported handle, sometimes absent (a cold
--     Claude run only echoes back the id it was given, so the two are the same
--     value; a tool that discovers its handle after the fact leaves this NULL
--     until a real run reports one). It is nullable, and is written only from a
--     RunResult a real run produced — never derived, never guessed, never
--     computed ahead of time.
--
-- The rename keeps session_ref's full existing constraint set unchanged: it
-- was NOT NULL as claude_session_id and stays NOT NULL as session_ref. SQLite's
-- RENAME COLUMN touches only the name, never the type or the constraints.
--
-- `agent` is added to both segments and runs so that a future "resume this
-- Codex thread" feature has somewhere to write today rather than needing yet
-- another migration once Codex support lands. Every schema change to this
-- table is one-way once real conversations exist in it, so the shape is sized
-- generously now.
--
-- SQLite refuses `ALTER TABLE ... ADD COLUMN x TEXT NOT NULL` on a non-empty
-- table unless a DEFAULT is given. `DEFAULT 'claude'` satisfies the
-- constraint and performs the backfill in the same statement — every row that
-- predates this migration was a Claude run, because Claude was the only agent
-- litefactory could run before this feature — so no separate UPDATE is needed
-- or written.
ALTER TABLE segments RENAME COLUMN claude_session_id TO session_ref;
ALTER TABLE segments ADD COLUMN agent TEXT NOT NULL DEFAULT 'claude';
ALTER TABLE segments ADD COLUMN resume_token TEXT;

ALTER TABLE runs ADD COLUMN agent TEXT NOT NULL DEFAULT 'claude';
