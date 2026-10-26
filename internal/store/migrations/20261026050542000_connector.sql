CREATE TABLE connector_owner (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1), owner_id INTEGER NOT NULL, generation INTEGER NOT NULL CHECK(generation>0)
);
INSERT INTO connector_owner VALUES(1,COALESCE((SELECT user_id FROM identities WHERE forge='github' AND role='owner'),0),1);
CREATE TABLE connector_clients (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, auth_method TEXT NOT NULL,
 secret_digest TEXT NOT NULL, redirects TEXT NOT NULL, created_at TEXT NOT NULL, expires_at TEXT NOT NULL
);
CREATE INDEX connector_clients_expiry ON connector_clients(expires_at);
CREATE TABLE connector_browsers (
 id TEXT PRIMARY KEY, client_id TEXT NOT NULL REFERENCES connector_clients(id),
 redirect_uri TEXT NOT NULL, state TEXT NOT NULL, resource TEXT NOT NULL, scope TEXT NOT NULL,
 pkce_challenge TEXT NOT NULL, nonce_digest TEXT NOT NULL, upstream_digest TEXT NOT NULL,
 generation INTEGER NOT NULL, verified INTEGER NOT NULL DEFAULT 0, expires_at TEXT NOT NULL
);
CREATE INDEX connector_browsers_expiry ON connector_browsers(expires_at);
CREATE TABLE connector_grants (
 id TEXT PRIMARY KEY, client_id TEXT NOT NULL REFERENCES connector_clients(id),
 resource TEXT NOT NULL, scope TEXT NOT NULL, owner_id INTEGER NOT NULL, generation INTEGER NOT NULL,
 created_at TEXT NOT NULL, expires_at TEXT NOT NULL, revoked_at TEXT, reason TEXT,
 coverage_version INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX connector_grants_expiry ON connector_grants(expires_at);
CREATE TABLE connector_codes (
 digest TEXT PRIMARY KEY, grant_id TEXT NOT NULL REFERENCES connector_grants(id) ON DELETE CASCADE,
 client_id TEXT NOT NULL, redirect_uri TEXT NOT NULL, resource TEXT NOT NULL, scope TEXT NOT NULL,
 pkce_challenge TEXT NOT NULL, expires_at TEXT NOT NULL, consumed_at TEXT
);
CREATE INDEX connector_codes_expiry ON connector_codes(expires_at);
CREATE TABLE connector_access (
 digest TEXT PRIMARY KEY, grant_id TEXT NOT NULL REFERENCES connector_grants(id) ON DELETE CASCADE,
 expires_at TEXT NOT NULL, revoked_at TEXT
);
CREATE INDEX connector_access_expiry ON connector_access(expires_at);
CREATE TABLE connector_refresh (
 digest TEXT PRIMARY KEY, grant_id TEXT NOT NULL REFERENCES connector_grants(id) ON DELETE CASCADE,
 parent_digest TEXT REFERENCES connector_refresh(digest), successor_digest TEXT,
 consumed_at TEXT, expires_at TEXT NOT NULL
);
CREATE INDEX connector_refresh_expiry ON connector_refresh(expires_at);
CREATE INDEX connector_refresh_grant ON connector_refresh(grant_id);
CREATE TRIGGER connector_owner_insert AFTER INSERT ON identities
WHEN NEW.forge='github' AND NEW.role='owner' AND COALESCE(NEW.user_id,0)<>(SELECT owner_id FROM connector_owner WHERE singleton=1)
BEGIN
 UPDATE connector_owner SET owner_id=COALESCE(NEW.user_id,0), generation=generation+1 WHERE singleton=1;
 UPDATE connector_grants SET revoked_at=COALESCE(revoked_at,NEW.derived_at),reason=COALESCE(reason,'owner_changed');
 DELETE FROM connector_browsers;
 DELETE FROM connector_codes;
END;
CREATE TRIGGER connector_owner_update AFTER UPDATE OF user_id ON identities
WHEN NEW.forge='github' AND NEW.role='owner' AND COALESCE(NEW.user_id,0)<>(SELECT owner_id FROM connector_owner WHERE singleton=1)
BEGIN
 UPDATE connector_owner SET owner_id=COALESCE(NEW.user_id,0), generation=generation+1 WHERE singleton=1;
 UPDATE connector_grants SET revoked_at=COALESCE(revoked_at,NEW.derived_at),reason=COALESCE(reason,'owner_changed');
 DELETE FROM connector_browsers;
 DELETE FROM connector_codes;
END;
CREATE TRIGGER connector_owner_delete AFTER DELETE ON identities
WHEN OLD.forge='github' AND OLD.role='owner'
BEGIN
 UPDATE connector_owner SET owner_id=0,generation=generation+1 WHERE singleton=1;
 UPDATE connector_grants SET revoked_at=COALESCE(revoked_at,OLD.derived_at),reason=COALESCE(reason,'owner_changed');
 DELETE FROM connector_browsers;
 DELETE FROM connector_codes;
END;
