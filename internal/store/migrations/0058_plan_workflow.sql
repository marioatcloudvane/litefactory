-- Plan selection is independent of content/submission/engineering workflow versions
-- and independent of the Research route chosen by the same session.
CREATE TABLE plan_execution_paths (
 session_id INTEGER PRIMARY KEY REFERENCES sessions(id),
 path TEXT NOT NULL CHECK(path IN ('legacy','graph'))
);
INSERT INTO plan_execution_paths(session_id,path)
 SELECT DISTINCT session_id,'legacy' FROM runs WHERE mode='plan';
CREATE TABLE plan_workflow_states (
 parent TEXT PRIMARY KEY REFERENCES sessions(task_ref),
 revision INTEGER NOT NULL CHECK(revision>0),
 state_json TEXT NOT NULL CHECK(length(CAST(state_json AS BLOB))<=4194304)
);
CREATE TABLE plan_workflow_history (
 parent TEXT NOT NULL,
 revision INTEGER NOT NULL,
 state_json TEXT NOT NULL,
 PRIMARY KEY(parent,revision),
 FOREIGN KEY(parent) REFERENCES plan_workflow_states(parent)
);
CREATE TRIGGER plan_workflow_history_immutable BEFORE UPDATE ON plan_workflow_history BEGIN SELECT RAISE(ABORT,'immutable Plan workflow history'); END;
CREATE TRIGGER plan_workflow_history_no_delete BEFORE DELETE ON plan_workflow_history BEGIN SELECT RAISE(ABORT,'immutable Plan workflow history'); END;
