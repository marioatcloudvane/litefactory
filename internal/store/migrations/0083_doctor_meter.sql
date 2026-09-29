-- Preserve the shared usage ledger while adding exclusive doctor ownership.
-- Child snapshots are required because the old owner FK cascades on drop.
PRAGMA defer_foreign_keys=ON;

CREATE TABLE meter_attempts_before_doctor AS SELECT * FROM meter_attempts;

CREATE TABLE meter_invocations_before_doctor AS SELECT * FROM meter_invocations;

CREATE TABLE meter_observations_before_doctor AS SELECT * FROM meter_observations;

CREATE TABLE meter_checkpoints_before_doctor AS SELECT * FROM meter_checkpoints;

CREATE TABLE meter_measurements_before_doctor AS SELECT * FROM meter_measurements;

CREATE TABLE meter_valuations_before_doctor AS SELECT * FROM meter_valuations;

CREATE TABLE meter_valuation_dimensions_before_doctor AS SELECT * FROM meter_valuation_dimensions;

DROP TABLE meter_valuation_dimensions;

DROP TABLE meter_valuations;

DROP TABLE meter_measurements;

DROP TABLE meter_checkpoints;

DROP TABLE meter_observations;

DROP TABLE meter_invocations;

DROP TABLE meter_attempts;

CREATE TABLE meter_attempts (
 id TEXT PRIMARY KEY NOT NULL, doctor_attempt_id TEXT REFERENCES doctor_attempts(id), run_id INTEGER REFERENCES runs(id) ON DELETE CASCADE,
 ordinal INTEGER NOT NULL CHECK(ordinal>0), coding_agent TEXT NOT NULL, agent_version TEXT, source_format TEXT,
 started_at TEXT NOT NULL, finished_at TEXT, outcome TEXT NOT NULL, collection_status TEXT NOT NULL,
 diagnostic_codes_json TEXT NOT NULL, price_revision_id INTEGER NOT NULL REFERENCES meter_price_revisions(id) ON DELETE RESTRICT, provider_reported_zero_cost_refusal INTEGER NOT NULL DEFAULT 0 CHECK (provider_reported_zero_cost_refusal IN (0, 1)), graph_node_attempt_id INTEGER REFERENCES graph_node_attempts(id),
 UNIQUE(run_id,ordinal), UNIQUE(doctor_attempt_id,ordinal),
 CHECK((run_id IS NOT NULL AND doctor_attempt_id IS NULL) OR (run_id IS NULL AND doctor_attempt_id IS NOT NULL AND graph_node_attempt_id IS NULL))
);

INSERT INTO meter_attempts(id,run_id,ordinal,coding_agent,agent_version,source_format,started_at,finished_at,outcome,collection_status,diagnostic_codes_json,price_revision_id,provider_reported_zero_cost_refusal,graph_node_attempt_id) SELECT id,run_id,ordinal,coding_agent,agent_version,source_format,started_at,finished_at,outcome,collection_status,diagnostic_codes_json,price_revision_id,provider_reported_zero_cost_refusal,graph_node_attempt_id FROM meter_attempts_before_doctor;

DROP TABLE meter_attempts_before_doctor;

CREATE INDEX meter_attempt_time ON meter_attempts(started_at,id);

CREATE INDEX meter_attempt_agent_time ON meter_attempts(coding_agent,started_at,id);

CREATE INDEX meter_attempt_price ON meter_attempts(price_revision_id);

CREATE INDEX idx_meter_graph_attempt ON meter_attempts(graph_node_attempt_id);

CREATE TABLE meter_invocations (
 attempt_id TEXT NOT NULL REFERENCES meter_attempts(id) ON DELETE CASCADE, id TEXT NOT NULL, parent_id TEXT,
 source_id TEXT NOT NULL, source_invocation_id TEXT NOT NULL, started_at TEXT, finished_at TEXT,
 lifecycle TEXT NOT NULL, collection_status TEXT NOT NULL, diagnostic_codes_json TEXT NOT NULL,
 PRIMARY KEY(attempt_id,id), UNIQUE(attempt_id,source_id,source_invocation_id),
 FOREIGN KEY(attempt_id,parent_id) REFERENCES meter_invocations(attempt_id,id) DEFERRABLE INITIALLY DEFERRED
);

