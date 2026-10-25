package store

import "testing"

// seedHistoricalRunSnapshot seeds the populated schema used by migration fixtures. Current
// run accessors require preparation_caveats, which these databases do not have
// yet. The literal supplies only the new field's default in the comparison
// snapshot; the insert still writes exclusively to the historical schema.
func seedHistoricalRunSnapshot(t *testing.T, db *DB, params AllocateRunParams, status RunStatus) Run {
	t.Helper()
	at := FormatTime(nowUTC())
	var finishedAt any
	var durationMS any
	if status == RunSucceeded {
		finishedAt, durationMS = at, int64(0)
	}
	var ordinal int
	if err := db.sql.QueryRowContext(t.Context(), `SELECT COALESCE(MAX(ordinal),0)+1 FROM runs WHERE session_id=?`, params.SessionID).Scan(&ordinal); err != nil {
		t.Fatal(err)
	}
	run, err := scanRun(db.sql.QueryRowContext(t.Context(), `
INSERT INTO runs(session_id,segment_id,ordinal,mode,agent,trigger_comment_id,status,started_at,finished_at,duration_ms,log_path)
VALUES(?,?,?,?,?,?,?,?,?,?,?)
RETURNING id,session_id,segment_id,ordinal,mode,agent,trigger_comment_id,status,
exit_code,error,started_at,finished_at,duration_ms,cost_usd,log_path,commit_sha,
result_comment_id,sync_outcome,sync_behind,sync_ahead,sync_commit_sha,
semantic_outcome,recovery_reason,sync_observations,published,publication,workflow,'[]'`,
		params.SessionID, params.SegmentID, ordinal, params.Mode, params.Agent,
		params.TriggerCommentID, status, at, finishedAt, durationMS, params.LogPath(ordinal)))
	if err != nil {
		t.Fatal(err)
	}
	return run
}
