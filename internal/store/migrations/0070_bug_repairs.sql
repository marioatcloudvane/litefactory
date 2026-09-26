CREATE TABLE bug_work_guards (
 session_id INTEGER PRIMARY KEY REFERENCES bug_records(session_id),
 owner TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('repair','checks','child','save'))
);
CREATE TABLE bug_repairs (
 session_id INTEGER NOT NULL REFERENCES bug_records(session_id),
 request TEXT NOT NULL,
 input_digest TEXT NOT NULL,
 ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 1 AND 3),
 analysis_revision INTEGER NOT NULL,
 analysis_digest TEXT NOT NULL,
 approval_review TEXT NOT NULL,
 cause TEXT NOT NULL CHECK(cause IN ('initial','test_failure','pr_feedback','reshape')),
 evidence TEXT NOT NULL,
 source_commit TEXT NOT NULL,
 source_tree TEXT NOT NULL,
 graph_attempt_id INTEGER NOT NULL REFERENCES graph_node_attempts(id),
 run_id INTEGER REFERENCES runs(id),
 state TEXT NOT NULL CHECK(state IN ('reserved','dispatched','completed','reshape','blocked','failed','uncertain')),
 reason TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(session_id,request), UNIQUE(session_id,ordinal)
);
CREATE UNIQUE INDEX bug_repairs_active ON bug_repairs(session_id) WHERE state IN ('reserved','dispatched');
CREATE TABLE bug_repair_completions (
 session_id INTEGER NOT NULL,
 request TEXT NOT NULL,
 result_id INTEGER NOT NULL,
 result_revision INTEGER NOT NULL,
 evidence BLOB NOT NULL,
 source_commit TEXT NOT NULL,
 source_tree TEXT NOT NULL,
 save_operation TEXT NOT NULL,
 PRIMARY KEY(session_id,request),
 FOREIGN KEY(session_id,request) REFERENCES bug_repairs(session_id,request)
);
CREATE TABLE bug_repair_scope_observations (
 session_id INTEGER NOT NULL,
 request TEXT NOT NULL,
 source_commit TEXT NOT NULL,
 source_tree TEXT NOT NULL,
 diff_digest TEXT NOT NULL,
 disposition TEXT NOT NULL CHECK(disposition IN ('within','outside','indeterminate')),
 evidence TEXT NOT NULL,
 paths BLOB NOT NULL,
 PRIMARY KEY(session_id,request,source_commit),
 FOREIGN KEY(session_id,request) REFERENCES bug_repairs(session_id,request)
);
