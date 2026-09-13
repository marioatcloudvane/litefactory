CREATE TABLE retirement_intents (
 id INTEGER PRIMARY KEY,
 session_id INTEGER NOT NULL REFERENCES sessions(id),
 sweep_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('worktree','local_branch','remote_branch')),
 resource TEXT NOT NULL,
 expected_sha TEXT NOT NULL,
 base_sha TEXT NOT NULL,
 action TEXT NOT NULL,
 outcome TEXT NOT NULL DEFAULT 'unknown',
 reason TEXT NOT NULL DEFAULT 'intent_recorded',
 observed_at TEXT NOT NULL,
 UNIQUE(session_id,kind,resource)
);
CREATE INDEX retirement_intents_owner ON retirement_intents(session_id,id);
