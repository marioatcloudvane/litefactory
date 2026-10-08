-- Widen the kind constraint while retaining every immutable step and backup.
-- SQLite requires rebuilding the table to change a CHECK constraint. This is
-- one migration transaction; no retained values or legacy kinds are converted.
CREATE TEMP TABLE doctor_repair_steps_saved AS SELECT * FROM doctor_repair_steps;
DROP TRIGGER doctor_repair_step_immutable;
DROP TRIGGER doctor_repair_step_no_delete;
DROP TABLE doctor_repair_steps;
CREATE TABLE doctor_repair_steps (
 repair_id TEXT NOT NULL REFERENCES doctor_repairs(id),
 number INTEGER NOT NULL CHECK(number>0),
 kind TEXT NOT NULL CHECK(kind IN('poll_flag','session_file','worktree_file','run_interrupt','session_unlock','report_completion','watermark_rewind','graph_dispatch_cancel','graph_wait_cancel','graph_attempt_cancel','graph_fail','release_instance','resume_story','park_story','stop_run','requeue_schedule','cancel_schedule','consume_comment','sync_branch')),
 target TEXT NOT NULL,
 before_value TEXT NOT NULL,
 after_value TEXT NOT NULL,
 source_identity TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN('pending','backed_up','write_intent','verified','uncertain')),
 backup BLOB,
 backup_reference TEXT NOT NULL DEFAULT '',
 backup_digest TEXT NOT NULL DEFAULT '',
 observed_after TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(repair_id,number)
);
INSERT INTO doctor_repair_steps SELECT * FROM doctor_repair_steps_saved;
DROP TABLE doctor_repair_steps_saved;
CREATE TRIGGER doctor_repair_step_immutable BEFORE UPDATE ON doctor_repair_steps WHEN NEW.repair_id<>OLD.repair_id OR NEW.number<>OLD.number OR NEW.kind<>OLD.kind OR NEW.target<>OLD.target OR NEW.before_value<>OLD.before_value OR NEW.after_value<>OLD.after_value OR NEW.source_identity<>OLD.source_identity BEGIN SELECT RAISE(ABORT,'immutable doctor repair step'); END;
CREATE TRIGGER doctor_repair_step_no_delete BEFORE DELETE ON doctor_repair_steps BEGIN SELECT RAISE(ABORT,'retained doctor repair backup'); END;
