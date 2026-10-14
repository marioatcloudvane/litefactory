-- Independent advisory requests never share workflow or doctor authority.

PRAGMA defer_foreign_keys=ON;

CREATE TABLE judge_requests (
 id INTEGER PRIMARY KEY, task TEXT NOT NULL, ticket_key TEXT NOT NULL, comment_id TEXT NOT NULL,
 session_id INTEGER NOT NULL REFERENCES sessions(id), trigger_comment_id INTEGER NOT NULL,
 source_json BLOB NOT NULL, configuration BLOB NOT NULL, evidence BLOB NOT NULL,
 report BLOB, created_at TEXT NOT NULL, UNIQUE(ticket_key,comment_id)
);
CREATE TABLE judge_attempts (
 id TEXT PRIMARY KEY NOT NULL, request_id INTEGER NOT NULL REFERENCES judge_requests(id),
 ordinal INTEGER NOT NULL CHECK(ordinal IN (1,2)), provider TEXT NOT NULL, model TEXT NOT NULL, thinking TEXT NOT NULL, observed_model TEXT NOT NULL DEFAULT '', provider_version TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL CHECK(state IN ('running','completed','failed','interrupted')), failure TEXT NOT NULL DEFAULT '',
 started_at TEXT NOT NULL, finished_at TEXT, UNIQUE(request_id,ordinal)
);

CREATE TABLE application_results_before_judge AS SELECT * FROM application_results;

CREATE TABLE application_result_attempts_before_judge AS SELECT * FROM application_result_attempts;

CREATE TABLE application_result_requests_before_judge AS SELECT * FROM application_result_requests;

CREATE TABLE application_result_revisions_before_judge AS SELECT * FROM application_result_revisions;

CREATE TABLE application_result_observations_before_judge AS SELECT * FROM application_result_observations;

CREATE TABLE meter_attempts_before_judge AS SELECT * FROM meter_attempts;

CREATE TABLE meter_invocations_before_judge AS SELECT * FROM meter_invocations;

CREATE TABLE meter_observations_before_judge AS SELECT * FROM meter_observations;

CREATE TABLE meter_checkpoints_before_judge AS SELECT * FROM meter_checkpoints;

CREATE TABLE meter_measurements_before_judge AS SELECT * FROM meter_measurements;

CREATE TABLE meter_valuations_before_judge AS SELECT * FROM meter_valuations;

CREATE TABLE meter_valuation_dimensions_before_judge AS SELECT * FROM meter_valuation_dimensions;

DROP TABLE meter_valuation_dimensions;

DROP TABLE meter_valuations;

DROP TABLE meter_measurements;

DROP TABLE meter_checkpoints;

DROP TABLE meter_observations;

DROP TABLE meter_invocations;

DROP TABLE meter_attempts;

DROP TABLE application_result_observations;

DROP TABLE application_result_revisions;

DROP TABLE application_result_requests;

DROP TABLE application_result_attempts;

DROP TABLE application_results;

CREATE TABLE application_results (
 id INTEGER PRIMARY KEY,
 task TEXT NOT NULL,
 mode TEXT NOT NULL,
 work_context TEXT NOT NULL,
 context_hash TEXT NOT NULL,
 schema_version INTEGER NOT NULL CHECK(schema_version=1),
 workflow_version INTEGER NOT NULL CHECK(workflow_version IN (1,2)),
 submission_version INTEGER NOT NULL CHECK(submission_version=2),
 revision INTEGER NOT NULL DEFAULT 0 CHECK(revision>=0),
 workflow TEXT NOT NULL DEFAULT '',
 CHECK((workflow='' AND mode IN ('research','plan','implement','validate','spike')) OR (workflow IN ('bug','doctor') AND mode='') OR (workflow='judge' AND mode='judge')),
 UNIQUE(task,mode,context_hash)
);

INSERT INTO application_results(id,task,mode,work_context,context_hash,schema_version,workflow_version,submission_version,revision,workflow) SELECT id,task,mode,work_context,context_hash,schema_version,workflow_version,submission_version,revision,workflow FROM application_results_before_judge;

DROP TABLE application_results_before_judge;

CREATE TRIGGER doctor_hold_application_results_delete BEFORE DELETE ON application_results WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (OLD.workflow NOT IN ('doctor','judge') AND (OLD.task=h.task_ref)) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;

