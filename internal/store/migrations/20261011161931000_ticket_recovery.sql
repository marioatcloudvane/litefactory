CREATE TABLE ticket_subscriptions (
 id TEXT PRIMARY KEY, repo_id INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
 connection_version_id INTEGER NOT NULL REFERENCES project_connection_versions(id),
 revision INTEGER NOT NULL CHECK(revision > 0), provider TEXT NOT NULL, api_origin TEXT NOT NULL,
 container_id TEXT NOT NULL, remote_hook_id TEXT NOT NULL DEFAULT '', document TEXT NOT NULL,
 UNIQUE(connection_version_id)
);
CREATE UNIQUE INDEX ticket_subscription_remote_owner ON ticket_subscriptions(provider,api_origin,container_id,remote_hook_id) WHERE remote_hook_id <> '';
CREATE TABLE ticket_subscription_operations (
 subscription_id TEXT NOT NULL REFERENCES ticket_subscriptions(id) ON DELETE CASCADE,
 revision INTEGER NOT NULL, state TEXT NOT NULL, document TEXT NOT NULL,
 PRIMARY KEY(subscription_id,revision)
);
CREATE TABLE ticket_recovery_positions (
 connection_version_id INTEGER PRIMARY KEY REFERENCES project_connection_versions(id),
 revision INTEGER NOT NULL, document TEXT NOT NULL
);
CREATE TABLE pending_ticket_observations (
 connection_version_id INTEGER NOT NULL REFERENCES project_connection_versions(id),
 ticket_id TEXT NOT NULL, generation INTEGER NOT NULL, document TEXT NOT NULL,
 PRIMARY KEY(connection_version_id,ticket_id)
);

CREATE TABLE ticket_observation_generations (connection_version_id INTEGER NOT NULL REFERENCES project_connection_versions(id),ticket_id TEXT NOT NULL,generation INTEGER NOT NULL,PRIMARY KEY(connection_version_id,ticket_id));
