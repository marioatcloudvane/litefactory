package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// SyncObservation records exact inputs and the outcome for one preparation
// phase. Empty RemoteSHA with outcome absent is an authoritative absence.
type SyncObservation struct {
	Phase     string   `json:"phase"`
	Ref       string   `json:"ref"`
	LocalSHA  string   `json:"local_sha"`
	RemoteSHA string   `json:"remote_sha"`
	Outcome   string   `json:"outcome"`
	Behind    int      `json:"behind"`
	Ahead     int      `json:"ahead"`
	ResultSHA string   `json:"result_sha"`
	Conflicts []string `json:"conflicts"`
	Reason    string   `json:"reason"`
}

// RecordRunSyncObservations preserves task and base outcomes independently.
func (db *DB) RecordRunSyncObservations(ctx context.Context, runID int64, observations []SyncObservation) (Run, error) {
	seen := make(map[string]bool)
	for _, s := range observations {
		if (s.Phase != "task" && s.Phase != "base") || seen[s.Phase] || s.Ref == "" || s.LocalSHA == "" || s.Behind < 0 || s.Ahead < 0 {
			return Run{}, fmt.Errorf("invalid synchronization observation")
		}
		seen[s.Phase] = true
		switch s.Outcome {
		case "up_to_date", "synced", "blocked", "skipped", "failed", "deferred_dirty", "absent":
		default:
			return Run{}, fmt.Errorf("invalid synchronization outcome %q", s.Outcome)
		}
	}
	data, err := json.Marshal(observations)
	if err != nil {
		return Run{}, fmt.Errorf("encode synchronization observations: %w", err)
	}
	run, err := scanRun(db.sql.QueryRowContext(ctx, `UPDATE runs SET sync_observations = ? WHERE id = ? RETURNING `+runColumns, string(data), runID))
	if err != nil {
		return Run{}, fmt.Errorf("record synchronization observations: %w", err)
	}
	return run, nil
}
