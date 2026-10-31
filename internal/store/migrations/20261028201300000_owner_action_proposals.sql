CREATE TABLE owner_action_proposals (
 id TEXT PRIMARY KEY,
 policy_version INTEGER NOT NULL,
 owner_id TEXT NOT NULL,
 owner_generation INTEGER NOT NULL,
 transport TEXT NOT NULL,
 client_id TEXT NOT NULL,
 grant_id TEXT NOT NULL,
 channel_id TEXT NOT NULL,
 discord_owner_id TEXT NOT NULL,
 request_key TEXT NOT NULL,
 request_digest TEXT NOT NULL,
 facts TEXT NOT NULL,
 payload TEXT NOT NULL,
 digest TEXT NOT NULL,
 repository_id INTEGER NOT NULL,
 repository_ref TEXT NOT NULL,
 scope TEXT NOT NULL,
 tier TEXT NOT NULL,
 state TEXT NOT NULL,
 reason TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 available_at TEXT NOT NULL DEFAULT '',
 confirm_not_before TEXT NOT NULL DEFAULT '',
 expires_at TEXT NOT NULL,
 completed_at TEXT NOT NULL DEFAULT '',
 execution_id TEXT NOT NULL UNIQUE,
 result TEXT NOT NULL DEFAULT '',
 proposal_message TEXT NOT NULL DEFAULT '',
 approval_channel TEXT NOT NULL DEFAULT '',
 approval_message TEXT NOT NULL DEFAULT '',
 delivery_state TEXT NOT NULL DEFAULT '',
 delivery_retry_at TEXT NOT NULL DEFAULT '',
 delivery_attempts INTEGER NOT NULL DEFAULT 0,
 UNIQUE(owner_id,owner_generation,transport,client_id,grant_id,request_key)
);
CREATE INDEX owner_action_history ON owner_action_proposals(owner_id,created_at,id);
CREATE INDEX owner_action_pending ON owner_action_proposals(state,expires_at);
CREATE UNIQUE INDEX owner_action_approval_binding ON owner_action_proposals(approval_channel,approval_message) WHERE approval_message<>'';
CREATE TRIGGER owner_action_facts_immutable BEFORE UPDATE OF policy_version,owner_id,owner_generation,transport,client_id,grant_id,channel_id,discord_owner_id,request_key,request_digest,facts,digest,repository_id,repository_ref,scope,tier,created_at,expires_at,execution_id ON owner_action_proposals
BEGIN SELECT RAISE(ABORT,'owner action facts are immutable'); END;
CREATE TABLE owner_action_attempts (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 proposal_id TEXT NOT NULL REFERENCES owner_action_proposals(id) ON DELETE CASCADE,
 received_at TEXT NOT NULL,
 method TEXT NOT NULL,
 principal TEXT NOT NULL,
 decision TEXT NOT NULL,
 state TEXT NOT NULL,
 evidence TEXT NOT NULL DEFAULT ''
);
CREATE INDEX owner_action_attempt_proposal ON owner_action_attempts(proposal_id,id);
-- Action coverage is independent of read coverage; no existing grant is upgraded.
CREATE TABLE connector_grant_action_repositories (
 grant_id TEXT NOT NULL REFERENCES connector_grants(id) ON DELETE CASCADE,
 repo_id INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
 repository_ref TEXT NOT NULL,
 PRIMARY KEY(grant_id,repo_id)
);
CREATE TRIGGER connector_action_repositories_immutable BEFORE UPDATE ON connector_grant_action_repositories
BEGIN SELECT RAISE(ABORT,'connector action coverage is immutable'); END;
CREATE TRIGGER owner_action_payload_immutable BEFORE UPDATE OF payload ON owner_action_proposals
WHEN NEW.payload<>OLD.payload AND NOT (NEW.payload='{}' AND NEW.state IN ('succeeded','failed','cancelled','expired','invalidated'))
BEGIN SELECT RAISE(ABORT,'owner action payload is immutable'); END;
