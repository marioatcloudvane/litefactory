CREATE TABLE stage_decision_deliveries (
 execution_id TEXT PRIMARY KEY,
 proposal_id TEXT NOT NULL UNIQUE,
 task_ref TEXT NOT NULL,
 rendered TEXT NOT NULL,
 digest TEXT NOT NULL,
 principal TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('pending','sending','uncertain','delivered','failed')),
 reason TEXT NOT NULL DEFAULT '',
 comment_url TEXT NOT NULL DEFAULT '',
 next_attempt_at TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL
);
CREATE INDEX stage_decision_delivery_ready ON stage_decision_deliveries(state,next_attempt_at,created_at);
