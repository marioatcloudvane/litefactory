package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// SessionRepository is the one rule for the repository a session works on: the
// retained execution target when the session has one, otherwise the live
// repository row with the base the session used before a base change (its
// legacy base pin) when it has one, otherwise the live row unchanged.
func (db *DB) SessionRepository(ctx context.Context, session Session) (Repo, error) {
	saved, err := db.GetExecutionSnapshot(ctx, session.ID)
	if err == nil {
		return saved.CodeTarget, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Repo{}, err
	}
	repo, err := db.GetRepo(ctx, session.RepoID)
	if err != nil {
		return Repo{}, err
	}
	base, err := db.LegacyBasePin(ctx, session.ID)
	if errors.Is(err, ErrNotFound) {
		return repo, nil
	}
	if err != nil {
		return Repo{}, err
	}
	repo.BaseBranch = base
	return repo, nil
}

// LegacyBasePin returns the base an unpinned session kept through a base
// change, or [ErrNotFound] when it has none.
func (db *DB) LegacyBasePin(ctx context.Context, sessionID int64) (string, error) {
	var base string
	err := db.sql.QueryRowContext(ctx, `SELECT base_branch FROM session_legacy_base_pins WHERE session_id=?`, sessionID).Scan(&base)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("store: read legacy base pin %d: %w", sessionID, err)
	}
	return base, nil
}
