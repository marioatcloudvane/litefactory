CREATE TABLE delivery_code_authorities (
 session_id INTEGER PRIMARY KEY REFERENCES sessions(id),
 code_version_id INTEGER NOT NULL REFERENCES project_connection_versions(id),
 api_origin TEXT NOT NULL,
 repository TEXT NOT NULL,
 repository_id TEXT NOT NULL,
 owner_id INTEGER NOT NULL CHECK(owner_id>0),
 publisher_id INTEGER NOT NULL CHECK(publisher_id>0 AND publisher_id<>owner_id)
);
CREATE TABLE delivery_pr_bindings (
 proposal_key TEXT PRIMARY KEY REFERENCES delivery_proposals(proposal_key),
 session_id INTEGER NOT NULL REFERENCES delivery_code_authorities(session_id),
 pr_id INTEGER NOT NULL CHECK(pr_id>0),
 pr_number INTEGER NOT NULL CHECK(pr_number>0),
 enrolled_at TEXT NOT NULL
);
CREATE TABLE delivery_control_sources (
 source_id TEXT PRIMARY KEY,
 session_id INTEGER NOT NULL REFERENCES delivery_code_authorities(session_id),
 kind TEXT NOT NULL CHECK(kind IN ('github-cli-request','github-pr-comment')),
 proposal_key TEXT NOT NULL DEFAULT '',
 generation INTEGER NOT NULL CHECK(generation>0),
 actor_id INTEGER NOT NULL CHECK(actor_id>0),
 comment_id INTEGER NOT NULL DEFAULT 0,
 body TEXT NOT NULL,
 created_at TEXT NOT NULL,
 completed INTEGER NOT NULL DEFAULT 0 CHECK(completed IN (0,1)),
 CHECK((kind='github-cli-request' AND comment_id=0 AND proposal_key='') OR
       (kind='github-pr-comment' AND comment_id>0 AND proposal_key<>''))
);
ALTER TABLE delivery_proposals ADD COLUMN approval_source_id TEXT NOT NULL DEFAULT '';
