-- Replaces the 33 dedicated bug_* tables with six, grouped by what they track
-- instead of by which step wrote them. This is a clean cutover: no bug task is
-- running when this migration deploys (a documented pre-ship release check),
-- so every retired table is dropped outright with nothing preserved or copied.
-- SQLite drops a table's own indexes and triggers when the table is dropped, so
-- no separate DROP INDEX/DROP TRIGGER statements are required. Dropping in any
-- order is safe: defer_foreign_keys postpones FK validation to commit time, by
-- which point every table in this statement is gone from the schema.
PRAGMA defer_foreign_keys=ON;

DROP TABLE bug_analysis_events;
DROP TABLE bug_analysis_approvals;
DROP TABLE bug_analysis_review_changes;
DROP TABLE bug_analysis_reviews;
DROP TABLE bug_analysis_answers;
DROP TABLE bug_analysis_revisions;
DROP TABLE bug_analysis_passes;
DROP TABLE bug_execution_sections;
DROP TABLE bug_test_resolutions;
DROP TABLE bug_test_events;
DROP TABLE bug_test_calls;
DROP TABLE bug_check_events;
DROP TABLE bug_check_commands;
DROP TABLE bug_check_phases;
DROP TABLE bug_repair_scope_observations;
DROP TABLE bug_repair_completions;
DROP TABLE bug_repairs;
DROP TABLE bug_closure_retries;
DROP TABLE bug_closure_observations;
DROP TABLE bug_closures;
DROP TABLE bug_merge_retries;
DROP TABLE bug_merge_observations;
DROP TABLE bug_merges;
DROP TABLE bug_delivery_replies;
DROP TABLE bug_delivery_reviews;
DROP TABLE bug_pr_observations;
DROP TABLE bug_pr_intents;
DROP TABLE bug_parent_continuations;
DROP TABLE bug_lifecycle_events;
DROP TABLE bug_lifecycles;
DROP TABLE bug_work_guards;
DROP TABLE bug_execution;
DROP TABLE bug_records;

-- bug_tasks: one row per bug session. Merges bug_records (repository, issue,
-- owner), bug_execution (the single optimistic revision, current analysis,
-- analysis passes used), bug_lifecycles (parent envelope, disposition, node,
-- reference, cancelled) and bug_work_guards (guard_owner, guard_kind).
CREATE TABLE bug_tasks (
 session_id INTEGER PRIMARY KEY REFERENCES workflow_admissions(session_id),
 repository TEXT NOT NULL,
 issue_id INTEGER NOT NULL CHECK(issue_id>0),
 issue_number INTEGER NOT NULL CHECK(issue_number>0),
 owner_id INTEGER NOT NULL CHECK(owner_id>0),
 revision INTEGER NOT NULL CHECK(revision>=1),
 current_analysis INTEGER NOT NULL DEFAULT 0 CHECK(current_analysis>=0),
 analysis_used INTEGER NOT NULL DEFAULT 0 CHECK(analysis_used BETWEEN 0 AND 8),
 parent_instance INTEGER REFERENCES graph_instances(id),
 parent_definition INTEGER REFERENCES graph_definitions(id),
 child_definition INTEGER REFERENCES graph_definitions(id),
 disposition TEXT NOT NULL DEFAULT 'running',
 node TEXT NOT NULL DEFAULT 'record',
 reference TEXT NOT NULL DEFAULT '',
 cancelled INTEGER NOT NULL DEFAULT 0 CHECK(cancelled IN (0,1)),
 guard_owner TEXT,
 guard_kind TEXT CHECK(guard_kind IN ('repair','checks','child','save')),
 CHECK((guard_owner IS NULL)=(guard_kind IS NULL)),
 CHECK((parent_instance IS NULL)=(parent_definition IS NULL) AND (parent_instance IS NULL)=(child_definition IS NULL))
);

