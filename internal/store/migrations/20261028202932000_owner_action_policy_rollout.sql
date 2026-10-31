-- Retain why legacy pending work stopped, independently of chat cleanup.
CREATE TABLE owner_action_policy_invalidations (
 transport TEXT NOT NULL,
 owner_id TEXT NOT NULL,
 channel_ref TEXT NOT NULL,
 proposal_ref TEXT NOT NULL,
 reason TEXT NOT NULL,
 invalidated_at TEXT NOT NULL,
 PRIMARY KEY(transport,owner_id,channel_ref,proposal_ref)
);
INSERT INTO owner_action_policy_invalidations
 SELECT transport,owner_id,channel_ref,code,'policy_changed_repropose',strftime('%Y-%m-%dT%H:%M:%fZ','now')
 FROM chat_natural_dialogues WHERE state IN ('awaiting_confirmation','clarifying');
UPDATE chat_natural_dialogues SET state='invalidated',payload='{}',digest='',code='',prompt_id=''
 WHERE state IN ('awaiting_confirmation','clarifying');
UPDATE owner_action_proposals SET state='invalidated',reason='policy_changed_repropose',completed_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
 WHERE policy_version<1 AND state IN ('awaiting_confirmation','awaiting_discord');
-- An older daemon cannot revive a migrated proposal by overwriting its state.
CREATE TRIGGER legacy_action_policy_fence BEFORE UPDATE OF state ON chat_natural_dialogues
WHEN OLD.state='invalidated' AND NEW.state IN ('awaiting_confirmation','clarifying','authorized')
BEGIN SELECT RAISE(ABORT,'action policy changed; request a fresh proposal'); END;
