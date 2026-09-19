-- A provider invocation belongs to exactly one logical graph attempt. Existing
-- attempts remain legacy; no historical usage is reassigned or duplicated.
ALTER TABLE meter_attempts ADD COLUMN graph_node_attempt_id INTEGER REFERENCES graph_node_attempts(id);
CREATE INDEX idx_meter_graph_attempt ON meter_attempts(graph_node_attempt_id);
