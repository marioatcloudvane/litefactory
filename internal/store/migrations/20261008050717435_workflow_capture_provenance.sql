ALTER TABLE research_launch_definitions ADD COLUMN provenance BLOB;
ALTER TABLE research_launch_definitions ADD COLUMN files_digest TEXT;
ALTER TABLE graph_node_attempts ADD COLUMN launch_id INTEGER REFERENCES research_launch_definitions(id);
ALTER TABLE graph_dispatches ADD COLUMN launch_id INTEGER REFERENCES research_launch_definitions(id);
UPDATE graph_node_attempts SET launch_id=(SELECT MIN(id) FROM research_launch_definitions WHERE instance_id=graph_node_attempts.instance_id HAVING COUNT(*)=1);
UPDATE graph_dispatches SET launch_id=(SELECT MIN(id) FROM research_launch_definitions WHERE instance_id=graph_dispatches.instance_id HAVING COUNT(*)=1);
