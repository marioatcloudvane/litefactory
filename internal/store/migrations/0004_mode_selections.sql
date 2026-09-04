-- Durable routing decision and one-shot label cleanup for a trigger.
CREATE TABLE mode_selections (
    session_id        INTEGER NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    trigger_comment_id INTEGER NOT NULL,
    mode              TEXT NOT NULL,
    label             TEXT NOT NULL,
    label_applied     INTEGER NOT NULL DEFAULT 0,
    cleanup_pending   INTEGER NOT NULL DEFAULT 0,
    created_at        TEXT NOT NULL,
    PRIMARY KEY (session_id, trigger_comment_id)
);

CREATE INDEX idx_mode_selections_cleanup
ON mode_selections (session_id) WHERE cleanup_pending = 1;
