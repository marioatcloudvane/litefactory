CREATE TABLE brainstorm_scan_receipts (
 scan_id TEXT PRIMARY KEY REFERENCES brainstorm_scans(id),
 execution_id TEXT NOT NULL UNIQUE REFERENCES brainstorm_scans(execution_id),
 request_id TEXT NOT NULL,
 digest TEXT NOT NULL,
 payload TEXT NOT NULL,
 created_at TEXT NOT NULL
);