CREATE TRIGGER doctor_hold_application_results_insert BEFORE INSERT ON application_results WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (NEW.workflow NOT IN ('doctor','judge') AND (NEW.task=h.task_ref)) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;

CREATE TRIGGER doctor_hold_application_results_update BEFORE UPDATE ON application_results WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (OLD.workflow NOT IN ('doctor','judge') AND (OLD.task=h.task_ref)) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) OR EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (NEW.workflow NOT IN ('doctor','judge') AND (NEW.task=h.task_ref)) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;

CREATE TABLE application_result_attempts (
 judge_attempt_id TEXT REFERENCES judge_attempts(id), doctor_attempt_id TEXT REFERENCES doctor_attempts(id),
 run_id INTEGER REFERENCES runs(id),
 attempt TEXT NOT NULL,
 result_id INTEGER NOT NULL REFERENCES application_results(id),
 active INTEGER NOT NULL CHECK(active IN (0,1)),
 PRIMARY KEY(run_id,attempt)
,
 CHECK((run_id IS NOT NULL AND doctor_attempt_id IS NULL AND judge_attempt_id IS NULL) OR (run_id IS NULL AND doctor_attempt_id IS NOT NULL AND judge_attempt_id IS NULL) OR (run_id IS NULL AND doctor_attempt_id IS NULL AND judge_attempt_id IS NOT NULL))
);

INSERT INTO application_result_attempts(doctor_attempt_id,run_id,attempt,result_id,active) SELECT doctor_attempt_id,run_id,attempt,result_id,active FROM application_result_attempts_before_judge;

DROP TABLE application_result_attempts_before_judge;

CREATE UNIQUE INDEX doctor_result_attempt ON application_result_attempts(doctor_attempt_id) WHERE doctor_attempt_id IS NOT NULL;

CREATE TRIGGER doctor_hold_application_result_attempts_delete BEFORE DELETE ON application_result_attempts WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM runs dr JOIN sessions ds ON ds.id=dr.session_id WHERE dr.id=OLD.run_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;

CREATE TRIGGER doctor_hold_application_result_attempts_insert BEFORE INSERT ON application_result_attempts WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM runs dr JOIN sessions ds ON ds.id=dr.session_id WHERE dr.id=NEW.run_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;

CREATE TRIGGER doctor_hold_application_result_attempts_update BEFORE UPDATE ON application_result_attempts WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM runs dr JOIN sessions ds ON ds.id=dr.session_id WHERE dr.id=OLD.run_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) OR EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM runs dr JOIN sessions ds ON ds.id=dr.session_id WHERE dr.id=NEW.run_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;

CREATE TABLE application_result_requests (
 judge_attempt_id TEXT REFERENCES judge_attempts(id), doctor_attempt_id TEXT REFERENCES doctor_attempts(id),
 id INTEGER PRIMARY KEY,
 result_id INTEGER NOT NULL REFERENCES application_results(id),
 run_id INTEGER,
 attempt TEXT NOT NULL,
 request_id TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 payload BLOB NOT NULL,
 diagnostics BLOB NOT NULL,
 input_state TEXT NOT NULL CHECK(input_state IN ('accepted','rejected')),
 applied_revision INTEGER NOT NULL CHECK(applied_revision>=0),
 submission_receipt_id INTEGER REFERENCES submission_receipts(id),
 UNIQUE(run_id,request_id),
 FOREIGN KEY(run_id,attempt) REFERENCES application_result_attempts(run_id,attempt)
,
 CHECK((run_id IS NOT NULL AND doctor_attempt_id IS NULL AND judge_attempt_id IS NULL) OR (run_id IS NULL AND doctor_attempt_id IS NOT NULL AND judge_attempt_id IS NULL) OR (run_id IS NULL AND doctor_attempt_id IS NULL AND judge_attempt_id IS NOT NULL))
);

INSERT INTO application_result_requests(doctor_attempt_id,id,result_id,run_id,attempt,request_id,request_hash,payload,diagnostics,input_state,applied_revision,submission_receipt_id) SELECT doctor_attempt_id,id,result_id,run_id,attempt,request_id,request_hash,payload,diagnostics,input_state,applied_revision,submission_receipt_id FROM application_result_requests_before_judge;

DROP TABLE application_result_requests_before_judge;

