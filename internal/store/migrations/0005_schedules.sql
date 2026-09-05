-- Durable factory-owned scheduled work and agent-proposed schedules.
CREATE TABLE schedules (
    id                INTEGER PRIMARY KEY,
    session_id        INTEGER NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    source_comment_id INTEGER NOT NULL,
    ordinal           INTEGER NOT NULL DEFAULT 1,
    owner_handle      TEXT NOT NULL,
    payload           TEXT NOT NULL,
    due_at            TEXT,
    delay_ms          INTEGER NOT NULL DEFAULT 0,
    status            TEXT NOT NULL,
    run_id            INTEGER REFERENCES runs (id) ON DELETE SET NULL,
    started_at        TEXT,
    finished_at       TEXT,
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL,
    UNIQUE (session_id, source_comment_id, ordinal)
);

CREATE INDEX idx_schedules_pending_due
ON schedules (status, due_at, id);

CREATE TABLE schedule_prerequisites (
    schedule_id      INTEGER NOT NULL REFERENCES schedules (id) ON DELETE CASCADE,
    prerequisite_id  INTEGER NOT NULL REFERENCES schedules (id) ON DELETE CASCADE,
    PRIMARY KEY (schedule_id, prerequisite_id),
    CHECK (schedule_id <> prerequisite_id)
);

CREATE INDEX idx_schedule_prerequisites_prerequisite
ON schedule_prerequisites (prerequisite_id, schedule_id);

CREATE TABLE schedule_proposals (
    id                INTEGER PRIMARY KEY,
    session_id        INTEGER NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    source_comment_id INTEGER NOT NULL,
    owner_handle      TEXT NOT NULL,
    description       TEXT NOT NULL,
    status            TEXT NOT NULL,
    created_at        TEXT NOT NULL,
    decided_at        TEXT,
    UNIQUE (session_id, source_comment_id)
);

CREATE TABLE schedule_proposal_items (
    id           INTEGER PRIMARY KEY,
    proposal_id  INTEGER NOT NULL REFERENCES schedule_proposals (id) ON DELETE CASCADE,
    ordinal      INTEGER NOT NULL,
    payload      TEXT NOT NULL,
    due_at       TEXT NOT NULL,
    UNIQUE (proposal_id, ordinal)
);
