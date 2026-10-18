CREATE TABLE chat_conversation_instructions (
 conversation_id TEXT PRIMARY KEY REFERENCES chat_conversations(id),
 capture BLOB NOT NULL CHECK(length(capture)>0 AND length(capture)<=16777216),
 legacy_baseline INTEGER NOT NULL CHECK(legacy_baseline IN (0,1)),
 created_at TEXT NOT NULL
);
CREATE TABLE chat_conversation_instruction_legacy (
 conversation_id TEXT PRIMARY KEY REFERENCES chat_conversations(id)
);
INSERT INTO chat_conversation_instruction_legacy(conversation_id) SELECT id FROM chat_conversations;
