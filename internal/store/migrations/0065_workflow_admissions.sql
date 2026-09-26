CREATE TABLE workflow_admissions (
 session_id INTEGER PRIMARY KEY REFERENCES sessions(id),
 version INTEGER NOT NULL CHECK(version = 1),
 family TEXT NOT NULL CHECK(family IN ('mode','standalone')),
 workflow TEXT NOT NULL CHECK(workflow IN ('research','plan','implement','validate','spike','bug')),
 source_trigger INTEGER NOT NULL CHECK(source_trigger >= 0),
 CHECK((family = 'standalone' AND workflow = 'bug') OR (family = 'mode' AND workflow <> 'bug'))
);

ALTER TABLE graph_definitions ADD COLUMN workflow TEXT NOT NULL DEFAULT '';
