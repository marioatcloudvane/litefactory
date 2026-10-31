-- A fresh conversation start: turns at or before this sequence are no longer resent, and the native session identity derives from it.
ALTER TABLE chat_conversations ADD COLUMN fresh_after INTEGER NOT NULL DEFAULT 0;
