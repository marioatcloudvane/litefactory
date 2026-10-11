-- Extend the existing acknowledgement owner with prepared bytes and a one-use
-- dispatch right. Rebuild only to widen the receipt CHECK for pending id=0;
-- retained composite identities and legacy confirmed receipts are unchanged.
ALTER TABLE story_refinement_acks RENAME TO story_refinement_acks_legacy;
CREATE TABLE story_refinement_acks (
 session_id INTEGER NOT NULL,
 comment_id INTEGER NOT NULL,
 acknowledgement_id INTEGER NOT NULL CHECK(acknowledgement_id >= 0),
 prepared_body TEXT NOT NULL DEFAULT '',
 dispatch_attempted INTEGER NOT NULL DEFAULT 0 CHECK(dispatch_attempted IN (0,1)),
 PRIMARY KEY(session_id,comment_id),
 FOREIGN KEY(session_id,comment_id) REFERENCES story_refinements(session_id,comment_id)
);
INSERT INTO story_refinement_acks(session_id,comment_id,acknowledgement_id)
SELECT session_id,comment_id,acknowledgement_id FROM story_refinement_acks_legacy;
-- Older pending admissions had no dispatch bit or prepared ACK bytes. Missing
-- receipt cannot prove that no POST occurred; retain that ambiguity without
-- granting a new send. New post-migration admissions prepare and claim normally.
INSERT INTO story_refinement_acks(session_id,comment_id,acknowledgement_id,dispatch_attempted)
SELECT r.session_id,r.comment_id,0,1 FROM story_refinements r
WHERE r.consumed=0 AND NOT EXISTS (
 SELECT 1 FROM story_refinement_acks a
 WHERE a.session_id=r.session_id AND a.comment_id=r.comment_id
);
DROP TABLE story_refinement_acks_legacy;
