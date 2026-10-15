ALTER TABLE judge_requests ADD COLUMN caller_kind TEXT NOT NULL DEFAULT 'manual' CHECK(caller_kind IN ('manual','workflow'));
ALTER TABLE judge_requests ADD COLUMN graph_instance_id INTEGER REFERENCES graph_instances(id);
ALTER TABLE judge_requests ADD COLUMN judge_node_attempt_id INTEGER REFERENCES graph_node_attempts(id);
ALTER TABLE judge_requests ADD COLUMN source_node TEXT NOT NULL DEFAULT '';
ALTER TABLE judge_requests ADD COLUMN source_node_attempt_id INTEGER REFERENCES graph_node_attempts(id);
CREATE UNIQUE INDEX workflow_judge_request ON judge_requests(judge_node_attempt_id) WHERE caller_kind='workflow';
PRAGMA defer_foreign_keys=ON;
CREATE TABLE graph_comments_before_advisory AS SELECT * FROM graph_comment_intents;
DROP TABLE graph_comment_intents;
CREATE TABLE graph_comment_intents (
 delivery_key TEXT PRIMARY KEY,
 instance_id INTEGER NOT NULL REFERENCES graph_instances(id),
 source_attempt_id INTEGER REFERENCES graph_node_attempts(id),
 node TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('question','review','final','failure','advisory')),
 candidate BLOB NOT NULL CHECK(length(candidate) BETWEEN 1 AND 1048576),
 stop_origin TEXT,
 stop_cause TEXT,
 stop_reason TEXT,
 created_at TEXT,
 CHECK(stop_origin IS NULL OR (kind='question' AND length(stop_origin)>0 AND length(stop_cause)>0 AND length(stop_reason)>0)),
 CHECK(source_attempt_id IS NOT NULL OR stop_origin IS NOT NULL)
);
INSERT INTO graph_comment_intents SELECT * FROM graph_comments_before_advisory;
DROP TABLE graph_comments_before_advisory;
CREATE TRIGGER doctor_hold_graph_comment_intents_insert BEFORE INSERT ON graph_comment_intents WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM graph_instances dg JOIN sessions ds ON ds.id=dg.session_id WHERE dg.id=NEW.instance_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;
CREATE TRIGGER doctor_hold_graph_comment_intents_update BEFORE UPDATE ON graph_comment_intents WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM graph_instances dg JOIN sessions ds ON ds.id=dg.session_id WHERE dg.id=OLD.instance_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) OR EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM graph_instances dg JOIN sessions ds ON ds.id=dg.session_id WHERE dg.id=NEW.instance_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;
CREATE TRIGGER doctor_hold_graph_comment_intents_delete BEFORE DELETE ON graph_comment_intents WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM graph_instances dg JOIN sessions ds ON ds.id=dg.session_id WHERE dg.id=OLD.instance_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;

CREATE TRIGGER workflow_judge_binding_insert BEFORE INSERT ON judge_requests
 WHEN (NEW.caller_kind='workflow' AND NOT EXISTS(
 SELECT 1 FROM graph_node_attempts j JOIN graph_instances i ON i.id=j.instance_id JOIN graph_node_attempts s ON s.instance_id=i.id
 WHERE i.id=NEW.graph_instance_id AND i.session_id=NEW.session_id AND j.id=NEW.judge_node_attempt_id AND j.status='running'
 AND s.id=NEW.source_node_attempt_id AND s.node=NEW.source_node AND s.status='succeeded'))
 OR (NEW.caller_kind='manual' AND (NEW.graph_instance_id IS NOT NULL OR NEW.judge_node_attempt_id IS NOT NULL OR NEW.source_node_attempt_id IS NOT NULL OR NEW.source_node<>''))
 BEGIN SELECT RAISE(ABORT,'workflow judge binding mismatch'); END;
CREATE TRIGGER workflow_judge_binding_immutable BEFORE UPDATE ON judge_requests
 WHEN OLD.caller_kind<>NEW.caller_kind OR OLD.graph_instance_id IS NOT NEW.graph_instance_id
 OR OLD.judge_node_attempt_id IS NOT NEW.judge_node_attempt_id OR OLD.source_node<>NEW.source_node
 OR OLD.source_node_attempt_id IS NOT NEW.source_node_attempt_id OR OLD.source_json<>NEW.source_json
 OR OLD.configuration<>NEW.configuration OR OLD.evidence<>NEW.evidence
 BEGIN SELECT RAISE(ABORT,'immutable judge request binding'); END;
