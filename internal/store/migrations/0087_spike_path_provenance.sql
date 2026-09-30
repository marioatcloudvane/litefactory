-- Historical Spike sessions keep the path they actually ran on. Legacy work is
-- backfilled once and graph work keeps graph; a later change of the default
-- never reinterprets either. Sessions with an existing path row are untouched.
INSERT INTO research_execution_paths(session_id,path)
 SELECT DISTINCT session_id,'legacy' FROM runs WHERE mode='spike'
 AND session_id NOT IN (SELECT i.session_id FROM graph_instances i JOIN graph_definitions d ON d.id=i.definition_id WHERE d.mode='spike')
 AND session_id NOT IN (SELECT session_id FROM research_execution_paths);
INSERT INTO research_execution_paths(session_id,path)
 SELECT DISTINCT i.session_id,'graph' FROM graph_instances i JOIN graph_definitions d ON d.id=i.definition_id WHERE d.mode='spike'
 AND i.session_id NOT IN (SELECT session_id FROM research_execution_paths);
