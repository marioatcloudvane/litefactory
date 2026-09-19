-- A dispatch that cannot start must eventually stop asking. Without a count,
-- a permanently malformed request retries every tick forever, spending a forge
-- observation each time and never getting closer to running. Automatic
-- continuations bound themselves the same way.
ALTER TABLE graph_dispatches ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0;
