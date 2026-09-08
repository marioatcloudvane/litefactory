-- Immutable execution inputs, distinct from editable GitHub stories.
CREATE TABLE approved_story_revisions (
 parent_task TEXT NOT NULL,
 generation INTEGER NOT NULL,
 story_key TEXT NOT NULL,
 parent_id INTEGER NOT NULL CHECK(parent_id > 0),
 title TEXT NOT NULL,
 body TEXT NOT NULL,
 digest TEXT NOT NULL,
 fetched_at TEXT NOT NULL,
 PRIMARY KEY(parent_task,generation,story_key),
 FOREIGN KEY(parent_task,generation,story_key) REFERENCES managed_stories(parent_task,generation,story_key)
);
CREATE TABLE story_scope_notices (
 parent_task TEXT NOT NULL,
 generation INTEGER NOT NULL,
 story_key TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('freeze','planning')),
 delivered INTEGER NOT NULL DEFAULT 0 CHECK(delivered IN (0,1)),
 PRIMARY KEY(parent_task,generation,story_key,kind),
 FOREIGN KEY(parent_task,generation,story_key) REFERENCES managed_stories(parent_task,generation,story_key)
);
