-- Submission-v2 compatibility marker rows carry no v1 validation findings; their
-- follow-up findings live in submission_receipts. Record every existing marker
-- run as follow-up-materialized so the legacy follow-up parser never retries it.
-- Only rows whose context_json is exactly the marker (store.SubmissionAssessmentMarker)
-- are touched; v1 assessments are unchanged. Re-running is a no-op.
INSERT INTO followup_materializations(run_id)
SELECT run_id FROM validation_assessments
WHERE context_json='{"submission_version":2,"findings_source":"submission_receipts"}'
ON CONFLICT(run_id) DO NOTHING;
