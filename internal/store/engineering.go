package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// PendingEngineering returns exact frozen story keys which still lack engineering
// evidence. A closed issue cannot satisfy this gate: only stored facts count.
func (db *DB) PendingEngineering(ctx context.Context, parent string, generation int) ([]string, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT story_key FROM managed_stories WHERE parent_task=? AND generation=? AND state!='completed' AND engineering_complete=0 ORDER BY story_key`, parent, generation)
	if err != nil {
		return nil, fmt.Errorf("store: pending engineering: %w", err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// EngineeringEvidence returns the latest retained successful checkpoint for an
// engineered revision. Bytes are the immutable envelope and complete snapshot,
// not a mutable pathname; Validate must still verify identity and attribution.
func (db *DB) EngineeringEvidence(ctx context.Context, parent string, generation int, key string) (StoryProgressEvent, error) {
	var runID int64
	var sequence int
	err := db.sql.QueryRowContext(ctx, `SELECT e.run_id,e.sequence FROM story_progress_events e JOIN story_progress_runs p USING(run_id) JOIN managed_stories s ON s.parent_task=p.parent_task AND s.generation=p.generation AND s.story_key=e.story_key WHERE p.parent_task=? AND p.generation=? AND e.story_key=? AND s.engineering_complete=1 AND e.kind='engineering_complete' AND e.conflicted=0 ORDER BY e.run_id DESC,e.sequence DESC LIMIT 1`, parent, generation, key).Scan(&runID, &sequence)
	if errors.Is(err, sql.ErrNoRows) {
		return StoryProgressEvent{}, ErrNotFound
	}
	if err != nil {
		return StoryProgressEvent{}, err
	}
	return db.GetStoryProgressEvent(ctx, runID, sequence)
}
