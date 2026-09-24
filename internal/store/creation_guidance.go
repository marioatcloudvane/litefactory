package store

import (
	"context"
	"database/sql"
	"errors"
)

// ResearchExecutionPath observes the frozen selection without admitting a path
// or starting work merely to describe publishing capability.
func (db *DB) ResearchExecutionPath(ctx context.Context, sessionID int64) (string, error) {
	var path string
	err := db.sql.QueryRowContext(ctx, `SELECT path FROM research_execution_paths WHERE session_id=?`, sessionID).Scan(&path)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return path, err
}
