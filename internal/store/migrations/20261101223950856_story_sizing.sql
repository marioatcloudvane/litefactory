ALTER TABLE approved_story_revisions ADD COLUMN sizing_json TEXT NOT NULL DEFAULT '';
CREATE TABLE published_story_sizing (parent_task TEXT NOT NULL, generation INTEGER NOT NULL, story_key TEXT NOT NULL, projection_revision TEXT NOT NULL, facts_json TEXT NOT NULL, PRIMARY KEY(parent_task,generation,story_key,projection_revision));
