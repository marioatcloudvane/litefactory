CREATE TABLE graph_effects (
 operation TEXT PRIMARY KEY,
 instance_id INTEGER NOT NULL REFERENCES graph_instances(id),
 source_attempt_id INTEGER NOT NULL REFERENCES graph_node_attempts(id),
 kind TEXT NOT NULL,
 binding BLOB NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('intent','dispatched','confirmed','uncertain','refused')),
 claim_attempt_id INTEGER REFERENCES graph_node_attempts(id),
 observation BLOB NOT NULL DEFAULT X'',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);

-- A mechanical owner continuation may authorize one immediately declared agent
-- invocation without spending that allowance on publication/conversation work.
ALTER TABLE graph_owner_waits ADD COLUMN allowance_attempt_id INTEGER REFERENCES graph_node_attempts(id);
