CREATE TABLE chat_natural_scans (
 channel_ref TEXT PRIMARY KEY, cursor TEXT NOT NULL, high_water TEXT NOT NULL DEFAULT '', before_id TEXT NOT NULL DEFAULT ''
);
CREATE TABLE chat_natural_messages (
 channel_ref TEXT NOT NULL, source_id TEXT NOT NULL, owner_id TEXT NOT NULL,
 reply_to TEXT NOT NULL DEFAULT '', content TEXT NOT NULL, created_at TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'staged', finished_at TEXT,
 PRIMARY KEY(channel_ref,source_id)
);
CREATE TABLE chat_natural_dialogues (
 transport TEXT NOT NULL, owner_id TEXT NOT NULL, channel_ref TEXT NOT NULL,
 revision INTEGER NOT NULL, state TEXT NOT NULL, source_id TEXT NOT NULL,
 payload TEXT NOT NULL, digest TEXT NOT NULL, code TEXT NOT NULL,
 prompt_id TEXT NOT NULL DEFAULT '', expires_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 PRIMARY KEY(transport,owner_id,channel_ref)
);
CREATE TABLE chat_natural_executions (
 identity TEXT PRIMARY KEY, transport TEXT NOT NULL, owner_id TEXT NOT NULL, channel_ref TEXT NOT NULL,
 revision INTEGER NOT NULL, payload TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'authorized',
 outcome TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, finished_at TEXT,
 UNIQUE(transport,owner_id,channel_ref,revision)
);
