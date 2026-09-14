CREATE TABLE creation_publication_artifacts (
 operation_id INTEGER NOT NULL REFERENCES mcp_creation_operations(id),
 ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 0 AND 4),
 dispatched INTEGER NOT NULL DEFAULT 0 CHECK(dispatched IN (0,1)),
 issue_id INTEGER NOT NULL DEFAULT 0,
 issue_number INTEGER NOT NULL DEFAULT 0,
 relationship TEXT NOT NULL DEFAULT 'pending' CHECK(relationship IN ('pending','uncertain','linked','attached')),
 PRIMARY KEY(operation_id,ordinal)
);
CREATE TABLE creation_publication_requests (
 run_id INTEGER NOT NULL,
 attempt TEXT NOT NULL,
 request_id TEXT NOT NULL,
 operation_id INTEGER NOT NULL REFERENCES mcp_creation_operations(id),
 PRIMARY KEY(run_id,attempt,request_id),
 FOREIGN KEY(run_id,attempt,request_id) REFERENCES family_requests(run_id,attempt,request_id)
);
