ALTER TABLE runs ADD COLUMN workflow TEXT NOT NULL DEFAULT '' CHECK(workflow IN ('','bug'));
-- Preserve existing result IDs and all foreign references while permitting a
-- standalone result identity with no effective mode. Existing rows and context
-- hashes remain unchanged.
PRAGMA defer_foreign_keys=ON;
CREATE TABLE application_results_before_bug AS SELECT * FROM application_results;
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
 CHECK((workflow='' AND mode IN ('research','plan','implement','validate','spike')) OR (workflow='bug' AND mode='')),
 UNIQUE(task,mode,context_hash)
);
INSERT INTO application_results(id,task,mode,work_context,context_hash,schema_version,workflow_version,submission_version,revision)
 SELECT id,task,mode,work_context,context_hash,schema_version,workflow_version,submission_version,revision FROM application_results_before_bug;
DROP TABLE application_results_before_bug;
