-- Retire selection in place so sessions and graph history keep their references.
ALTER TABLE repos DROP COLUMN aiteam_team;
ALTER TABLE repos DROP COLUMN aiteam_verified_at;
ALTER TABLE repos DROP COLUMN external_team;
