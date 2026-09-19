CREATE TABLE research_execution_paths (
 session_id INTEGER PRIMARY KEY REFERENCES sessions(id),
 path TEXT NOT NULL CHECK(path IN ('legacy','graph'))
);
-- Backfill existing legacy work once; later changes of defaults never migrate it.
INSERT INTO research_execution_paths(session_id,path)
 SELECT DISTINCT session_id,'legacy' FROM runs WHERE mode='research'
 AND session_id NOT IN (SELECT i.session_id FROM graph_instances i JOIN graph_definitions d ON d.id=i.definition_id WHERE d.mode='research');
INSERT INTO research_execution_paths(session_id,path)
 SELECT DISTINCT i.session_id,'graph' FROM graph_instances i JOIN graph_definitions d ON d.id=i.definition_id WHERE d.mode='research';
CREATE TABLE research_launch_definitions (
 id INTEGER PRIMARY KEY,
 session_id INTEGER NOT NULL REFERENCES sessions(id),
 trigger_id INTEGER NOT NULL,
 instance_id INTEGER NOT NULL REFERENCES graph_instances(id),
 content_digest TEXT NOT NULL,
 topology_digest TEXT NOT NULL,
 files BLOB NOT NULL,
 UNIQUE(session_id,trigger_id)
);
CREATE TABLE research_successors (
 instance_id INTEGER PRIMARY KEY REFERENCES graph_instances(id),
 predecessor_id INTEGER NOT NULL REFERENCES graph_instances(id),
 trigger_id INTEGER NOT NULL,
 reply_body TEXT NOT NULL CHECK(length(CAST(reply_body AS BLOB))<=65536)
);
