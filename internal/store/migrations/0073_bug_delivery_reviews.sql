CREATE TABLE bug_pr_intents (
 session_id INTEGER PRIMARY KEY REFERENCES bug_records(session_id),
 operation TEXT NOT NULL, input_digest TEXT NOT NULL, branch TEXT NOT NULL,
 creator_id INTEGER NOT NULL, marker TEXT NOT NULL, dispatched INTEGER NOT NULL DEFAULT 0,
 pr BLOB, cancelled INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE bug_pr_observations (
 session_id INTEGER NOT NULL REFERENCES bug_records(session_id),
 operation TEXT NOT NULL, input_digest TEXT NOT NULL, content BLOB NOT NULL,
 PRIMARY KEY(session_id,operation)
);
CREATE TABLE bug_delivery_reviews (
 session_id INTEGER NOT NULL REFERENCES bug_records(session_id), digest TEXT NOT NULL,
 evidence BLOB NOT NULL, pr BLOB NOT NULL, presentation BLOB NOT NULL, body TEXT NOT NULL, content_digest TEXT NOT NULL,
 delivery_key TEXT NOT NULL UNIQUE, bot_id INTEGER NOT NULL, comment_id INTEGER NOT NULL DEFAULT 0,
 active INTEGER NOT NULL DEFAULT 1, reason TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(session_id,digest)
);
CREATE UNIQUE INDEX bug_delivery_active_review ON bug_delivery_reviews(session_id) WHERE active=1;
CREATE TABLE bug_delivery_replies (
 session_id INTEGER NOT NULL REFERENCES bug_records(session_id), comment_id INTEGER NOT NULL,
 digest TEXT NOT NULL, input_digest TEXT NOT NULL, kind TEXT NOT NULL,
 body TEXT NOT NULL, author_id INTEGER NOT NULL, observed_at TEXT NOT NULL,
 PRIMARY KEY(session_id,comment_id),
 FOREIGN KEY(session_id,digest) REFERENCES bug_delivery_reviews(session_id,digest)
);
CREATE UNIQUE INDEX bug_delivery_approval ON bug_delivery_replies(session_id,digest) WHERE kind='approved';
