-- The archive's delete transaction is the only writer of this table. A row lets
-- the delete of one Plan parent's protected history rows through the otherwise
-- unconditional BEFORE DELETE triggers; it exists only inside that transaction.
CREATE TABLE plan_archive_grants (
    parent TEXT PRIMARY KEY
);
DROP TRIGGER plan_story_candidates_no_delete;
CREATE TRIGGER plan_story_candidates_no_delete BEFORE DELETE ON plan_story_candidates WHEN NOT EXISTS (SELECT 1 FROM plan_archive_grants WHERE parent = OLD.parent) BEGIN SELECT RAISE(ABORT,'immutable Plan candidate'); END;
DROP TRIGGER plan_story_effects_no_delete;
CREATE TRIGGER plan_story_effects_no_delete BEFORE DELETE ON plan_story_effects WHEN NOT EXISTS (SELECT 1 FROM plan_archive_grants WHERE parent = OLD.parent) BEGIN SELECT RAISE(ABORT,'immutable Plan effect'); END;
DROP TRIGGER plan_story_no_delete;
CREATE TRIGGER plan_story_no_delete BEFORE DELETE ON plan_story_drafts WHEN NOT EXISTS (SELECT 1 FROM plan_archive_grants WHERE parent = OLD.parent) BEGIN SELECT RAISE(ABORT,'reserved Plan ordinal'); END;
DROP TRIGGER plan_workflow_history_no_delete;
CREATE TRIGGER plan_workflow_history_no_delete BEFORE DELETE ON plan_workflow_history WHEN NOT EXISTS (SELECT 1 FROM plan_archive_grants WHERE parent = OLD.parent) BEGIN SELECT RAISE(ABORT,'immutable Plan workflow history'); END;
DROP TRIGGER plan_story_owner_edits_no_delete;
CREATE TRIGGER plan_story_owner_edits_no_delete BEFORE DELETE ON plan_story_owner_edits WHEN NOT EXISTS (SELECT 1 FROM plan_archive_grants WHERE parent = OLD.parent) BEGIN SELECT RAISE(ABORT,'immutable owner edit observation'); END;
DROP TRIGGER plan_story_authorities_no_delete;
CREATE TRIGGER plan_story_authorities_no_delete BEFORE DELETE ON plan_story_authorities WHEN NOT EXISTS (SELECT 1 FROM plan_archive_grants WHERE parent = OLD.parent) BEGIN SELECT RAISE(ABORT,'immutable Plan authority'); END;
