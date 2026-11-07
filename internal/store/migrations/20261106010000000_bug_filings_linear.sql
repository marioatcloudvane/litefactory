ALTER TABLE bug_filings ADD COLUMN tracker TEXT NOT NULL DEFAULT 'github' CHECK(tracker IN ('github','linear'));
ALTER TABLE bug_filings ADD COLUMN effect_key TEXT NOT NULL DEFAULT '';
ALTER TABLE bug_filings ADD COLUMN label_id TEXT NOT NULL DEFAULT '';
ALTER TABLE bug_filings ADD COLUMN destination TEXT NOT NULL DEFAULT '';
