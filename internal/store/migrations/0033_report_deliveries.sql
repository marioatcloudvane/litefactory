-- Intent and the possibly-sent boundary precede any report/reply POST. A
-- receipt and source completion are distinct so local recovery never reposts.
CREATE TABLE report_deliveries (
    delivery_key TEXT PRIMARY KEY,
    identity_version INTEGER NOT NULL CHECK (identity_version = 1),
    session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    trigger_id INTEGER NOT NULL CHECK (trigger_id >= 0),
    run_id INTEGER REFERENCES runs(id) ON DELETE CASCADE,
    task TEXT NOT NULL,
    destination TEXT NOT NULL,
    trigger_namespace TEXT NOT NULL,
    purpose TEXT NOT NULL,
    bot_user_id INTEGER NOT NULL CHECK (bot_user_id > 0),
    body TEXT NOT NULL,
    marker TEXT NOT NULL,
    possibly_sent INTEGER NOT NULL DEFAULT 0 CHECK (possibly_sent IN (0,1)),
    comment_id INTEGER CHECK (comment_id > 0),
    completed INTEGER NOT NULL DEFAULT 0 CHECK (completed IN (0,1)),
    legacy INTEGER NOT NULL DEFAULT 0 CHECK (legacy IN (0,1)),
    CHECK (comment_id IS NULL OR possibly_sent = 1),
    CHECK (completed = 0 OR comment_id IS NOT NULL),
    CHECK (legacy = 0 OR possibly_sent = 1)
);
CREATE INDEX report_deliveries_pending ON report_deliveries(session_id, trigger_id) WHERE completed = 0;

-- Old allocated reports have no dispatch journal. Absence of a new intent is
-- therefore not proof that no previous POST happened. New allocations are not
-- added here; deletion removes provenance rather than misclassifying reused IDs.
CREATE TABLE report_delivery_legacy_runs (
    run_id INTEGER PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE
);
INSERT INTO report_delivery_legacy_runs(run_id) SELECT id FROM runs;
