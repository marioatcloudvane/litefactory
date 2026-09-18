-- Graph definitions are immutable path/digest pins; DOT bytes stay outside SQLite.
CREATE TABLE graph_definitions (
    id INTEGER PRIMARY KEY,
    team TEXT NOT NULL CHECK (length(team) > 0),
    mode TEXT NOT NULL CHECK (length(mode) > 0),
    profile_version INTEGER NOT NULL CHECK (profile_version >= 1),
    path TEXT NOT NULL CHECK (length(path) > 0),
    digest TEXT NOT NULL CHECK (length(digest) = 64),
    team_snapshot_digest TEXT,
    created_at TEXT NOT NULL,
    UNIQUE (team, mode, digest)
);
CREATE TABLE graph_instances (
    id INTEGER PRIMARY KEY,
    session_id INTEGER NOT NULL REFERENCES sessions(id),
    definition_id INTEGER NOT NULL REFERENCES graph_definitions(id),
    status TEXT NOT NULL CHECK (status IN ('running','succeeded','failed','cancelled')),
    current_node TEXT NOT NULL CHECK (length(current_node) > 0),
    next_due_at TEXT,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    CHECK ((status = 'running' AND finished_at IS NULL) OR
           (status <> 'running' AND finished_at IS NOT NULL AND next_due_at IS NULL))
);
CREATE INDEX idx_graph_instances_due ON graph_instances(next_due_at, id);
CREATE TABLE graph_node_attempts (
    id INTEGER PRIMARY KEY,
    instance_id INTEGER NOT NULL REFERENCES graph_instances(id),
    node TEXT NOT NULL CHECK (length(node) > 0),
    attempt INTEGER NOT NULL CHECK (attempt > 0),
    run_id INTEGER REFERENCES runs(id),
    status TEXT NOT NULL CHECK (status IN ('pending','running','succeeded','failed','cancelled')),
    outcome TEXT,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    CHECK (outcome IS NULL OR status = 'succeeded'),
    CHECK ((status IN ('pending','running') AND finished_at IS NULL) OR
           (status IN ('succeeded','failed','cancelled') AND finished_at IS NOT NULL)),
    UNIQUE (instance_id, node, attempt)
);
CREATE UNIQUE INDEX idx_graph_attempt_running ON graph_node_attempts(instance_id, node) WHERE status = 'running';
CREATE TABLE graph_outputs (
    id INTEGER PRIMARY KEY,
    node_attempt_id INTEGER NOT NULL REFERENCES graph_node_attempts(id),
    kind TEXT NOT NULL CHECK (kind IN ('text','json','diagnostic')),
    payload BLOB NOT NULL,
    owner_comment_id INTEGER,
    created_at TEXT NOT NULL
);
CREATE INDEX idx_graph_outputs_attempt ON graph_outputs(node_attempt_id, id);
CREATE TABLE graph_transitions (
    id INTEGER PRIMARY KEY,
    instance_id INTEGER NOT NULL REFERENCES graph_instances(id),
    from_node TEXT NOT NULL CHECK (length(from_node) > 0),
    to_node TEXT NOT NULL CHECK (length(to_node) > 0),
    on_outcome TEXT NOT NULL CHECK (length(on_outcome) > 0),
    at TEXT NOT NULL
);
CREATE INDEX idx_graph_transitions_instance ON graph_transitions(instance_id, id);
