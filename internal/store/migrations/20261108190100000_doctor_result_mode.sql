-- litefactory:rebuild-tables application_results
-- The Doctor is a graph workflow with mode 'doctor'; its node results are
-- ordinary mode results. The retired Doctor runtime's rows (workflow 'doctor',
-- mode '') stay readable as history.
CREATE TABLE application_results_rebuild (
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
 CHECK((workflow='' AND mode IN ('research','plan','implement','validate','spike','doctor')) OR (workflow IN ('bug','doctor') AND mode='') OR (workflow='judge' AND mode='judge')),
 UNIQUE(task,mode,context_hash)
);
INSERT INTO application_results_rebuild(id,task,mode,work_context,context_hash,schema_version,workflow_version,submission_version,revision,workflow)
 SELECT id,task,mode,work_context,context_hash,schema_version,workflow_version,submission_version,revision,workflow FROM application_results;
DROP TABLE application_results;
ALTER TABLE application_results_rebuild RENAME TO application_results;
