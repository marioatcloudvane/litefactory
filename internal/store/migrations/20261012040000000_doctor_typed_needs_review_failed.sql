-- Settle typed repairs that a stop or refusal fenced as needs_review. Typed
-- repairs are terminal when fenced (failed), matching typed recovery, so they
-- no longer occupy the task's active-repair slot. Legacy file repairs keep
-- their needs_review state.
UPDATE doctor_repair_holds SET active=0 WHERE repair_id IN (
 SELECT r.id FROM doctor_repairs r WHERE r.state='needs_review'
 AND EXISTS(SELECT 1 FROM doctor_repair_steps s WHERE s.repair_id=r.id)
 AND NOT EXISTS(SELECT 1 FROM doctor_repair_steps s WHERE s.repair_id=r.id AND s.kind NOT IN ('cancel_schedule','consume_comment','park_story','release_instance','requeue_schedule','resume_story','stop_run','sync_branch'))
);
UPDATE doctor_repairs SET state='failed',fenced=1,revision=revision+1 WHERE state='needs_review'
 AND EXISTS(SELECT 1 FROM doctor_repair_steps s WHERE s.repair_id=doctor_repairs.id)
 AND NOT EXISTS(SELECT 1 FROM doctor_repair_steps s WHERE s.repair_id=doctor_repairs.id AND s.kind NOT IN ('cancel_schedule','consume_comment','park_story','release_instance','requeue_schedule','resume_story','stop_run','sync_branch'));
