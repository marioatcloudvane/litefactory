CREATE TABLE graph_comment_intents (
    delivery_key TEXT PRIMARY KEY,
    instance_id INTEGER NOT NULL REFERENCES graph_instances(id),
    source_attempt_id INTEGER NOT NULL REFERENCES graph_node_attempts(id),
    node TEXT NOT NULL,
    kind TEXT NOT NULL CHECK(kind IN ('question','final','failure')),
    candidate BLOB NOT NULL CHECK(length(candidate) BETWEEN 1 AND 1048576)
);
