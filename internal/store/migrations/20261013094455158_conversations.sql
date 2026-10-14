CREATE TABLE chat_conversations (
 id TEXT PRIMARY KEY, transport TEXT NOT NULL, channel_ref TEXT NOT NULL, owner_id TEXT NOT NULL,
 name TEXT NOT NULL, provider TEXT NOT NULL CHECK(provider IN ('claude','codex')),
 session_handle TEXT NOT NULL DEFAULT '', state_dir TEXT NOT NULL,
 active INTEGER NOT NULL DEFAULT 1, delivery_paused INTEGER NOT NULL DEFAULT 0, health_reason TEXT NOT NULL DEFAULT '',
 revision INTEGER NOT NULL DEFAULT 1, next_sequence INTEGER NOT NULL DEFAULT 1,
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX chat_conversation_active ON chat_conversations(transport,channel_ref) WHERE active=1;
CREATE TABLE chat_conversation_turns (
 id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL REFERENCES chat_conversations(id),
 source_id TEXT NOT NULL, sequence INTEGER NOT NULL, prompt TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','running','completed','interrupted','unavailable')),
 generation INTEGER NOT NULL DEFAULT 0, worker TEXT NOT NULL DEFAULT '', lease_until TEXT,
 reply_id TEXT NOT NULL DEFAULT '', delivered_at TEXT, discord_message_id TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL, finished_at TEXT,
 UNIQUE(conversation_id,source_id), UNIQUE(conversation_id,sequence)
);
CREATE INDEX chat_conversation_pending ON chat_conversation_turns(conversation_id,sequence,state);
