-- Separate phase observations; historical runs retain an empty inventory.
ALTER TABLE runs ADD COLUMN sync_observations TEXT NOT NULL DEFAULT '[]';
