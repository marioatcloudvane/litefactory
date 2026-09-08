CREATE TABLE story_progress_runs (
 run_id INTEGER PRIMARY KEY REFERENCES runs(id),
 parent_task TEXT NOT NULL,
 generation INTEGER NOT NULL,
 worktree TEXT NOT NULL,
 active INTEGER NOT NULL DEFAULT 1,
 last_sequence INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE story_progress_events (
 run_id INTEGER NOT NULL REFERENCES story_progress_runs(run_id),
 sequence INTEGER NOT NULL,
 story_key TEXT NOT NULL,
 attempt INTEGER NOT NULL,
 kind TEXT NOT NULL,
 digest TEXT NOT NULL,
 payload BLOB NOT NULL,
 PRIMARY KEY(run_id,sequence)
);
CREATE TABLE story_progress_effects (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 run_id INTEGER NOT NULL REFERENCES story_progress_runs(run_id),
 sequence INTEGER NOT NULL,
 parent_task TEXT NOT NULL,
 generation INTEGER NOT NULL,
 story_key TEXT NOT NULL,
 issue_number INTEGER NOT NULL,
 issue_id INTEGER NOT NULL,
 parent_id INTEGER NOT NULL,
 revision TEXT NOT NULL,
 kind TEXT NOT NULL,
 detail TEXT NOT NULL,
 implementation_digest TEXT NOT NULL DEFAULT '',
 comment_id INTEGER NOT NULL DEFAULT 0,
 delivered INTEGER NOT NULL DEFAULT 0,
 superseded INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX story_pending_effects ON story_progress_effects(delivered,superseded,id);
CREATE TABLE story_progress_scope (
 run_id INTEGER NOT NULL REFERENCES story_progress_runs(run_id),
 story_key TEXT NOT NULL,
 PRIMARY KEY(run_id,story_key)
);
ALTER TABLE story_progress_events ADD COLUMN conflicted INTEGER NOT NULL DEFAULT 0;
