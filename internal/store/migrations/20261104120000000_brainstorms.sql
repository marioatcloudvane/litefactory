CREATE TABLE brainstorms (
 id TEXT PRIMARY KEY, owner_id INTEGER NOT NULL, owner_generation INTEGER NOT NULL, repository_ref TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('open','closed')),
 close_reason TEXT NOT NULL DEFAULT '' CHECK(close_reason IN ('','owner','expired')),
 revision INTEGER NOT NULL DEFAULT 0, next_seq INTEGER NOT NULL DEFAULT 1, next_scan_seq INTEGER NOT NULL DEFAULT 1,
 grant_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, last_activity_at TEXT NOT NULL, closed_at TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX brainstorms_one_open ON brainstorms(owner_id,owner_generation,repository_ref) WHERE state='open';
CREATE INDEX brainstorms_by_repo ON brainstorms(owner_id,owner_generation,repository_ref,created_at);
CREATE TABLE brainstorm_entries (
 id TEXT PRIMARY KEY, brainstorm_id TEXT NOT NULL REFERENCES brainstorms(id), seq INTEGER NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('idea','decision','open_question','related_issue','document_note','exchange')),
 text TEXT NOT NULL, document_name TEXT NOT NULL DEFAULT '', issue_reference TEXT NOT NULL DEFAULT '', scan_id TEXT NOT NULL DEFAULT '',
 request_key TEXT NOT NULL, digest TEXT NOT NULL, created_at TEXT NOT NULL,
 CHECK((kind='document_note') = (document_name<>'')),
 UNIQUE(brainstorm_id,seq), UNIQUE(brainstorm_id,request_key)
);
CREATE TABLE brainstorm_scans (
 id TEXT PRIMARY KEY, brainstorm_id TEXT NOT NULL REFERENCES brainstorms(id), scan_seq INTEGER NOT NULL,
 owner_id INTEGER NOT NULL, owner_generation INTEGER NOT NULL, repository_ref TEXT NOT NULL,
 request_key TEXT NOT NULL, digest TEXT NOT NULL, question TEXT NOT NULL,
 team_version TEXT NOT NULL, team_files TEXT NOT NULL,
 basis_commit TEXT NOT NULL DEFAULT '', grant_id TEXT NOT NULL, consent_revision TEXT NOT NULL,
 job_id TEXT NOT NULL UNIQUE, execution_id TEXT NOT NULL UNIQUE,
 answer TEXT NOT NULL DEFAULT '', research TEXT NOT NULL DEFAULT '', questions TEXT NOT NULL DEFAULT '[]',
 usage TEXT NOT NULL DEFAULT '', cost TEXT NOT NULL DEFAULT '', failure_reason TEXT NOT NULL DEFAULT '',
 exchange_seq INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, finished_at TEXT NOT NULL DEFAULT '',
 UNIQUE(brainstorm_id,scan_seq), UNIQUE(brainstorm_id,request_key)
);
