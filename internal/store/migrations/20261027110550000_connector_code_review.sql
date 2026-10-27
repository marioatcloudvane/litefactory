CREATE TABLE connector_code_targets (
 grant_id TEXT PRIMARY KEY REFERENCES connector_grants(id) ON DELETE CASCADE,
 task_ref TEXT NOT NULL,
 session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 repo_id INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
 code_version_id INTEGER NOT NULL REFERENCES project_connection_versions(id),
 repository_ref TEXT NOT NULL,
 clone_path TEXT NOT NULL,
 remote_url TEXT NOT NULL,
 task_branch TEXT NOT NULL,
 base_branch TEXT NOT NULL,
 code_remote_id TEXT NOT NULL
);
CREATE TRIGGER connector_code_targets_immutable BEFORE UPDATE ON connector_code_targets
BEGIN SELECT RAISE(ABORT, 'connector code target is immutable'); END;
