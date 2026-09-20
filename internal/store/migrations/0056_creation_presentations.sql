CREATE TABLE mcp_creation_presentations (
 run_id INTEGER PRIMARY KEY REFERENCES runs(id),
 session_id INTEGER NOT NULL REFERENCES sessions(id),
 source_task TEXT NOT NULL REFERENCES sessions(task_ref),
 proposal_hash TEXT NOT NULL REFERENCES mcp_creation_proposals(hash),
 relationship TEXT NOT NULL CHECK(relationship IN ('linked','attach')),
 artifacts INTEGER NOT NULL CHECK(artifacts BETWEEN 1 AND 5),
 retained_ns INTEGER NOT NULL,
 delivery_key TEXT NOT NULL DEFAULT '',
 presented INTEGER NOT NULL DEFAULT 0 CHECK(presented IN (0,1))
);
