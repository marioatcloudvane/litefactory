CREATE TABLE recovery_notice_dispositions (
 id INTEGER PRIMARY KEY,
 session_id INTEGER NOT NULL REFERENCES sessions(id),
 source_key TEXT NOT NULL,
 disposition TEXT NOT NULL,
 delivery_key TEXT NOT NULL UNIQUE,
 previous_key TEXT NOT NULL,
 UNIQUE(session_id,source_key,disposition)
);
