-- 0049 pinned graph_comment_intents.kind to the three publications that existed
-- then. A review has since become a kind of its own -- it shows the owner
-- finished work and waits on their answer -- and the store already writes it,
-- but the CHECK still refused the value. So a review-gated stage ran an agent to
-- completion and died on its retention insert with "CHECK constraint failed:
-- kind IN ('question','final','failure')", losing the authored comment; #318 and
-- #313 both lost a full shaping pass that way. Widen the constraint to the four
-- kinds GraphCommentKind accepts.
--
-- SQLite cannot alter a CHECK in place, so the table is rebuilt. It is rebuilt
-- under its own name rather than renamed aside, because graph_owner_waits holds
-- a foreign key to graph_comment_intents(delivery_key) and a rename would
-- rewrite that reference to point at the table about to be dropped. Dropping
-- first and re-inserting after is what lets defer_foreign_keys settle: the drop
-- raises the deferred count for the waits that lost their parent and the insert
-- brings it back to zero before COMMIT, which SQLite checks there. The carrier
-- holds the rows across the drop; it is a bare copy because nothing may
-- reference it.
PRAGMA defer_foreign_keys=ON;
CREATE TABLE graph_comment_intents_before_review_kind AS SELECT * FROM graph_comment_intents;
DROP TABLE graph_comment_intents;
CREATE TABLE graph_comment_intents (
    delivery_key TEXT PRIMARY KEY,
    instance_id INTEGER NOT NULL REFERENCES graph_instances(id),
    source_attempt_id INTEGER NOT NULL REFERENCES graph_node_attempts(id),
    node TEXT NOT NULL,
    kind TEXT NOT NULL CHECK(kind IN ('question','review','final','failure')),
    candidate BLOB NOT NULL CHECK(length(candidate) BETWEEN 1 AND 1048576)
);
INSERT INTO graph_comment_intents(delivery_key,instance_id,source_attempt_id,node,kind,candidate)
 SELECT delivery_key,instance_id,source_attempt_id,node,kind,candidate FROM graph_comment_intents_before_review_kind;
DROP TABLE graph_comment_intents_before_review_kind;
