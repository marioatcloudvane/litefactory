-- The original wait remains authoritative; reminder receipts use the report journal.
CREATE TABLE research_wait_reminders (
 wait_delivery_key TEXT PRIMARY KEY REFERENCES graph_owner_waits(delivery_key),
 published_at TEXT NOT NULL,
 reminder_delivery_key TEXT UNIQUE REFERENCES report_deliveries(delivery_key),
 suppressed INTEGER NOT NULL DEFAULT 0 CHECK(suppressed IN (0,1))
);