-- bug_analyses: one row per analyst pass. Merges bug_analysis_passes,
-- bug_analysis_revisions, the current bug_analysis_reviews row for this pass
-- and its bug_analysis_approvals row. A superseded review or an approval that
-- stops applying moves to bug_events (kind 'review') before this row's review
-- columns are reused or the pass ages out; bug_events.delivery_key keeps that
-- history resolvable by delivery key, matching this row's own lookup.
CREATE TABLE bug_analyses (
 session_id INTEGER NOT NULL REFERENCES bug_tasks(session_id),
 invocation TEXT NOT NULL,
 input_digest TEXT NOT NULL CHECK(length(input_digest)=64),
 ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 1 AND 8),
 graph_attempt_id INTEGER NOT NULL REFERENCES graph_node_attempts(id),
 state TEXT NOT NULL CHECK(state IN ('reserved','dispatched','succeeded','failed','cancelled','uncertain')),
 run_id INTEGER REFERENCES runs(id),
 pass_reason TEXT NOT NULL DEFAULT '',
 revision INTEGER,
 digest TEXT,
 content BLOB CHECK(content IS NULL OR length(content)<=1048576),
 review_ready INTEGER CHECK(review_ready IN (0,1)),
 response TEXT CHECK(response IN ('ask','done','blocked')),
 reason TEXT NOT NULL DEFAULT '',
 evidence BLOB,
 operation TEXT,
 revision_input_digest TEXT,
 result_id INTEGER,
 result_revision INTEGER,
 review TEXT,
 review_delivery_key TEXT,
 review_content_digest TEXT,
 review_comment_id INTEGER,
 review_active INTEGER NOT NULL DEFAULT 0 CHECK(review_active IN (0,1)),
 approval_reply_id INTEGER,
 approval_owner_id INTEGER,
 approval_approved_at TEXT,
 PRIMARY KEY(session_id,invocation),
 UNIQUE(session_id,ordinal),
 UNIQUE(graph_attempt_id),
 UNIQUE(session_id,revision),
 UNIQUE(session_id,operation),
 UNIQUE(session_id,review),
 UNIQUE(review_delivery_key),
 UNIQUE(session_id,approval_reply_id),
 FOREIGN KEY(result_id,result_revision) REFERENCES application_result_revisions(result_id,revision)
);
CREATE UNIQUE INDEX bug_analyses_active ON bug_analyses(session_id) WHERE state IN ('reserved','dispatched');
CREATE UNIQUE INDEX bug_analyses_current_review ON bug_analyses(session_id) WHERE review_active=1;
CREATE TRIGGER bug_cancel_blocks_analysis_insert BEFORE INSERT ON bug_analyses
WHEN EXISTS(SELECT 1 FROM bug_tasks WHERE session_id=NEW.session_id AND cancelled=1)
BEGIN SELECT RAISE(ABORT,'Bug lifecycle cancelled'); END;
CREATE TRIGGER bug_cancel_blocks_analysis_dispatch BEFORE UPDATE OF state ON bug_analyses
WHEN NEW.state='dispatched' AND EXISTS(SELECT 1 FROM bug_tasks WHERE session_id=NEW.session_id AND cancelled=1)
BEGIN SELECT RAISE(ABORT,'Bug lifecycle cancelled'); END;

