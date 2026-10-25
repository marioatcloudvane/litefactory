ALTER TABLE comments ADD COLUMN reply_to_comment_id INTEGER NOT NULL DEFAULT 0 CHECK(reply_to_comment_id>=0);
ALTER TABLE graph_owner_waits ADD COLUMN reply_to_comment_id INTEGER NOT NULL DEFAULT 0 CHECK(reply_to_comment_id>=0);
ALTER TABLE owner_decision_bindings ADD COLUMN reply_to_comment_id INTEGER NOT NULL DEFAULT 0 CHECK(reply_to_comment_id>=0);
