-- Candidate bytes/evidence are immutable. Delivery, owner authorization and
-- worktree operations are separate append-only facts so restart never turns
-- a pending candidate into adopted work.
CREATE TABLE validate_repair_candidates (
 id TEXT PRIMARY KEY,
 instance_id INTEGER NOT NULL CHECK(instance_id>0),
 round_id INTEGER NOT NULL REFERENCES validate_repair_rounds(id),
 content BLOB NOT NULL,
 created_at TEXT NOT NULL,
 UNIQUE(round_id)
);
CREATE TRIGGER validate_repair_candidates_immutable BEFORE UPDATE ON validate_repair_candidates
BEGIN SELECT RAISE(ABORT,'repair candidate is immutable'); END;
CREATE TABLE validate_repair_candidate_reviews (
 candidate_id TEXT PRIMARY KEY REFERENCES validate_repair_candidates(id),
 review_digest TEXT NOT NULL,
 completed_at TEXT NOT NULL
);
CREATE TRIGGER validate_repair_candidate_reviews_immutable BEFORE UPDATE ON validate_repair_candidate_reviews
BEGIN SELECT RAISE(ABORT,'repair review receipt is immutable'); END;
CREATE TABLE validate_repair_candidate_decisions (
 candidate_id TEXT PRIMARY KEY REFERENCES validate_repair_candidates(id),
 action TEXT NOT NULL CHECK(action IN ('accept','discard')),
 review_digest TEXT NOT NULL,
 report_revision TEXT NOT NULL,
 principal TEXT NOT NULL,
 source_id TEXT NOT NULL UNIQUE,
 decided_at TEXT NOT NULL
);
CREATE TRIGGER validate_repair_candidate_decisions_immutable BEFORE UPDATE ON validate_repair_candidate_decisions
BEGIN SELECT RAISE(ABORT,'repair candidate decision is immutable'); END;
-- Capture intent can precede the immutable candidate row: it must survive a
-- failed capture and block work rather than depend on a successful snapshot.
CREATE TABLE validate_repair_candidate_journal (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 candidate_id TEXT NOT NULL,
 instance_id INTEGER NOT NULL CHECK(instance_id>0),
 round_id INTEGER NOT NULL REFERENCES validate_repair_rounds(id),
 phase TEXT NOT NULL CHECK(phase IN ('capture','reset','apply')),
 state TEXT NOT NULL CHECK(state IN ('intent','complete','blocked')),
 expected_base TEXT NOT NULL DEFAULT '',
 expected_result TEXT NOT NULL DEFAULT '',
 result_commit TEXT NOT NULL DEFAULT '',
 reason TEXT NOT NULL DEFAULT '',
 recorded_at TEXT NOT NULL,
 UNIQUE(candidate_id,phase,state)
);
CREATE TRIGGER validate_repair_candidate_journal_immutable BEFORE UPDATE ON validate_repair_candidate_journal
BEGIN SELECT RAISE(ABORT,'repair operation journal is immutable'); END;