CREATE UNIQUE INDEX doctor_result_request ON application_result_requests(result_id,request_id) WHERE doctor_attempt_id IS NOT NULL;

CREATE TRIGGER application_result_requests_doctor_owner BEFORE INSERT ON application_result_requests
 WHEN NEW.doctor_attempt_id IS NOT NULL AND NOT EXISTS(
 SELECT 1 FROM application_result_attempts a WHERE a.doctor_attempt_id=NEW.doctor_attempt_id AND a.attempt=NEW.attempt)
 BEGIN SELECT RAISE(ABORT,'doctor result owner mismatch'); END;

CREATE TRIGGER doctor_hold_application_result_requests_delete BEFORE DELETE ON application_result_requests WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM runs dr JOIN sessions ds ON ds.id=dr.session_id WHERE dr.id=OLD.run_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;

CREATE TRIGGER doctor_hold_application_result_requests_insert BEFORE INSERT ON application_result_requests WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM runs dr JOIN sessions ds ON ds.id=dr.session_id WHERE dr.id=NEW.run_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;

CREATE TRIGGER doctor_hold_application_result_requests_update BEFORE UPDATE ON application_result_requests WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM runs dr JOIN sessions ds ON ds.id=dr.session_id WHERE dr.id=OLD.run_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) OR EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM runs dr JOIN sessions ds ON ds.id=dr.session_id WHERE dr.id=NEW.run_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;

CREATE TABLE application_result_revisions (
 judge_attempt_id TEXT REFERENCES judge_attempts(id), doctor_attempt_id TEXT REFERENCES doctor_attempts(id),
 result_id INTEGER NOT NULL REFERENCES application_results(id),
 revision INTEGER NOT NULL CHECK(revision>=1),
 run_id INTEGER,
 attempt TEXT NOT NULL,
 request_row INTEGER NOT NULL UNIQUE REFERENCES application_result_requests(id),
 snapshot BLOB NOT NULL,
 snapshot_hash TEXT NOT NULL,
 retired BLOB NOT NULL,
 PRIMARY KEY(result_id,revision),
 FOREIGN KEY(run_id,attempt) REFERENCES application_result_attempts(run_id,attempt)
,
 CHECK((run_id IS NOT NULL AND doctor_attempt_id IS NULL AND judge_attempt_id IS NULL) OR (run_id IS NULL AND doctor_attempt_id IS NOT NULL AND judge_attempt_id IS NULL) OR (run_id IS NULL AND doctor_attempt_id IS NULL AND judge_attempt_id IS NOT NULL))
);

INSERT INTO application_result_revisions(doctor_attempt_id,result_id,revision,run_id,attempt,request_row,snapshot,snapshot_hash,retired) SELECT doctor_attempt_id,result_id,revision,run_id,attempt,request_row,snapshot,snapshot_hash,retired FROM application_result_revisions_before_judge;

DROP TABLE application_result_revisions_before_judge;

CREATE TRIGGER application_result_revisions_doctor_owner BEFORE INSERT ON application_result_revisions
 WHEN NEW.doctor_attempt_id IS NOT NULL AND NOT EXISTS(
 SELECT 1 FROM application_result_attempts a WHERE a.doctor_attempt_id=NEW.doctor_attempt_id AND a.attempt=NEW.attempt)
 BEGIN SELECT RAISE(ABORT,'doctor result owner mismatch'); END;

CREATE TRIGGER doctor_hold_application_result_revisions_delete BEFORE DELETE ON application_result_revisions WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM runs dr JOIN sessions ds ON ds.id=dr.session_id WHERE dr.id=OLD.run_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;

CREATE TRIGGER doctor_hold_application_result_revisions_insert BEFORE INSERT ON application_result_revisions WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM runs dr JOIN sessions ds ON ds.id=dr.session_id WHERE dr.id=NEW.run_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;

CREATE TRIGGER doctor_hold_application_result_revisions_update BEFORE UPDATE ON application_result_revisions WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM runs dr JOIN sessions ds ON ds.id=dr.session_id WHERE dr.id=OLD.run_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) OR EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM runs dr JOIN sessions ds ON ds.id=dr.session_id WHERE dr.id=NEW.run_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;

