CREATE TABLE chat_inbox_ask (
 transport TEXT NOT NULL, message_id TEXT NOT NULL, channel_ref TEXT NOT NULL,
 command TEXT NOT NULL, arguments TEXT NOT NULL DEFAULT '{}',
 state TEXT NOT NULL CHECK(state IN ('answering','pending','sent','confirmed','failed')),
 attempts INTEGER NOT NULL DEFAULT 0, next_attempt_at TEXT NOT NULL,
 reply_text TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
 finished_at TEXT, last_error_at TEXT,
 question TEXT NOT NULL DEFAULT '' CHECK(length(CAST(question AS BLOB))<=8192),
 project_option TEXT NOT NULL DEFAULT '', resolved_repo_id INTEGER REFERENCES repos(id),
 answer_attempts INTEGER NOT NULL DEFAULT 0 CHECK(answer_attempts BETWEEN 0 AND 2),
 PRIMARY KEY(transport,message_id)
);
INSERT INTO chat_inbox_ask(transport,message_id,channel_ref,command,arguments,state,attempts,next_attempt_at,reply_text,created_at,finished_at,last_error_at)
SELECT transport,message_id,channel_ref,command,arguments,state,attempts,next_attempt_at,reply_text,created_at,finished_at,last_error_at FROM chat_inbox;
DROP TABLE chat_inbox;
ALTER TABLE chat_inbox_ask RENAME TO chat_inbox;
CREATE INDEX chat_inbox_due ON chat_inbox(transport,state,next_attempt_at);
