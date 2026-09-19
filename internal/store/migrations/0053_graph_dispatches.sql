-- A graph instance advances one agent stage per run, because one comment causes
-- at most one run and every agent stage needs its own. A dispatch row is that
-- run's trigger: the factory mints a private identity the same way automatic
-- continuations and manual schedules already do, so the runs table's uniqueness
-- and its whole crash-replay guarantee stay exactly as they are.
--
-- One pending dispatch per instance at a time. The partial unique index is what
-- makes a double-park impossible even if two ticks park the same instance.
CREATE TABLE graph_dispatches (
 id INTEGER PRIMARY KEY,
 instance_id INTEGER NOT NULL REFERENCES graph_instances(id) ON DELETE CASCADE,
 session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 node TEXT NOT NULL,
 due_at TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','reserved','running','finished','cancelled')),
 allocated_run_id INTEGER UNIQUE REFERENCES runs(id),
 reserved_at TEXT,
 finished_at TEXT,
 cancellation_reason TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL
);
CREATE INDEX graph_dispatches_pending ON graph_dispatches(status, due_at, id);
CREATE UNIQUE INDEX graph_dispatches_one_open ON graph_dispatches(instance_id) WHERE status IN ('pending','reserved','running');
