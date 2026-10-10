CREATE TABLE chat_notice_positions (
 transport TEXT NOT NULL,
 source TEXT NOT NULL,
 last_id INTEGER NOT NULL,
 initial_id INTEGER NOT NULL,
 initial_at TEXT NOT NULL,
 seen TEXT NOT NULL DEFAULT '{}',
 pending TEXT NOT NULL DEFAULT '[]',
 revision INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(transport,source)
);
CREATE TABLE chat_notices (
 transport TEXT NOT NULL,
 message_id TEXT NOT NULL,
 channel_ref TEXT NOT NULL,
 command TEXT NOT NULL,
 task_ref TEXT NOT NULL,
 arguments TEXT NOT NULL DEFAULT '{}',
 state TEXT NOT NULL CHECK(state IN ('pending','sent','confirmed','failed')),
 attempts INTEGER NOT NULL DEFAULT 0,
 next_attempt_at TEXT NOT NULL,
 reply_text TEXT NOT NULL,
 created_at TEXT NOT NULL,
 finished_at TEXT,
 last_error_at TEXT,
 PRIMARY KEY(transport,message_id)
);
CREATE INDEX chat_notices_due ON chat_notices(transport,state,next_attempt_at);
