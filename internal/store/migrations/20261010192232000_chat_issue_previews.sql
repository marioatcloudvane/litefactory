CREATE TABLE chat_issue_previews (
 id TEXT PRIMARY KEY, transport TEXT NOT NULL, channel_ref TEXT NOT NULL,
 repo_id INTEGER NOT NULL REFERENCES repos(id), source_message_id TEXT NOT NULL,
 code TEXT NOT NULL UNIQUE, content_hash TEXT NOT NULL, title TEXT NOT NULL, body TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('previewed','superseded','creating','uncertain','created','expired')),
 created_at TEXT NOT NULL, expires_at TEXT NOT NULL, last_attempt_at TEXT,
 last_confirm_message_id TEXT NOT NULL DEFAULT '', claim_version INTEGER NOT NULL DEFAULT 0, lease_until TEXT,
 issue_number INTEGER NOT NULL DEFAULT 0, issue_link TEXT NOT NULL DEFAULT '', finished_at TEXT,
 UNIQUE(transport,source_message_id)
);
CREATE UNIQUE INDEX chat_issue_current ON chat_issue_previews(transport,channel_ref)
 WHERE state IN ('previewed','creating','uncertain');
CREATE INDEX chat_issue_retention ON chat_issue_previews(state,finished_at,expires_at);
