-- 0007_run_sync — persists the outcome of a run's base-branch auto-sync
-- attempt (US-9), so the history of what happened to a task's branch
-- survives past the comment that first disclosed it (US-3, US-4).
--
-- Four columns, all on runs, written once by internal/turn right after the
-- runs row itself is allocated:
--
--   - sync_outcome names which of syncBase's outcomes this run reached:
--     up_to_date, synced, blocked, skipped, or failed. It follows the
--     project's "empty text carries no meaning" convention rather than the
--     agent column's single-value backfill (0003): there is no correct
--     outcome to invent for a run that predates auto-sync altogether, so
--     DEFAULT '' — SQL NULL's Go-side value — is what those rows keep.
--   - sync_behind and sync_ahead are the divergence syncBase measured before
--     deciding, in commits. Zero is a real, common value here — a run that
--     never diverged from its base is exactly what up_to_date means — so
--     these are plain non-null integers rather than nullable columns; a run
--     written before this migration reads back 0/0, which is the same
--     "nothing to report" reading a real up-to-date run would produce.
--   - sync_commit_sha is the merge commit syncBase produced, and is left
--     nullable rather than NOT NULL DEFAULT: it carries a real value only
--     when sync_outcome = 'synced' and the merge actually moved the branch,
--     and "" would be indistinguishable from "no merge happened" anyway
--     (mirrors the existing runs.commit_sha column's own convention).
--
-- Conflicting file paths are deliberately not a column here: US-4's refusal
-- renders them once, live, from the MergePreview result syncBase already
-- holds in memory, and reloading them from storage later is out of this
-- story's scope.
ALTER TABLE runs ADD COLUMN sync_outcome TEXT NOT NULL DEFAULT '';
ALTER TABLE runs ADD COLUMN sync_behind INTEGER NOT NULL DEFAULT 0;
ALTER TABLE runs ADD COLUMN sync_ahead INTEGER NOT NULL DEFAULT 0;
ALTER TABLE runs ADD COLUMN sync_commit_sha TEXT;