INSERT INTO meter_invocations(attempt_id,id,parent_id,source_id,source_invocation_id,started_at,finished_at,lifecycle,collection_status,diagnostic_codes_json) SELECT attempt_id,id,parent_id,source_id,source_invocation_id,started_at,finished_at,lifecycle,collection_status,diagnostic_codes_json FROM meter_invocations_before_doctor;

DROP TABLE meter_invocations_before_doctor;

CREATE UNIQUE INDEX meter_one_root ON meter_invocations(attempt_id) WHERE parent_id IS NULL;

CREATE INDEX meter_invocation_parent ON meter_invocations(attempt_id,parent_id,id);

CREATE TABLE meter_observations (
 id INTEGER PRIMARY KEY, attempt_id TEXT NOT NULL REFERENCES meter_attempts(id) ON DELETE CASCADE,
 source_id TEXT NOT NULL, event_key TEXT NOT NULL, invocation_id TEXT, observed_at TEXT, recorded_at TEXT NOT NULL,
 semantics TEXT NOT NULL, counter_epoch TEXT NOT NULL, source_sequence TEXT, actual_model TEXT, model_provenance TEXT NOT NULL,
 dimensions_json TEXT NOT NULL, payload_sha256 TEXT NOT NULL, validation_status TEXT NOT NULL, diagnostic_codes_json TEXT NOT NULL,
 UNIQUE(attempt_id,source_id,event_key), FOREIGN KEY(attempt_id,invocation_id) REFERENCES meter_invocations(attempt_id,id)
);

INSERT INTO meter_observations(id,attempt_id,source_id,event_key,invocation_id,observed_at,recorded_at,semantics,counter_epoch,source_sequence,actual_model,model_provenance,dimensions_json,payload_sha256,validation_status,diagnostic_codes_json) SELECT id,attempt_id,source_id,event_key,invocation_id,observed_at,recorded_at,semantics,counter_epoch,source_sequence,actual_model,model_provenance,dimensions_json,payload_sha256,validation_status,diagnostic_codes_json FROM meter_observations_before_doctor;

DROP TABLE meter_observations_before_doctor;

CREATE INDEX meter_observation_invocation ON meter_observations(attempt_id,invocation_id,id);

CREATE INDEX meter_observation_counter ON meter_observations(attempt_id,source_id,counter_epoch,id);

CREATE TABLE meter_checkpoints (
 attempt_id TEXT NOT NULL REFERENCES meter_attempts(id) ON DELETE CASCADE, source_id TEXT NOT NULL,
 counter_key TEXT NOT NULL, counter_epoch TEXT NOT NULL, invocation_id TEXT, model_key TEXT NOT NULL,
 baseline_dimensions_json TEXT NOT NULL, last_dimensions_json TEXT NOT NULL, source_cursor TEXT,
 last_observation_id INTEGER REFERENCES meter_observations(id), updated_at TEXT NOT NULL,
 PRIMARY KEY(attempt_id,source_id,counter_key,counter_epoch), FOREIGN KEY(attempt_id,invocation_id) REFERENCES meter_invocations(attempt_id,id)
);

INSERT INTO meter_checkpoints(attempt_id,source_id,counter_key,counter_epoch,invocation_id,model_key,baseline_dimensions_json,last_dimensions_json,source_cursor,last_observation_id,updated_at) SELECT attempt_id,source_id,counter_key,counter_epoch,invocation_id,model_key,baseline_dimensions_json,last_dimensions_json,source_cursor,last_observation_id,updated_at FROM meter_checkpoints_before_doctor;

DROP TABLE meter_checkpoints_before_doctor;

CREATE INDEX meter_checkpoint_observation ON meter_checkpoints(last_observation_id);

