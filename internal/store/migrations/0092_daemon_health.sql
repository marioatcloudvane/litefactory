-- Display-only health snapshot the daemon publishes for `daemon status`. Nothing
-- in the daemon reads these tables to make a decision; they are replaced whole
-- whenever a value changes and rebuilt at every start.
--
-- daemon_listener_status holds at most one row: where the webhook listener is
-- bound, and when this daemon started (the "since start" the per-repository
-- last-delivery column is measured from).
CREATE TABLE daemon_listener_status (
    id                INTEGER PRIMARY KEY CHECK (id = 1),
    listen_address    TEXT NOT NULL,
    listening         INTEGER NOT NULL CHECK (listening IN (0, 1)),
    daemon_started_at TEXT NOT NULL,
    written_at        TEXT NOT NULL
);

-- repo_discovery_status holds one row per registered repository. NULL
-- last_delivery_at means no valid delivery since the daemon started.
CREATE TABLE repo_discovery_status (
    repo_id               INTEGER PRIMARY KEY REFERENCES repos (id) ON DELETE CASCADE,
    last_delivery_at      TEXT,
    last_poll_finished_at TEXT,
    next_poll_due_at      TEXT,
    poll_owed             INTEGER NOT NULL CHECK (poll_owed IN (0, 1)),
    secret_present        INTEGER NOT NULL CHECK (secret_present IN (0, 1)),
    webhook_mode          TEXT NOT NULL,
    written_at            TEXT NOT NULL
);
