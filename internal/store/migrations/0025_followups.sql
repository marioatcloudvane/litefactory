CREATE TABLE followup_proposals (
 proposal_key TEXT PRIMARY KEY, parent_task TEXT NOT NULL, generation INTEGER NOT NULL,
 run_id INTEGER NOT NULL REFERENCES validation_assessments(run_id),
 title TEXT NOT NULL, body TEXT NOT NULL,
 approval_comment INTEGER NOT NULL DEFAULT 0, actor_id INTEGER NOT NULL DEFAULT 0,
 attachment_comment INTEGER NOT NULL DEFAULT 0,
 attach INTEGER NOT NULL DEFAULT 0, attempted INTEGER NOT NULL DEFAULT 0,
 issue_id INTEGER NOT NULL DEFAULT 0, issue_number INTEGER NOT NULL DEFAULT 0,
 attached INTEGER NOT NULL DEFAULT 0, proposed INTEGER NOT NULL DEFAULT 0,
 reported INTEGER NOT NULL DEFAULT 0, diagnostic TEXT NOT NULL DEFAULT ''
);

CREATE TABLE followup_materializations (run_id INTEGER PRIMARY KEY REFERENCES validation_assessments(run_id));
