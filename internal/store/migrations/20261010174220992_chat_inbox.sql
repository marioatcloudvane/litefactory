CREATE TABLE chat_inbox (
 transport TEXT NOT NULL,
 message_id TEXT NOT NULL,
 channel_ref TEXT NOT NULL,
 command TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('pending','sent','confirmed','failed')),
 attempts INTEGER NOT NULL DEFAULT 0,
 next_attempt_at TEXT NOT NULL,
 reply_text TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 finished_at TEXT,
 last_error_at TEXT,
 PRIMARY KEY(transport,message_id)
);
CREATE INDEX chat_inbox_due ON chat_inbox(transport,state,next_attempt_at);
