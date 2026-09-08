-- Child discussions have an independent delivery ledger, never a parent watermark.
CREATE TABLE story_refinements (
 session_id INTEGER NOT NULL REFERENCES sessions(id),
 comment_id INTEGER NOT NULL CHECK(comment_id > 0),
 child_number INTEGER NOT NULL CHECK(child_number > 0),
 child_id INTEGER NOT NULL CHECK(child_id > 0),
 generation INTEGER NOT NULL CHECK(generation > 0),
 actor_id INTEGER NOT NULL CHECK(actor_id > 0),
 actor_handle TEXT NOT NULL,
 body TEXT NOT NULL,
 consumed INTEGER NOT NULL DEFAULT 0 CHECK(consumed IN (0,1)),
 PRIMARY KEY(session_id,comment_id)
);
CREATE INDEX story_refinements_pending ON story_refinements(session_id,consumed,comment_id);
CREATE TABLE story_discovery (
 session_id INTEGER NOT NULL REFERENCES sessions(id),
 child_number INTEGER NOT NULL,
 comment_id INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(session_id,child_number)
);
CREATE TABLE story_discovery_rotation (
 session_id INTEGER PRIMARY KEY REFERENCES sessions(id),
 child_number INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE story_refinement_acks (
 session_id INTEGER NOT NULL,
 comment_id INTEGER NOT NULL,
 acknowledgement_id INTEGER NOT NULL CHECK(acknowledgement_id > 0),
 PRIMARY KEY(session_id,comment_id),
 FOREIGN KEY(session_id,comment_id) REFERENCES story_refinements(session_id,comment_id)
);