CREATE TABLE application_result_observations (
 judge_attempt_id TEXT REFERENCES judge_attempts(id), doctor_attempt_id TEXT REFERENCES doctor_attempts(id),
 request_row INTEGER NOT NULL REFERENCES application_result_requests(id),
 sequence INTEGER NOT NULL CHECK(sequence>=1),
 run_id INTEGER,
 attempt TEXT NOT NULL,
 dimension TEXT NOT NULL,
 state TEXT NOT NULL,
 PRIMARY KEY(request_row,sequence),
 FOREIGN KEY(run_id,attempt) REFERENCES application_result_attempts(run_id,attempt)
,
 CHECK((run_id IS NOT NULL AND doctor_attempt_id IS NULL AND judge_attempt_id IS NULL) OR (run_id IS NULL AND doctor_attempt_id IS NOT NULL AND judge_attempt_id IS NULL) OR (run_id IS NULL AND doctor_attempt_id IS NULL AND judge_attempt_id IS NOT NULL))
);

INSERT INTO application_result_observations(doctor_attempt_id,request_row,sequence,run_id,attempt,dimension,state) SELECT doctor_attempt_id,request_row,sequence,run_id,attempt,dimension,state FROM application_result_observations_before_judge;

DROP TABLE application_result_observations_before_judge;

CREATE TRIGGER application_result_observations_doctor_owner BEFORE INSERT ON application_result_observations
 WHEN NEW.doctor_attempt_id IS NOT NULL AND NOT EXISTS(
 SELECT 1 FROM application_result_attempts a WHERE a.doctor_attempt_id=NEW.doctor_attempt_id AND a.attempt=NEW.attempt)
 BEGIN SELECT RAISE(ABORT,'doctor result owner mismatch'); END;

CREATE TRIGGER doctor_hold_application_result_observations_delete BEFORE DELETE ON application_result_observations WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM runs dr JOIN sessions ds ON ds.id=dr.session_id WHERE dr.id=OLD.run_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;

CREATE TRIGGER doctor_hold_application_result_observations_insert BEFORE INSERT ON application_result_observations WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM runs dr JOIN sessions ds ON ds.id=dr.session_id WHERE dr.id=NEW.run_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;

CREATE TRIGGER doctor_hold_application_result_observations_update BEFORE UPDATE ON application_result_observations WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM runs dr JOIN sessions ds ON ds.id=dr.session_id WHERE dr.id=OLD.run_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) OR EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM runs dr JOIN sessions ds ON ds.id=dr.session_id WHERE dr.id=NEW.run_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;

CREATE TABLE meter_attempts (
 id TEXT PRIMARY KEY NOT NULL, presentation_owner TEXT REFERENCES chat_presentation_owners(id), chat_repo_id INTEGER REFERENCES repos(id), judge_attempt_id TEXT REFERENCES judge_attempts(id), doctor_attempt_id TEXT REFERENCES doctor_attempts(id), run_id INTEGER REFERENCES runs(id) ON DELETE CASCADE,
 ordinal INTEGER NOT NULL CHECK(ordinal>0), coding_agent TEXT NOT NULL, agent_version TEXT, source_format TEXT,
 started_at TEXT NOT NULL, finished_at TEXT, outcome TEXT NOT NULL, collection_status TEXT NOT NULL,
 diagnostic_codes_json TEXT NOT NULL, price_revision_id INTEGER NOT NULL REFERENCES meter_price_revisions(id) ON DELETE RESTRICT, provider_reported_zero_cost_refusal INTEGER NOT NULL DEFAULT 0 CHECK (provider_reported_zero_cost_refusal IN (0, 1)), graph_node_attempt_id INTEGER REFERENCES graph_node_attempts(id),
 UNIQUE(run_id,ordinal), UNIQUE(doctor_attempt_id,ordinal),
 CHECK(((presentation_owner IS NOT NULL AND run_id IS NULL AND doctor_attempt_id IS NULL AND chat_repo_id IS NULL AND graph_node_attempt_id IS NULL) OR (presentation_owner IS NULL AND ((run_id IS NOT NULL AND doctor_attempt_id IS NULL AND chat_repo_id IS NULL) OR (run_id IS NULL AND doctor_attempt_id IS NOT NULL AND chat_repo_id IS NULL AND graph_node_attempt_id IS NULL) OR (run_id IS NULL AND doctor_attempt_id IS NULL AND chat_repo_id IS NOT NULL AND graph_node_attempt_id IS NULL)))) AND judge_attempt_id IS NULL OR (judge_attempt_id IS NOT NULL AND run_id IS NULL AND doctor_attempt_id IS NULL AND chat_repo_id IS NULL AND presentation_owner IS NULL AND graph_node_attempt_id IS NULL))
);

