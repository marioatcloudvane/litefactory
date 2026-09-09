CREATE TABLE delivery_proposals (
 proposal_key TEXT PRIMARY KEY, parent_task TEXT NOT NULL, generation INTEGER NOT NULL,
 parent_id INTEGER NOT NULL, branch TEXT NOT NULL, head TEXT NOT NULL, target TEXT NOT NULL,
 readiness TEXT NOT NULL, title TEXT NOT NULL, body TEXT NOT NULL,
 pr_id INTEGER NOT NULL DEFAULT 0, pr_number INTEGER NOT NULL DEFAULT 0,
 create_attempted INTEGER NOT NULL DEFAULT 0, proposed INTEGER NOT NULL DEFAULT 0,
 approval_comment INTEGER NOT NULL DEFAULT 0, actor_id INTEGER NOT NULL DEFAULT 0,
 merge_attempted INTEGER NOT NULL DEFAULT 0, merge_confirmed INTEGER NOT NULL DEFAULT 0,
 merge_sha TEXT NOT NULL DEFAULT '', close_attempted INTEGER NOT NULL DEFAULT 0,
 closed INTEGER NOT NULL DEFAULT 0, reported INTEGER NOT NULL DEFAULT 0,
 superseded INTEGER NOT NULL DEFAULT 0, diagnostic TEXT NOT NULL DEFAULT ''
);

CREATE TABLE delivery_readiness (
 record_key TEXT PRIMARY KEY, parent_task TEXT NOT NULL, generation INTEGER NOT NULL,
 full_report TEXT NOT NULL
);
