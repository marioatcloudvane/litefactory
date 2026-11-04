CREATE TABLE brainstorm_task_batches (
 id TEXT PRIMARY KEY, owner_action_id TEXT NOT NULL DEFAULT '', execution_id TEXT NOT NULL UNIQUE,
 owner_id TEXT NOT NULL, owner_generation INTEGER NOT NULL, repository_id INTEGER NOT NULL,
 repository_ref TEXT NOT NULL, brainstorm_id TEXT NOT NULL, draft_set_id TEXT NOT NULL UNIQUE,
 state TEXT NOT NULL CHECK(state IN ('running','settled')),
 created_at TEXT NOT NULL, settled_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE brainstorm_task_continuations (
 id TEXT PRIMARY KEY, batch_id TEXT NOT NULL REFERENCES brainstorm_task_batches(id),
 execution_id TEXT NOT NULL UNIQUE, kind TEXT NOT NULL CHECK(kind IN ('create','continue','recreate')),
 items TEXT NOT NULL, frozen_digest TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('running','settled')),
 summary_state TEXT NOT NULL DEFAULT 'pending' CHECK(summary_state IN ('pending','sent','no_channel')),
 created_at TEXT NOT NULL, settled_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX brainstorm_task_continuations_batch ON brainstorm_task_continuations(batch_id, state);
CREATE TABLE brainstorm_task_items (
 id TEXT PRIMARY KEY, batch_id TEXT NOT NULL REFERENCES brainstorm_task_batches(id),
 draft_id TEXT NOT NULL UNIQUE, ordinal INTEGER NOT NULL, generation INTEGER NOT NULL DEFAULT 1,
 title TEXT NOT NULL, body TEXT NOT NULL, marker TEXT NOT NULL,
 ledger TEXT NOT NULL DEFAULT '{}', ledger_digest TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL CHECK(state IN ('pending','creating','uncertain','uncertain_exhausted','not_created','created','plan_starting','plan_started','plan_start_failed')),
 issue_id INTEGER NOT NULL DEFAULT 0, issue_number INTEGER NOT NULL DEFAULT 0, issue_link TEXT NOT NULL DEFAULT '',
 session_id INTEGER NOT NULL DEFAULT 0, attempt_at TEXT NOT NULL DEFAULT '', checks_done INTEGER NOT NULL DEFAULT 0,
 next_check_at TEXT NOT NULL DEFAULT '', create_retries INTEGER NOT NULL DEFAULT 0,
 failed_launch_attempts INTEGER NOT NULL DEFAULT 0, reason TEXT NOT NULL DEFAULT '',
 updated_at TEXT NOT NULL,
 UNIQUE(batch_id, ordinal)
);
CREATE INDEX brainstorm_task_items_state ON brainstorm_task_items(state, batch_id);
