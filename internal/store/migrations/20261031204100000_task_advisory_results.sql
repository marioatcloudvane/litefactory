CREATE TABLE task_advisory_result_receipts (
 execution_id TEXT NOT NULL REFERENCES advisory_admissions(execution_id),
 request_id TEXT NOT NULL,
 digest TEXT NOT NULL,
 payload TEXT NOT NULL,
 created_at TEXT NOT NULL,
 PRIMARY KEY(execution_id,request_id),
 UNIQUE(execution_id)
);
CREATE TABLE task_advisory_invocations (
 execution_id TEXT PRIMARY KEY REFERENCES advisory_admissions(execution_id),
 fence INTEGER NOT NULL,
 price_revision_id INTEGER NOT NULL REFERENCES meter_price_revisions(id) ON DELETE RESTRICT,
 provider TEXT NOT NULL,
 model TEXT NOT NULL,
 thinking TEXT NOT NULL,
 deadline INTEGER NOT NULL,
 observed_model TEXT NOT NULL DEFAULT '',
 provider_version TEXT NOT NULL DEFAULT '',
 outcome TEXT NOT NULL DEFAULT 'running',
 usage TEXT NOT NULL DEFAULT '{}',
 started_at TEXT NOT NULL,
 finished_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE task_advisory_usage_events (
 execution_id TEXT NOT NULL REFERENCES task_advisory_invocations(execution_id),
 sequence INTEGER NOT NULL,
 event TEXT NOT NULL,
 PRIMARY KEY(execution_id,sequence)
);
