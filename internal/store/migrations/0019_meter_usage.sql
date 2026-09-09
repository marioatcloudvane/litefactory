-- Metering evidence is independent of run outcome and report delivery.
CREATE TABLE meter_price_revisions (
 id INTEGER PRIMARY KEY, created_at TEXT NOT NULL, currency TEXT NOT NULL CHECK(currency='USD'),
 rate_scale INTEGER NOT NULL CHECK(rate_scale=9), valuation_scale INTEGER NOT NULL CHECK(valuation_scale=12),
 rounding_policy TEXT NOT NULL CHECK(rounding_policy='half_even_per_measurement_dimension_v1')
);
INSERT INTO meter_price_revisions VALUES(1,'1970-01-01T00:00:00.000Z','USD',9,12,'half_even_per_measurement_dimension_v1');
CREATE TABLE meter_attempts (
 id TEXT PRIMARY KEY NOT NULL, run_id INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
 ordinal INTEGER NOT NULL CHECK(ordinal>0), coding_agent TEXT NOT NULL, agent_version TEXT, source_format TEXT,
 started_at TEXT NOT NULL, finished_at TEXT, outcome TEXT NOT NULL, collection_status TEXT NOT NULL,
 diagnostic_codes_json TEXT NOT NULL, price_revision_id INTEGER NOT NULL REFERENCES meter_price_revisions(id) ON DELETE RESTRICT,
 UNIQUE(run_id,ordinal)
);
CREATE INDEX meter_attempt_time ON meter_attempts(started_at,id);
CREATE INDEX meter_attempt_agent_time ON meter_attempts(coding_agent,started_at,id);
CREATE INDEX meter_attempt_price ON meter_attempts(price_revision_id);
CREATE INDEX meter_run_mode ON runs(mode,session_id,id);
CREATE TABLE meter_invocations (
 attempt_id TEXT NOT NULL REFERENCES meter_attempts(id) ON DELETE CASCADE, id TEXT NOT NULL, parent_id TEXT,
 source_id TEXT NOT NULL, source_invocation_id TEXT NOT NULL, started_at TEXT, finished_at TEXT,
 lifecycle TEXT NOT NULL, collection_status TEXT NOT NULL, diagnostic_codes_json TEXT NOT NULL,
 PRIMARY KEY(attempt_id,id), UNIQUE(attempt_id,source_id,source_invocation_id),
 FOREIGN KEY(attempt_id,parent_id) REFERENCES meter_invocations(attempt_id,id) DEFERRABLE INITIALLY DEFERRED
);
CREATE UNIQUE INDEX meter_one_root ON meter_invocations(attempt_id) WHERE parent_id IS NULL;
CREATE INDEX meter_invocation_parent ON meter_invocations(attempt_id,parent_id,id);
CREATE TABLE meter_observations (
 id INTEGER PRIMARY KEY, attempt_id TEXT NOT NULL REFERENCES meter_attempts(id) ON DELETE CASCADE,
 source_id TEXT NOT NULL, event_key TEXT NOT NULL, invocation_id TEXT, observed_at TEXT, recorded_at TEXT NOT NULL,
 semantics TEXT NOT NULL, counter_epoch TEXT NOT NULL, source_sequence TEXT, actual_model TEXT, model_provenance TEXT NOT NULL,
 dimensions_json TEXT NOT NULL, payload_sha256 TEXT NOT NULL, validation_status TEXT NOT NULL, diagnostic_codes_json TEXT NOT NULL,
 UNIQUE(attempt_id,source_id,event_key), FOREIGN KEY(attempt_id,invocation_id) REFERENCES meter_invocations(attempt_id,id)
);
CREATE INDEX meter_observation_invocation ON meter_observations(attempt_id,invocation_id,id);
CREATE INDEX meter_observation_counter ON meter_observations(attempt_id,source_id,counter_epoch,id);
CREATE TABLE meter_checkpoints (
 attempt_id TEXT NOT NULL REFERENCES meter_attempts(id) ON DELETE CASCADE, source_id TEXT NOT NULL,
 counter_key TEXT NOT NULL, counter_epoch TEXT NOT NULL, invocation_id TEXT, model_key TEXT NOT NULL,
 baseline_dimensions_json TEXT NOT NULL, last_dimensions_json TEXT NOT NULL, source_cursor TEXT,
 last_observation_id INTEGER REFERENCES meter_observations(id), updated_at TEXT NOT NULL,
 PRIMARY KEY(attempt_id,source_id,counter_key,counter_epoch), FOREIGN KEY(attempt_id,invocation_id) REFERENCES meter_invocations(attempt_id,id)
);
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
CREATE INDEX meter_measurement_model ON meter_measurements(actual_model,attempt_id,id);
CREATE INDEX meter_measurement_observation ON meter_measurements(last_observation_id);
