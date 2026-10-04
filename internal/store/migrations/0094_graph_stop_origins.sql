-- Shared owner waits may originate before a subprocess can be allocated.
-- Preserve all historical values and doctor write holds while removing the
-- requirement to invent a successful source attempt for a factory stop.
PRAGMA defer_foreign_keys=ON;
CREATE TABLE graph_comments_before_stop_origins AS SELECT * FROM graph_comment_intents;
CREATE TABLE graph_waits_before_stop_origins AS SELECT * FROM graph_owner_waits;
DROP TABLE graph_owner_waits;
DROP TABLE graph_comment_intents;
CREATE TABLE graph_comment_intents (
 delivery_key TEXT PRIMARY KEY,
 instance_id INTEGER NOT NULL REFERENCES graph_instances(id),
 source_attempt_id INTEGER REFERENCES graph_node_attempts(id),
 node TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('question','review','final','failure')),
 candidate BLOB NOT NULL CHECK(length(candidate) BETWEEN 1 AND 1048576),
 stop_origin TEXT,
 stop_cause TEXT,
 stop_reason TEXT,
 created_at TEXT,
 CHECK(stop_origin IS NULL OR (kind='question' AND length(stop_origin)>0 AND length(stop_cause)>0 AND length(stop_reason)>0)),
 CHECK(source_attempt_id IS NOT NULL OR stop_origin IS NOT NULL)
);
INSERT INTO graph_comment_intents(delivery_key,instance_id,source_attempt_id,node,kind,candidate)
 SELECT delivery_key,instance_id,source_attempt_id,node,kind,candidate FROM graph_comments_before_stop_origins;
CREATE TABLE graph_owner_waits (
 delivery_key TEXT PRIMARY KEY REFERENCES graph_comment_intents(delivery_key),
 instance_id INTEGER NOT NULL REFERENCES graph_instances(id),
 source_attempt_id INTEGER REFERENCES graph_node_attempts(id),
 state TEXT NOT NULL CHECK(state IN ('waiting','claimed','allocated','cancelled')),
 reply_id INTEGER,
 reply_body TEXT,
 reply_author_id INTEGER,
 claimed_at TEXT,
 continuation_attempt_id INTEGER REFERENCES graph_node_attempts(id),
 origin_node TEXT,
 allow_runless INTEGER NOT NULL DEFAULT 0 CHECK(allow_runless IN (0,1)),
 CHECK(reply_body IS NULL OR length(CAST(reply_body AS BLOB)) <= 65536),
 CHECK(source_attempt_id IS NOT NULL OR (origin_node IS NOT NULL AND length(origin_node)>0)),
 UNIQUE(instance_id,reply_id)
);
INSERT INTO graph_owner_waits(delivery_key,instance_id,source_attempt_id,state,reply_id,reply_body,reply_author_id,claimed_at,continuation_attempt_id)
 SELECT delivery_key,instance_id,source_attempt_id,state,reply_id,reply_body,reply_author_id,claimed_at,continuation_attempt_id FROM graph_waits_before_stop_origins;
CREATE UNIQUE INDEX idx_graph_owner_wait_active ON graph_owner_waits(instance_id) WHERE state IN ('waiting','claimed');
DROP TABLE graph_waits_before_stop_origins;
DROP TABLE graph_comments_before_stop_origins;
CREATE TRIGGER doctor_hold_graph_comment_intents_insert BEFORE INSERT ON graph_comment_intents WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM graph_instances dg JOIN sessions ds ON ds.id=dg.session_id WHERE dg.id=NEW.instance_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;
CREATE TRIGGER doctor_hold_graph_comment_intents_update BEFORE UPDATE ON graph_comment_intents WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM graph_instances dg JOIN sessions ds ON ds.id=dg.session_id WHERE dg.id=OLD.instance_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) OR EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM graph_instances dg JOIN sessions ds ON ds.id=dg.session_id WHERE dg.id=NEW.instance_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;
CREATE TRIGGER doctor_hold_graph_comment_intents_delete BEFORE DELETE ON graph_comment_intents WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM graph_instances dg JOIN sessions ds ON ds.id=dg.session_id WHERE dg.id=OLD.instance_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;
CREATE TRIGGER doctor_hold_graph_owner_waits_insert BEFORE INSERT ON graph_owner_waits WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM graph_instances dg JOIN sessions ds ON ds.id=dg.session_id WHERE dg.id=NEW.instance_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;
CREATE TRIGGER doctor_hold_graph_owner_waits_update BEFORE UPDATE ON graph_owner_waits WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM graph_instances dg JOIN sessions ds ON ds.id=dg.session_id WHERE dg.id=OLD.instance_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) OR EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM graph_instances dg JOIN sessions ds ON ds.id=dg.session_id WHERE dg.id=NEW.instance_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;
CREATE TRIGGER doctor_hold_graph_owner_waits_delete BEFORE DELETE ON graph_owner_waits WHEN EXISTS(SELECT 1 FROM doctor_repair_holds h WHERE h.active=1 AND (EXISTS(SELECT 1 FROM graph_instances dg JOIN sessions ds ON ds.id=dg.session_id WHERE dg.id=OLD.instance_id AND EXISTS(SELECT 1 FROM doctor_held_session_owners dv WHERE dv.session_id=ds.id AND dv.repair_id=h.repair_id))) AND NOT EXISTS(SELECT 1 FROM doctor_repair_write_permits permit WHERE permit.repair_id=h.repair_id)) BEGIN SELECT RAISE(ABORT,'issue held for doctor repair'); END;
