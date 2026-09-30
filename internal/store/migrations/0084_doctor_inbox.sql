-- Doctor routing is independent of workflow cursors and admission locks.
CREATE TABLE doctor_scans (
 repo_id INTEGER PRIMARY KEY REFERENCES repos(id),
 activated_at TEXT NOT NULL,
 completed_at TEXT NOT NULL,
 window_end TEXT NOT NULL,
 page INTEGER NOT NULL DEFAULT 1 CHECK(page>0)
);
CREATE TABLE doctor_inbox (
 id INTEGER PRIMARY KEY,
 repo_id INTEGER NOT NULL REFERENCES repos(id),
 task_ref TEXT NOT NULL,
 issue_id INTEGER NOT NULL CHECK(issue_id>0),
 issue_number INTEGER NOT NULL CHECK(issue_number>0),
 comment_id INTEGER NOT NULL CHECK(comment_id>0),
 actor_id INTEGER NOT NULL CHECK(actor_id>0),
 created_at TEXT NOT NULL,
 operation TEXT NOT NULL CHECK(operation IN ('diagnose','stop','apply','usage')),
 instructions TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('queued','claimed','completed','stopped')) DEFAULT 'queued',
 claim_owner TEXT NOT NULL DEFAULT '',
 attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 2),
 result_body TEXT NOT NULL DEFAULT '',
 conversation_id INTEGER REFERENCES doctor_conversations(id),
 proposal_id INTEGER REFERENCES doctor_proposals(id),
 UNIQUE(repo_id,comment_id)
);
CREATE INDEX doctor_inbox_queued ON doctor_inbox(state,id);
CREATE TRIGGER doctor_inbox_identity_immutable BEFORE UPDATE ON doctor_inbox
 WHEN NEW.repo_id<>OLD.repo_id OR NEW.task_ref<>OLD.task_ref OR NEW.issue_id<>OLD.issue_id OR NEW.issue_number<>OLD.issue_number OR NEW.comment_id<>OLD.comment_id OR NEW.actor_id<>OLD.actor_id OR NEW.created_at<>OLD.created_at OR NEW.operation<>OLD.operation OR NEW.instructions<>OLD.instructions
 BEGIN SELECT RAISE(ABORT,'immutable doctor input'); END;
CREATE TRIGGER doctor_inbox_no_delete BEFORE DELETE ON doctor_inbox BEGIN SELECT RAISE(ABORT,'retained doctor input'); END;
PRAGMA defer_foreign_keys=ON;
CREATE TABLE doctor_report_old AS SELECT * FROM report_deliveries;
DROP TABLE report_deliveries;
CREATE TABLE report_deliveries (
    delivery_key TEXT PRIMARY KEY,
    identity_version INTEGER NOT NULL CHECK (identity_version = 1),
    session_id INTEGER REFERENCES sessions(id) ON DELETE CASCADE,
    trigger_id INTEGER NOT NULL CHECK (trigger_id >= 0),
    run_id INTEGER REFERENCES runs(id) ON DELETE CASCADE,
    task TEXT NOT NULL,
    destination TEXT NOT NULL,
    trigger_namespace TEXT NOT NULL,
    purpose TEXT NOT NULL,
    bot_user_id INTEGER NOT NULL CHECK (bot_user_id > 0),
    body TEXT NOT NULL,
    marker TEXT NOT NULL,
    possibly_sent INTEGER NOT NULL DEFAULT 0 CHECK (possibly_sent IN (0,1)),
    comment_id INTEGER CHECK (comment_id > 0),
    completed INTEGER NOT NULL DEFAULT 0 CHECK (completed IN (0,1)),
    legacy INTEGER NOT NULL DEFAULT 0 CHECK (legacy IN (0,1)),
    doctor_command_id INTEGER REFERENCES doctor_inbox(id),
    doctor_probe_page INTEGER NOT NULL DEFAULT 1 CHECK(doctor_probe_page>0),
    CHECK (comment_id IS NULL OR possibly_sent = 1),
    CHECK (completed = 0 OR comment_id IS NOT NULL),
    CHECK (legacy = 0 OR possibly_sent = 1)
,
 CHECK((session_id IS NOT NULL AND doctor_command_id IS NULL) OR (session_id IS NULL AND run_id IS NULL AND doctor_command_id IS NOT NULL AND trigger_namespace='doctor'))
);
INSERT INTO report_deliveries (delivery_key,identity_version,session_id,trigger_id,run_id,task,destination,trigger_namespace,purpose,bot_user_id,body,marker,possibly_sent,comment_id,completed,legacy) SELECT delivery_key,identity_version,session_id,trigger_id,run_id,task,destination,trigger_namespace,purpose,bot_user_id,body,marker,possibly_sent,comment_id,completed,legacy FROM doctor_report_old;
DROP TABLE doctor_report_old;
CREATE INDEX report_deliveries_pending ON report_deliveries(session_id,trigger_id) WHERE completed=0;
CREATE INDEX doctor_deliveries_pending ON report_deliveries(doctor_command_id) WHERE completed=0 AND doctor_command_id IS NOT NULL;
CREATE TABLE doctor_presentations (
 command_id INTEGER NOT NULL REFERENCES doctor_inbox(id),
 proposal_id INTEGER REFERENCES doctor_proposals(id),
 entry_point TEXT NOT NULL CHECK(entry_point='github'),
 part INTEGER NOT NULL CHECK(part>0),
 total INTEGER NOT NULL CHECK(total>=part),
 delivery_key TEXT NOT NULL UNIQUE REFERENCES report_deliveries(delivery_key),
 PRIMARY KEY(command_id,part)
);

CREATE TRIGGER doctor_presentation_immutable BEFORE UPDATE ON doctor_presentations BEGIN SELECT RAISE(ABORT,'immutable doctor presentation'); END;
CREATE TRIGGER doctor_presentation_no_delete BEFORE DELETE ON doctor_presentations BEGIN SELECT RAISE(ABORT,'retained doctor presentation'); END;
