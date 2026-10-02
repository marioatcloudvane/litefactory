package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// RepoWebhook is the webhook litefactory registered on one repository.
type RepoWebhook struct {
	// RepoID is the registered repository.
	RepoID int64
	// HookID is GitHub's id for the hook.
	HookID int64
	// URL is the delivery URL the hook was registered with.
	URL string
	// UpdatedAt is when GitHub last confirmed the hook.
	UpdatedAt time.Time
}

// GetRepoWebhook reads the recorded hook for repoID, returning [ErrNotFound]
// when none is recorded.
func (db *DB) GetRepoWebhook(ctx context.Context, repoID int64) (RepoWebhook, error) {
	var (
		hook RepoWebhook
		at   sql.NullString
	)
	err := db.sql.QueryRowContext(ctx,
		`SELECT repo_id, hook_id, url, updated_at FROM repo_webhooks WHERE repo_id = ?`, repoID).
		Scan(&hook.RepoID, &hook.HookID, &hook.URL, &at)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return RepoWebhook{}, fmt.Errorf("store: read repo webhook %d: %w", repoID, ErrNotFound)
	case err != nil:
		return RepoWebhook{}, fmt.Errorf("store: read repo webhook %d: %w", repoID, err)
	}
	if hook.UpdatedAt, err = scanTime(at); err != nil {
		return RepoWebhook{}, fmt.Errorf("store: parse repo webhook %d: %w", repoID, err)
	}
	return hook, nil
}

// SetRepoWebhook records the confirmed hook for repoID, replacing any earlier
// record. Callers write it only after GitHub confirmed the hook exists.
func (db *DB) SetRepoWebhook(ctx context.Context, repoID, hookID int64, url string, at time.Time) error {
	if hookID <= 0 {
		return fmt.Errorf("store: write repo webhook %d: hook id %d is not positive", repoID, hookID)
	}
	const stmt = `
INSERT INTO repo_webhooks (repo_id, hook_id, url, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT (repo_id) DO UPDATE SET
    hook_id = excluded.hook_id, url = excluded.url, updated_at = excluded.updated_at`
	if _, err := db.sql.ExecContext(ctx, stmt, repoID, hookID, url, FormatTime(at)); err != nil {
		return fmt.Errorf("store: write repo webhook %d: %w", repoID, err)
	}
	return nil
}

// ClearRepoWebhook forgets the recorded hook for repoID. Clearing a repo with
// no record is not an error.
func (db *DB) ClearRepoWebhook(ctx context.Context, repoID int64) error {
	if _, err := db.sql.ExecContext(ctx, `DELETE FROM repo_webhooks WHERE repo_id = ?`, repoID); err != nil {
		return fmt.Errorf("store: clear repo webhook %d: %w", repoID, err)
	}
	return nil
}
