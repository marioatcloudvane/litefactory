CREATE TABLE stage_decision_receipts (
 execution_id TEXT PRIMARY KEY,
 task_ref TEXT NOT NULL,
 target TEXT NOT NULL,
 change_text TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('applied','refused','not_applied')),
 receipt TEXT NOT NULL,
 dispatch_id INTEGER,
 created_at TEXT NOT NULL
);
CREATE INDEX stage_decision_dispatch ON stage_decision_receipts(dispatch_id);
