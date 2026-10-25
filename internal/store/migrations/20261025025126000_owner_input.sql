CREATE TABLE owner_instruction_inputs (
 comment_id INTEGER PRIMARY KEY REFERENCES comments(id),
 session_id INTEGER NOT NULL REFERENCES sessions(id),
 target_instance_id INTEGER NOT NULL DEFAULT 0,
 target_node TEXT NOT NULL DEFAULT '',
 target_launch_id INTEGER NOT NULL DEFAULT 0,
 assigned_attempt_id INTEGER REFERENCES graph_node_attempts(id),
 assigned_at TEXT,
 CHECK ((target_instance_id=0 AND target_node='' AND target_launch_id=0) OR (target_instance_id>0 AND target_node<>'')),
 CHECK ((assigned_attempt_id IS NULL)=(assigned_at IS NULL))
);
CREATE INDEX owner_instruction_pending ON owner_instruction_inputs(session_id,assigned_attempt_id);
CREATE TABLE issue_owner_guidance (
 id INTEGER PRIMARY KEY,
 session_id INTEGER NOT NULL REFERENCES sessions(id),
 comment_id INTEGER NOT NULL UNIQUE REFERENCES comments(id),
 body TEXT NOT NULL,
 CHECK(length(CAST(body AS BLOB))<=65536)
);
CREATE TABLE owner_run_guidance (
 run_id INTEGER PRIMARY KEY REFERENCES runs(id),
 guidance_id INTEGER REFERENCES issue_owner_guidance(id)
);
