-- Additive coverage evidence. Existing suite and entry rows remain unchanged.
CREATE TABLE validation_requests (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 lineage TEXT NOT NULL CHECK(length(lineage)>0),
 source_key TEXT NOT NULL CHECK(length(source_key)>0),
 policy TEXT NOT NULL CHECK(policy IN ('auto','full')),
 generation INTEGER NOT NULL CHECK(generation>0),
 commit_sha TEXT NOT NULL CHECK(length(commit_sha) IN (40,64)),
 created_at TEXT NOT NULL,
 UNIQUE(lineage,source_key), UNIQUE(lineage,generation)
);
CREATE TABLE validation_evidence (
 id TEXT PRIMARY KEY CHECK(length(id)=64),
 request_id INTEGER NOT NULL REFERENCES validation_requests(id),
 run_id INTEGER REFERENCES suite_runs(id),
 kind TEXT NOT NULL CHECK(kind IN ('manifest','observation','origin','comparison','assessment')),
 version INTEGER NOT NULL CHECK(version=1),
 complete INTEGER NOT NULL CHECK(complete IN (0,1)),
 payload TEXT NOT NULL CHECK(length(payload)<=4194304),
 reference_ids TEXT NOT NULL,
 created_at TEXT NOT NULL
);
CREATE INDEX validation_evidence_request ON validation_evidence(request_id,kind);
CREATE INDEX validation_evidence_run ON validation_evidence(run_id);
CREATE TRIGGER validation_requests_no_update BEFORE UPDATE ON validation_requests
BEGIN SELECT RAISE(ABORT,'validation request is immutable'); END;
CREATE TRIGGER validation_requests_no_delete BEFORE DELETE ON validation_requests
BEGIN SELECT RAISE(ABORT,'validation request is retained'); END;
CREATE TRIGGER validation_evidence_no_update BEFORE UPDATE ON validation_evidence
BEGIN SELECT RAISE(ABORT,'validation evidence is immutable'); END;
CREATE TRIGGER validation_evidence_no_delete BEFORE DELETE ON validation_evidence
BEGIN SELECT RAISE(ABORT,'validation evidence is retained'); END;
