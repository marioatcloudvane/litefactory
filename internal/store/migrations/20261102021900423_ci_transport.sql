CREATE TABLE ci_transport (
    key TEXT PRIMARY KEY,
    not_before TEXT NOT NULL DEFAULT '',
    provenance TEXT NOT NULL DEFAULT '',
    failures INTEGER NOT NULL DEFAULT 0,
    lease TEXT NOT NULL DEFAULT '',
    lease_until TEXT NOT NULL DEFAULT ''
);
