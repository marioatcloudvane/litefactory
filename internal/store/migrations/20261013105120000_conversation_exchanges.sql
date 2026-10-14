ALTER TABLE chat_conversation_turns ADD COLUMN source TEXT NOT NULL DEFAULT '';
ALTER TABLE chat_conversation_turns ADD COLUMN reply_to TEXT NOT NULL DEFAULT '';
CREATE TABLE chat_conversation_exchanges (
 turn_id TEXT NOT NULL REFERENCES chat_conversation_turns(id), ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 1 AND 16),
 kind TEXT NOT NULL, request TEXT NOT NULL, result TEXT NOT NULL DEFAULT '', provider_attempt TEXT NOT NULL DEFAULT '',
 captured_at TEXT NOT NULL, complete INTEGER NOT NULL DEFAULT 0, gaps TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(turn_id,ordinal)
);
CREATE TABLE chat_conversation_references (
 id TEXT PRIMARY KEY, turn_id TEXT NOT NULL REFERENCES chat_conversation_turns(id),
 kind TEXT NOT NULL, repository TEXT NOT NULL DEFAULT '', target TEXT NOT NULL DEFAULT '', revision TEXT NOT NULL DEFAULT '',
 content TEXT NOT NULL DEFAULT '', expires_at TEXT NOT NULL
);
CREATE INDEX chat_conversation_reference_turn ON chat_conversation_references(turn_id);
CREATE TABLE chat_conversation_actions (
 turn_id TEXT PRIMARY KEY REFERENCES chat_conversation_turns(id), dialogue_id TEXT NOT NULL DEFAULT '',
 dialogue_revision INTEGER NOT NULL DEFAULT 0, execution_id TEXT NOT NULL DEFAULT '', preview_id TEXT NOT NULL DEFAULT '',
 payload_digest TEXT NOT NULL DEFAULT '', outcome TEXT NOT NULL DEFAULT ''
);
ALTER TABLE chat_inbox ADD COLUMN conversation_turn_id TEXT NOT NULL DEFAULT '';
