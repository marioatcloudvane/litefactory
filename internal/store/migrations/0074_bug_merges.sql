CREATE TABLE bug_merges (
 session_id INTEGER NOT NULL REFERENCES bug_records(session_id),
 operation TEXT NOT NULL,
 review_digest TEXT NOT NULL,
 binding BLOB NOT NULL,
 dispatched INTEGER NOT NULL DEFAULT 0,
 outcome TEXT NOT NULL DEFAULT 'pending',
 reason TEXT NOT NULL DEFAULT '',
 exception TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(session_id,operation)
);
CREATE UNIQUE INDEX bug_merge_unresolved ON bug_merges(session_id) WHERE outcome IN ('pending','confirmed');
CREATE TABLE bug_merge_observations (
 session_id INTEGER NOT NULL,
 operation TEXT NOT NULL,
 sequence INTEGER NOT NULL,
 content BLOB NOT NULL,
 PRIMARY KEY(session_id,operation,sequence),
 FOREIGN KEY(session_id,operation) REFERENCES bug_merges(session_id,operation)
);
CREATE TABLE bug_merge_retries (
 session_id INTEGER NOT NULL,
 operation TEXT NOT NULL,
 continuation TEXT NOT NULL,
 evidence TEXT NOT NULL,
 PRIMARY KEY(session_id,operation,continuation),
 FOREIGN KEY(session_id,operation) REFERENCES bug_merges(session_id,operation)
);
