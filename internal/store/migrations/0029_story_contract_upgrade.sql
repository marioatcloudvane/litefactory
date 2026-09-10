-- Upgrade publication is an explicit, revision-bound owner operation. Pending
-- intent blocks daemon admissions without holding a transaction across forge IO.
CREATE TABLE story_contract_upgrades (
 parent_task TEXT PRIMARY KEY REFERENCES managed_features(parent_task),
 source_generation INTEGER NOT NULL,
 target_generation INTEGER NOT NULL,
 scope_hash TEXT NOT NULL,
 owner_id INTEGER NOT NULL CHECK(owner_id>0),
 state TEXT NOT NULL CHECK(state IN ('pending','completed')),
 payload BLOB NOT NULL
);
CREATE TABLE story_contract_upgrade_items (
 parent_task TEXT NOT NULL REFERENCES story_contract_upgrades(parent_task),
 story_key TEXT NOT NULL,
 confirmed INTEGER NOT NULL DEFAULT 0 CHECK(confirmed IN (0,1)),
 PRIMARY KEY(parent_task,story_key)
);
CREATE TRIGGER story_contract_upgrade_run_admission BEFORE INSERT ON runs
WHEN EXISTS(SELECT 1 FROM sessions s JOIN story_contract_upgrades u ON u.parent_task=s.task_ref WHERE s.id=NEW.session_id AND u.state='pending')
BEGIN SELECT RAISE(ABORT,'story contract upgrade is pending; resume explicit apply before starting a run'); END;
CREATE TRIGGER story_contract_upgrade_feature_guard BEFORE UPDATE ON managed_features
WHEN EXISTS(SELECT 1 FROM story_contract_upgrades u WHERE u.parent_task=OLD.parent_task AND u.state='pending')
BEGIN SELECT RAISE(ABORT,'story contract upgrade is pending; feature state remains locked'); END;
CREATE TRIGGER story_contract_upgrade_story_guard BEFORE UPDATE ON managed_stories
WHEN EXISTS(SELECT 1 FROM story_contract_upgrades u WHERE u.parent_task=OLD.parent_task AND u.state='pending')
BEGIN SELECT RAISE(ABORT,'story contract upgrade is pending; story scope remains locked'); END;
