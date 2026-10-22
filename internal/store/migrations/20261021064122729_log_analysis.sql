CREATE TABLE log_analyses (
 id TEXT PRIMARY KEY,
 start_key TEXT UNIQUE,
 request_digest TEXT NOT NULL,
 scope_json TEXT NOT NULL,
 limits_json TEXT NOT NULL,
 grant_digest TEXT NOT NULL,
 provenance_json TEXT NOT NULL,
 generation INTEGER NOT NULL CHECK(generation>0),
 state TEXT NOT NULL CHECK(state IN ('running','completed','incomplete','failed')),
 disposition TEXT NOT NULL CHECK(disposition IN ('proposals','no_warranted_change','insufficient_evidence','not_evaluated')),
 calls INTEGER NOT NULL DEFAULT 0 CHECK(calls>=0),
 outbound_bytes INTEGER NOT NULL DEFAULT 0 CHECK(outbound_bytes>=0),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 reason TEXT NOT NULL DEFAULT ''
);
CREATE TABLE log_analysis_books (
 analysis_id TEXT PRIMARY KEY REFERENCES log_analyses(id),
 root_key TEXT NOT NULL UNIQUE REFERENCES standards_run_bindings(root_run_id)
);
CREATE TABLE log_analysis_evidence (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 analysis_id TEXT NOT NULL REFERENCES log_analyses(id),
 generation INTEGER NOT NULL,
 call_key TEXT NOT NULL,
 metadata_json TEXT NOT NULL,
 created_at TEXT NOT NULL,
 UNIQUE(analysis_id,call_key)
);
CREATE INDEX log_analysis_evidence_page ON log_analysis_evidence(analysis_id,id);
CREATE TABLE log_analysis_reports (
 analysis_id TEXT PRIMARY KEY REFERENCES log_analyses(id),
 request_id TEXT NOT NULL,
 request_digest TEXT NOT NULL,
 report_json TEXT NOT NULL,
 receipt_json TEXT NOT NULL,
 created_at TEXT NOT NULL
);
CREATE TABLE log_analysis_proposals (
 analysis_id TEXT NOT NULL REFERENCES log_analysis_reports(analysis_id),
 candidate_key TEXT NOT NULL,
 proposal_id TEXT NOT NULL,
 version INTEGER NOT NULL,
 PRIMARY KEY(analysis_id,candidate_key),
 UNIQUE(proposal_id,version),
 FOREIGN KEY(proposal_id,version) REFERENCES standards_proposal_versions(proposal_id,version)
);
CREATE TRIGGER log_analysis_scope_immutable BEFORE UPDATE OF id,start_key,request_digest,scope_json,limits_json,grant_digest,created_at ON log_analyses BEGIN SELECT RAISE(ABORT,'immutable analysis request'); END;
CREATE TRIGGER log_analysis_evidence_immutable BEFORE UPDATE ON log_analysis_evidence BEGIN SELECT RAISE(ABORT,'immutable analysis evidence'); END;
CREATE TRIGGER log_analysis_report_immutable BEFORE UPDATE ON log_analysis_reports BEGIN SELECT RAISE(ABORT,'immutable analysis report'); END;
CREATE TRIGGER log_analysis_proposal_immutable BEFORE UPDATE ON log_analysis_proposals BEGIN SELECT RAISE(ABORT,'immutable analysis proposal link'); END;
CREATE TRIGGER log_analysis_book_immutable BEFORE UPDATE ON log_analysis_books BEGIN SELECT RAISE(ABORT,'immutable analysis book association'); END;
CREATE TRIGGER log_analysis_book_retained BEFORE DELETE ON log_analysis_books BEGIN SELECT RAISE(ABORT,'retained analysis book association'); END;
CREATE TRIGGER log_analysis_binding_retained BEFORE DELETE ON standards_run_bindings WHEN EXISTS(SELECT 1 FROM log_analysis_books WHERE root_key=OLD.root_run_id) BEGIN SELECT RAISE(ABORT,'retained analyst standards binding'); END;
CREATE TRIGGER log_analysis_selection_retained BEFORE DELETE ON standards_run_selections WHEN EXISTS(SELECT 1 FROM log_analysis_books WHERE root_key=OLD.root_run_id) BEGIN SELECT RAISE(ABORT,'retained analyst standards selection'); END;
CREATE TRIGGER log_analysis_observation_retained BEFORE DELETE ON standards_run_observations WHEN EXISTS(SELECT 1 FROM log_analysis_books WHERE root_key=OLD.root_run_id) BEGIN SELECT RAISE(ABORT,'retained analyst standards observation'); END;
CREATE TRIGGER log_analysis_evidence_retained BEFORE DELETE ON log_analysis_evidence BEGIN SELECT RAISE(ABORT,'retained analysis evidence'); END;
CREATE TRIGGER log_analysis_report_retained BEFORE DELETE ON log_analysis_reports BEGIN SELECT RAISE(ABORT,'retained analysis report'); END;
CREATE TRIGGER log_analysis_proposal_retained BEFORE DELETE ON log_analysis_proposals BEGIN SELECT RAISE(ABORT,'retained analysis proposal link'); END;
