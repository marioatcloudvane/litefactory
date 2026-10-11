-- The decisions a story worker made where the plan was silent, one per line,
-- kept from its accepted completed answer for the Validate reviewers.
ALTER TABLE implement_stories ADD COLUMN deviations TEXT NOT NULL DEFAULT '';
