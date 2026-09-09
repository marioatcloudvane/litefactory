CREATE TABLE acceptance_decisions (
 id INTEGER PRIMARY KEY,
 run_id INTEGER NOT NULL REFERENCES validation_assessments(run_id),
 parent_task TEXT NOT NULL,
 generation INTEGER NOT NULL,
 story_key TEXT NOT NULL,
 issue_number INTEGER NOT NULL,
 issue_id INTEGER NOT NULL,
 parent_id INTEGER NOT NULL,
 revision TEXT NOT NULL,
 snapshot TEXT NOT NULL,
 frozen_body TEXT NOT NULL,
 findings_json TEXT NOT NULL,
 accepted INTEGER NOT NULL CHECK(accepted IN (0,1)),
 comment_id INTEGER NOT NULL DEFAULT 0,
 projected INTEGER NOT NULL DEFAULT 0 CHECK(projected IN (0,1)),
 delivered INTEGER NOT NULL DEFAULT 0 CHECK(delivered IN (0,1)),
 diagnostic TEXT NOT NULL DEFAULT '',
 UNIQUE(run_id,story_key)
);

ALTER TABLE validation_assessments ADD COLUMN materialized INTEGER NOT NULL DEFAULT 0 CHECK(materialized IN (0,1));
