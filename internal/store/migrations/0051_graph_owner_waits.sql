CREATE TABLE graph_owner_waits (
 delivery_key TEXT PRIMARY KEY REFERENCES graph_comment_intents(delivery_key),
 instance_id INTEGER NOT NULL REFERENCES graph_instances(id),
 source_attempt_id INTEGER NOT NULL REFERENCES graph_node_attempts(id),
 state TEXT NOT NULL CHECK(state IN ('waiting','claimed','allocated','cancelled')),
 reply_id INTEGER,
 reply_body TEXT,
 reply_author_id INTEGER,
 claimed_at TEXT,
 continuation_attempt_id INTEGER REFERENCES graph_node_attempts(id),
 CHECK(reply_body IS NULL OR length(CAST(reply_body AS BLOB)) <= 65536),
 UNIQUE(instance_id,reply_id)
);
CREATE UNIQUE INDEX idx_graph_owner_wait_active ON graph_owner_waits(instance_id) WHERE state IN ('waiting','claimed');
