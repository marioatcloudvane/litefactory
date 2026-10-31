CREATE TABLE durable_workflow_requests (
 owner_id TEXT NOT NULL, owner_generation INTEGER NOT NULL, request_key TEXT NOT NULL,
 digest TEXT NOT NULL, proposal_id TEXT NOT NULL UNIQUE REFERENCES owner_action_proposals(id),
 job_id TEXT NOT NULL, snapshot TEXT NOT NULL, inputs TEXT NOT NULL, kind TEXT NOT NULL,
 expected_revision INTEGER NOT NULL, created_at TEXT NOT NULL,
 PRIMARY KEY(owner_id,owner_generation,request_key)
);
CREATE TABLE durable_workflow_jobs (
 id TEXT PRIMARY KEY, owner_id TEXT NOT NULL, owner_generation INTEGER NOT NULL,
 snapshot TEXT NOT NULL, state TEXT NOT NULL, revision INTEGER NOT NULL DEFAULT 0,
 result TEXT NOT NULL DEFAULT '{}', questions TEXT NOT NULL DEFAULT '[]',
 domain_ref TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE durable_workflow_attempts (
 id INTEGER PRIMARY KEY AUTOINCREMENT, job_id TEXT NOT NULL REFERENCES durable_workflow_jobs(id),
 proposal_id TEXT NOT NULL UNIQUE REFERENCES owner_action_proposals(id), execution_id TEXT NOT NULL UNIQUE,
 state TEXT NOT NULL, fence INTEGER NOT NULL DEFAULT 0, queued_at TEXT NOT NULL,
 started_at TEXT NOT NULL DEFAULT '', deadline TEXT NOT NULL DEFAULT '', finished_at TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX durable_workflow_active_job ON durable_workflow_attempts(job_id) WHERE state IN ('queued','running');
CREATE TABLE durable_workflow_worker (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1), attempt_id INTEGER NOT NULL DEFAULT 0,
 generation INTEGER NOT NULL DEFAULT 0, expires_at TEXT NOT NULL DEFAULT '', heartbeat_at TEXT NOT NULL DEFAULT ''
);
INSERT INTO durable_workflow_worker(singleton) VALUES(1);
CREATE TABLE durable_workflow_notices (
 id INTEGER PRIMARY KEY AUTOINCREMENT, job_id TEXT NOT NULL REFERENCES durable_workflow_jobs(id),
 revision INTEGER NOT NULL, event TEXT NOT NULL, destination TEXT NOT NULL,
 state TEXT NOT NULL, body TEXT NOT NULL, message_id TEXT NOT NULL DEFAULT '', reason TEXT NOT NULL DEFAULT '',
 retry_at TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
 UNIQUE(job_id,revision,event,destination)
);
