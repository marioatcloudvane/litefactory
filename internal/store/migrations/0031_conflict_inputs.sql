ALTER TABLE conflict_proposals ADD COLUMN inputs TEXT NOT NULL DEFAULT '{}' CHECK (length(inputs) <= 8192);
