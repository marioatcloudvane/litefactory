-- The Validate reporter writes the owner summary once per tested commit and
-- answers at most a bounded number of owner comments per run. Each row is one
-- reporter attempt; once settled (accepted or fallback) it never changes, so
-- the report is always rendered from the same stored bytes.
CREATE TABLE validate_reporter_results (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 instance_id INTEGER NOT NULL CHECK(instance_id>0),
 commit_sha TEXT NOT NULL CHECK(length(commit_sha) IN (40,64)),
 mode TEXT NOT NULL CHECK(mode IN ('report','follow-up')),
 attempt TEXT NOT NULL CHECK(length(attempt)>0),
 comment_id INTEGER NOT NULL DEFAULT 0 CHECK(comment_id>=0),
 question TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL CHECK(state IN ('pending','accepted','fallback')),
 result TEXT NOT NULL DEFAULT '',
 reason TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 finished_at TEXT NOT NULL DEFAULT '',
 CHECK((mode='follow-up')=(comment_id>0)),
 UNIQUE(attempt)
);
CREATE UNIQUE INDEX validate_reporter_results_report ON validate_reporter_results(instance_id,commit_sha) WHERE mode='report';
CREATE UNIQUE INDEX validate_reporter_results_comment ON validate_reporter_results(instance_id,comment_id) WHERE mode='follow-up';
CREATE TRIGGER validate_reporter_results_settled BEFORE UPDATE ON validate_reporter_results WHEN OLD.state<>'pending'
BEGIN SELECT RAISE(ABORT,'a settled reporter result never changes'); END;

-- One owner-confirmed upstream factory Bug for one item of a Validate run. The
-- intent is written before the remote call; a filed row never changes.
CREATE TABLE validate_factory_bugs (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 instance_id INTEGER NOT NULL CHECK(instance_id>0),
 commit_sha TEXT NOT NULL CHECK(length(commit_sha) IN (40,64)),
 item TEXT NOT NULL CHECK(length(item)>0),
 comment_id INTEGER NOT NULL CHECK(comment_id>0),
 author_id INTEGER NOT NULL CHECK(author_id>0),
 marker TEXT NOT NULL CHECK(length(marker)>0),
 state TEXT NOT NULL CHECK(state IN ('intent','filed','failed')),
 reference TEXT NOT NULL DEFAULT '',
 url TEXT NOT NULL DEFAULT '',
 error TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 UNIQUE(instance_id,commit_sha,item)
);
CREATE TRIGGER validate_factory_bugs_filed BEFORE UPDATE ON validate_factory_bugs WHEN OLD.state='filed'
BEGIN SELECT RAISE(ABORT,'a filed factory Bug never changes'); END;
