CREATE TABLE mcp_runtime (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 instance TEXT NOT NULL
);
CREATE TABLE mcp_attempt_leases (
 run_id INTEGER NOT NULL,
 attempt TEXT NOT NULL,
 instance TEXT NOT NULL,
 permission_digest TEXT NOT NULL,
 expires_ns INTEGER NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('active','replaced','cancelled','closed')),
 PRIMARY KEY(run_id,attempt),
 FOREIGN KEY(run_id,attempt) REFERENCES application_result_attempts(run_id,attempt)
);
