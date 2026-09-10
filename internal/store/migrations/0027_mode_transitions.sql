-- Trigger-scoped label transition intent; never a persistent preference.
ALTER TABLE mode_selections ADD COLUMN previous_label TEXT NOT NULL DEFAULT '';
ALTER TABLE mode_selections ADD COLUMN previous_label_removed INTEGER NOT NULL DEFAULT 0 CHECK (previous_label_removed IN (0, 1));
