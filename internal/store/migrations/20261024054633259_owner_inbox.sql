CREATE TABLE owner_inbox_snapshots (
 id TEXT PRIMARY KEY,
 owner TEXT NOT NULL,
 transport TEXT NOT NULL,
 conversation TEXT NOT NULL,
 captured_at TEXT NOT NULL,
 expires_at TEXT NOT NULL,
 metadata TEXT NOT NULL
);
CREATE INDEX owner_inbox_expiry ON owner_inbox_snapshots(expires_at);
CREATE TABLE owner_inbox_entries (
 snapshot_id TEXT NOT NULL REFERENCES owner_inbox_snapshots(id) ON DELETE CASCADE,
 ordinal INTEGER NOT NULL,
 decision_key TEXT NOT NULL,
 content TEXT NOT NULL,
 PRIMARY KEY(snapshot_id,ordinal),
 UNIQUE(snapshot_id,decision_key)
);
