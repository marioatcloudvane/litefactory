-- New managed plans only. Existing file-based plans are not converted.
CREATE TABLE managed_features (
 parent_task TEXT PRIMARY KEY,
 generation INTEGER NOT NULL CHECK (generation > 0),
 state TEXT NOT NULL CHECK (state IN ('planning','freezing','implementing','completed')),
 ready INTEGER NOT NULL DEFAULT 0 CHECK(ready IN (0,1))
);
CREATE TABLE managed_stories (
 parent_task TEXT NOT NULL REFERENCES managed_features(parent_task),
 generation INTEGER NOT NULL CHECK (generation > 0),
 story_key TEXT NOT NULL,
 issue_number INTEGER NOT NULL CHECK (issue_number > 0),
 issue_id INTEGER NOT NULL CHECK (issue_id > 0),
 body_hash TEXT NOT NULL,
 dependencies TEXT NOT NULL,
 state TEXT NOT NULL CHECK (state IN ('planning','frozen','running','blocked','verified','completed')),
 PRIMARY KEY(parent_task,generation,story_key),
 UNIQUE(parent_task,generation,issue_number),
 UNIQUE(parent_task,generation,issue_id)
);
CREATE TABLE story_publications (
 parent_task TEXT NOT NULL REFERENCES managed_features(parent_task),
 generation INTEGER NOT NULL,
 story_key TEXT NOT NULL,
 operation_key TEXT NOT NULL UNIQUE,
 desired_hash TEXT NOT NULL,
 issue_number INTEGER NOT NULL DEFAULT 0,
 issue_id INTEGER NOT NULL DEFAULT 0,
 delivered INTEGER NOT NULL DEFAULT 0 CHECK(delivered IN (0,1)),
 PRIMARY KEY(parent_task,generation,story_key)
);
CREATE TABLE story_revision_operations (
 operation_key TEXT PRIMARY KEY,
 parent_task TEXT NOT NULL REFERENCES managed_features(parent_task),
 generation INTEGER NOT NULL,
 story_key TEXT NOT NULL,
 expected_base TEXT NOT NULL,
 desired_hash TEXT NOT NULL,
 delivered INTEGER NOT NULL DEFAULT 0 CHECK(delivered IN (0,1))
);
