ALTER TABLE conflict_proposals ADD COLUMN format INTEGER NOT NULL DEFAULT 0;
ALTER TABLE conflict_proposal_files ADD COLUMN details TEXT NOT NULL DEFAULT '{}';
