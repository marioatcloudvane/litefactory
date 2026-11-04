-- litefactory:rebuild-tables durable_workflow_jobs durable_workflow_attempts meter_attempts
CREATE TABLE durable_workflow_jobs_rebuild (
 id TEXT PRIMARY KEY, owner_id TEXT NOT NULL, owner_generation INTEGER NOT NULL,
 snapshot TEXT NOT NULL, state TEXT NOT NULL, revision INTEGER NOT NULL DEFAULT 0,
 result TEXT NOT NULL DEFAULT '{}', questions TEXT NOT NULL DEFAULT '[]',
 domain_ref TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 admission_kind TEXT NOT NULL DEFAULT 'confirmed_workflow' CHECK(admission_kind IN ('confirmed_workflow','advisory','brainstorm_scan'))
);
INSERT INTO durable_workflow_jobs_rebuild(id,owner_id,owner_generation,snapshot,state,revision,result,questions,domain_ref,summary,created_at,updated_at,admission_kind)
 SELECT id,owner_id,owner_generation,snapshot,state,revision,result,questions,domain_ref,summary,created_at,updated_at,admission_kind FROM durable_workflow_jobs;
CREATE TABLE durable_workflow_attempts_rebuild (
 id INTEGER PRIMARY KEY AUTOINCREMENT, job_id TEXT NOT NULL REFERENCES durable_workflow_jobs(id),
 proposal_id TEXT UNIQUE REFERENCES owner_action_proposals(id), execution_id TEXT NOT NULL UNIQUE,
 state TEXT NOT NULL, fence INTEGER NOT NULL DEFAULT 0, queued_at TEXT NOT NULL,
 started_at TEXT NOT NULL DEFAULT '', deadline TEXT NOT NULL DEFAULT '', finished_at TEXT NOT NULL DEFAULT '',
 admission_kind TEXT NOT NULL DEFAULT 'confirmed_workflow' CHECK(admission_kind IN ('confirmed_workflow','advisory','brainstorm_scan')),
 advisory_id TEXT UNIQUE REFERENCES advisory_admissions(id),
 brainstorm_scan_id TEXT UNIQUE REFERENCES brainstorm_scans(id),
 CHECK((admission_kind='confirmed_workflow' AND proposal_id IS NOT NULL AND advisory_id IS NULL AND brainstorm_scan_id IS NULL) OR
       (admission_kind='advisory' AND proposal_id IS NULL AND advisory_id IS NOT NULL AND brainstorm_scan_id IS NULL) OR
       (admission_kind='brainstorm_scan' AND proposal_id IS NULL AND advisory_id IS NULL AND brainstorm_scan_id IS NOT NULL))
);
INSERT INTO durable_workflow_attempts_rebuild(id,job_id,proposal_id,execution_id,state,fence,queued_at,started_at,deadline,finished_at,admission_kind,advisory_id)
 SELECT id,job_id,proposal_id,execution_id,state,fence,queued_at,started_at,deadline,finished_at,admission_kind,advisory_id FROM durable_workflow_attempts;
-- Keep the high-water mark even if the highest historical attempt was deleted.
INSERT INTO sqlite_sequence(name,seq)
 SELECT 'durable_workflow_attempts_rebuild',seq FROM sqlite_sequence
 WHERE name='durable_workflow_attempts'
 AND NOT EXISTS(SELECT 1 FROM sqlite_sequence WHERE name='durable_workflow_attempts_rebuild');
UPDATE sqlite_sequence SET seq=MAX(seq,COALESCE((SELECT seq FROM sqlite_sequence WHERE name='durable_workflow_attempts'),0))
 WHERE name='durable_workflow_attempts_rebuild';
CREATE TABLE meter_attempts_rebuild (
 id TEXT PRIMARY KEY NOT NULL, advisory_execution_id TEXT REFERENCES advisory_admissions(execution_id), authoring_session_id TEXT REFERENCES standards_authoring_sessions(id), presentation_owner TEXT REFERENCES chat_presentation_owners(id), chat_repo_id INTEGER REFERENCES repos(id), judge_attempt_id TEXT REFERENCES judge_attempts(id), doctor_attempt_id TEXT REFERENCES doctor_attempts(id), run_id INTEGER REFERENCES runs(id) ON DELETE CASCADE,
 ordinal INTEGER NOT NULL CHECK(ordinal>0), coding_agent TEXT NOT NULL, agent_version TEXT, source_format TEXT,
 started_at TEXT NOT NULL, finished_at TEXT, outcome TEXT NOT NULL, collection_status TEXT NOT NULL,
 diagnostic_codes_json TEXT NOT NULL, price_revision_id INTEGER NOT NULL REFERENCES meter_price_revisions(id) ON DELETE RESTRICT, provider_reported_zero_cost_refusal INTEGER NOT NULL DEFAULT 0 CHECK(provider_reported_zero_cost_refusal IN (0,1)), graph_node_attempt_id INTEGER REFERENCES graph_node_attempts(id),
 brainstorm_scan_id TEXT REFERENCES brainstorm_scans(id),
 UNIQUE(run_id,ordinal), UNIQUE(doctor_attempt_id,ordinal), UNIQUE(authoring_session_id,ordinal), UNIQUE(advisory_execution_id), UNIQUE(brainstorm_scan_id),
 CHECK((advisory_execution_id IS NOT NULL)+(brainstorm_scan_id IS NOT NULL)+(authoring_session_id IS NOT NULL)+(presentation_owner IS NOT NULL)+(chat_repo_id IS NOT NULL)+(judge_attempt_id IS NOT NULL)+(doctor_attempt_id IS NOT NULL)+(run_id IS NOT NULL)=1),
 CHECK(graph_node_attempt_id IS NULL OR run_id IS NOT NULL)
);
INSERT INTO meter_attempts_rebuild(id,advisory_execution_id,authoring_session_id,presentation_owner,chat_repo_id,judge_attempt_id,doctor_attempt_id,run_id,ordinal,coding_agent,agent_version,source_format,started_at,finished_at,outcome,collection_status,diagnostic_codes_json,price_revision_id,provider_reported_zero_cost_refusal,graph_node_attempt_id)
 SELECT id,advisory_execution_id,authoring_session_id,presentation_owner,chat_repo_id,judge_attempt_id,doctor_attempt_id,run_id,ordinal,coding_agent,agent_version,source_format,started_at,finished_at,outcome,collection_status,diagnostic_codes_json,price_revision_id,provider_reported_zero_cost_refusal,graph_node_attempt_id FROM meter_attempts;
DROP TABLE meter_attempts;
DROP TABLE durable_workflow_attempts;
DROP TABLE durable_workflow_jobs;
ALTER TABLE durable_workflow_jobs_rebuild RENAME TO durable_workflow_jobs;
ALTER TABLE durable_workflow_attempts_rebuild RENAME TO durable_workflow_attempts;
ALTER TABLE meter_attempts_rebuild RENAME TO meter_attempts;
