CREATE TABLE drift_analyses (
 id TEXT PRIMARY KEY,
 run_id TEXT NOT NULL UNIQUE REFERENCES drift_analyst_runs(id) DEFERRABLE INITIALLY DEFERRED,
 repository TEXT NOT NULL,
 request_key TEXT,
 request_digest TEXT NOT NULL,
 generation INTEGER NOT NULL,
 version INTEGER NOT NULL,
 execution TEXT NOT NULL,
 record_json TEXT NOT NULL,
 UNIQUE(repository,request_key)
);
CREATE TABLE drift_analyst_runs (
 id TEXT PRIMARY KEY CHECK(id<>''),
 analysis_id TEXT NOT NULL UNIQUE REFERENCES drift_analyses(id),
 created_at TEXT NOT NULL
);
CREATE TRIGGER drift_analyst_run_immutable BEFORE UPDATE ON drift_analyst_runs BEGIN SELECT RAISE(ABORT,'immutable drift analyst run'); END;
CREATE TRIGGER drift_analyst_run_link BEFORE INSERT ON drift_analyst_runs
WHEN NOT EXISTS(SELECT 1 FROM drift_analyses WHERE id=NEW.analysis_id AND run_id=NEW.id)
BEGIN SELECT RAISE(ABORT,'invalid drift analyst run link'); END;
CREATE TABLE drift_evidence (
 analysis_id TEXT NOT NULL REFERENCES drift_analyses(id),
 id TEXT NOT NULL,
 evidence_json TEXT NOT NULL,
 PRIMARY KEY(analysis_id,id)
);
CREATE TABLE drift_receipts (
 analysis_id TEXT NOT NULL REFERENCES drift_analyses(id),
 request_id TEXT NOT NULL,
 digest TEXT NOT NULL,
 receipt_json TEXT NOT NULL,
 PRIMARY KEY(analysis_id,request_id)
);
CREATE TABLE drift_proposal_links (
 analysis_id TEXT NOT NULL REFERENCES drift_analyses(id),
 candidate_key TEXT NOT NULL,
 proposal_id TEXT NOT NULL REFERENCES standards_proposals(id),
 version INTEGER NOT NULL,
 digest TEXT NOT NULL,
 PRIMARY KEY(analysis_id,candidate_key)
);
CREATE TRIGGER drift_evidence_immutable BEFORE UPDATE ON drift_evidence
WHEN NOT (
 OLD.analysis_id IS NEW.analysis_id AND OLD.id IS NEW.id
 AND EXISTS(SELECT 1 FROM drift_analyses WHERE id=OLD.analysis_id AND execution<>'running')
 AND json_extract(OLD.evidence_json,'$.excerpt_expired') IS NOT 1
 AND json_extract(NEW.evidence_json,'$.excerpt_expired') IS 1
 AND json_extract(NEW.evidence_json,'$.text') IS ''
 AND COALESCE(json_extract(NEW.evidence_json,'$.excerpt_bytes'),0) IS length(CAST(json_extract(OLD.evidence_json,'$.text') AS BLOB))
 AND json_extract(NEW.evidence_json,'$.omission') IS CASE WHEN COALESCE(json_extract(OLD.evidence_json,'$.omission'),'')='' THEN 'retained excerpt expired' ELSE json_extract(OLD.evidence_json,'$.omission')||'; retained excerpt expired' END
 AND json_remove(NEW.evidence_json,'$.text','$.omission','$.excerpt_expired','$.excerpt_bytes') IS json_remove(OLD.evidence_json,'$.text','$.omission','$.excerpt_expired','$.excerpt_bytes')
)
BEGIN SELECT RAISE(ABORT,'immutable drift evidence'); END;
CREATE TRIGGER drift_scope_immutable BEFORE UPDATE ON drift_analyses
WHEN OLD.id IS NOT NEW.id OR OLD.run_id IS NOT NEW.run_id OR OLD.repository IS NOT NEW.repository OR OLD.request_key IS NOT NEW.request_key OR OLD.request_digest IS NOT NEW.request_digest
 OR json_extract(OLD.record_json,'$.scope') IS NOT json_extract(NEW.record_json,'$.scope')
 OR json_extract(OLD.record_json,'$.limits') IS NOT json_extract(NEW.record_json,'$.limits')
 OR json_extract(OLD.record_json,'$.run_id') IS NOT json_extract(NEW.record_json,'$.run_id')
 OR (json_extract(OLD.record_json,'$.pinned') IS NOT NULL AND json_extract(OLD.record_json,'$.pinned') IS NOT json_extract(NEW.record_json,'$.pinned'))
BEGIN SELECT RAISE(ABORT,'immutable drift scope'); END;
CREATE TRIGGER drift_receipt_immutable BEFORE UPDATE ON drift_receipts BEGIN SELECT RAISE(ABORT,'immutable drift receipt'); END;
CREATE TRIGGER drift_proposal_link_immutable BEFORE UPDATE ON drift_proposal_links BEGIN SELECT RAISE(ABORT,'immutable drift proposal link'); END;
