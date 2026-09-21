CREATE TABLE plan_story_owner_edits (
 parent TEXT NOT NULL,
 story_key TEXT NOT NULL,
 candidate TEXT NOT NULL,
 base_revision TEXT NOT NULL,
 observed_revision TEXT NOT NULL,
 observed_title TEXT NOT NULL,
 observed_body TEXT NOT NULL,
 draft_json TEXT NOT NULL,
 stage TEXT NOT NULL CHECK(stage IN ('story','technical','acceptance')),
 PRIMARY KEY(parent,story_key,candidate),
 FOREIGN KEY(parent,story_key) REFERENCES plan_story_drafts(parent,story_key)
);
CREATE TRIGGER plan_story_owner_edits_immutable BEFORE UPDATE ON plan_story_owner_edits BEGIN SELECT RAISE(ABORT,'immutable owner edit observation'); END;
CREATE TRIGGER plan_story_owner_edits_no_delete BEFORE DELETE ON plan_story_owner_edits BEGIN SELECT RAISE(ABORT,'immutable owner edit observation'); END;
