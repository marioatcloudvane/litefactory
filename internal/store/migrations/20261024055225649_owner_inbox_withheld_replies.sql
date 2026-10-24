CREATE TABLE owner_inbox_withheld_replies (
 transport TEXT NOT NULL,
 reply_id TEXT NOT NULL,
 notice TEXT NOT NULL,
 created_at TEXT NOT NULL,
 PRIMARY KEY(transport,reply_id),
 FOREIGN KEY(transport,reply_id) REFERENCES chat_inbox(transport,message_id) ON DELETE CASCADE
);
