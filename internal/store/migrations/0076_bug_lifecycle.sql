CREATE TABLE bug_lifecycles (
 session_id INTEGER PRIMARY KEY REFERENCES bug_records(session_id),
 parent_instance INTEGER NOT NULL REFERENCES graph_instances(id),
 parent_definition INTEGER NOT NULL REFERENCES graph_definitions(id),
 child_definition INTEGER NOT NULL REFERENCES graph_definitions(id),
 revision INTEGER NOT NULL DEFAULT 1,
 disposition TEXT NOT NULL DEFAULT 'running',
 node TEXT NOT NULL DEFAULT 'record',
 reference TEXT NOT NULL DEFAULT '',
 cancelled INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE bug_lifecycle_events (
 session_id INTEGER NOT NULL REFERENCES bug_records(session_id),
 event TEXT NOT NULL,
 kind TEXT NOT NULL,
 attempt_id INTEGER REFERENCES graph_node_attempts(id),
 body TEXT NOT NULL,
 delivery_key TEXT NOT NULL UNIQUE,
 reported INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(session_id,event)
);
CREATE TABLE bug_parent_continuations (
 session_id INTEGER NOT NULL REFERENCES bug_lifecycles(session_id),
 operation TEXT NOT NULL,
 cause TEXT NOT NULL,
 reference TEXT NOT NULL,
 prior_instance INTEGER NOT NULL REFERENCES graph_instances(id),
 successor INTEGER NOT NULL REFERENCES graph_instances(id),
 entry TEXT NOT NULL,
 PRIMARY KEY(session_id,operation),
 UNIQUE(session_id,cause,reference)
);
-- Cancellation is a durable dispatch boundary, not a claim of process cleanup.
CREATE TRIGGER bug_parent_analysis_cancel BEFORE INSERT ON bug_analysis_passes
WHEN EXISTS(SELECT 1 FROM bug_lifecycles WHERE session_id=NEW.session_id AND cancelled=1)
BEGIN SELECT RAISE(ABORT,'Bug lifecycle cancelled'); END;
CREATE TRIGGER bug_parent_repair_cancel BEFORE INSERT ON bug_repairs
WHEN EXISTS(SELECT 1 FROM bug_lifecycles WHERE session_id=NEW.session_id AND cancelled=1)
BEGIN SELECT RAISE(ABORT,'Bug lifecycle cancelled'); END;
CREATE TRIGGER bug_parent_repair_dispatch_cancel BEFORE UPDATE OF state ON bug_repairs
WHEN NEW.state='dispatched' AND EXISTS(SELECT 1 FROM bug_lifecycles WHERE session_id=NEW.session_id AND cancelled=1)
BEGIN SELECT RAISE(ABORT,'Bug lifecycle cancelled'); END;
CREATE TRIGGER bug_parent_analysis_dispatch_cancel BEFORE UPDATE OF state ON bug_analysis_passes
WHEN NEW.state='dispatched' AND EXISTS(SELECT 1 FROM bug_lifecycles WHERE session_id=NEW.session_id AND cancelled=1)
BEGIN SELECT RAISE(ABORT,'Bug lifecycle cancelled'); END;
CREATE TRIGGER bug_parent_merge_dispatch_cancel BEFORE UPDATE OF dispatched ON bug_merges
WHEN NEW.dispatched=1 AND OLD.dispatched=0 AND EXISTS(SELECT 1 FROM bug_lifecycles WHERE session_id=NEW.session_id AND cancelled=1)
BEGIN SELECT RAISE(ABORT,'Bug lifecycle cancelled'); END;
CREATE TRIGGER bug_parent_close_dispatch_cancel BEFORE UPDATE OF dispatched ON bug_closures
WHEN NEW.dispatched=1 AND OLD.dispatched=0 AND EXISTS(SELECT 1 FROM bug_lifecycles WHERE session_id=NEW.session_id AND cancelled=1)
BEGIN SELECT RAISE(ABORT,'Bug lifecycle cancelled'); END;
CREATE TRIGGER bug_parent_pr_dispatch_cancel BEFORE UPDATE OF dispatched ON bug_pr_intents
WHEN NEW.dispatched=1 AND OLD.dispatched=0 AND EXISTS(SELECT 1 FROM bug_lifecycles WHERE session_id=NEW.session_id AND cancelled=1)
BEGIN SELECT RAISE(ABORT,'Bug lifecycle cancelled'); END;
CREATE TRIGGER bug_parent_child_cancel BEFORE INSERT ON bug_test_calls
WHEN EXISTS(SELECT 1 FROM bug_lifecycles WHERE session_id=NEW.session_id AND cancelled=1)
BEGIN SELECT RAISE(ABORT,'Bug lifecycle cancelled'); END;
CREATE TABLE bug_analysis_review_changes (
 session_id INTEGER NOT NULL REFERENCES bug_records(session_id),
 comment_id INTEGER NOT NULL,
 review TEXT NOT NULL,
 author_id INTEGER NOT NULL,
 body TEXT NOT NULL,
 observed_at TEXT NOT NULL,
 PRIMARY KEY(session_id,comment_id),
 FOREIGN KEY(session_id,review) REFERENCES bug_analysis_reviews(session_id,review)
);
CREATE TABLE bug_test_resolutions (
 session_id INTEGER NOT NULL REFERENCES bug_records(session_id),
 call_id INTEGER NOT NULL REFERENCES bug_test_calls(id),
 comment_id INTEGER NOT NULL,
 author_id INTEGER NOT NULL,
 body TEXT NOT NULL,
 resolution TEXT NOT NULL,
 PRIMARY KEY(session_id,comment_id),
 UNIQUE(call_id)
);