INSERT INTO meter_attempts(id,presentation_owner,chat_repo_id,doctor_attempt_id,run_id,ordinal,coding_agent,agent_version,source_format,started_at,finished_at,outcome,collection_status,diagnostic_codes_json,price_revision_id,provider_reported_zero_cost_refusal,graph_node_attempt_id) SELECT id,presentation_owner,chat_repo_id,doctor_attempt_id,run_id,ordinal,coding_agent,agent_version,source_format,started_at,finished_at,outcome,collection_status,diagnostic_codes_json,price_revision_id,provider_reported_zero_cost_refusal,graph_node_attempt_id FROM meter_attempts_before_judge;

DROP TABLE meter_attempts_before_judge;

CREATE INDEX idx_meter_graph_attempt ON meter_attempts(graph_node_attempt_id);

CREATE INDEX meter_attempt_agent_time ON meter_attempts(coding_agent,started_at,id);

CREATE INDEX meter_attempt_price ON meter_attempts(price_revision_id);

CREATE INDEX meter_attempt_time ON meter_attempts(started_at,id);

CREATE TABLE meter_invocations (
 attempt_id TEXT NOT NULL REFERENCES meter_attempts(id) ON DELETE CASCADE, id TEXT NOT NULL, parent_id TEXT,
 source_id TEXT NOT NULL, source_invocation_id TEXT NOT NULL, started_at TEXT, finished_at TEXT,
 lifecycle TEXT NOT NULL, collection_status TEXT NOT NULL, diagnostic_codes_json TEXT NOT NULL,
 PRIMARY KEY(attempt_id,id), UNIQUE(attempt_id,source_id,source_invocation_id),
 FOREIGN KEY(attempt_id,parent_id) REFERENCES meter_invocations(attempt_id,id) DEFERRABLE INITIALLY DEFERRED
);

INSERT INTO meter_invocations(attempt_id,id,parent_id,source_id,source_invocation_id,started_at,finished_at,lifecycle,collection_status,diagnostic_codes_json) SELECT attempt_id,id,parent_id,source_id,source_invocation_id,started_at,finished_at,lifecycle,collection_status,diagnostic_codes_json FROM meter_invocations_before_judge;

DROP TABLE meter_invocations_before_judge;

CREATE INDEX meter_invocation_parent ON meter_invocations(attempt_id,parent_id,id);

CREATE UNIQUE INDEX meter_one_root ON meter_invocations(attempt_id) WHERE parent_id IS NULL;

CREATE TABLE meter_observations (
 id INTEGER PRIMARY KEY, attempt_id TEXT NOT NULL REFERENCES meter_attempts(id) ON DELETE CASCADE,
 source_id TEXT NOT NULL, event_key TEXT NOT NULL, invocation_id TEXT, observed_at TEXT, recorded_at TEXT NOT NULL,
 semantics TEXT NOT NULL, counter_epoch TEXT NOT NULL, source_sequence TEXT, actual_model TEXT, model_provenance TEXT NOT NULL,
 dimensions_json TEXT NOT NULL, payload_sha256 TEXT NOT NULL, validation_status TEXT NOT NULL, diagnostic_codes_json TEXT NOT NULL,
 UNIQUE(attempt_id,source_id,event_key), FOREIGN KEY(attempt_id,invocation_id) REFERENCES meter_invocations(attempt_id,id)
);

INSERT INTO meter_observations(id,attempt_id,source_id,event_key,invocation_id,observed_at,recorded_at,semantics,counter_epoch,source_sequence,actual_model,model_provenance,dimensions_json,payload_sha256,validation_status,diagnostic_codes_json) SELECT id,attempt_id,source_id,event_key,invocation_id,observed_at,recorded_at,semantics,counter_epoch,source_sequence,actual_model,model_provenance,dimensions_json,payload_sha256,validation_status,diagnostic_codes_json FROM meter_observations_before_judge;

DROP TABLE meter_observations_before_judge;

