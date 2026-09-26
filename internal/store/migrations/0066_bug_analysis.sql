CREATE TABLE bug_records (
 session_id INTEGER PRIMARY KEY REFERENCES workflow_admissions(session_id),
 version INTEGER NOT NULL CHECK(version=1),
 repository TEXT NOT NULL,
 issue_id INTEGER NOT NULL CHECK(issue_id>0),
 issue_number INTEGER NOT NULL CHECK(issue_number>0),
 owner_id INTEGER NOT NULL CHECK(owner_id>0),
 workflow TEXT NOT NULL CHECK(workflow='bug'),
 workflow_version INTEGER NOT NULL CHECK(workflow_version=1)
);
CREATE TABLE bug_execution (
 session_id INTEGER PRIMARY KEY REFERENCES bug_records(session_id),
 version INTEGER NOT NULL CHECK(version=1),
 revision INTEGER NOT NULL CHECK(revision>=1),
 current_analysis INTEGER NOT NULL DEFAULT 0 CHECK(current_analysis>=0),
 analysis_used INTEGER NOT NULL DEFAULT 0 CHECK(analysis_used BETWEEN 0 AND 8)
);
CREATE TABLE bug_analysis_passes (
 session_id INTEGER NOT NULL REFERENCES bug_records(session_id),
 invocation TEXT NOT NULL,
 input_digest TEXT NOT NULL CHECK(length(input_digest)=64),
 ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 1 AND 8),
 graph_attempt_id INTEGER NOT NULL REFERENCES graph_node_attempts(id),
 state TEXT NOT NULL CHECK(state IN ('reserved','dispatched','succeeded','failed','cancelled','uncertain')),
 run_id INTEGER REFERENCES runs(id),
 reason TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(session_id,invocation),
 UNIQUE(session_id,ordinal),
 UNIQUE(graph_attempt_id)
);
CREATE UNIQUE INDEX bug_analysis_active ON bug_analysis_passes(session_id) WHERE state IN ('reserved','dispatched');
CREATE TABLE bug_analysis_revisions (
 session_id INTEGER NOT NULL REFERENCES bug_records(session_id),
 revision INTEGER NOT NULL CHECK(revision>=1),
 digest TEXT NOT NULL CHECK(length(digest)=64),
 content BLOB NOT NULL CHECK(length(content)<=1048576),
 review_ready INTEGER NOT NULL CHECK(review_ready IN (0,1)),
 invocation TEXT NOT NULL,
 operation TEXT NOT NULL,
 input_digest TEXT NOT NULL CHECK(length(input_digest)=64),
 result_id INTEGER NOT NULL REFERENCES application_results(id),
 result_revision INTEGER NOT NULL CHECK(result_revision>0),
 PRIMARY KEY(session_id,revision),
 UNIQUE(session_id,operation),
 UNIQUE(session_id,invocation),
 FOREIGN KEY(session_id,invocation) REFERENCES bug_analysis_passes(session_id,invocation),
 FOREIGN KEY(result_id,result_revision) REFERENCES application_result_revisions(result_id,revision)
);
CREATE TABLE bug_analysis_answers (
 session_id INTEGER NOT NULL REFERENCES bug_records(session_id),
 observation TEXT NOT NULL,
 question_id TEXT NOT NULL,
 comment_id INTEGER NOT NULL CHECK(comment_id>0),
 author_id INTEGER NOT NULL CHECK(author_id>0),
 body TEXT NOT NULL CHECK(length(CAST(body AS BLOB)) BETWEEN 1 AND 65536),
 observed_at TEXT NOT NULL,
 PRIMARY KEY(session_id,observation)
);
CREATE TABLE bug_analysis_reviews (
 session_id INTEGER NOT NULL REFERENCES bug_records(session_id),
 review TEXT NOT NULL,
 analysis_revision INTEGER NOT NULL,
 analysis_digest TEXT NOT NULL CHECK(length(analysis_digest)=64),
 delivery_key TEXT NOT NULL UNIQUE REFERENCES graph_comment_intents(delivery_key),
 comment_id INTEGER,
 content_digest TEXT NOT NULL CHECK(length(content_digest)=64),
 active INTEGER NOT NULL CHECK(active IN(0,1)),
 PRIMARY KEY(session_id,review),
 FOREIGN KEY(session_id,analysis_revision) REFERENCES bug_analysis_revisions(session_id,revision)
);
CREATE UNIQUE INDEX bug_analysis_current_review ON bug_analysis_reviews(session_id) WHERE active=1;
CREATE TABLE bug_analysis_approvals (
 session_id INTEGER NOT NULL REFERENCES bug_records(session_id),
 review TEXT NOT NULL,
 analysis_revision INTEGER NOT NULL,
 analysis_digest TEXT NOT NULL,
 reply_id INTEGER NOT NULL CHECK(reply_id>0),
 owner_id INTEGER NOT NULL CHECK(owner_id>0),
 approved_at TEXT NOT NULL,
 PRIMARY KEY(session_id,review),
 UNIQUE(session_id,reply_id),
 FOREIGN KEY(session_id,review) REFERENCES bug_analysis_reviews(session_id,review)
);
CREATE TABLE bug_analysis_events (
 id INTEGER PRIMARY KEY,
 session_id INTEGER NOT NULL REFERENCES bug_records(session_id),
 kind TEXT NOT NULL,
 reference TEXT NOT NULL,
 reason TEXT NOT NULL,
 recorded_at TEXT NOT NULL
);
