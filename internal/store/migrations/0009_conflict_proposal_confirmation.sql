-- 0009_conflict_proposal_confirmation — the confirmation half of the
-- conflict-proposal lifecycle (US-12): an owner may confirm a pending
-- proposal by naming its id and a fixed confirmation phrase, which applies
-- exactly the listed files and completes the merge.
--
-- base_sha and branch_sha are the base and branch tips a proposal's
-- suggestion was computed against, captured once at proposal time
-- (internal/turn's attemptConflictSuggestion) precisely so a later
-- confirmation can tell "this suggestion is still current" from "the branch
-- or base moved since" (S-15): both are re-read fresh and compared against
-- these two columns inside the CloneGate at apply time, immediately before
-- anything is written. A mismatch marks the proposal superseded rather than
-- applying a suggestion computed against code that no longer exists.
--
-- Existing rows predate this column and default to '': a proposal created
-- before this migration can never be confirmed (its stored shas can never
-- match a real branch/base pair), which is the safe failure — refuse rather
-- than apply against an unknown starting point.
--
-- confirmed_by and confirmed_comment_id record who confirmed and which
-- comment did it, for the disclosure and for AC-12.9's idempotent replay.
-- applied_commit_sha and applied_at are set only once a merge commit has
-- actually been made; status gains a fourth value, "applied", alongside the
-- three US-11 already defined.
ALTER TABLE conflict_proposals ADD COLUMN base_sha TEXT NOT NULL DEFAULT '';
ALTER TABLE conflict_proposals ADD COLUMN branch_sha TEXT NOT NULL DEFAULT '';
ALTER TABLE conflict_proposals ADD COLUMN confirmed_by TEXT;
ALTER TABLE conflict_proposals ADD COLUMN confirmed_comment_id INTEGER;
ALTER TABLE conflict_proposals ADD COLUMN applied_commit_sha TEXT;
ALTER TABLE conflict_proposals ADD COLUMN applied_at TEXT;
