-- The Plan parallel fast track is retired: every story takes the normal
-- per-story path, so its private member runs are no longer kept. The table had
-- no doctor_hold triggers; its index goes with it.
DROP INDEX IF EXISTS plan_group_members_parent;
DROP TABLE IF EXISTS plan_group_members;
