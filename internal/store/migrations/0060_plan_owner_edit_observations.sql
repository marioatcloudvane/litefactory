-- A semantic candidate may have multiple presentation-only observations.
ALTER TABLE plan_story_owner_edits RENAME TO plan_story_owner_edits_previous;
DROP TRIGGER plan_story_owner_edits_immutable;
DROP TRIGGER plan_story_owner_edits_no_delete;
CREATE TABLE plan_story_owner_edits (
 parent TEXT NOT NULL,
 story_key TEXT NOT NULL,
 observation_id TEXT NOT NULL,
 candidate TEXT NOT NULL,
 base_revision TEXT NOT NULL,
 observed_revision TEXT NOT NULL,
 observed_title TEXT NOT NULL,
 observed_body TEXT NOT NULL,
 draft_json TEXT NOT NULL,
 stage TEXT NOT NULL CHECK(stage IN ('story','technical','acceptance')),
 PRIMARY KEY(parent,story_key,observation_id),
 FOREIGN KEY(parent,story_key) REFERENCES plan_story_drafts(parent,story_key)
);
INSERT INTO plan_story_owner_edits(parent,story_key,observation_id,candidate,base_revision,observed_revision,observed_title,observed_body,draft_json,stage)
SELECT parent,story_key,base_revision || ':' || observed_revision,candidate,base_revision,observed_revision,observed_title,observed_body,draft_json,stage FROM plan_story_owner_edits_previous;
DROP TABLE plan_story_owner_edits_previous;
CREATE INDEX plan_owner_edit_candidates ON plan_story_owner_edits(parent,story_key,candidate);
CREATE TRIGGER plan_story_owner_edits_immutable BEFORE UPDATE ON plan_story_owner_edits BEGIN SELECT RAISE(ABORT,'immutable owner edit observation'); END;
CREATE TRIGGER plan_story_owner_edits_no_delete BEFORE DELETE ON plan_story_owner_edits BEGIN SELECT RAISE(ABORT,'immutable owner edit observation'); END;
