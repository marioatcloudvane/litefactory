CREATE TABLE project_approver_appointments (
 id INTEGER PRIMARY KEY,
 repo_id INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
 revision INTEGER NOT NULL CHECK(revision > 0),
 provider TEXT NOT NULL, api_origin TEXT NOT NULL, actor_id TEXT NOT NULL,
 handle TEXT NOT NULL, verified_at TEXT NOT NULL, provenance TEXT NOT NULL,
 activated_at TEXT NOT NULL, require_fresh INTEGER NOT NULL CHECK(require_fresh IN (0,1)),
 UNIQUE(repo_id,revision)
);
CREATE TABLE project_approvers (
 repo_id INTEGER PRIMARY KEY REFERENCES repos(id) ON DELETE CASCADE,
 appointment_id INTEGER NOT NULL REFERENCES project_approver_appointments(id)
);
CREATE TABLE project_authority_claims (
 id INTEGER PRIMARY KEY,
 repo_id INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
 appointment_id INTEGER REFERENCES project_approver_appointments(id),
 revision INTEGER NOT NULL, actor_provider TEXT NOT NULL, actor_origin TEXT NOT NULL, actor_id TEXT NOT NULL,
 source_provider TEXT NOT NULL, source_origin TEXT NOT NULL, source_id TEXT NOT NULL,
 source_created_at TEXT NOT NULL, kind TEXT NOT NULL, claim_key TEXT NOT NULL,
 completed INTEGER NOT NULL DEFAULT 0 CHECK(completed IN (0,1)), created_at TEXT NOT NULL,
 UNIQUE(repo_id,kind,claim_key)
);
CREATE TABLE project_approver_initialization (
 repo_id INTEGER PRIMARY KEY REFERENCES repos(id) ON DELETE CASCADE,
 status TEXT NOT NULL, reason TEXT NOT NULL, updated_at TEXT NOT NULL
);

CREATE TABLE project_legacy_approver_eligibility (
 repo_id INTEGER PRIMARY KEY REFERENCES repos(id) ON DELETE CASCADE
);
INSERT INTO project_legacy_approver_eligibility(repo_id) SELECT id FROM repos;

CREATE TABLE project_legacy_approver_identities (
 repo_id INTEGER PRIMARY KEY REFERENCES repos(id) ON DELETE CASCADE,
 forge TEXT NOT NULL, role TEXT NOT NULL, handle TEXT NOT NULL,
 user_id INTEGER, source TEXT NOT NULL, derived_at TEXT NOT NULL
);
INSERT INTO project_legacy_approver_identities SELECT r.id,i.forge,i.role,i.handle,i.user_id,i.source,i.derived_at FROM repos r JOIN identities i ON i.forge='github' AND i.role='owner';
CREATE TABLE project_legacy_authority_evidence (
 repo_id INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
 appointment_id INTEGER NOT NULL REFERENCES project_approver_appointments(id),
 record_kind TEXT NOT NULL, record_key TEXT NOT NULL,
 actor_provider TEXT NOT NULL, actor_origin TEXT NOT NULL, actor_id TEXT NOT NULL,
 provenance TEXT NOT NULL,
 PRIMARY KEY(repo_id,record_kind,record_key)
);
CREATE TABLE doctor_command_sources (
 command_id INTEGER PRIMARY KEY REFERENCES doctor_inbox(id),
 connection_version_id INTEGER NOT NULL REFERENCES project_connection_versions(id),
 publisher_id INTEGER NOT NULL CHECK(publisher_id>0),
 provider TEXT NOT NULL, api_origin TEXT NOT NULL, remote_id TEXT NOT NULL,
 container TEXT NOT NULL, locator_key TEXT NOT NULL
);
