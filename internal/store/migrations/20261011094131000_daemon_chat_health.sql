-- The Discord chat state `daemon status` prints beside the webhook listener, on
-- the same single display-only row. Empty when chat is off, "available" once
-- chat runs, "unavailable: <reason>" while it cannot (for example while the bot
-- token cannot be resolved).
ALTER TABLE daemon_listener_status ADD COLUMN chat_status TEXT NOT NULL DEFAULT '';
