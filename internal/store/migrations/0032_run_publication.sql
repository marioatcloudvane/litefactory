-- Publication state has to outlive the turn that established it. A report whose
-- post fails is rebuilt from this row on a later tick, and the verified push
-- fact lived only in memory: a published commit was then reported as "in the
-- local worktree only". Historical runs default to unpublished with no text,
-- which reads exactly as it did before.
ALTER TABLE runs ADD COLUMN published INTEGER NOT NULL DEFAULT 0;
ALTER TABLE runs ADD COLUMN publication TEXT NOT NULL DEFAULT '';
