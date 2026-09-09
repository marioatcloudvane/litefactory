-- Semantic evidence survives the reporting lifecycle. Manual schedule identity
-- is unchanged; automatic work is owned by the interrupted run instead.
ALTER TABLE runs ADD COLUMN semantic_outcome TEXT NOT NULL DEFAULT '';
ALTER TABLE runs ADD COLUMN recovery_reason TEXT NOT NULL DEFAULT '';
CREATE TABLE automatic_continuations (
 id INTEGER PRIMARY KEY,
 source_run_id INTEGER NOT NULL UNIQUE REFERENCES runs(id),
 root_run_id INTEGER NOT NULL REFERENCES runs(id),
 session_id INTEGER NOT NULL REFERENCES sessions(id),
 segment_id INTEGER NOT NULL REFERENCES segments(id),
 attempt INTEGER NOT NULL CHECK(attempt BETWEEN 1 AND 3),
 origin TEXT NOT NULL DEFAULT 'session_limit' CHECK(origin = 'session_limit'),
 due_at TEXT NOT NULL,
 reset_bound TEXT NOT NULL,
 precision TEXT NOT NULL,
 context_json TEXT NOT NULL,
 prerequisite_id INTEGER REFERENCES schedules(id),
 status TEXT NOT NULL DEFAULT 'pending',
 allocated_run_id INTEGER UNIQUE REFERENCES runs(id),
 reserved_at TEXT,
 finished_at TEXT,
 cancellation_reason TEXT NOT NULL DEFAULT '',
 announcement_comment_id INTEGER,
 cancellation_comment_id INTEGER,
 created_at TEXT NOT NULL
);
CREATE INDEX automatic_continuations_pending ON automatic_continuations(status, due_at, id);
CREATE INDEX automatic_continuations_chain ON automatic_continuations(root_run_id, attempt);
-- Explicitly records why an original prerequisite is nonterminal during a chain.
ALTER TABLE schedules ADD COLUMN recovery_root_run_id INTEGER REFERENCES runs(id);
