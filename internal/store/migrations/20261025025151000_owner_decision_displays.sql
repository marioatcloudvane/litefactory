CREATE TABLE owner_decision_displays (
 id INTEGER PRIMARY KEY, task_ref TEXT NOT NULL, action TEXT NOT NULL,
 reference TEXT NOT NULL, target TEXT NOT NULL, revision TEXT NOT NULL,
 instance_id INTEGER NOT NULL DEFAULT 0, publication_id INTEGER NOT NULL,
 tested_commit TEXT NOT NULL DEFAULT '', delivery_commit TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL, resolved INTEGER NOT NULL DEFAULT 0, requires_reference INTEGER NOT NULL DEFAULT 0,
 UNIQUE(task_ref,action,target,revision,publication_id)
);
CREATE INDEX owner_decision_displays_lookup ON owner_decision_displays(task_ref,action,id);
CREATE TABLE owner_decision_bindings (
 task_ref TEXT NOT NULL, comment_id INTEGER NOT NULL, author_id INTEGER NOT NULL,
 body TEXT NOT NULL, display_id INTEGER NOT NULL REFERENCES owner_decision_displays(id),
 normalized TEXT NOT NULL, created_at TEXT NOT NULL,
 PRIMARY KEY(task_ref,comment_id)
);
CREATE INDEX owner_decision_displays_reference ON owner_decision_displays(reference);
