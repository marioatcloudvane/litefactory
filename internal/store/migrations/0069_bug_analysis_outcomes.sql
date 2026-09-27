ALTER TABLE bug_analysis_revisions ADD COLUMN response TEXT NOT NULL DEFAULT 'ask' CHECK(response IN ('ask','done','blocked'));
ALTER TABLE bug_analysis_revisions ADD COLUMN reason TEXT NOT NULL DEFAULT '';
ALTER TABLE bug_analysis_revisions ADD COLUMN evidence BLOB NOT NULL DEFAULT '[]';
UPDATE bug_analysis_revisions SET response='done' WHERE review_ready=1;
