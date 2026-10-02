CREATE TABLE plan_story_authorities (
 parent TEXT NOT NULL,
 request_id TEXT NOT NULL,
 authority_json TEXT NOT NULL,
 PRIMARY KEY(parent,request_id),
 FOREIGN KEY(parent,request_id) REFERENCES plan_story_candidates(parent,request_id)
);
CREATE TRIGGER plan_story_authorities_immutable BEFORE UPDATE ON plan_story_authorities BEGIN SELECT RAISE(ABORT,'immutable Plan authority'); END;
CREATE TRIGGER plan_story_authorities_no_delete BEFORE DELETE ON plan_story_authorities BEGIN SELECT RAISE(ABORT,'immutable Plan authority'); END;
CREATE TABLE plan_story_completions (
 parent TEXT NOT NULL,
 request_id TEXT NOT NULL,
 PRIMARY KEY(parent,request_id),
 FOREIGN KEY(parent,request_id) REFERENCES plan_story_candidates(parent,request_id)
);
CREATE TABLE plan_story_recovery_cursors (
 parent TEXT PRIMARY KEY,
 request_id TEXT NOT NULL
);
