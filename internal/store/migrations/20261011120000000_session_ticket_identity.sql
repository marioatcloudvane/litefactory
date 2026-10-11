-- Companion identity metadata: historical sessions and effect bytes remain
-- untouched. An unresolved legacy binding records the trusted origin before
-- remote resolution; a verified immutable ID can then be added exactly once.
CREATE TABLE session_ticket_identities (
    session_id INTEGER PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    provider TEXT NOT NULL CHECK(provider <> ''),
    api_origin TEXT NOT NULL CHECK(api_origin <> ''),
    container TEXT NOT NULL CHECK(container <> ''),
    locator_key TEXT NOT NULL CHECK(locator_key <> ''),
    remote_id TEXT NOT NULL DEFAULT '',
    task_ref TEXT NOT NULL CHECK(task_ref <> ''),
    UNIQUE(provider, api_origin, task_ref),
    UNIQUE(provider, api_origin, container, locator_key)
);
CREATE UNIQUE INDEX session_ticket_remote_identity
ON session_ticket_identities(provider, api_origin, remote_id)
WHERE remote_id <> '';