-- bug_repairs: one row per lifetime repair attempt (cap of 3, enforced in code
-- by counting rows in ReserveBugRepair -- never a CHECK on the ordinal, so a
-- later story can grant more attempts without rebuilding this table). Merges
-- bug_repairs, bug_repair_completions, the latest bug_repair_scope_observations
-- for the repair's immutable source, and the current factory test call for this
-- repair (formerly bug_test_calls). An earlier test generation for the same
-- repair moves to bug_events (kind 'test_generation') before these columns are
-- reused for the next generation.
CREATE TABLE bug_repairs (
 session_id INTEGER NOT NULL REFERENCES bug_tasks(session_id),
 request TEXT NOT NULL,
 input_digest TEXT NOT NULL,
 ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 1 AND 3),
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
 PRIMARY KEY(session_id,request),
 UNIQUE(session_id,ordinal),
 UNIQUE(session_id,test_operation),
 UNIQUE(test_parent_attempt),
 UNIQUE(test_child_instance)
);
CREATE UNIQUE INDEX bug_repairs_active ON bug_repairs(session_id) WHERE state IN ('reserved','dispatched');
CREATE UNIQUE INDEX bug_repairs_test_pending ON bug_repairs(session_id) WHERE test_outcome='pending';
CREATE UNIQUE INDEX bug_repairs_test_parent_wait ON bug_repairs(test_parent_instance) WHERE test_consumed=0;
CREATE TRIGGER bug_cancel_blocks_repair_insert BEFORE INSERT ON bug_repairs
WHEN EXISTS(SELECT 1 FROM bug_tasks WHERE session_id=NEW.session_id AND cancelled=1)
BEGIN SELECT RAISE(ABORT,'Bug lifecycle cancelled'); END;
CREATE TRIGGER bug_cancel_blocks_repair_dispatch BEFORE UPDATE OF state ON bug_repairs
WHEN NEW.state='dispatched' AND EXISTS(SELECT 1 FROM bug_tasks WHERE session_id=NEW.session_id AND cancelled=1)
BEGIN SELECT RAISE(ABORT,'Bug lifecycle cancelled'); END;

-- bug_checks: one row per check phase run. Merges bug_check_phases and
-- bug_check_commands (kept as a bounded ordered JSON list on the row instead of
-- a child table). "source" is checked in code, not by a CHECK rule, so a later
-- story can add fixer self-checks without rebuilding this table.
CREATE TABLE bug_checks (
 session_id INTEGER NOT NULL REFERENCES bug_tasks(session_id),
 operation TEXT NOT NULL,
 input_digest TEXT NOT NULL,
 binding BLOB NOT NULL,
 recipe BLOB NOT NULL,
 command_count INTEGER NOT NULL CHECK(command_count BETWEEN 1 AND 64),
 commands BLOB NOT NULL DEFAULT '[]',
 outcome TEXT NOT NULL DEFAULT 'pending',
 reason TEXT NOT NULL DEFAULT '',
 cancelled INTEGER NOT NULL DEFAULT 0 CHECK(cancelled IN (0,1)),
 cleanup INTEGER NOT NULL DEFAULT 1 CHECK(cleanup IN (0,1)),
 current INTEGER NOT NULL DEFAULT 1 CHECK(current IN (0,1)),
 unreliable INTEGER NOT NULL DEFAULT 0 CHECK(unreliable IN (0,1)),
 source TEXT NOT NULL DEFAULT 'factory',
 repair TEXT,
 generation TEXT,
 phase TEXT,
 PRIMARY KEY(session_id,operation)
);
CREATE TRIGGER bug_cancel_blocks_checks_insert BEFORE INSERT ON bug_checks
WHEN EXISTS(SELECT 1 FROM bug_tasks WHERE session_id=NEW.session_id AND cancelled=1)
BEGIN SELECT RAISE(ABORT,'Bug lifecycle cancelled'); END;

