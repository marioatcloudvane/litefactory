-- Preserve approved scope and retained backups while adding terminal typed states.
PRAGMA defer_foreign_keys=ON;
CREATE TEMP TABLE doctor_repairs_saved AS SELECT * FROM doctor_repairs;
DROP TRIGGER doctor_repair_immutable;
DROP TRIGGER doctor_repair_no_delete;
DROP TABLE doctor_repairs;
CREATE TABLE doctor_repairs (
 id TEXT PRIMARY KEY,
 source_id TEXT NOT NULL UNIQUE,
 principal TEXT NOT NULL,
 task_ref TEXT NOT NULL,
 conversation_id INTEGER NOT NULL REFERENCES doctor_conversations(id),
 generation INTEGER NOT NULL,
 proposal_id INTEGER NOT NULL UNIQUE REFERENCES doctor_proposals(id),
 proposal_digest TEXT NOT NULL,
 presentation_id TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('reserved','quiescing','backed_up','applying','verifying','needs_review','verified','restart_pending','recovered','stopped','completed','failed')),
 revision INTEGER NOT NULL DEFAULT 1,
 fenced INTEGER NOT NULL DEFAULT 0 CHECK(fenced IN(0,1)),
 exclusion_id TEXT NOT NULL DEFAULT '',
 failure TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX doctor_repair_active ON doctor_repairs(task_ref) WHERE state NOT IN ('recovered','stopped','completed','failed');
INSERT INTO doctor_repairs SELECT * FROM doctor_repairs_saved;
DROP TABLE doctor_repairs_saved;
CREATE TRIGGER doctor_repair_immutable BEFORE UPDATE ON doctor_repairs WHEN NEW.id<>OLD.id OR NEW.source_id<>OLD.source_id OR NEW.principal<>OLD.principal OR NEW.task_ref<>OLD.task_ref OR NEW.conversation_id<>OLD.conversation_id OR NEW.generation<>OLD.generation OR NEW.proposal_id<>OLD.proposal_id OR NEW.proposal_digest<>OLD.proposal_digest OR NEW.presentation_id<>OLD.presentation_id BEGIN SELECT RAISE(ABORT,'immutable doctor repair approval'); END;
CREATE TRIGGER doctor_repair_no_delete BEFORE DELETE ON doctor_repairs BEGIN SELECT RAISE(ABORT,'retained doctor repair history'); END;
