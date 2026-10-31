CREATE TABLE task_discussions (
 id TEXT PRIMARY KEY, owner_id INTEGER NOT NULL, owner_generation INTEGER NOT NULL,
 repository_ref TEXT NOT NULL, task_ref TEXT NOT NULL, client_id TEXT NOT NULL,
 request_key TEXT NOT NULL, revision INTEGER NOT NULL DEFAULT 0, active_job TEXT NOT NULL DEFAULT '',
 exchanges TEXT NOT NULL DEFAULT '[]', omitted_count INTEGER NOT NULL DEFAULT 0,
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 UNIQUE(owner_id,owner_generation,repository_ref,task_ref,request_key)
);
CREATE TABLE advisory_admissions (
 id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL REFERENCES task_discussions(id),
 owner_id INTEGER NOT NULL, owner_generation INTEGER NOT NULL, repository_ref TEXT NOT NULL, task_ref TEXT NOT NULL,
 operation TEXT NOT NULL CHECK(operation IN ('ask','judge')), request_key TEXT NOT NULL, digest TEXT NOT NULL,
 basis TEXT NOT NULL, context TEXT NOT NULL, question TEXT NOT NULL, grant_id TEXT NOT NULL,
 consent_revision TEXT NOT NULL, conversation_revision INTEGER NOT NULL, job_id TEXT NOT NULL UNIQUE,
 execution_id TEXT NOT NULL UNIQUE, created_at TEXT NOT NULL, target_guard TEXT NOT NULL DEFAULT '',
 UNIQUE(owner_id,owner_generation,repository_ref,task_ref,operation,request_key)
);
ALTER TABLE durable_workflow_jobs ADD COLUMN admission_kind TEXT NOT NULL DEFAULT 'confirmed_workflow' CHECK(admission_kind IN ('confirmed_workflow','advisory'));
CREATE TABLE durable_workflow_attempts_new (
 id INTEGER PRIMARY KEY AUTOINCREMENT, job_id TEXT NOT NULL REFERENCES durable_workflow_jobs(id),
 proposal_id TEXT UNIQUE REFERENCES owner_action_proposals(id), execution_id TEXT NOT NULL UNIQUE,
 state TEXT NOT NULL, fence INTEGER NOT NULL DEFAULT 0, queued_at TEXT NOT NULL,
 started_at TEXT NOT NULL DEFAULT '', deadline TEXT NOT NULL DEFAULT '', finished_at TEXT NOT NULL DEFAULT '',
 admission_kind TEXT NOT NULL DEFAULT 'confirmed_workflow' CHECK(admission_kind IN ('confirmed_workflow','advisory')),
 advisory_id TEXT UNIQUE REFERENCES advisory_admissions(id),
 CHECK((admission_kind='confirmed_workflow' AND proposal_id IS NOT NULL AND advisory_id IS NULL) OR
       (admission_kind='advisory' AND proposal_id IS NULL AND advisory_id IS NOT NULL))
);
INSERT INTO durable_workflow_attempts_new(id,job_id,proposal_id,execution_id,state,fence,queued_at,started_at,deadline,finished_at)
 SELECT id,job_id,proposal_id,execution_id,state,fence,queued_at,started_at,deadline,finished_at FROM durable_workflow_attempts;
-- Keep the high-water mark even if the highest historical attempt was deleted.
INSERT INTO sqlite_sequence(name,seq)
 SELECT 'durable_workflow_attempts_new',seq FROM sqlite_sequence
 WHERE name='durable_workflow_attempts'
 AND NOT EXISTS(SELECT 1 FROM sqlite_sequence WHERE name='durable_workflow_attempts_new');
UPDATE sqlite_sequence SET seq=MAX(seq,COALESCE((SELECT seq FROM sqlite_sequence WHERE name='durable_workflow_attempts'),0))
 WHERE name='durable_workflow_attempts_new';
DROP TABLE durable_workflow_attempts;
ALTER TABLE durable_workflow_attempts_new RENAME TO durable_workflow_attempts;
CREATE UNIQUE INDEX durable_workflow_active_job ON durable_workflow_attempts(job_id) WHERE state IN ('queued','running','stop_requested');
CREATE TABLE discussion_conclusions (
 id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL REFERENCES task_discussions(id),
 owner_id INTEGER NOT NULL, owner_generation INTEGER NOT NULL, repository_ref TEXT NOT NULL, task_ref TEXT NOT NULL,
 conclusion_key TEXT NOT NULL, digest TEXT NOT NULL, summary TEXT NOT NULL, rendered TEXT NOT NULL,
 source_job TEXT NOT NULL DEFAULT '', grant_id TEXT NOT NULL, consent_revision TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('pending','sending','delivered','failed','uncertain')),
 reason TEXT NOT NULL DEFAULT '', comment_url TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, next_attempt_at TEXT NOT NULL DEFAULT '',
 UNIQUE(owner_id,owner_generation,repository_ref,task_ref,conversation_id,conclusion_key)
);
