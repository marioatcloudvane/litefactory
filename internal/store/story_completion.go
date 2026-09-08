package store

import "context"

// MarkStoryAggregateSucceeded records the factory's successful aggregate run and
// save/push boundary. It never closes stories; individual evidence does that.
func (db *DB) MarkStoryAggregateSucceeded(ctx context.Context, runID int64) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE story_progress_runs SET aggregate_succeeded=1 WHERE run_id=? AND EXISTS(SELECT 1 FROM runs WHERE id=? AND (status='succeeded' OR (status='reported' AND coalesce(error,'')='')))`, runID, runID)
	return err
}

// CompleteManagedFeature waits for all scoped outcomes and pending remote
// acknowledgements, independently of when each story was closed. Caller holds
// the parent's progress lock so no concurrent ingestion can invalidate the gate.
func (db *DB) CompleteManagedFeature(ctx context.Context, parent string) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE managed_features SET state='completed' WHERE parent_task=? AND state='implementing'
 AND EXISTS(SELECT 1 FROM managed_stories s WHERE s.parent_task=managed_features.parent_task AND s.generation=managed_features.generation)
 AND NOT EXISTS(SELECT 1 FROM managed_stories s WHERE s.parent_task=managed_features.parent_task AND s.generation=managed_features.generation AND s.state!='completed')
 AND EXISTS(SELECT 1 FROM story_progress_runs p WHERE p.parent_task=managed_features.parent_task AND p.generation=managed_features.generation AND p.aggregate_succeeded=1)
 AND NOT EXISTS(SELECT 1 FROM story_progress_runs p WHERE p.parent_task=managed_features.parent_task AND p.active=1)
 AND NOT EXISTS(SELECT 1 FROM story_progress_effects e WHERE e.parent_task=managed_features.parent_task AND e.delivered=0 AND e.superseded=0)`, parent)
	return err
}

// ListStoryAggregateParents discovers gates whose delivery may now be complete.
func (db *DB) ListStoryAggregateParents(ctx context.Context) ([]string, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT DISTINCT p.parent_task FROM story_progress_runs p JOIN managed_features f ON f.parent_task=p.parent_task AND f.generation=p.generation WHERE p.aggregate_succeeded=1 AND f.state='implementing'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var parents []string
	for rows.Next() {
		var parent string
		if err := rows.Scan(&parent); err != nil {
			return nil, err
		}
		parents = append(parents, parent)
	}
	return parents, rows.Err()
}
