-- Scheduling is separate from delivery intent: even an intent write can fail.
-- The daemon retains missing writes in memory and supplies all policy/time values.
CREATE TABLE report_recoveries (
    recovery_key TEXT PRIMARY KEY,
    session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    trigger_id INTEGER NOT NULL CHECK (trigger_id >= 0),
    delivery_key TEXT NOT NULL DEFAULT '',
    task TEXT NOT NULL DEFAULT '',
    run_id INTEGER REFERENCES runs(id) ON DELETE CASCADE,
    attempts INTEGER NOT NULL CHECK (attempts >= 0),
    next_at TEXT NOT NULL,
    state TEXT NOT NULL,
    reason TEXT NOT NULL,
    escalated INTEGER NOT NULL CHECK (escalated IN (0,1))
);
CREATE INDEX report_recoveries_source ON report_recoveries(session_id, trigger_id);
