-- Implement ledger: one row per (parent, generation, story key). It holds where
-- each story of an implement job stands, so the factory activities that start,
-- merge and report stories read and move one durable record. Moves are guarded
-- by the row's revision; the running cap is enforced where a row becomes running.
CREATE TABLE implement_stories (
 parent TEXT NOT NULL CHECK(length(parent)>0),
 generation INTEGER NOT NULL CHECK(generation>0),
 story_key TEXT NOT NULL CHECK(length(story_key)>0),
 position INTEGER NOT NULL CHECK(position>=0),
 branch TEXT NOT NULL CHECK(length(branch)>0),
 state TEXT NOT NULL CHECK(state IN ('waiting','running','done','merged','parked')),
 reason TEXT NOT NULL DEFAULT '',
 merge_commit TEXT NOT NULL DEFAULT '',
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 state_at TEXT NOT NULL,
 PRIMARY KEY(parent,generation,story_key),
 CHECK(state<>'parked' OR length(reason)>0),
 CHECK(state<>'merged' OR length(merge_commit)>0)
);
CREATE INDEX implement_stories_state ON implement_stories(parent,generation,state);
