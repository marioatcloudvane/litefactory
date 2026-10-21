CREATE TABLE standards_authoring_sessions (
 id TEXT PRIMARY KEY, mode TEXT NOT NULL CHECK(mode IN ('create','edit')), target_id TEXT NOT NULL,
 current_version INTEGER NOT NULL, status TEXT NOT NULL, active_attempt TEXT,
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE standards_authoring_versions (
 session_id TEXT NOT NULL REFERENCES standards_authoring_sessions(id), version INTEGER NOT NULL,
 status TEXT NOT NULL, data_json TEXT NOT NULL, created_at TEXT NOT NULL,
 PRIMARY KEY(session_id,version)
);
CREATE TABLE standards_authoring_attempts (
 id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES standards_authoring_sessions(id),
 input_version INTEGER NOT NULL, token TEXT NOT NULL UNIQUE, lease_until TEXT NOT NULL,
 attempt_json TEXT NOT NULL,
 FOREIGN KEY(session_id,input_version) REFERENCES standards_authoring_versions(session_id,version)
);
CREATE TABLE standards_authoring_completions (
 session_id TEXT PRIMARY KEY REFERENCES standards_authoring_sessions(id), version INTEGER NOT NULL,
 proposal_id TEXT NOT NULL UNIQUE, proposal_version INTEGER NOT NULL,
 FOREIGN KEY(session_id,version) REFERENCES standards_authoring_versions(session_id,version),
 FOREIGN KEY(proposal_id,proposal_version) REFERENCES standards_proposal_versions(proposal_id,version)
);
CREATE TRIGGER standards_authoring_versions_immutable_update BEFORE UPDATE ON standards_authoring_versions BEGIN SELECT RAISE(ABORT, 'immutable authoring version'); END;
CREATE TRIGGER standards_authoring_versions_immutable_delete BEFORE DELETE ON standards_authoring_versions BEGIN SELECT RAISE(ABORT, 'immutable authoring version'); END;
CREATE TRIGGER standards_authoring_completions_immutable_update BEFORE UPDATE ON standards_authoring_completions BEGIN SELECT RAISE(ABORT, 'immutable authoring completion'); END;
CREATE TRIGGER standards_authoring_completions_immutable_delete BEFORE DELETE ON standards_authoring_completions BEGIN SELECT RAISE(ABORT, 'immutable authoring completion'); END;
