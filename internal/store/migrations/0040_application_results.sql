-- Application results are separate from managed contribution admission/recovery.
CREATE TABLE application_results (
 id INTEGER PRIMARY KEY,
 task TEXT NOT NULL,
 mode TEXT NOT NULL CHECK(mode IN ('research','plan','implement','validate','spike')),
 work_context TEXT NOT NULL,
 context_hash TEXT NOT NULL,
 schema_version INTEGER NOT NULL CHECK(schema_version=1),
 workflow_version INTEGER NOT NULL CHECK(workflow_version IN (1,2)),
 submission_version INTEGER NOT NULL CHECK(submission_version=2),
 revision INTEGER NOT NULL DEFAULT 0 CHECK(revision>=0),
 UNIQUE(task,mode,context_hash)
);
CREATE TABLE application_result_attempts (
 run_id INTEGER NOT NULL REFERENCES runs(id),
 attempt TEXT NOT NULL,
 result_id INTEGER NOT NULL REFERENCES application_results(id),
 active INTEGER NOT NULL CHECK(active IN (0,1)),
 PRIMARY KEY(run_id,attempt)
);
CREATE TABLE application_result_requests (
 id INTEGER PRIMARY KEY,
 result_id INTEGER NOT NULL REFERENCES application_results(id),
 run_id INTEGER NOT NULL,
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
);
CREATE TABLE application_result_revisions (
 result_id INTEGER NOT NULL REFERENCES application_results(id),
 revision INTEGER NOT NULL CHECK(revision>=1),
 run_id INTEGER NOT NULL,
 attempt TEXT NOT NULL,
 request_row INTEGER NOT NULL UNIQUE REFERENCES application_result_requests(id),
 snapshot BLOB NOT NULL,
 snapshot_hash TEXT NOT NULL,
 retired BLOB NOT NULL,
 PRIMARY KEY(result_id,revision),
 FOREIGN KEY(run_id,attempt) REFERENCES application_result_attempts(run_id,attempt)
);
CREATE TABLE application_result_observations (
 request_row INTEGER NOT NULL REFERENCES application_result_requests(id),
 sequence INTEGER NOT NULL CHECK(sequence>=1),
 run_id INTEGER NOT NULL,
 attempt TEXT NOT NULL,
 dimension TEXT NOT NULL,
 state TEXT NOT NULL,
 PRIMARY KEY(request_row,sequence),
 FOREIGN KEY(run_id,attempt) REFERENCES application_result_attempts(run_id,attempt)
);