CREATE TABLE meter_measurements (
 id INTEGER PRIMARY KEY, attempt_id TEXT NOT NULL REFERENCES meter_attempts(id) ON DELETE CASCADE,
 invocation_id TEXT NOT NULL, model_key TEXT NOT NULL, actual_model TEXT, model_provenance TEXT NOT NULL,
 input_uncached_tokens INTEGER CHECK(input_uncached_tokens>=0), cache_read_tokens INTEGER CHECK(cache_read_tokens>=0),
 cache_write_tokens INTEGER CHECK(cache_write_tokens>=0), cache_write_5m_tokens INTEGER CHECK(cache_write_5m_tokens>=0),
 cache_write_1h_tokens INTEGER CHECK(cache_write_1h_tokens>=0), output_tokens INTEGER CHECK(output_tokens>=0), reasoning_tokens INTEGER CHECK(reasoning_tokens>=0),
 collection_status TEXT NOT NULL, diagnostic_codes_json TEXT NOT NULL, last_observation_id INTEGER NOT NULL REFERENCES meter_observations(id),
 revision INTEGER NOT NULL CHECK(revision>0), updated_at TEXT NOT NULL,
 UNIQUE(attempt_id,invocation_id,model_key), FOREIGN KEY(attempt_id,invocation_id) REFERENCES meter_invocations(attempt_id,id)
);

INSERT INTO meter_measurements(id,attempt_id,invocation_id,model_key,actual_model,model_provenance,input_uncached_tokens,cache_read_tokens,cache_write_tokens,cache_write_5m_tokens,cache_write_1h_tokens,output_tokens,reasoning_tokens,collection_status,diagnostic_codes_json,last_observation_id,revision,updated_at) SELECT id,attempt_id,invocation_id,model_key,actual_model,model_provenance,input_uncached_tokens,cache_read_tokens,cache_write_tokens,cache_write_5m_tokens,cache_write_1h_tokens,output_tokens,reasoning_tokens,collection_status,diagnostic_codes_json,last_observation_id,revision,updated_at FROM meter_measurements_before_doctor;

DROP TABLE meter_measurements_before_doctor;

CREATE INDEX meter_measurement_model ON meter_measurements(actual_model,attempt_id,id);

CREATE INDEX meter_measurement_observation ON meter_measurements(last_observation_id);

CREATE TABLE meter_valuations (
 id INTEGER PRIMARY KEY,
 measurement_id INTEGER NOT NULL REFERENCES meter_measurements(id) ON DELETE CASCADE,
 measurement_revision INTEGER NOT NULL CHECK(measurement_revision>0),
 price_revision_id INTEGER NOT NULL REFERENCES meter_price_revisions(id) ON DELETE RESTRICT,
 pricing_status TEXT NOT NULL, estimated_usd TEXT, priced_subtotal_usd TEXT NOT NULL, created_at TEXT NOT NULL,
 UNIQUE(measurement_id,measurement_revision)
);

INSERT INTO meter_valuations(id,measurement_id,measurement_revision,price_revision_id,pricing_status,estimated_usd,priced_subtotal_usd,created_at) SELECT id,measurement_id,measurement_revision,price_revision_id,pricing_status,estimated_usd,priced_subtotal_usd,created_at FROM meter_valuations_before_doctor;

DROP TABLE meter_valuations_before_doctor;

CREATE INDEX meter_valuation_price ON meter_valuations(price_revision_id);

CREATE TABLE meter_valuation_dimensions (
 valuation_id INTEGER NOT NULL REFERENCES meter_valuations(id) ON DELETE CASCADE,
 dimension TEXT NOT NULL, quantity INTEGER CHECK(quantity>=0), usd_per_million TEXT, amount_usd TEXT, pricing_status TEXT NOT NULL,
 PRIMARY KEY(valuation_id,dimension)
);

INSERT INTO meter_valuation_dimensions(valuation_id,dimension,quantity,usd_per_million,amount_usd,pricing_status) SELECT valuation_id,dimension,quantity,usd_per_million,amount_usd,pricing_status FROM meter_valuation_dimensions_before_doctor;

DROP TABLE meter_valuation_dimensions_before_doctor;
