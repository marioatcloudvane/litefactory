-- Doctor history is independent of workflow sessions, including their cleanup.
CREATE TABLE doctor_conversations (
 id INTEGER PRIMARY KEY,
 task_ref TEXT NOT NULL,
 generation INTEGER NOT NULL CHECK(generation > 0),
 state TEXT NOT NULL CHECK(state IN ('active','stopped')),
 revision INTEGER NOT NULL DEFAULT 0,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 UNIQUE(task_ref,generation)
);
CREATE UNIQUE INDEX doctor_active ON doctor_conversations(task_ref) WHERE state='active';
CREATE TABLE doctor_attempts (
 id TEXT PRIMARY KEY,
 conversation_id INTEGER NOT NULL REFERENCES doctor_conversations(id),
 source_id TEXT NOT NULL UNIQUE,
 owner_text TEXT NOT NULL,
 provider TEXT NOT NULL DEFAULT '',
 configured_model TEXT NOT NULL DEFAULT '',
 observed_model TEXT NOT NULL DEFAULT '',
 prompt_version TEXT NOT NULL,
 guide_version TEXT NOT NULL,
 evidence TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL CHECK(state IN ('running','failed','interrupted','completed')),
 failure TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 finished_at TEXT
);
CREATE UNIQUE INDEX doctor_writer ON doctor_attempts(conversation_id) WHERE state='running';

CREATE TRIGGER doctor_conversations_no_delete BEFORE DELETE ON doctor_conversations BEGIN SELECT RAISE(ABORT,'retained doctor history'); END;
CREATE TRIGGER doctor_attempts_no_delete BEFORE DELETE ON doctor_attempts BEGIN SELECT RAISE(ABORT,'retained doctor attempt'); END;
CREATE TRIGGER doctor_conversation_identity_immutable BEFORE UPDATE ON doctor_conversations
 WHEN NEW.task_ref<>OLD.task_ref OR NEW.generation<>OLD.generation OR (OLD.state='stopped' AND NEW.state<>'stopped')
 BEGIN SELECT RAISE(ABORT,'immutable doctor conversation identity'); END;
CREATE TRIGGER doctor_attempt_identity_immutable BEFORE UPDATE ON doctor_attempts
 WHEN NEW.id<>OLD.id OR NEW.conversation_id<>OLD.conversation_id OR NEW.source_id<>OLD.source_id OR NEW.owner_text<>OLD.owner_text OR NEW.prompt_version<>OLD.prompt_version OR NEW.guide_version<>OLD.guide_version OR (OLD.evidence<>'' AND NEW.evidence<>OLD.evidence)
 BEGIN SELECT RAISE(ABORT,'immutable doctor attempt input'); END;
