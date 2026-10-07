-- Validate repair rounds, reviewer assessments and outcomes, plus the record of
-- an agent attempt that ran out of its time budget. They are Validate's own
-- (the timeout record is general). A finished row never changes; a running round or a
-- pending reviewer row is the only thing that may still be completed. Rows are
-- removed only by archiving the whole task, never one by one.

-- A graph agent attempt cancelled because its declared time budget ran out.
CREATE TABLE graph_attempt_timeouts (
 node_attempt_id INTEGER PRIMARY KEY REFERENCES graph_node_attempts(id),
 budget_seconds INTEGER NOT NULL CHECK(budget_seconds>0),
 timed_out_at TEXT NOT NULL
);
CREATE TRIGGER graph_attempt_timeouts_immutable BEFORE UPDATE ON graph_attempt_timeouts
BEGIN SELECT RAISE(ABORT,'timeout record is immutable'); END;

-- One repair attempt of one Validate graph instance. At most two rounds.
CREATE TABLE validate_repair_rounds (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 instance_id INTEGER NOT NULL CHECK(instance_id>0),
 round INTEGER NOT NULL CHECK(round IN (1,2)),
 suite_run_id INTEGER NOT NULL DEFAULT 0,
 pre_commit TEXT NOT NULL CHECK(length(pre_commit) IN (40,64)),
 node_attempt_id INTEGER NOT NULL DEFAULT 0,
 response TEXT NOT NULL DEFAULT '' CHECK(response IN ('','repaired','handback','blocked')),
 state TEXT NOT NULL CHECK(state IN ('running','kept','too_big','handback','blocked','timed_out','interrupted','unusable')),
 reason TEXT NOT NULL DEFAULT '',
 files TEXT NOT NULL DEFAULT '[]',
 kinds TEXT NOT NULL DEFAULT '[]',
 diagnosis TEXT NOT NULL DEFAULT '',
 diff_digest TEXT NOT NULL DEFAULT '',
 diff TEXT NOT NULL DEFAULT '',
 result_commit TEXT NOT NULL DEFAULT '' CHECK(result_commit='' OR length(result_commit) IN (40,64)),
 started_at TEXT NOT NULL,
 finished_at TEXT NOT NULL DEFAULT '',
 UNIQUE(instance_id,round),
 CHECK(state='running' OR length(finished_at)>0),
 CHECK((state='kept')=(result_commit<>''))
);
CREATE TRIGGER validate_repair_rounds_immutable BEFORE UPDATE ON validate_repair_rounds WHEN OLD.state<>'running'
BEGIN SELECT RAISE(ABORT,'repair round is finished and immutable'); END;

-- The accepted result of one reviewer for one tested commit.
CREATE TABLE validate_reviews (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 instance_id INTEGER NOT NULL CHECK(instance_id>0),
 role TEXT NOT NULL CHECK(role IN ('acceptance','code','architecture')),
 commit_sha TEXT NOT NULL CHECK(length(commit_sha) IN (40,64)),
 tree_digest TEXT NOT NULL DEFAULT '',
 suite_run_id INTEGER NOT NULL DEFAULT 0,
 attempt TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL CHECK(state IN ('pending','accepted','failed','malformed')),
 reason TEXT NOT NULL DEFAULT '',
 findings TEXT NOT NULL DEFAULT '[]',
 started_at TEXT NOT NULL,
 finished_at TEXT NOT NULL DEFAULT '',
 UNIQUE(instance_id,role,commit_sha),
 CHECK(state='pending' OR length(finished_at)>0)
);
CREATE TRIGGER validate_reviews_immutable BEFORE UPDATE ON validate_reviews WHEN OLD.state<>'pending'
BEGIN SELECT RAISE(ABORT,'reviewer assessment is finished and immutable'); END;

-- The end of Validate's part of one run: one row per instance and final commit.
CREATE TABLE validate_outcomes (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 instance_id INTEGER NOT NULL CHECK(instance_id>0),
 commit_sha TEXT NOT NULL CHECK(length(commit_sha) IN (40,64)),
 outcome TEXT NOT NULL CHECK(outcome IN ('assessed','handed_back','blocked')),
 reason TEXT NOT NULL DEFAULT '',
 assessment TEXT NOT NULL DEFAULT '[]',
 handback TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 UNIQUE(instance_id,commit_sha)
);
CREATE TRIGGER validate_outcomes_immutable BEFORE UPDATE ON validate_outcomes
BEGIN SELECT RAISE(ABORT,'validate outcome is immutable'); END;