CREATE INDEX meter_observation_counter ON meter_observations(attempt_id,source_id,counter_epoch,id);

CREATE INDEX meter_observation_invocation ON meter_observations(attempt_id,invocation_id,id);

CREATE TABLE meter_checkpoints (
 attempt_id TEXT NOT NULL REFERENCES meter_attempts(id) ON DELETE CASCADE, source_id TEXT NOT NULL,
 counter_key TEXT NOT NULL, counter_epoch TEXT NOT NULL, invocation_id TEXT, model_key TEXT NOT NULL,
 baseline_dimensions_json TEXT NOT NULL, last_dimensions_json TEXT NOT NULL, source_cursor TEXT,
 last_observation_id INTEGER REFERENCES meter_observations(id), updated_at TEXT NOT NULL,
 PRIMARY KEY(attempt_id,source_id,counter_key,counter_epoch), FOREIGN KEY(attempt_id,invocation_id) REFERENCES meter_invocations(attempt_id,id)
);

INSERT INTO meter_checkpoints(attempt_id,source_id,counter_key,counter_epoch,invocation_id,model_key,baseline_dimensions_json,last_dimensions_json,source_cursor,last_observation_id,updated_at) SELECT attempt_id,source_id,counter_key,counter_epoch,invocation_id,model_key,baseline_dimensions_json,last_dimensions_json,source_cursor,last_observation_id,updated_at FROM meter_checkpoints_before_judge;

DROP TABLE meter_checkpoints_before_judge;

CREATE INDEX meter_checkpoint_observation ON meter_checkpoints(last_observation_id);

CREATE TABLE meter_measurements (
 id INTEGER PRIMARY KEY, attempt_id TEXT NOT NULL REFERENCES meter_attempts(id) ON DELETE CASCADE,
 invocation_id TEXT NOT NULL, model_key TEXT NOT NULL, actual_model TEXT, model_provenance TEXT NOT NULL,
 input_uncached_tokens INTEGER CHECK(input_uncached_tokens>=0), cache_read_tokens INTEGER CHECK(cache_read_tokens>=0),
 cache_write_tokens INTEGER CHECK(cache_write_tokens>=0), cache_write_5m_tokens INTEGER CHECK(cache_write_5m_tokens>=0),
 cache_write_1h_tokens INTEGER CHECK(cache_write_1h_tokens>=0), output_tokens INTEGER CHECK(output_tokens>=0), reasoning_tokens INTEGER CHECK(reasoning_tokens>=0),
 collection_status TEXT NOT NULL, diagnostic_codes_json TEXT NOT NULL, last_observation_id INTEGER NOT NULL REFERENCES meter_observations(id),
 revision INTEGER NOT NULL CHECK(revision>0), updated_at TEXT NOT NULL,
 UNIQUE(attempt_id,invocation_id,model_key), FOREIGN KEY(attempt_id,invocation_id) REFERENCES meter_invocations(attempt_id,id)
);

INSERT INTO meter_measurements(id,attempt_id,invocation_id,model_key,actual_model,model_provenance,input_uncached_tokens,cache_read_tokens,cache_write_tokens,cache_write_5m_tokens,cache_write_1h_tokens,output_tokens,reasoning_tokens,collection_status,diagnostic_codes_json,last_observation_id,revision,updated_at) SELECT id,attempt_id,invocation_id,model_key,actual_model,model_provenance,input_uncached_tokens,cache_read_tokens,cache_write_tokens,cache_write_5m_tokens,cache_write_1h_tokens,output_tokens,reasoning_tokens,collection_status,diagnostic_codes_json,last_observation_id,revision,updated_at FROM meter_measurements_before_judge;

DROP TABLE meter_measurements_before_judge;

CREATE INDEX meter_measurement_model ON meter_measurements(actual_model,attempt_id,id);

CREATE INDEX meter_measurement_observation ON meter_measurements(last_observation_id);

CREATE TABLE meter_valuations (
 id INTEGER PRIMARY KEY,
 measurement_id INTEGER NOT NULL REFERENCES meter_measurements(id) ON DELETE CASCADE,
 measurement_revision INTEGER NOT NULL CHECK(measurement_revision>0),
 price_revision_id INTEGER NOT NULL REFERENCES meter_price_revisions(id) ON DELETE RESTRICT,
 pricing_status TEXT NOT NULL, estimated_usd TEXT, priced_subtotal_usd TEXT NOT NULL, created_at TEXT NOT NULL,
 UNIQUE(measurement_id,measurement_revision)
);