-- bug_delivery: one row per bug for the whole pull-request journey. Merges
-- bug_pr_intents, the latest bug_pr_observations, the current
-- bug_delivery_reviews row, the delivery approval reply, the current
-- bug_merges row (with its retry continuation folded in as columns) and the
-- bug_closures row (with its retry continuation folded in as columns).
-- Superseded reviews, non-approval replies, merge/close observations and
-- earlier merge operations move to bug_events before these columns are reused.
CREATE TABLE bug_delivery (
 session_id INTEGER PRIMARY KEY REFERENCES bug_tasks(session_id),
 pr_operation TEXT NOT NULL,
 pr_input_digest TEXT NOT NULL,
 pr_branch TEXT NOT NULL,
 pr_creator_id INTEGER NOT NULL,
 pr_marker TEXT NOT NULL,
 pr_dispatched INTEGER NOT NULL DEFAULT 0 CHECK(pr_dispatched IN (0,1)),
 pr_cancelled INTEGER NOT NULL DEFAULT 0 CHECK(pr_cancelled IN (0,1)),
 pr BLOB,
 review_digest TEXT,
 review_evidence BLOB,
 review_pr BLOB,
 review_presentation BLOB,
 review_body TEXT,
 review_content_digest TEXT,
 review_delivery_key TEXT,
 review_bot_id INTEGER,
 review_comment_id INTEGER,
 review_active INTEGER NOT NULL DEFAULT 0 CHECK(review_active IN (0,1)),
 review_reason TEXT NOT NULL DEFAULT '',
 approval_comment_id INTEGER,
 approval_owner_id INTEGER,
 approval_digest TEXT,
 approval_body TEXT,
 approval_at TEXT,
 merge_operation TEXT,
 merge_review_digest TEXT,
 merge_binding BLOB,
 merge_dispatched INTEGER NOT NULL DEFAULT 0 CHECK(merge_dispatched IN (0,1)),
 merge_outcome TEXT NOT NULL DEFAULT 'pending',
 merge_reason TEXT NOT NULL DEFAULT '',
 merge_exception TEXT NOT NULL DEFAULT '',
 merge_retry_continuation TEXT,
 merge_retry_evidence TEXT,
 close_operation TEXT,
 close_merge_operation TEXT,
 close_endpoint TEXT,
 close_binding BLOB,
 close_dispatched INTEGER NOT NULL DEFAULT 0 CHECK(close_dispatched IN (0,1)),
 close_confirmed INTEGER NOT NULL DEFAULT 0 CHECK(close_confirmed IN (0,1)),
 close_outcome TEXT NOT NULL DEFAULT 'pending',
 close_reason TEXT NOT NULL DEFAULT '',
 close_conflict INTEGER NOT NULL DEFAULT 0 CHECK(close_conflict IN (0,1)),
 close_retry_continuation TEXT,
 close_retry_evidence TEXT,
 UNIQUE(review_delivery_key)
);
CREATE TRIGGER bug_cancel_blocks_pr_dispatch BEFORE UPDATE OF pr_dispatched ON bug_delivery
WHEN NEW.pr_dispatched=1 AND OLD.pr_dispatched=0 AND EXISTS(SELECT 1 FROM bug_tasks WHERE session_id=NEW.session_id AND cancelled=1)
BEGIN SELECT RAISE(ABORT,'Bug lifecycle cancelled'); END;
CREATE TRIGGER bug_cancel_blocks_merge_dispatch BEFORE UPDATE OF merge_dispatched ON bug_delivery
WHEN NEW.merge_dispatched=1 AND OLD.merge_dispatched=0 AND EXISTS(SELECT 1 FROM bug_tasks WHERE session_id=NEW.session_id AND cancelled=1)
BEGIN SELECT RAISE(ABORT,'Bug lifecycle cancelled'); END;
CREATE TRIGGER bug_cancel_blocks_close_dispatch BEFORE UPDATE OF close_dispatched ON bug_delivery
WHEN NEW.close_dispatched=1 AND OLD.close_dispatched=0 AND EXISTS(SELECT 1 FROM bug_tasks WHERE session_id=NEW.session_id AND cancelled=1)
BEGIN SELECT RAISE(ABORT,'Bug lifecycle cancelled'); END;

-- bug_events: one append-only log for the whole bug. Replaces
-- bug_analysis_events, bug_check_events, bug_test_events, bug_lifecycle_events,
-- bug_parent_continuations, bug_delivery_replies, the three observation tables,
-- the two retry tables' history, the three narrow side logs (answers, review
-- changes, test resolutions) and bug_execution_sections. kind is one of a
-- closed list validated in code. Rows are never updated except "reported".
CREATE TABLE bug_events (
 id INTEGER PRIMARY KEY,
 session_id INTEGER NOT NULL REFERENCES bug_tasks(session_id),
 kind TEXT NOT NULL,
 reference TEXT NOT NULL,
 attempt_id INTEGER REFERENCES graph_node_attempts(id),
 delivery_key TEXT,
 content BLOB NOT NULL CHECK(length(content)<=1048576),
 reported INTEGER NOT NULL DEFAULT 0 CHECK(reported IN (0,1)),
 recorded_at TEXT NOT NULL,
 UNIQUE(session_id,kind,reference),
 UNIQUE(delivery_key)
);
