package store

import (
	"context"
	"errors"
	"strings"
)

// ResolvePreventionRepository resolves one case-insensitive GitHub identity.
// Ambiguous registrations never select credentials or local evidence by row order.
func (db *DB) ResolvePreventionRepository(ctx context.Context, repository string) (Repo, error) {
	parts := strings.Split(repository, "/")
	if len(parts) != 2 {
		return Repo{}, errors.New("prevention analysis: repository identity required")
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT `+repoColumns+` FROM repos WHERE forge='github' AND owner=? COLLATE NOCASE AND name=? COLLATE NOCASE LIMIT 2`, parts[0], parts[1])
	if err != nil {
		return Repo{}, err
	}
	defer rows.Close()
	var found Repo
	count := 0
	for rows.Next() {
		r, e := scanRepo(rows)
		if e != nil {
			return Repo{}, e
		}
		found = r
		count++
	}
	if err = rows.Err(); err != nil {
		return Repo{}, err
	}
	if count == 0 {
		return Repo{}, ErrNotFound
	}
	if count > 1 {
		return Repo{}, ErrPreventionConflict
	}
	return found, nil
}

// ReadPreventionRepositoryEvidence fences local observations to the selected
// registration as well as its GitHub issue identity.
func (db *DB) ReadPreventionRepositoryEvidence(ctx context.Context, repoID int64, repository string, issue, limit, byteLimit int) ([]PreventionLocalObservation, error) {
	if repoID < 1 {
		return nil, errors.New("prevention analysis: registered repository required")
	}
	return db.readPreventionBugEvidence(ctx, repoID, repository, issue, limit, byteLimit)
}
