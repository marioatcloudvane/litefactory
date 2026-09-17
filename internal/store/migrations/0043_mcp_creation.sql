ALTER TABLE mcp_attempt_leases ADD COLUMN bound_ns INTEGER NOT NULL DEFAULT 0;
CREATE TABLE mcp_creation_proposals (
 hash TEXT PRIMARY KEY,
 repository TEXT NOT NULL,
 source_task TEXT NOT NULL REFERENCES sessions(task_ref),
 relationship TEXT NOT NULL CHECK(relationship IN ('linked','attach')),
 artifacts INTEGER NOT NULL CHECK(artifacts BETWEEN 1 AND 5),
 envelope BLOB NOT NULL,
 retained_ns INTEGER NOT NULL
);
CREATE TABLE mcp_creation_current (
 source_task TEXT PRIMARY KEY REFERENCES sessions(task_ref),
 proposal_hash TEXT NOT NULL REFERENCES mcp_creation_proposals(hash),
 revision INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE mcp_creation_intents (
 id INTEGER PRIMARY KEY,
 repository TEXT NOT NULL,
 source_task TEXT NOT NULL,
 comment_id INTEGER NOT NULL,
 owner_id INTEGER NOT NULL,
 proposal_revision INTEGER NOT NULL,
 proposal_hash TEXT NOT NULL REFERENCES mcp_creation_proposals(hash),
 created_ns INTEGER NOT NULL,
 expires_ns INTEGER NOT NULL,
 UNIQUE(repository,comment_id)
);
CREATE TABLE mcp_creation_grants (
 id INTEGER PRIMARY KEY,
 intent_id INTEGER NOT NULL UNIQUE REFERENCES mcp_creation_intents(id),
 run_id INTEGER NOT NULL,
 attempt TEXT NOT NULL,
 instance TEXT NOT NULL,
 expires_ns INTEGER NOT NULL,
 UNIQUE(run_id,attempt),
 FOREIGN KEY(run_id,attempt) REFERENCES mcp_attempt_leases(run_id,attempt)
);
CREATE TABLE mcp_creation_operations (
 id INTEGER PRIMARY KEY,
 grant_id INTEGER NOT NULL UNIQUE REFERENCES mcp_creation_grants(id),
 repository TEXT NOT NULL,
 proposal_hash TEXT NOT NULL REFERENCES mcp_creation_proposals(hash),
 artifacts INTEGER NOT NULL CHECK(artifacts BETWEEN 1 AND 5),
 owner_digest TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('reserved','dispatched','confirmed','uncertain','failed','released')),
 dispatched INTEGER NOT NULL DEFAULT 0 CHECK(dispatched IN (0,1)),
 confirmed_ns INTEGER NOT NULL DEFAULT 0,
 created_ns INTEGER NOT NULL
);
CREATE INDEX mcp_creation_quota ON mcp_creation_operations(repository,state,confirmed_ns);
