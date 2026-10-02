CREATE TABLE bug_execution_sections (
 session_id INTEGER NOT NULL REFERENCES bug_records(session_id),
 section TEXT NOT NULL CHECK(section IN ('repair','checks','delivery','failures')),
 revision INTEGER NOT NULL CHECK(revision>0),
 operation TEXT NOT NULL,
 input_digest TEXT NOT NULL CHECK(length(input_digest)=64),
 content BLOB NOT NULL CHECK(length(content) BETWEEN 1 AND 1048576),
 PRIMARY KEY(session_id,section,revision),
 UNIQUE(session_id,operation)
);
