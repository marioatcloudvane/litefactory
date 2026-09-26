CREATE TABLE bug_closures (
 session_id INTEGER PRIMARY KEY REFERENCES bug_records(session_id),
 operation TEXT NOT NULL,
 merge_operation TEXT NOT NULL,
 endpoint TEXT NOT NULL,
 binding BLOB NOT NULL,
 dispatched INTEGER NOT NULL DEFAULT 0,
 confirmed INTEGER NOT NULL DEFAULT 0,
 outcome TEXT NOT NULL DEFAULT 'pending',
 reason TEXT NOT NULL DEFAULT '',
 conflict INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE bug_closure_observations (
 session_id INTEGER NOT NULL REFERENCES bug_closures(session_id),
 sequence INTEGER NOT NULL,
 content BLOB NOT NULL,
 PRIMARY KEY(session_id,sequence)
);
CREATE TABLE bug_closure_retries (
 session_id INTEGER NOT NULL REFERENCES bug_closures(session_id),
 continuation TEXT NOT NULL,
 evidence TEXT NOT NULL,
 PRIMARY KEY(session_id,continuation)
);
