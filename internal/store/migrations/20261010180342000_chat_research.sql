ALTER TABLE graph_owner_waits ADD COLUMN reply_source TEXT NOT NULL DEFAULT 'forge' CHECK(reply_source IN ('forge','chat'));

DROP INDEX chat_inbox_due;
ALTER TABLE chat_inbox RENAME TO chat_inbox_previous;
CREATE TABLE chat_inbox (
 transport TEXT NOT NULL,
 message_id TEXT NOT NULL,
 channel_ref TEXT NOT NULL,
 command TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('working','pending','sent','confirmed','failed')),
 attempts INTEGER NOT NULL DEFAULT 0,
 next_attempt_at TEXT NOT NULL,
 reply_text TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 finished_at TEXT,
 last_error_at TEXT,
 target_ref TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(transport,message_id)
);
INSERT INTO chat_inbox(transport,message_id,channel_ref,command,state,attempts,next_attempt_at,reply_text,created_at,finished_at,last_error_at)
 SELECT transport,message_id,channel_ref,command,state,attempts,next_attempt_at,reply_text,created_at,finished_at,last_error_at FROM chat_inbox_previous;
DROP TABLE chat_inbox_previous;
CREATE INDEX chat_inbox_due ON chat_inbox(transport,state,next_attempt_at);
