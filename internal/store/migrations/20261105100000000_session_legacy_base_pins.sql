CREATE TABLE session_legacy_base_pins (
 session_id INTEGER PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
 repo_id INTEGER NOT NULL REFERENCES repos(id),
 base_branch TEXT NOT NULL,
 created_at TEXT NOT NULL
);
