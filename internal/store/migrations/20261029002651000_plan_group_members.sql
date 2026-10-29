-- Plan-only child attempts retain their own execution identities while the
-- ordinary instance stays at its coordinator node.
CREATE TABLE plan_group_members (
 id INTEGER PRIMARY KEY,
 parent_attempt_id INTEGER NOT NULL REFERENCES graph_node_attempts(id),
 role TEXT NOT NULL CHECK(role IN ('story','technical','acceptance')),
 node TEXT NOT NULL CHECK(length(node)>0),
 basis TEXT NOT NULL CHECK(length(basis)>0),
 sequence INTEGER NOT NULL CHECK(sequence>0),
 attempt_id INTEGER UNIQUE REFERENCES graph_node_attempts(id),
 run_id INTEGER UNIQUE REFERENCES runs(id),
 UNIQUE(parent_attempt_id,role,sequence)
);
CREATE INDEX plan_group_members_parent ON plan_group_members(parent_attempt_id,id);
