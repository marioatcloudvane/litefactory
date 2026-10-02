-- Spike-owned retained records of one graph instance: the worktree baseline a
-- gate saved, the measured check of the change made after it, and the issue
-- filing state. Each is keyed by instance and writing node, so a replay finds
-- and refreshes its own record and never another node's.
CREATE TABLE spike_records (
 instance_id INTEGER NOT NULL REFERENCES graph_instances(id),
 node TEXT NOT NULL CHECK(length(node)>0),
 kind TEXT NOT NULL CHECK(kind IN ('baseline','check','issue')),
 payload TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 PRIMARY KEY(instance_id,node,kind)
);
