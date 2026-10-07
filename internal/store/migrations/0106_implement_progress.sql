-- Advisory worker activity has no workflow or completion meaning.
CREATE TABLE implement_progress_reports (
 id INTEGER NOT NULL UNIQUE CHECK(id>0),
 parent TEXT NOT NULL,
 generation INTEGER NOT NULL,
 story_key TEXT NOT NULL,
 run_id INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
 attempt TEXT NOT NULL,
 sequence INTEGER NOT NULL CHECK(sequence>=1),
 kind TEXT NOT NULL CHECK(kind IN ('implementation','testing','repair')),
 message TEXT NOT NULL CHECK(length(CAST(message AS BLOB)) BETWEEN 1 AND 2048),
 message_hash TEXT NOT NULL,
 story_attempts INTEGER NOT NULL CHECK(story_attempts>=0),
 recorded_at TEXT NOT NULL,
 PRIMARY KEY(run_id,attempt,sequence),
 FOREIGN KEY(parent,generation,story_key) REFERENCES implement_stories(parent,generation,story_key) ON DELETE CASCADE
);
CREATE INDEX implement_progress_parent ON implement_progress_reports(parent,generation,story_key,kind);
