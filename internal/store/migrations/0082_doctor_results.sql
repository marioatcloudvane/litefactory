-- Extend the existing result ledger with exclusive doctor ownership.
PRAGMA defer_foreign_keys=ON;

CREATE TABLE application_results_before_doctor AS SELECT * FROM application_results;

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
 CHECK((workflow='' AND mode IN ('research','plan','implement','validate','spike')) OR (workflow IN ('bug','doctor') AND mode='')),
 UNIQUE(task,mode,context_hash)
);

INSERT INTO application_results(id,task,mode,work_context,context_hash,schema_version,workflow_version,submission_version,revision,workflow) SELECT id,task,mode,work_context,context_hash,schema_version,workflow_version,submission_version,revision,workflow FROM application_results_before_doctor;

DROP TABLE application_results_before_doctor;

CREATE TABLE application_result_attempts_before_doctor AS SELECT * FROM application_result_attempts;

DROP TABLE application_result_attempts;

CREATE TABLE application_result_attempts (
 doctor_attempt_id TEXT REFERENCES doctor_attempts(id),
 run_id INTEGER REFERENCES runs(id),
 attempt TEXT NOT NULL,
 result_id INTEGER NOT NULL REFERENCES application_results(id),
 active INTEGER NOT NULL CHECK(active IN (0,1)),
 PRIMARY KEY(run_id,attempt)
,
 CHECK((run_id IS NOT NULL AND doctor_attempt_id IS NULL) OR (run_id IS NULL AND doctor_attempt_id IS NOT NULL))
);

INSERT INTO application_result_attempts(run_id,attempt,result_id,active) SELECT run_id,attempt,result_id,active FROM application_result_attempts_before_doctor;

DROP TABLE application_result_attempts_before_doctor;

CREATE TABLE application_result_requests_before_doctor AS SELECT * FROM application_result_requests;

DROP TABLE application_result_requests;

CREATE TABLE application_result_requests (
 doctor_attempt_id TEXT REFERENCES doctor_attempts(id),
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
 CHECK((run_id IS NOT NULL AND doctor_attempt_id IS NULL) OR (run_id IS NULL AND doctor_attempt_id IS NOT NULL))
);

INSERT INTO application_result_requests(id,result_id,run_id,attempt,request_id,request_hash,payload,diagnostics,input_state,applied_revision,submission_receipt_id) SELECT id,result_id,run_id,attempt,request_id,request_hash,payload,diagnostics,input_state,applied_revision,submission_receipt_id FROM application_result_requests_before_doctor;

DROP TABLE application_result_requests_before_doctor;

CREATE TABLE application_result_revisions_before_doctor AS SELECT * FROM application_result_revisions;

DROP TABLE application_result_revisions;

CREATE TABLE application_result_revisions (
 doctor_attempt_id TEXT REFERENCES doctor_attempts(id),
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
 CHECK((run_id IS NOT NULL AND doctor_attempt_id IS NULL) OR (run_id IS NULL AND doctor_attempt_id IS NOT NULL))
);

INSERT INTO application_result_revisions(result_id,revision,run_id,attempt,request_row,snapshot,snapshot_hash,retired) SELECT result_id,revision,run_id,attempt,request_row,snapshot,snapshot_hash,retired FROM application_result_revisions_before_doctor;

DROP TABLE application_result_revisions_before_doctor;

CREATE TABLE application_result_observations_before_doctor AS SELECT * FROM application_result_observations;

DROP TABLE application_result_observations;

CREATE TABLE application_result_observations (
 doctor_attempt_id TEXT REFERENCES doctor_attempts(id),
 request_row INTEGER NOT NULL REFERENCES application_result_requests(id),
 sequence INTEGER NOT NULL CHECK(sequence>=1),
 run_id INTEGER,
 attempt TEXT NOT NULL,
 dimension TEXT NOT NULL,
 state TEXT NOT NULL,
 PRIMARY KEY(request_row,sequence),
 FOREIGN KEY(run_id,attempt) REFERENCES application_result_attempts(run_id,attempt)
,
 CHECK((run_id IS NOT NULL AND doctor_attempt_id IS NULL) OR (run_id IS NULL AND doctor_attempt_id IS NOT NULL))
);

INSERT INTO application_result_observations(request_row,sequence,run_id,attempt,dimension,state) SELECT request_row,sequence,run_id,attempt,dimension,state FROM application_result_observations_before_doctor;

DROP TABLE application_result_observations_before_doctor;

CREATE UNIQUE INDEX doctor_result_attempt ON application_result_attempts(doctor_attempt_id) WHERE doctor_attempt_id IS NOT NULL;

CREATE UNIQUE INDEX doctor_result_request ON application_result_requests(result_id,request_id) WHERE doctor_attempt_id IS NOT NULL;

CREATE TRIGGER application_result_requests_doctor_owner BEFORE INSERT ON application_result_requests
 WHEN NEW.doctor_attempt_id IS NOT NULL AND NOT EXISTS(
 SELECT 1 FROM application_result_attempts a WHERE a.doctor_attempt_id=NEW.doctor_attempt_id AND a.attempt=NEW.attempt)
 BEGIN SELECT RAISE(ABORT,'doctor result owner mismatch'); END;

CREATE TRIGGER application_result_revisions_doctor_owner BEFORE INSERT ON application_result_revisions
 WHEN NEW.doctor_attempt_id IS NOT NULL AND NOT EXISTS(
 SELECT 1 FROM application_result_attempts a WHERE a.doctor_attempt_id=NEW.doctor_attempt_id AND a.attempt=NEW.attempt)
 BEGIN SELECT RAISE(ABORT,'doctor result owner mismatch'); END;

CREATE TRIGGER application_result_observations_doctor_owner BEFORE INSERT ON application_result_observations
 WHEN NEW.doctor_attempt_id IS NOT NULL AND NOT EXISTS(
 SELECT 1 FROM application_result_attempts a WHERE a.doctor_attempt_id=NEW.doctor_attempt_id AND a.attempt=NEW.attempt)
 BEGIN SELECT RAISE(ABORT,'doctor result owner mismatch'); END;

CREATE TABLE doctor_proposals (
 id INTEGER PRIMARY KEY,
 conversation_id INTEGER NOT NULL REFERENCES doctor_conversations(id),
 attempt_id TEXT NOT NULL REFERENCES doctor_attempts(id),
 result_id INTEGER NOT NULL,
 result_revision INTEGER NOT NULL,
 payload BLOB NOT NULL,
 UNIQUE(result_id,result_revision),
 FOREIGN KEY(result_id,result_revision) REFERENCES application_result_revisions(result_id,revision)
);
ALTER TABLE doctor_conversations ADD COLUMN current_proposal_id INTEGER REFERENCES doctor_proposals(id);
ALTER TABLE doctor_conversations ADD COLUMN current_result_id INTEGER REFERENCES application_results(id);
ALTER TABLE doctor_conversations ADD COLUMN current_result_revision INTEGER;
CREATE TRIGGER doctor_proposals_immutable BEFORE UPDATE ON doctor_proposals BEGIN SELECT RAISE(ABORT,'immutable doctor proposal'); END;
CREATE TRIGGER doctor_proposals_no_delete BEFORE DELETE ON doctor_proposals BEGIN SELECT RAISE(ABORT,'retained doctor proposal'); END;
