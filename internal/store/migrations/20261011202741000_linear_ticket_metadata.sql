-- Linear configuration and local compatibility keys are additive: existing
-- GitHub identities, session names and prepared publication bytes stay intact.
ALTER TABLE project_connection_versions ADD COLUMN linear_configuration TEXT NOT NULL DEFAULT '{}';
ALTER TABLE session_execution_snapshots ADD COLUMN code_actor_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE session_execution_snapshots ADD COLUMN code_actor_login TEXT NOT NULL DEFAULT '';
CREATE TABLE ticket_local_slots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    kind TEXT NOT NULL CHECK(kind IN ('issue','comment','actor')),
    provider TEXT NOT NULL, api_origin TEXT NOT NULL, remote_id TEXT NOT NULL,
    UNIQUE(kind,provider,api_origin,remote_id)
);
-- This range keeps local compatibility slots separate from existing numeric
-- GitHub identifiers. A slot is never sent to a provider as a remote identity.
INSERT INTO sqlite_sequence(name,seq) VALUES('ticket_local_slots',4503599627370496);
CREATE TABLE ticket_effect_preparations (
    effect_key TEXT PRIMARY KEY REFERENCES ticket_effect_identities(effect_key),
    creation_uuid TEXT NOT NULL DEFAULT '', representation TEXT NOT NULL,
    authored_content TEXT NOT NULL, observed_content TEXT NOT NULL DEFAULT '',
    observed INTEGER NOT NULL DEFAULT 0 CHECK(observed IN (0,1))
);
CREATE UNIQUE INDEX ticket_creation_uuid ON ticket_effect_preparations(creation_uuid) WHERE creation_uuid <> '';
