-- Keep interrupted recovery identities from starving newer admitted work.
ALTER TABLE owner_action_proposals ADD COLUMN progressed_at TEXT NOT NULL DEFAULT '';
