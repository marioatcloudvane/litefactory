CREATE TABLE chat_conversation_answers (
 id TEXT PRIMARY KEY, turn_id TEXT NOT NULL UNIQUE REFERENCES chat_conversation_turns(id),
 transport TEXT NOT NULL, owner_id TEXT NOT NULL, channel_ref TEXT NOT NULL,
 blocks TEXT NOT NULL, version INTEGER NOT NULL, proposal_part INTEGER NOT NULL DEFAULT 0,
 created_at TEXT NOT NULL, completed_at TEXT, expired_at TEXT
);
CREATE TABLE chat_conversation_answer_parts (
 answer_id TEXT NOT NULL REFERENCES chat_conversation_answers(id), ordinal INTEGER NOT NULL,
 transport TEXT NOT NULL, reply_id TEXT NOT NULL, discord_message_id TEXT NOT NULL DEFAULT '',
 acknowledged_at TEXT, PRIMARY KEY(answer_id,ordinal), UNIQUE(transport,reply_id)
);
CREATE INDEX chat_answer_selection ON chat_conversation_answers(transport,owner_id,channel_ref,created_at);
CREATE TABLE chat_conversation_details (
 transport TEXT NOT NULL, request_id TEXT NOT NULL, answer_id TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(transport,request_id)
);
CREATE TABLE chat_conversation_details_parts (
 transport TEXT NOT NULL, request_id TEXT NOT NULL, reply_id TEXT NOT NULL, ordinal INTEGER NOT NULL,
 acknowledged_at TEXT, PRIMARY KEY(transport,request_id,ordinal), UNIQUE(transport,reply_id)
);
ALTER TABLE chat_conversation_details ADD COLUMN paused INTEGER NOT NULL DEFAULT 0;
