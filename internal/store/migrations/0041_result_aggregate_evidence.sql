CREATE TABLE application_result_aggregates (
 result_id INTEGER NOT NULL REFERENCES application_results(id),
 reference TEXT NOT NULL,
 run_id INTEGER NOT NULL,
 attempt TEXT NOT NULL,
 parent_task TEXT NOT NULL,
 context_hash TEXT NOT NULL,
 selected BLOB NOT NULL,
 selected_hash TEXT NOT NULL,
 digest TEXT NOT NULL,
 files BLOB NOT NULL,
 PRIMARY KEY(result_id,reference),
 FOREIGN KEY(run_id,attempt) REFERENCES application_result_attempts(run_id,attempt)
);
CREATE TABLE application_result_aggregate_checks (
 result_id INTEGER NOT NULL,
 aggregate_ref TEXT NOT NULL,
 reference TEXT NOT NULL,
 execution_id TEXT NOT NULL,
 run_id INTEGER NOT NULL,
 attempt TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind='aggregate'),
 name TEXT NOT NULL,
 digest TEXT NOT NULL,
 executed INTEGER NOT NULL CHECK(executed IN (0,1)),
 passed INTEGER NOT NULL CHECK(passed IN (0,1)),
 evidence TEXT NOT NULL,
 PRIMARY KEY(result_id,reference),
 UNIQUE(run_id,execution_id),
 FOREIGN KEY(result_id,aggregate_ref) REFERENCES application_result_aggregates(result_id,reference),
 FOREIGN KEY(run_id,attempt) REFERENCES application_result_attempts(run_id,attempt)
);
