-- Additive metadata for existing ledgers. Keys and prepared payloads remain
-- owned by their original tables; unresolved identities never permit dispatch.
CREATE TABLE ticket_effect_identities (
    effect_key TEXT PRIMARY KEY CHECK(effect_key <> ''),
    provider TEXT NOT NULL CHECK(provider <> ''),
    api_origin TEXT NOT NULL CHECK(api_origin <> ''),
    container TEXT NOT NULL CHECK(container <> ''),
    locator_key TEXT NOT NULL CHECK(locator_key <> ''),
    remote_id TEXT NOT NULL DEFAULT '',
    actor_provider TEXT NOT NULL DEFAULT '',
    actor_origin TEXT NOT NULL DEFAULT '',
    actor_id TEXT NOT NULL DEFAULT ''
);
