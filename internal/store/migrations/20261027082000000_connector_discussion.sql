CREATE TABLE connector_grant_targets (
 grant_id TEXT PRIMARY KEY REFERENCES connector_grants(id) ON DELETE CASCADE,
 task_ref TEXT NOT NULL,
 session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 repo_id INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
 repository_ref TEXT NOT NULL
);
CREATE TRIGGER connector_grant_targets_immutable BEFORE UPDATE ON connector_grant_targets
BEGIN SELECT RAISE(ABORT, 'connector target is immutable'); END;
