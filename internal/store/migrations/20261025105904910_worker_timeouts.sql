ALTER TABLE graph_attempt_timeouts ADD COLUMN budget_ns INTEGER;
CREATE TABLE worker_attempt_timing (
 node_attempt_id INTEGER NOT NULL DEFAULT 0,
 run_id INTEGER NOT NULL DEFAULT 0,
 started_at TEXT NOT NULL,
 deadline TEXT NOT NULL,
 allowance_ns INTEGER NOT NULL CHECK(allowance_ns>0),
 source TEXT NOT NULL,
 CHECK((node_attempt_id>0) != (run_id>0)),
 PRIMARY KEY(node_attempt_id,run_id)
);
CREATE TRIGGER worker_attempt_timing_immutable BEFORE UPDATE ON worker_attempt_timing
BEGIN SELECT RAISE(ABORT,'worker timing is immutable'); END;
ALTER TABLE implement_stories ADD COLUMN timeout_attempt_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE implement_stories ADD COLUMN timeout_park_revision INTEGER NOT NULL DEFAULT 0;
ALTER TABLE implement_stories ADD COLUMN timeout_delivery_key TEXT NOT NULL DEFAULT '';
ALTER TABLE implement_stories ADD COLUMN timeout_comment_id TEXT NOT NULL DEFAULT '';
ALTER TABLE implement_stories ADD COLUMN timeout_notice_at TEXT NOT NULL DEFAULT '';
ALTER TABLE implement_stories ADD COLUMN timeout_notice_remote_at TEXT NOT NULL DEFAULT '';
CREATE TABLE implement_timeout_replies (
 source TEXT NOT NULL,
 task TEXT NOT NULL,
 comment_id TEXT NOT NULL,
 parent TEXT NOT NULL,
 generation INTEGER NOT NULL,
 story_key TEXT NOT NULL,
 timeout_attempt_id INTEGER NOT NULL,
 park_revision INTEGER NOT NULL,
 delivery_key TEXT NOT NULL,
 notice_comment_id TEXT NOT NULL,
 guidance TEXT NOT NULL,
 created_at TEXT NOT NULL,
 applied INTEGER NOT NULL DEFAULT 0 CHECK(applied IN (0,1)),
 applied_at TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(source,task,comment_id,story_key)
);
