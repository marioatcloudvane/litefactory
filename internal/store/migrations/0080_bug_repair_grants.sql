-- 0079 documented bug_repairs.ordinal as "never a CHECK ... so a later story
-- can grant more attempts without rebuilding this table", but the CREATE
-- TABLE it shipped kept CHECK(ordinal BETWEEN 1 AND 3) anyway, which would
-- reject every granted attempt's INSERT (ordinal 4, 5, ...) with a CHECK
-- constraint failure. This migration is that rebuild: it drops the stale
-- upper bound (the lifetime cap stays enforced in ReserveBugRepair, now as
-- 3 + granted attempts) and adds grant_reference, the nullable column an
-- owner's granted attempt is reserved against. grant_reference is null for
-- ordinals 1-3 and, for a granted attempt, names the exhaustion decision that
-- authorized it (exhaustion:<n>); UNIQUE(grant_reference) makes sure one
-- grant pays for exactly one repair row.
--
-- SQLite cannot alter a CHECK or add a UNIQUE column in place, so the table
-- is rebuilt under its own name, matching 0055's approach: nothing else
-- references bug_repairs by foreign key, so the carrier only needs to
-- survive the drop, not resolve inbound references.
PRAGMA defer_foreign_keys=ON;
CREATE TABLE bug_repairs_before_grants AS SELECT * FROM bug_repairs;
DROP TABLE bug_repairs;
CREATE TABLE bug_repairs (
 session_id INTEGER NOT NULL REFERENCES bug_tasks(session_id),
 request TEXT NOT NULL,
 input_digest TEXT NOT NULL,
 ordinal INTEGER NOT NULL CHECK(ordinal>=1),
 analysis_revision INTEGER NOT NULL,
 analysis_digest TEXT NOT NULL,
 approval_review TEXT NOT NULL,
 cause TEXT NOT NULL CHECK(cause IN ('initial','test_failure','pr_feedback','reshape')),
 evidence TEXT NOT NULL,
 source_commit TEXT NOT NULL,
 source_tree TEXT NOT NULL,
 graph_attempt_id INTEGER NOT NULL REFERENCES graph_node_attempts(id),
 run_id INTEGER REFERENCES runs(id),
 state TEXT NOT NULL CHECK(state IN ('reserved','dispatched','completed','reshape','blocked','failed','uncertain')),
 reason TEXT NOT NULL DEFAULT '',
 result_id INTEGER,
 result_revision INTEGER,
 completion_evidence BLOB,
 completed_commit TEXT,
 completed_tree TEXT,
 save_operation TEXT,
 scope_source_commit TEXT,
 scope_source_tree TEXT,
 scope_diff_digest TEXT,
 scope_disposition TEXT CHECK(scope_disposition IN ('within','outside','indeterminate')),
 scope_evidence TEXT,
 scope_paths BLOB,
 test_operation TEXT,
 test_input_digest TEXT,
 test_binding BLOB,
 test_generation INTEGER,
 test_parent_instance INTEGER REFERENCES graph_instances(id),
 test_parent_attempt INTEGER REFERENCES graph_node_attempts(id),
 test_child_instance INTEGER REFERENCES graph_instances(id),
 test_definition_digest TEXT,
 test_outcome TEXT CHECK(test_outcome IN ('pending','passed','failed','blocked','cancelled')),
 test_reason TEXT NOT NULL DEFAULT '',
 test_cancelled INTEGER NOT NULL DEFAULT 0 CHECK(test_cancelled IN (0,1)),
 test_consumed INTEGER NOT NULL DEFAULT 0 CHECK(test_consumed IN (0,1)),
 test_current INTEGER NOT NULL DEFAULT 1 CHECK(test_current IN (0,1)),
 test_created_at TEXT,
 grant_reference TEXT,
 PRIMARY KEY(session_id,request),
 UNIQUE(session_id,ordinal),
 UNIQUE(session_id,test_operation),
 UNIQUE(test_parent_attempt),
 UNIQUE(test_child_instance),
 UNIQUE(grant_reference)
);
INSERT INTO bug_repairs(session_id,request,input_digest,ordinal,analysis_revision,analysis_digest,approval_review,cause,evidence,source_commit,source_tree,graph_attempt_id,run_id,state,reason,result_id,result_revision,completion_evidence,completed_commit,completed_tree,save_operation,scope_source_commit,scope_source_tree,scope_diff_digest,scope_disposition,scope_evidence,scope_paths,test_operation,test_input_digest,test_binding,test_generation,test_parent_instance,test_parent_attempt,test_child_instance,test_definition_digest,test_outcome,test_reason,test_cancelled,test_consumed,test_current,test_created_at,grant_reference)
 SELECT session_id,request,input_digest,ordinal,analysis_revision,analysis_digest,approval_review,cause,evidence,source_commit,source_tree,graph_attempt_id,run_id,state,reason,result_id,result_revision,completion_evidence,completed_commit,completed_tree,save_operation,scope_source_commit,scope_source_tree,scope_diff_digest,scope_disposition,scope_evidence,scope_paths,test_operation,test_input_digest,test_binding,test_generation,test_parent_instance,test_parent_attempt,test_child_instance,test_definition_digest,test_outcome,test_reason,test_cancelled,test_consumed,test_current,test_created_at,NULL
 FROM bug_repairs_before_grants;
DROP TABLE bug_repairs_before_grants;
CREATE UNIQUE INDEX bug_repairs_active ON bug_repairs(session_id) WHERE state IN ('reserved','dispatched');
CREATE UNIQUE INDEX bug_repairs_test_pending ON bug_repairs(session_id) WHERE test_outcome='pending';
CREATE UNIQUE INDEX bug_repairs_test_parent_wait ON bug_repairs(test_parent_instance) WHERE test_consumed=0;
CREATE TRIGGER bug_cancel_blocks_repair_insert BEFORE INSERT ON bug_repairs
WHEN EXISTS(SELECT 1 FROM bug_tasks WHERE session_id=NEW.session_id AND cancelled=1)
BEGIN SELECT RAISE(ABORT,'Bug lifecycle cancelled'); END;
CREATE TRIGGER bug_cancel_blocks_repair_dispatch BEFORE UPDATE OF state ON bug_repairs
WHEN NEW.state='dispatched' AND EXISTS(SELECT 1 FROM bug_tasks WHERE session_id=NEW.session_id AND cancelled=1)
BEGIN SELECT RAISE(ABORT,'Bug lifecycle cancelled'); END;
