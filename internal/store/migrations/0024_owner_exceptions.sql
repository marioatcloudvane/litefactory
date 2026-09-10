CREATE TABLE owner_exceptions (
 id INTEGER PRIMARY KEY,
 parent_task TEXT NOT NULL,
 generation INTEGER NOT NULL,
 comment_id INTEGER NOT NULL,
 actor_id INTEGER NOT NULL,
 request_json TEXT NOT NULL,
 expansion_json TEXT NOT NULL,
 report_comment_id INTEGER NOT NULL DEFAULT 0,
 UNIQUE(parent_task, comment_id)
);
ALTER TABLE acceptance_decisions RENAME TO acceptance_decisions_before_exceptions;
CREATE TABLE acceptance_decisions (
 id INTEGER PRIMARY KEY,
 run_id INTEGER REFERENCES validation_assessments(run_id),
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
 exception_id INTEGER NOT NULL DEFAULT 0,
 UNIQUE(run_id,story_key,exception_id)
);
INSERT INTO acceptance_decisions(id,run_id,parent_task,generation,story_key,issue_number,issue_id,parent_id,revision,snapshot,frozen_body,findings_json,accepted,comment_id,projected,delivered,diagnostic)
 SELECT * FROM acceptance_decisions_before_exceptions;
DROP TABLE acceptance_decisions_before_exceptions;