INSERT INTO meter_valuations(id,measurement_id,measurement_revision,price_revision_id,pricing_status,estimated_usd,priced_subtotal_usd,created_at) SELECT id,measurement_id,measurement_revision,price_revision_id,pricing_status,estimated_usd,priced_subtotal_usd,created_at FROM meter_valuations_before_judge;

DROP TABLE meter_valuations_before_judge;

CREATE INDEX meter_valuation_price ON meter_valuations(price_revision_id);

CREATE TABLE meter_valuation_dimensions (
 valuation_id INTEGER NOT NULL REFERENCES meter_valuations(id) ON DELETE CASCADE,
 dimension TEXT NOT NULL, quantity INTEGER CHECK(quantity>=0), usd_per_million TEXT, amount_usd TEXT, pricing_status TEXT NOT NULL,
 PRIMARY KEY(valuation_id,dimension)
);

INSERT INTO meter_valuation_dimensions(valuation_id,dimension,quantity,usd_per_million,amount_usd,pricing_status) SELECT valuation_id,dimension,quantity,usd_per_million,amount_usd,pricing_status FROM meter_valuation_dimensions_before_judge;

DROP TABLE meter_valuation_dimensions_before_judge;

CREATE UNIQUE INDEX judge_result_attempt ON application_result_attempts(judge_attempt_id) WHERE judge_attempt_id IS NOT NULL;

CREATE UNIQUE INDEX judge_result_request ON application_result_requests(result_id,request_id) WHERE judge_attempt_id IS NOT NULL;

CREATE UNIQUE INDEX judge_meter_ordinal ON meter_attempts(judge_attempt_id,ordinal) WHERE judge_attempt_id IS NOT NULL;

CREATE TRIGGER application_result_requests_judge_owner BEFORE INSERT ON application_result_requests WHEN NEW.judge_attempt_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM application_result_attempts a WHERE a.judge_attempt_id=NEW.judge_attempt_id AND a.attempt=NEW.attempt AND a.result_id=NEW.result_id) BEGIN SELECT RAISE(ABORT,'judge result owner mismatch'); END;

CREATE TRIGGER application_result_revisions_judge_owner BEFORE INSERT ON application_result_revisions WHEN NEW.judge_attempt_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM application_result_attempts a WHERE a.judge_attempt_id=NEW.judge_attempt_id AND a.attempt=NEW.attempt AND a.result_id=NEW.result_id) BEGIN SELECT RAISE(ABORT,'judge result owner mismatch'); END;

CREATE TRIGGER application_result_observations_judge_owner BEFORE INSERT ON application_result_observations WHEN NEW.judge_attempt_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM application_result_attempts a WHERE a.judge_attempt_id=NEW.judge_attempt_id AND a.attempt=NEW.attempt AND a.result_id=(SELECT result_id FROM application_result_requests WHERE id=NEW.request_row)) BEGIN SELECT RAISE(ABORT,'judge result owner mismatch'); END;

CREATE TRIGGER judge_requests_pin BEFORE UPDATE OF task,ticket_key,comment_id,session_id,trigger_comment_id,source_json,configuration,evidence ON judge_requests BEGIN SELECT RAISE(ABORT,'immutable judge request'); END;

CREATE TRIGGER judge_report_immutable BEFORE UPDATE OF report ON judge_requests WHEN OLD.report IS NOT NULL BEGIN SELECT RAISE(ABORT,'immutable judge report'); END;

DROP TRIGGER doctor_hold_plan_workflow_history_insert;
-- Retain verifiable current legacy drafts before introducing the new save clock.
INSERT INTO plan_workflow_history(parent,revision,state_json) SELECT parent,revision,state_json FROM plan_workflow_states WHERE EXISTS(SELECT 1 FROM json_each(CASE WHEN json_valid(state_json) THEN CASE WHEN json_type(state_json,'$.stories')='array' THEN json_extract(state_json,'$.stories') ELSE '[]' END ELSE '[]' END) WHERE length(CASE WHEN type='object' THEN COALESCE(json_extract(value,'$.draft.name'),'') ELSE '' END)>0 AND length(CASE WHEN type='object' THEN COALESCE(json_extract(value,'$.draft.user_story.want'),'') ELSE '' END)>0) AND NOT EXISTS(SELECT 1 FROM plan_workflow_history h WHERE h.parent=plan_workflow_states.parent AND h.revision=plan_workflow_states.revision);

