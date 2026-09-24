CREATE TABLE plan_story_notice_states (
 parent TEXT NOT NULL,
 story_key TEXT NOT NULL,
 material_json TEXT NOT NULL,
 PRIMARY KEY(parent,story_key)
);
CREATE TABLE plan_story_notices (
 id INTEGER PRIMARY KEY,
 parent TEXT NOT NULL,
 story_key TEXT NOT NULL,
 material_json TEXT NOT NULL,
 delivered INTEGER NOT NULL DEFAULT 0 CHECK(delivered IN (0,1)),
 attempts INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE plan_reply_explanations (
 delivery_key TEXT PRIMARY KEY,
 parent TEXT NOT NULL,
 story_key TEXT NOT NULL,
 child_number INTEGER NOT NULL,
 comment_id INTEGER NOT NULL,
 reason TEXT NOT NULL,
 UNIQUE(parent,comment_id)
);
