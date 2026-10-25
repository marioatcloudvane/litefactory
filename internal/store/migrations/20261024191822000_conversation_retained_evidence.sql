-- Conversation-owned protected projections retain source identity separately
-- from admitted bytes. Purging an owning turn invalidates every derived view.
CREATE TABLE chat_conversation_evidence (
 turn_id TEXT NOT NULL REFERENCES chat_conversation_turns(id),
 ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 1 AND 128),
 source_turn_id TEXT NOT NULL REFERENCES chat_conversation_turns(id),
 repository TEXT NOT NULL, issue TEXT NOT NULL,
 metadata BLOB NOT NULL, content TEXT NOT NULL,
 purged INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(turn_id,ordinal)
);
CREATE INDEX chat_conversation_evidence_source ON chat_conversation_evidence(source_turn_id);
CREATE TRIGGER chat_conversation_evidence_purge AFTER UPDATE OF source ON chat_conversation_turns
WHEN OLD.source<>'' AND NEW.source=''
BEGIN
 UPDATE chat_conversation_evidence SET content='',purged=1 WHERE source_turn_id=NEW.id OR turn_id=NEW.id;
END;
