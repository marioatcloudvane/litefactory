CREATE TABLE brainstorm_task_launch_attempts (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 item_id TEXT NOT NULL,
 ordinal INTEGER NOT NULL,
 session_id INTEGER NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('pending','reserved','running','succeeded','failed')),
 run_id INTEGER NOT NULL DEFAULT 0,
 outcome TEXT NOT NULL DEFAULT '' CHECK(outcome IN ('','replayed','deferred','failed','started')),
 reason TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 UNIQUE(item_id, ordinal)
);
CREATE UNIQUE INDEX brainstorm_task_launch_attempts_one_open ON brainstorm_task_launch_attempts(item_id) WHERE state IN ('pending','reserved','running');
CREATE TABLE brainstorm_plan_launch_seeds (
 launch_id INTEGER NOT NULL UNIQUE,
 item_id TEXT NOT NULL,
 attempt_id INTEGER NOT NULL,
 ledger_digest TEXT NOT NULL,
 brief_digest TEXT NOT NULL DEFAULT '',
 technical_digest TEXT NOT NULL DEFAULT '',
 session_dir TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','writing','complete','skipped_existing')),
 reason TEXT NOT NULL DEFAULT '',
 completed_at TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL
);
