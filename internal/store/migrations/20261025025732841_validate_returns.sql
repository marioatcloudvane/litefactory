-- Reviewed owner returns retain immutable context and one dispatch selection.
CREATE TABLE validate_returns (
 instance_id INTEGER PRIMARY KEY,
 parent TEXT NOT NULL,
 generation INTEGER NOT NULL,
 commit_sha TEXT NOT NULL,
 report_revision TEXT NOT NULL,
 comment_id INTEGER NOT NULL,
 author_id INTEGER NOT NULL,
 explanation TEXT NOT NULL,
 context TEXT NOT NULL,
 selection TEXT NOT NULL DEFAULT '',
 selection_source TEXT NOT NULL DEFAULT '',
 applied INTEGER NOT NULL DEFAULT 0,
 created_at TEXT NOT NULL
);
CREATE INDEX validate_returns_parent ON validate_returns(parent,generation,instance_id);
