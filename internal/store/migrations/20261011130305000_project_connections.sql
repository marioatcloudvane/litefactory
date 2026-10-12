-- litefactory:rebuild-sessions
CREATE TABLE sessions_replacement (
 id INTEGER PRIMARY KEY,
 repo_id INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
 issue_number INTEGER NOT NULL, task_ref TEXT NOT NULL UNIQUE,
 feature_slug TEXT, branch TEXT, worktree_path TEXT, state TEXT NOT NULL,
 lock_owner TEXT, lock_acquired_at TEXT, created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL, needs_poll INTEGER NOT NULL DEFAULT 0
);
INSERT INTO sessions_replacement SELECT id,repo_id,issue_number,task_ref,feature_slug,branch,worktree_path,state,lock_owner,lock_acquired_at,created_at,updated_at,needs_poll FROM sessions;
DROP TABLE sessions;
ALTER TABLE sessions_replacement RENAME TO sessions;
CREATE TABLE project_connection_versions (
 id INTEGER PRIMARY KEY,
 repo_id INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
 role TEXT NOT NULL CHECK(role IN ('code','ticket')),
 revision INTEGER NOT NULL CHECK(revision > 0),
 provider TEXT NOT NULL, api_origin TEXT NOT NULL, destination TEXT NOT NULL,
 remote_id TEXT NOT NULL DEFAULT '',
 credential_source TEXT NOT NULL DEFAULT '', credential_service TEXT NOT NULL DEFAULT '',
 credential_account TEXT NOT NULL DEFAULT '', credential_env TEXT NOT NULL DEFAULT '',
 enabled INTEGER NOT NULL, provenance TEXT NOT NULL,
 clone_path TEXT NOT NULL DEFAULT '', remote_url TEXT NOT NULL DEFAULT '',
 base_branch TEXT NOT NULL DEFAULT '', feature_prefix TEXT NOT NULL DEFAULT '', worktree_root TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 UNIQUE(repo_id,role,revision)
);
CREATE TABLE project_connections (
 repo_id INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
 role TEXT NOT NULL, version_id INTEGER NOT NULL REFERENCES project_connection_versions(id),
 PRIMARY KEY(repo_id,role)
);
CREATE TABLE project_ticket_bindings (
 id INTEGER PRIMARY KEY,
 repo_id INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
 connection_version_id INTEGER NOT NULL REFERENCES project_connection_versions(id),
 revision INTEGER NOT NULL,
 provider TEXT NOT NULL, api_origin TEXT NOT NULL, remote_id TEXT NOT NULL,
 container TEXT NOT NULL, locator_key TEXT NOT NULL,
 enabled INTEGER NOT NULL, provenance TEXT NOT NULL,
 UNIQUE(provider,api_origin,remote_id)
);
CREATE TABLE session_execution_snapshots (
 session_id INTEGER PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
 repo_id INTEGER NOT NULL REFERENCES repos(id),
 binding_id INTEGER NOT NULL REFERENCES project_ticket_bindings(id),
 code_version_id INTEGER NOT NULL REFERENCES project_connection_versions(id),
 ticket_version_id INTEGER NOT NULL REFERENCES project_connection_versions(id),
 actor_id INTEGER NOT NULL CHECK(actor_id > 0), actor_login TEXT NOT NULL,
 code_remote_id TEXT NOT NULL DEFAULT '', ticket_remote_id TEXT NOT NULL DEFAULT '',
 naming_suffix TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
);
CREATE TABLE project_connection_observations (
 version_id INTEGER PRIMARY KEY REFERENCES project_connection_versions(id) ON DELETE CASCADE,
 checked_at TEXT NOT NULL, status TEXT NOT NULL, reason TEXT NOT NULL, remedy TEXT NOT NULL,
 capabilities TEXT NOT NULL
);
CREATE TABLE project_connection_imports (
 repo_id INTEGER PRIMARY KEY REFERENCES repos(id) ON DELETE CASCADE,
 status TEXT NOT NULL, reason TEXT NOT NULL, updated_at TEXT NOT NULL
);
