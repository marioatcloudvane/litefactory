-- Managed repository rights for explicitly tagged graph actions. Grants and
-- reviewer assignments are never deleted: closing one sets revoked_at or
-- removed_at once. Every change appends one rights_history row in the same
-- transaction; the history is insert-only.
CREATE TABLE rights_grants (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 repo_id INTEGER NOT NULL REFERENCES repos(id),
 subject_id INTEGER NOT NULL CHECK(subject_id>0),
 subject_login TEXT NOT NULL,
 workflow TEXT NOT NULL CHECK(workflow<>''),
 stage TEXT NOT NULL CHECK(stage<>''),
 action TEXT NOT NULL CHECK(action<>''),
 task_ref TEXT NOT NULL DEFAULT '',
 expires_at TEXT,
 granted_by INTEGER NOT NULL CHECK(granted_by>0),
 created_at TEXT NOT NULL,
 revoked_at TEXT,
 revoked_by INTEGER,
 CHECK(expires_at IS NULL OR expires_at>created_at),
 CHECK((revoked_at IS NULL)=(revoked_by IS NULL))
);
CREATE UNIQUE INDEX rights_grants_active ON rights_grants(repo_id,subject_id,workflow,stage,action,task_ref) WHERE revoked_at IS NULL;
CREATE INDEX rights_grants_subject ON rights_grants(repo_id,subject_id);
CREATE TRIGGER rights_grants_no_delete BEFORE DELETE ON rights_grants BEGIN SELECT RAISE(ABORT,'rights grants are never deleted'); END;
CREATE TRIGGER rights_grants_close_once BEFORE UPDATE ON rights_grants
 WHEN OLD.revoked_at IS NOT NULL OR NEW.revoked_at IS NULL OR NEW.id<>OLD.id OR NEW.repo_id<>OLD.repo_id OR NEW.subject_id<>OLD.subject_id
  OR NEW.subject_login<>OLD.subject_login OR NEW.workflow<>OLD.workflow OR NEW.stage<>OLD.stage OR NEW.action<>OLD.action
  OR NEW.task_ref<>OLD.task_ref OR NEW.expires_at IS NOT OLD.expires_at OR NEW.granted_by<>OLD.granted_by OR NEW.created_at<>OLD.created_at
 BEGIN SELECT RAISE(ABORT,'a rights grant may only be revoked once'); END;
CREATE TABLE reviewer_assignments (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 repo_id INTEGER NOT NULL REFERENCES repos(id),
 task_ref TEXT NOT NULL CHECK(task_ref<>''),
 workflow TEXT NOT NULL CHECK(workflow<>''),
 stage TEXT NOT NULL CHECK(stage<>''),
 reviewer_id INTEGER NOT NULL CHECK(reviewer_id>0),
 reviewer_login TEXT NOT NULL,
 assigned_by INTEGER NOT NULL CHECK(assigned_by>0),
 created_at TEXT NOT NULL,
 removed_at TEXT,
 removed_by INTEGER,
 CHECK((removed_at IS NULL)=(removed_by IS NULL))
);
CREATE UNIQUE INDEX reviewer_assignments_open ON reviewer_assignments(repo_id,task_ref,workflow,stage,reviewer_id) WHERE removed_at IS NULL;
CREATE TRIGGER reviewer_assignments_no_delete BEFORE DELETE ON reviewer_assignments BEGIN SELECT RAISE(ABORT,'reviewer assignments are never deleted'); END;
CREATE TRIGGER reviewer_assignments_close_once BEFORE UPDATE ON reviewer_assignments
 WHEN OLD.removed_at IS NOT NULL OR NEW.removed_at IS NULL OR NEW.id<>OLD.id OR NEW.repo_id<>OLD.repo_id OR NEW.task_ref<>OLD.task_ref
  OR NEW.workflow<>OLD.workflow OR NEW.stage<>OLD.stage OR NEW.reviewer_id<>OLD.reviewer_id OR NEW.reviewer_login<>OLD.reviewer_login
  OR NEW.assigned_by<>OLD.assigned_by OR NEW.created_at<>OLD.created_at
 BEGIN SELECT RAISE(ABORT,'a reviewer assignment may only be removed once'); END;
CREATE TABLE rights_history (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT,
 repo_id INTEGER NOT NULL REFERENCES repos(id),
 at TEXT NOT NULL,
 actor_id INTEGER NOT NULL CHECK(actor_id>0),
 kind TEXT NOT NULL CHECK(kind IN ('grant','revoke','assign','remove')),
 record_id INTEGER NOT NULL,
 snapshot_json TEXT NOT NULL
);
CREATE INDEX rights_history_repo ON rights_history(repo_id,sequence);
CREATE TRIGGER rights_history_immutable BEFORE UPDATE ON rights_history BEGIN SELECT RAISE(ABORT,'immutable rights history'); END;
CREATE TRIGGER rights_history_no_delete BEFORE DELETE ON rights_history BEGIN SELECT RAISE(ABORT,'immutable rights history'); END;
