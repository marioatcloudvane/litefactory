-- Content/submission versions never rewrite the persisted workflow or acceptance.
ALTER TABLE managed_features ADD COLUMN content_version INTEGER NOT NULL DEFAULT 1 CHECK(content_version IN (1,2));
ALTER TABLE managed_features ADD COLUMN submission_version INTEGER NOT NULL DEFAULT 1 CHECK(submission_version IN (1,2));
CREATE TABLE submission_generations (
 parent_task TEXT NOT NULL REFERENCES managed_features(parent_task), generation INTEGER NOT NULL,
 content_version INTEGER NOT NULL CHECK(content_version IN (1,2)), submission_version INTEGER NOT NULL CHECK(submission_version IN (1,2)),
 PRIMARY KEY(parent_task,generation)
);
INSERT INTO submission_generations SELECT parent_task,generation,1,1 FROM managed_features;
INSERT OR IGNORE INTO submission_generations SELECT parent_task,generation,1,1 FROM managed_stories;
CREATE TABLE submission_projections (
 parent_task TEXT NOT NULL, generation INTEGER NOT NULL, story_key TEXT NOT NULL, issue_id INTEGER NOT NULL,
 revision TEXT NOT NULL, content_version INTEGER NOT NULL, title TEXT NOT NULL, body TEXT NOT NULL, content BLOB NOT NULL,
 PRIMARY KEY(parent_task,generation,story_key,revision),
 FOREIGN KEY(parent_task,generation) REFERENCES submission_generations(parent_task,generation)
);
CREATE TABLE submission_criterion_history (
 parent_task TEXT NOT NULL REFERENCES managed_features(parent_task), story_key TEXT NOT NULL, criterion_id TEXT NOT NULL,
 first_revision TEXT NOT NULL, retired_revision TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(parent_task,story_key,criterion_id)
);
CREATE TABLE submission_attempts (
 run_id INTEGER NOT NULL REFERENCES runs(id), attempt TEXT NOT NULL, parent_task TEXT NOT NULL, generation INTEGER NOT NULL,
 stage TEXT NOT NULL CHECK(stage IN ('plan','engineering','legacy','validate')), scope_hash TEXT NOT NULL, active INTEGER NOT NULL CHECK(active IN (0,1)),
 PRIMARY KEY(run_id,attempt), FOREIGN KEY(parent_task,generation) REFERENCES submission_generations(parent_task,generation)
);
CREATE UNIQUE INDEX submission_active_attempt ON submission_attempts(run_id) WHERE active=1;
CREATE TABLE submission_scope (
 run_id INTEGER NOT NULL, attempt TEXT NOT NULL, story_key TEXT NOT NULL, issue_id INTEGER NOT NULL, revision TEXT NOT NULL,
 PRIMARY KEY(run_id,attempt,story_key), FOREIGN KEY(run_id,attempt) REFERENCES submission_attempts(run_id,attempt)
);
CREATE TABLE submission_run_counters (
 run_id INTEGER PRIMARY KEY REFERENCES runs(id), sequence INTEGER NOT NULL DEFAULT 0, retained_bytes INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE submission_receipts (
 id INTEGER PRIMARY KEY, run_id INTEGER NOT NULL, attempt TEXT NOT NULL, request_id TEXT NOT NULL, request_hash TEXT NOT NULL,
 story_key TEXT NOT NULL, revision TEXT NOT NULL, sequence INTEGER NOT NULL, story_attempt INTEGER NOT NULL,
 payload BLOB NOT NULL, diagnostics BLOB NOT NULL, input_state TEXT NOT NULL, engineering_state TEXT NOT NULL,
 acceptance_state TEXT NOT NULL, effect_state TEXT NOT NULL, plan_state TEXT NOT NULL,
 UNIQUE(run_id,request_id), UNIQUE(run_id,sequence),
 FOREIGN KEY(run_id,attempt) REFERENCES submission_attempts(run_id,attempt)
);
CREATE TABLE submission_snapshots (
 run_id INTEGER NOT NULL REFERENCES runs(id), reference TEXT NOT NULL, attempt TEXT NOT NULL, parent_task TEXT NOT NULL, generation INTEGER NOT NULL,
 story_key TEXT NOT NULL, revision TEXT NOT NULL, digest TEXT NOT NULL, payload BLOB NOT NULL,
 PRIMARY KEY(run_id,reference)
);
CREATE TABLE submission_checks (
 run_id INTEGER NOT NULL, reference TEXT NOT NULL, attempt TEXT NOT NULL, snapshot_ref TEXT NOT NULL, name TEXT NOT NULL,
 executed INTEGER NOT NULL CHECK(executed IN (0,1)), passed INTEGER NOT NULL CHECK(passed IN (0,1)),
 provenance BLOB NOT NULL, result BLOB NOT NULL,
 PRIMARY KEY(run_id,reference), FOREIGN KEY(run_id,snapshot_ref) REFERENCES submission_snapshots(run_id,reference)
);