CREATE TRIGGER doctor_hold_plan_workflow_history_insert BEFORE INSERT ON plan_workflow_history WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND ((NEW.parent=h.task_ref OR EXISTS(SELECT 1 FROM managed_stories dm WHERE dm.parent_task=NEW.parent AND h.task_ref=substr(dm.parent_task,1,instr(dm.parent_task,'#'))||dm.issue_number) OR EXISTS(SELECT 1 FROM plan_story_drafts dp WHERE dp.parent=NEW.parent AND h.task_ref=substr(dp.parent,1,instr(dp.parent,'#'))||dp.issue_number))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;

-- New saves have one database commit order across eligible source families.
-- Active identifies the latest incarnation of an ID, not source availability.
-- Archive deletes preserve this global ledger; recreated saves supersede it.
CREATE TABLE judge_source_commits (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT,
 kind TEXT NOT NULL, source_id TEXT NOT NULL, revision INTEGER NOT NULL,
 active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1))
);
CREATE UNIQUE INDEX judge_source_current ON judge_source_commits(kind,source_id,revision) WHERE active=1;
CREATE TRIGGER judge_application_commit AFTER INSERT ON application_result_revisions
 BEGIN UPDATE judge_source_commits SET active=0 WHERE kind='result' AND source_id=CAST(NEW.result_id AS TEXT) AND revision=NEW.revision AND active=1; INSERT INTO judge_source_commits(kind,source_id,revision) VALUES('result',CAST(NEW.result_id AS TEXT),NEW.revision); END;
CREATE TRIGGER judge_plan_commit AFTER INSERT ON plan_workflow_history
 BEGIN UPDATE judge_source_commits SET active=0 WHERE kind='plan' AND source_id=NEW.parent AND revision=NEW.revision AND active=1; INSERT INTO judge_source_commits(kind,source_id,revision) VALUES('plan',NEW.parent,NEW.revision); END;
CREATE TRIGGER judge_graph_commit AFTER INSERT ON graph_outputs
 BEGIN UPDATE judge_source_commits SET active=0 WHERE kind='graph' AND source_id=CAST(NEW.id AS TEXT) AND revision=1 AND active=1; INSERT INTO judge_source_commits(kind,source_id,revision) VALUES('graph',CAST(NEW.id AS TEXT),1); END;

CREATE TRIGGER judge_plan_saved_insert AFTER INSERT ON plan_workflow_states WHEN EXISTS(SELECT 1 FROM json_each(CASE WHEN json_valid(NEW.state_json) THEN CASE WHEN json_type(NEW.state_json,'$.stories')='array' THEN json_extract(NEW.state_json,'$.stories') ELSE '[]' END ELSE '[]' END) WHERE length(CASE WHEN type='object' THEN COALESCE(json_extract(value,'$.draft.name'),'') ELSE '' END)>0 AND length(CASE WHEN type='object' THEN COALESCE(json_extract(value,'$.draft.user_story.want'),'') ELSE '' END)>0) BEGIN INSERT INTO plan_workflow_history(parent,revision,state_json) VALUES(NEW.parent,NEW.revision,NEW.state_json); END;

CREATE TRIGGER judge_plan_saved_update AFTER UPDATE ON plan_workflow_states WHEN EXISTS(SELECT 1 FROM json_each(CASE WHEN json_valid(NEW.state_json) THEN CASE WHEN json_type(NEW.state_json,'$.stories')='array' THEN json_extract(NEW.state_json,'$.stories') ELSE '[]' END ELSE '[]' END) WHERE length(CASE WHEN type='object' THEN COALESCE(json_extract(value,'$.draft.name'),'') ELSE '' END)>0 AND length(CASE WHEN type='object' THEN COALESCE(json_extract(value,'$.draft.user_story.want'),'') ELSE '' END)>0) BEGIN INSERT INTO plan_workflow_history(parent,revision,state_json) VALUES(NEW.parent,NEW.revision,NEW.state_json); END;

