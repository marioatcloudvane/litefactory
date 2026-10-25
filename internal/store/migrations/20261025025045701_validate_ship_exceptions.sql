ALTER TABLE owner_exceptions ADD COLUMN validate_instance_id INTEGER REFERENCES graph_instances(id);
CREATE INDEX owner_exceptions_validate_instance ON owner_exceptions(validate_instance_id,id);
