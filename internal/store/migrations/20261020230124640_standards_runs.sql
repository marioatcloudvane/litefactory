CREATE TABLE standards_run_bindings (
 root_run_id TEXT PRIMARY KEY,
 binding_json TEXT NOT NULL
);
CREATE TABLE standards_run_selections (
 root_run_id TEXT NOT NULL REFERENCES standards_run_bindings(root_run_id),
 consumer_key TEXT NOT NULL,
 version INTEGER NOT NULL,
 selection_json TEXT NOT NULL,
 PRIMARY KEY(root_run_id,consumer_key,version)
);
CREATE TABLE standards_run_observations (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 root_run_id TEXT NOT NULL REFERENCES standards_run_bindings(root_run_id),
 consumer_key TEXT NOT NULL,
 observation_json TEXT NOT NULL
);

CREATE TABLE standards_run_aliases (run_id INTEGER PRIMARY KEY REFERENCES runs(id),root_run_id INTEGER NOT NULL);
CREATE TRIGGER standards_run_binding_no_update BEFORE UPDATE ON standards_run_bindings BEGIN SELECT RAISE(ABORT,'immutable run standards binding'); END;
CREATE TRIGGER standards_run_selection_no_update BEFORE UPDATE ON standards_run_selections BEGIN SELECT RAISE(ABORT,'immutable run standards selection'); END;
CREATE TRIGGER standards_run_observation_no_update BEFORE UPDATE ON standards_run_observations BEGIN SELECT RAISE(ABORT,'immutable run standards observation'); END;

CREATE TABLE standards_graph_run_aliases (run_id INTEGER PRIMARY KEY REFERENCES runs(id),launch_id INTEGER NOT NULL REFERENCES research_launch_definitions(id));
CREATE TABLE standards_graph_invocations (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 session_id INTEGER NOT NULL REFERENCES sessions(id),
 source_trigger INTEGER NOT NULL,
 root_run_id INTEGER NOT NULL,
 UNIQUE(session_id,source_trigger)
);
CREATE TABLE standards_story_invocations (
 parent TEXT NOT NULL,
 generation INTEGER NOT NULL,
 story_key TEXT NOT NULL,
 state_at TEXT NOT NULL,
 root_run_id INTEGER NOT NULL,
 PRIMARY KEY(parent,generation,story_key,state_at)
);
CREATE TRIGGER standards_run_binding_no_delete BEFORE DELETE ON standards_run_bindings WHEN EXISTS(SELECT 1 FROM runs WHERE CAST(id AS TEXT)=OLD.root_run_id) OR EXISTS(SELECT 1 FROM standards_run_aliases WHERE CAST(root_run_id AS TEXT)=OLD.root_run_id) OR EXISTS(SELECT 1 FROM standards_graph_invocations WHERE CAST(root_run_id AS TEXT)=OLD.root_run_id) OR EXISTS(SELECT 1 FROM standards_story_invocations WHERE CAST(root_run_id AS TEXT)=OLD.root_run_id) BEGIN SELECT RAISE(ABORT,'retained run standards binding'); END;
CREATE TRIGGER standards_run_selection_no_delete BEFORE DELETE ON standards_run_selections WHEN EXISTS(SELECT 1 FROM runs WHERE CAST(id AS TEXT)=OLD.root_run_id) OR EXISTS(SELECT 1 FROM standards_run_aliases WHERE CAST(root_run_id AS TEXT)=OLD.root_run_id) OR EXISTS(SELECT 1 FROM standards_graph_invocations WHERE CAST(root_run_id AS TEXT)=OLD.root_run_id) OR EXISTS(SELECT 1 FROM standards_story_invocations WHERE CAST(root_run_id AS TEXT)=OLD.root_run_id) BEGIN SELECT RAISE(ABORT,'retained run standards selection'); END;
CREATE TRIGGER standards_run_observation_no_delete BEFORE DELETE ON standards_run_observations WHEN EXISTS(SELECT 1 FROM runs WHERE CAST(id AS TEXT)=OLD.root_run_id) OR EXISTS(SELECT 1 FROM standards_run_aliases WHERE CAST(root_run_id AS TEXT)=OLD.root_run_id) OR EXISTS(SELECT 1 FROM standards_graph_invocations WHERE CAST(root_run_id AS TEXT)=OLD.root_run_id) OR EXISTS(SELECT 1 FROM standards_story_invocations WHERE CAST(root_run_id AS TEXT)=OLD.root_run_id) BEGIN SELECT RAISE(ABORT,'retained run standards observation'); END;
