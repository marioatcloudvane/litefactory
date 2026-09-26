CREATE TABLE bug_check_phases (
 session_id INTEGER NOT NULL REFERENCES bug_records(session_id),
 operation TEXT NOT NULL,
 input_digest TEXT NOT NULL,
 binding BLOB NOT NULL,
 recipe BLOB NOT NULL,
 command_count INTEGER NOT NULL CHECK(command_count BETWEEN 1 AND 64),
 outcome TEXT NOT NULL DEFAULT 'pending',
 reason TEXT NOT NULL DEFAULT '',
 cancelled INTEGER NOT NULL DEFAULT 0 CHECK(cancelled IN (0,1)),
 cleanup INTEGER NOT NULL DEFAULT 1 CHECK(cleanup IN (0,1)),
 current INTEGER NOT NULL DEFAULT 1 CHECK(current IN (0,1)),
 unreliable INTEGER NOT NULL DEFAULT 0 CHECK(unreliable IN (0,1)),
 PRIMARY KEY(session_id,operation)
);
CREATE TABLE bug_check_commands (
 session_id INTEGER NOT NULL,
 operation TEXT NOT NULL,
 ordinal INTEGER NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('claimed','started','completed')),
 observation BLOB,
 PRIMARY KEY(session_id,operation,ordinal),
 FOREIGN KEY(session_id,operation) REFERENCES bug_check_phases(session_id,operation)
);
CREATE TABLE bug_check_events (
 id INTEGER PRIMARY KEY,
 session_id INTEGER NOT NULL,
 operation TEXT NOT NULL,
 kind TEXT NOT NULL,
 content BLOB NOT NULL,
 FOREIGN KEY(session_id,operation) REFERENCES bug_check_phases(session_id,operation)
);
