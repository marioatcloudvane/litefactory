CREATE TABLE prevention_analyses (
 id TEXT PRIMARY KEY,
 request_key TEXT NOT NULL UNIQUE,
 scope_digest TEXT NOT NULL,
 repository TEXT NOT NULL,
 attempt_id TEXT NOT NULL,
 status TEXT NOT NULL,
 record_json TEXT NOT NULL
);
CREATE TABLE prevention_analysis_proposals (
 analysis_id TEXT NOT NULL REFERENCES prevention_analyses(id),
 ordinal INTEGER NOT NULL,
 result_digest TEXT NOT NULL,
 proposal_id TEXT NOT NULL,
 proposal_version INTEGER NOT NULL,
 provenance_json TEXT NOT NULL,
 PRIMARY KEY(analysis_id,ordinal),
 FOREIGN KEY(proposal_id,proposal_version) REFERENCES standards_proposal_versions(proposal_id,version)
);
CREATE TRIGGER prevention_analysis_no_delete BEFORE DELETE ON prevention_analyses BEGIN SELECT RAISE(ABORT,'retained prevention analysis'); END;
CREATE TRIGGER prevention_proposal_no_update BEFORE UPDATE ON prevention_analysis_proposals BEGIN SELECT RAISE(ABORT,'immutable prevention provenance'); END;
CREATE TRIGGER prevention_proposal_no_delete BEFORE DELETE ON prevention_analysis_proposals BEGIN SELECT RAISE(ABORT,'retained prevention provenance'); END;
CREATE TRIGGER prevention_binding_no_delete BEFORE DELETE ON standards_run_bindings WHEN EXISTS(SELECT 1 FROM prevention_analyses WHERE 'analysis:'||id=OLD.root_run_id) BEGIN SELECT RAISE(ABORT,'retained analysis standards binding'); END;
CREATE TRIGGER prevention_selection_no_delete BEFORE DELETE ON standards_run_selections WHEN EXISTS(SELECT 1 FROM prevention_analyses WHERE 'analysis:'||id=OLD.root_run_id) BEGIN SELECT RAISE(ABORT,'retained analysis standards selection'); END;
CREATE TRIGGER prevention_observation_no_delete BEFORE DELETE ON standards_run_observations WHEN EXISTS(SELECT 1 FROM prevention_analyses WHERE 'analysis:'||id=OLD.root_run_id) BEGIN SELECT RAISE(ABORT,'retained analysis standards observation'); END;
