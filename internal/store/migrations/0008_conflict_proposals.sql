-- 0008_conflict_proposals — durable record of a suggested resolution for a
-- run that was refused because its task branch could not merge cleanly with
-- the base branch (US-4, US-11).
--
-- This story has no confirmation grammar: a proposal is produced, persisted
-- and posted in one terminal run, and there is no accept/reject path yet
-- (that is later work, out of scope here). The status vocabulary is
-- therefore only three values wide — pending, superseded, expired — rather
-- than schedule_proposals' four: nothing in this story ever decides a
-- proposal, so there is no "accepted"/"rejected" state to reach.
--
-- Only the model's own suggested resolution and the conflicting file paths
-- are stored. The raw conflicting hunks/content collected from the worktree
-- and sent to the model are never written to any column, in this table or
-- any other (S-9, S-17): a row here answers "what did litefactory suggest",
-- never "what did the conflicting code look like".
CREATE TABLE conflict_proposals (
    id                 INTEGER PRIMARY KEY,
    session_id         INTEGER NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    trigger_comment_id INTEGER NOT NULL,
    model              TEXT NOT NULL,
    status             TEXT NOT NULL,
    created_at         TEXT NOT NULL,
    resolved_at        TEXT,
    -- One session may have at most one durable proposal per triggering
    -- comment; replaying the same blocked-run trigger returns the original
    -- proposal rather than creating a second one, the same replay guarantee
    -- schedule_proposals gives on (session_id, source_comment_id).
    UNIQUE (session_id, trigger_comment_id)
);

-- Supersede any earlier pending proposal for the same session as soon as a
-- new one is recorded (AC-11.8): a session never has two pending proposals.
CREATE INDEX idx_conflict_proposals_session_status
ON conflict_proposals (session_id, status);

CREATE TABLE conflict_proposal_files (
    id          INTEGER PRIMARY KEY,
    proposal_id INTEGER NOT NULL REFERENCES conflict_proposals (id) ON DELETE CASCADE,
    ordinal     INTEGER NOT NULL,
    path        TEXT NOT NULL,
    resolution  TEXT NOT NULL,
    UNIQUE (proposal_id, ordinal)
);
