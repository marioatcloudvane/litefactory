-- Validate suite records: one row per attempt to run the full suite of one
-- graph instance on one exact commit, and one row per declared command of that
-- attempt. They are Validate's own; no other mode reads or writes them.
-- At most one attempt is running per factory home. A finished attempt or
-- command is immutable. A command is claimed (running, with its process
-- identity) before it is final, so a restart can find what it left behind.
CREATE TABLE suite_runs (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 instance_id INTEGER NOT NULL CHECK(instance_id>0),
 node TEXT NOT NULL CHECK(length(node)>0),
 attempt INTEGER NOT NULL CHECK(attempt>0),
 commit_sha TEXT NOT NULL CHECK(length(commit_sha) IN (40,64)),
 recipe_digest TEXT NOT NULL DEFAULT '',
 tree_before TEXT NOT NULL DEFAULT '',
 tree_after TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL CHECK(state IN ('running','passed','failed','blocked','cancelled','interrupted')),
 reason TEXT NOT NULL DEFAULT '',
 started_at TEXT NOT NULL,
 finished_at TEXT NOT NULL DEFAULT '',
 UNIQUE(instance_id,commit_sha,recipe_digest,attempt),
 CHECK(state='running' OR length(finished_at)>0)
);
CREATE UNIQUE INDEX suite_runs_one_running ON suite_runs(state) WHERE state='running';
CREATE INDEX suite_runs_key ON suite_runs(instance_id,commit_sha,id);
CREATE TRIGGER suite_runs_immutable BEFORE UPDATE ON suite_runs WHEN OLD.state<>'running'
BEGIN SELECT RAISE(ABORT,'suite run is finished and immutable'); END;
CREATE TRIGGER suite_runs_no_delete BEFORE DELETE ON suite_runs WHEN OLD.state<>'running'
BEGIN SELECT RAISE(ABORT,'suite run is finished and immutable'); END;

CREATE TABLE suite_commands (
 run_id INTEGER NOT NULL REFERENCES suite_runs(id),
 position INTEGER NOT NULL CHECK(position>=0),
 argv TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('running','passed','failed','blocked','cancelled','interrupted')),
 exit_code INTEGER,
 reason TEXT NOT NULL DEFAULT '',
 started_at TEXT NOT NULL,
 finished_at TEXT NOT NULL DEFAULT '',
 process_identity TEXT NOT NULL DEFAULT '',
 cleanup_confirmed INTEGER NOT NULL DEFAULT 0 CHECK(cleanup_confirmed IN (0,1)),
 output_tail TEXT NOT NULL DEFAULT '',
 output_truncated INTEGER NOT NULL DEFAULT 0 CHECK(output_truncated IN (0,1)),
 output_digest TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(run_id,position),
 CHECK(state<>'passed' OR cleanup_confirmed=1)
) WITHOUT ROWID;
CREATE TRIGGER suite_commands_immutable BEFORE UPDATE ON suite_commands WHEN OLD.state<>'running'
BEGIN SELECT RAISE(ABORT,'suite command is finished and immutable'); END;
CREATE TRIGGER suite_commands_no_delete BEFORE DELETE ON suite_commands WHEN OLD.state<>'running'
BEGIN SELECT RAISE(ABORT,'suite command is finished and immutable'); END;
