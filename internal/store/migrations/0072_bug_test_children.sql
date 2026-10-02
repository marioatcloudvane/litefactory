CREATE TABLE bug_test_calls (
 id INTEGER PRIMARY KEY,
 session_id INTEGER NOT NULL REFERENCES bug_records(session_id),
 operation TEXT NOT NULL,
 input_digest TEXT NOT NULL,
 binding BLOB NOT NULL,
 repair TEXT NOT NULL,
 generation INTEGER NOT NULL CHECK(generation>0),
 parent_instance INTEGER NOT NULL REFERENCES graph_instances(id),
 parent_attempt INTEGER NOT NULL UNIQUE REFERENCES graph_node_attempts(id),
 child_instance INTEGER NOT NULL UNIQUE REFERENCES graph_instances(id),
 definition_digest TEXT NOT NULL,
 outcome TEXT NOT NULL DEFAULT 'pending' CHECK(outcome IN ('pending','passed','failed','blocked','cancelled')),
 reason TEXT NOT NULL DEFAULT '',
 cancelled INTEGER NOT NULL DEFAULT 0 CHECK(cancelled IN (0,1)),
 consumed INTEGER NOT NULL DEFAULT 0 CHECK(consumed IN (0,1)),
 current INTEGER NOT NULL DEFAULT 1 CHECK(current IN (0,1)),
 created_at TEXT NOT NULL,
 UNIQUE(session_id,operation), UNIQUE(session_id,repair,generation)
);
CREATE UNIQUE INDEX bug_test_one_parent_wait ON bug_test_calls(parent_instance) WHERE consumed=0;
CREATE UNIQUE INDEX bug_test_one_active ON bug_test_calls(session_id) WHERE outcome='pending';
CREATE TABLE bug_test_events (
 id INTEGER PRIMARY KEY,
 call_id INTEGER NOT NULL REFERENCES bug_test_calls(id),
 kind TEXT NOT NULL,
 evidence TEXT NOT NULL
);
-- Child ownership persists between phases, when the command-level guard is absent.
CREATE TRIGGER bug_test_guard_insert BEFORE INSERT ON bug_work_guards
WHEN EXISTS(SELECT 1 FROM bug_test_calls c WHERE c.session_id=NEW.session_id AND c.outcome='pending'
 AND NOT(NEW.kind='checks' AND NEW.owner IN ('bug-test:'||c.id||':regression','bug-test:'||c.id||':focused','bug-test:'||c.id||':full')))
BEGIN SELECT RAISE(ABORT,'Bug test child owns worktree'); END;
CREATE TRIGGER bug_test_repair_insert BEFORE INSERT ON bug_repairs
WHEN EXISTS(SELECT 1 FROM bug_test_calls WHERE session_id=NEW.session_id AND outcome='pending')
BEGIN SELECT RAISE(ABORT,'Bug test child owns worktree'); END;
-- Cancellation and launch are serialized even before a phase record exists.
CREATE TRIGGER bug_test_command_cancel BEFORE INSERT ON bug_check_commands
WHEN EXISTS(SELECT 1 FROM bug_test_calls c WHERE c.session_id=NEW.session_id AND c.cancelled=1
 AND NEW.operation IN ('bug-test:'||c.id||':regression','bug-test:'||c.id||':focused','bug-test:'||c.id||':full'))
BEGIN SELECT RAISE(ABORT,'Bug test child cancelled'); END;
