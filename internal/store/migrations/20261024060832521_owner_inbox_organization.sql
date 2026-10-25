CREATE TABLE owner_inbox_organizations (
 snapshot_id TEXT NOT NULL REFERENCES owner_inbox_snapshots(id) ON DELETE CASCADE,
 version INTEGER NOT NULL CHECK(version>0),
 content TEXT NOT NULL,
 PRIMARY KEY(snapshot_id,version)
);
