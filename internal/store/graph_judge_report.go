package store

import (
	"context"
	"errors"
)

// ListGraphJudgeAttempts reads advisory launches whose pinned result revision
// belongs to this graph. Requested models are retained separately from observed
// models; neither a different graph on this ticket nor later revisions match.
func (db *DB) ListGraphJudgeAttempts(ctx context.Context, instanceID int64) ([]JudgeAttempt, error) {
	if instanceID < 1 {
		return nil, errors.New("store: a graph instance is required for judge evidence")
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT DISTINCT j.id,j.request_id,j.ordinal,j.provider,j.model,j.thinking,j.state,j.failure,j.observed_model,j.provider_version FROM judge_attempts j JOIN judge_requests q ON q.id=j.request_id JOIN application_result_revisions r ON r.result_id=json_extract(q.source_json,'$.ID') AND r.revision=json_extract(q.source_json,'$.Revision') JOIN graph_node_attempts n ON n.run_id=r.run_id JOIN graph_instances g ON g.id=n.instance_id JOIN sessions s ON s.id=g.session_id WHERE n.instance_id=? AND q.session_id=g.session_id AND q.task=s.task_ref AND json_extract(q.source_json,'$.Kind')='result' ORDER BY j.request_id,j.ordinal`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JudgeAttempt
	for rows.Next() {
		a, err := scanJudgeAttempt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
