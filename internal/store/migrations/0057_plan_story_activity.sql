-- Drafts remain separate from complete/frozen managed story contracts.
CREATE TABLE plan_story_drafts (
 parent TEXT NOT NULL,
 story_key TEXT NOT NULL,
 ordinal INTEGER NOT NULL CHECK(ordinal > 0),
 issue_id INTEGER NOT NULL DEFAULT 0,
 issue_number INTEGER NOT NULL DEFAULT 0,
 revision TEXT NOT NULL DEFAULT '',
 candidate TEXT NOT NULL DEFAULT '',
 stage TEXT NOT NULL DEFAULT 'story',
 draft_json TEXT NOT NULL DEFAULT '',
 retired INTEGER NOT NULL DEFAULT 0 CHECK(retired IN (0,1)),
 PRIMARY KEY(parent, story_key), UNIQUE(parent, ordinal)
);
CREATE UNIQUE INDEX plan_story_issue_identity ON plan_story_drafts(parent,issue_id) WHERE issue_id > 0;
CREATE TABLE plan_story_candidates (
 parent TEXT NOT NULL,
 request_id TEXT NOT NULL,
 story_key TEXT NOT NULL,
 candidate TEXT NOT NULL,
 request_json TEXT NOT NULL,
 PRIMARY KEY(parent,request_id), UNIQUE(parent,story_key,candidate),
 FOREIGN KEY(parent,story_key) REFERENCES plan_story_drafts(parent,story_key)
);
CREATE TABLE plan_story_effects (
 id INTEGER PRIMARY KEY,
 parent TEXT NOT NULL,
 request_id TEXT NOT NULL,
 phase TEXT NOT NULL,
 outcome TEXT NOT NULL CHECK(outcome IN ('pending','applied','conflict','failed','uncertain')),
 issue_id INTEGER NOT NULL DEFAULT 0,
 issue_number INTEGER NOT NULL DEFAULT 0,
 detail TEXT NOT NULL DEFAULT '',
 revision TEXT NOT NULL DEFAULT '',
 FOREIGN KEY(parent,request_id) REFERENCES plan_story_candidates(parent,request_id)
);
CREATE TRIGGER plan_story_candidates_immutable BEFORE UPDATE ON plan_story_candidates BEGIN SELECT RAISE(ABORT,'immutable Plan candidate'); END;
CREATE TRIGGER plan_story_candidates_no_delete BEFORE DELETE ON plan_story_candidates BEGIN SELECT RAISE(ABORT,'immutable Plan candidate'); END;
CREATE TRIGGER plan_story_effects_immutable BEFORE UPDATE ON plan_story_effects BEGIN SELECT RAISE(ABORT,'immutable Plan effect'); END;
CREATE TRIGGER plan_story_effects_no_delete BEFORE DELETE ON plan_story_effects BEGIN SELECT RAISE(ABORT,'immutable Plan effect'); END;
CREATE TRIGGER plan_story_ordinal_immutable BEFORE UPDATE OF ordinal ON plan_story_drafts WHEN NEW.ordinal <> OLD.ordinal BEGIN SELECT RAISE(ABORT,'immutable Plan ordinal'); END;
CREATE TRIGGER plan_story_no_delete BEFORE DELETE ON plan_story_drafts BEGIN SELECT RAISE(ABORT,'reserved Plan ordinal'); END;
