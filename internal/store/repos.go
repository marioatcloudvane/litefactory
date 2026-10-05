package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Repo is a registered clone: a repository that already exists on disk where
// Mario keeps it, which litefactory watches and creates worktrees beside.
// litefactory never clones and never writes inside the registered clone (S-6).
type Repo struct {
	// ID is the surrogate key.
	ID int64
	// Path is the absolute, symlink-resolved path of the clone. It is unique,
	// and it is what makes `repo add` idempotent (AC-1.8).
	Path string
	// Forge, Owner and Name are parsed from the origin URL.
	Forge string
	Owner string
	Name  string
	// RemoteURL is the origin URL verbatim.
	RemoteURL string
	// BaseBranch is the branch task branches are cut from.
	BaseBranch string
	// FeaturePrefix is the branch prefix, "lf" by default.
	FeaturePrefix string
	// WorktreeRoot is the absolute directory task worktrees are created under.
	WorktreeRoot string
	// LastFetchAt is when `git fetch --prune` last ran against this clone. The
	// zero time means never, which `repo list` prints as "never" (AC-1.9). It
	// is owned by [DB.TouchRepoFetch] and is never changed by an upsert.
	LastFetchAt time.Time
	// CreatedAt and UpdatedAt are bookkeeping.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Slug renders the repository as owner/name — the form `--repo` accepts and
// `repo list` prints.
func (r Repo) Slug() string { return r.Owner + "/" + r.Name }

// UpsertRepo registers r, keyed on its path.
//
// Registering an already-registered path updates the mutable fields — base
// branch, worktree root, feature prefix, remote — and creates no
// second row (AC-1.8). LastFetchAt is deliberately not among them: a
// re-registration must not make litefactory think it has fetched.
//
// The stored row is returned, including the id the caller needs for sessions.
func (db *DB) UpsertRepo(ctx context.Context, r Repo) (Repo, error) {
	if r.Path == "" {
		return Repo{}, errors.New("store: upsert repo: path is required")
	}
	now := nowUTC()

	const stmt = `
INSERT INTO repos (
    path, forge, owner, name, remote_url, base_branch, feature_prefix,
    worktree_root, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (path) DO UPDATE SET
    forge              = excluded.forge,
    owner              = excluded.owner,
    name               = excluded.name,
    remote_url         = excluded.remote_url,
    base_branch        = excluded.base_branch,
    feature_prefix     = excluded.feature_prefix,
    worktree_root      = excluded.worktree_root,
    updated_at         = excluded.updated_at
RETURNING ` + repoColumns

	row := db.sql.QueryRowContext(ctx, stmt,
		r.Path, r.Forge, r.Owner, r.Name, r.RemoteURL, r.BaseBranch, r.FeaturePrefix,
		r.WorktreeRoot, FormatTime(now), FormatTime(now))

	stored, err := scanRepo(row)
	if err != nil {
		return Repo{}, fmt.Errorf("store: upsert repo %s: %w", r.Path, err)
	}
	return stored, nil
}

// GetRepo reads a repository by id, returning [ErrNotFound] when there is none.
func (db *DB) GetRepo(ctx context.Context, id int64) (Repo, error) {
	repo, err := scanRepo(db.sql.QueryRowContext(ctx,
		`SELECT `+repoColumns+` FROM repos WHERE id = ?`, id))
	if err != nil {
		return Repo{}, fmt.Errorf("store: read repo %d: %w", id, err)
	}
	return repo, nil
}

// GetRepoByPath reads a repository by its absolute path, returning
// [ErrNotFound] when the path is not registered.
func (db *DB) GetRepoByPath(ctx context.Context, path string) (Repo, error) {
	repo, err := scanRepo(db.sql.QueryRowContext(ctx,
		`SELECT `+repoColumns+` FROM repos WHERE path = ?`, path))
	if err != nil {
		return Repo{}, fmt.Errorf("store: read repo at %s: %w", path, err)
	}
	return repo, nil
}

// GetRepoBySlug reads a repository by forge, owner and name, returning
// [ErrNotFound] when it is not registered. This is how `--repo owner/name`
// resolves.
func (db *DB) GetRepoBySlug(ctx context.Context, forge, owner, name string) (Repo, error) {
	repo, err := scanRepo(db.sql.QueryRowContext(ctx,
		`SELECT `+repoColumns+` FROM repos WHERE forge = ? AND owner = ? AND name = ?`,
		forge, owner, name))
	if err != nil {
		return Repo{}, fmt.Errorf("store: read repo %s/%s: %w", owner, name, err)
	}
	return repo, nil
}

// ListRepos returns every registered repository, ordered by owner then name,
// so `repo list` output and the poll order are both stable.
func (db *DB) ListRepos(ctx context.Context) ([]Repo, error) {
	rows, err := db.sql.QueryContext(ctx,
		`SELECT `+repoColumns+` FROM repos ORDER BY owner, name, path`)
	if err != nil {
		return nil, fmt.Errorf("store: list repos: %w", err)
	}
	defer rows.Close()

	var out []Repo
	for rows.Next() {
		repo, err := scanRepo(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list repos: %w", err)
		}
		out = append(out, repo)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list repos: %w", err)
	}
	return out, nil
}

// TouchRepoFetch records that `git fetch --prune` completed against the clone
// at the given instant (AC-3.5). The caller passes the time because the fetch
// is what happened, not the write.
func (db *DB) TouchRepoFetch(ctx context.Context, repoID int64, at time.Time) error {
	res, err := db.sql.ExecContext(ctx,
		`UPDATE repos SET last_fetch_at = ?, updated_at = ? WHERE id = ?`,
		nullTime(at), FormatTime(nowUTC()), repoID)
	if err != nil {
		return fmt.Errorf("store: record fetch for repo %d: %w", repoID, err)
	}
	return requireAffected(res, fmt.Sprintf("store: record fetch for repo %d", repoID))
}

// repoColumns is the column list every repo read shares, in the order
// scanRepo expects.
const repoColumns = `
    id, path, forge, owner, name, remote_url, base_branch, feature_prefix,
    worktree_root, last_fetch_at,
    created_at, updated_at`

func scanRepo(s scanner) (Repo, error) {
	var (
		r           Repo
		lastFetchAt sql.NullString
		createdAt   sql.NullString
		updatedAt   sql.NullString
	)
	err := s.Scan(&r.ID, &r.Path, &r.Forge, &r.Owner, &r.Name, &r.RemoteURL, &r.BaseBranch,
		&r.FeaturePrefix, &r.WorktreeRoot, &lastFetchAt,
		&createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Repo{}, ErrNotFound
		}
		return Repo{}, err
	}

	for _, field := range []struct {
		src sql.NullString
		dst *time.Time
	}{
		{lastFetchAt, &r.LastFetchAt},
		{createdAt, &r.CreatedAt},
		{updatedAt, &r.UpdatedAt},
	} {
		t, err := scanTime(field.src)
		if err != nil {
			return Repo{}, err
		}
		*field.dst = t
	}
	return r, nil
}

// requireAffected turns an update that matched no row into [ErrNotFound],
// so "you asked me to change a row that does not exist" is never silent.
func requireAffected(res sql.Result, context string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: %w", context, err)
	}
	if n == 0 {
		return fmt.Errorf("%s: %w", context, ErrNotFound)
	}
	return nil
}
