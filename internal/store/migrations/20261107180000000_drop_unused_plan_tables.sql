-- Plan rework 1 deletes features that never ran: stage decisions from chat,
-- owner edits read back from story issues, and story contract upgrades. Their
-- tables hold no rows. Dropping a table also drops its indexes and its own
-- triggers (doctor_hold_* and immutability guards); the contract upgrade
-- guards live on other tables and are dropped by name first.
DROP TRIGGER IF EXISTS story_contract_upgrade_run_admission;
DROP TRIGGER IF EXISTS story_contract_upgrade_feature_guard;
DROP TRIGGER IF EXISTS story_contract_upgrade_story_guard;
DROP TABLE IF EXISTS story_contract_upgrade_items;
DROP TABLE IF EXISTS story_contract_upgrades;
DROP TABLE IF EXISTS plan_story_owner_edits;
DROP TABLE IF EXISTS stage_decision_receipts;
DROP TABLE IF EXISTS stage_decision_deliveries;
