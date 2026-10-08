package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
)

// RepositoryTeamBinding selects one exact installed version for a registered clone.
// Local versions are editable and never carry an immutable release digest.
type RepositoryTeamBinding struct {
	RepoID                                     int64
	Source, Slug, Version, Kind, ReleaseDigest string
}

var bindingSlug = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
var bindingVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

func validateRepositoryTeamBinding(b RepositoryTeamBinding) error {
	parts := bindingVersion.FindStringSubmatch(b.Version)
	if b.RepoID < 1 || !bindingSlug.MatchString(b.Slug) || parts == nil {
		return errors.New("invalid repository team binding identity")
	}
	for _, part := range strings.Split(parts[4], ".") {
		if len(part) > 1 && part[0] == '0' && strings.Trim(part, "0123456789") == "" {
			return errors.New("invalid repository team binding version")
		}
	}
	if b.Kind == "local" && b.Source == "local" && b.ReleaseDigest == "" {
		return nil
	}
	source, err := config.NormalizeTeamSource(b.Source)
	digest, digestErr := hex.DecodeString(b.ReleaseDigest)
	if b.Kind != "fetched" || err != nil || source != b.Source || digestErr != nil || len(digest) != 32 || hex.EncodeToString(digest) != b.ReleaseDigest {
		return errors.New("invalid repository team binding source or release digest")
	}
	return nil
}

const repositoryTeamColumns = `repo_id,source,slug,version,kind,release_digest`

func scanRepositoryTeamBinding(s scanner) (RepositoryTeamBinding, error) {
	var b RepositoryTeamBinding
	if err := s.Scan(&b.RepoID, &b.Source, &b.Slug, &b.Version, &b.Kind, &b.ReleaseDigest); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return b, ErrNotFound
		}
		return b, err
	}
	return b, validateRepositoryTeamBinding(b)
}

// GetRepositoryTeamBinding returns ErrNotFound only when no binding exists.
// Invalid stored state is an error; callers must never fall back to built-ins.
func (db *DB) GetRepositoryTeamBinding(ctx context.Context, repoID int64) (RepositoryTeamBinding, error) {
	b, err := scanRepositoryTeamBinding(db.sql.QueryRowContext(ctx, `SELECT `+repositoryTeamColumns+` FROM repository_team_bindings WHERE repo_id=?`, repoID))
	if err != nil {
		return RepositoryTeamBinding{}, fmt.Errorf("store: read repository team binding: %w", err)
	}
	return b, nil
}

// ReplaceRepositoryTeamBinding atomically replaces a validated binding.
// The caller holds the catalogue lock across installed-version validation and this write.
// A repeated exact binding returns changed=false.
func (db *DB) ReplaceRepositoryTeamBinding(ctx context.Context, b RepositoryTeamBinding) (changed bool, err error) {
	if err = validateRepositoryTeamBinding(b); err != nil {
		return false, err
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		prior, e := scanRepositoryTeamBinding(tx.QueryRowContext(ctx, `SELECT `+repositoryTeamColumns+` FROM repository_team_bindings WHERE repo_id=?`, b.RepoID))
		if e != nil && !errors.Is(e, ErrNotFound) {
			return e
		}
		if e == nil && prior == b {
			return nil
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO repository_team_bindings(repo_id,source,slug,version,kind,release_digest) VALUES(?,?,?,?,?,?) ON CONFLICT(repo_id) DO UPDATE SET source=excluded.source,slug=excluded.slug,version=excluded.version,kind=excluded.kind,release_digest=excluded.release_digest`, b.RepoID, b.Source, b.Slug, b.Version, b.Kind, b.ReleaseDigest)
		changed = e == nil
		return e
	})
	if err != nil {
		return false, fmt.Errorf("store: replace repository team binding: %w", err)
	}
	return changed, nil
}

// DeleteRepositoryTeamBinding returns the removed selection and a no-op flag.
// The caller holds the same catalogue lock used by link and removal.
func (db *DB) DeleteRepositoryTeamBinding(ctx context.Context, repoID int64) (prior RepositoryTeamBinding, changed bool, err error) {
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		prior, e = scanRepositoryTeamBinding(tx.QueryRowContext(ctx, `SELECT `+repositoryTeamColumns+` FROM repository_team_bindings WHERE repo_id=?`, repoID))
		if errors.Is(e, ErrNotFound) {
			var id int64
			if e = tx.QueryRowContext(ctx, `SELECT id FROM repos WHERE id=?`, repoID).Scan(&id); errors.Is(e, sql.ErrNoRows) {
				return ErrNotFound
			}
			return e
		}
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `DELETE FROM repository_team_bindings WHERE repo_id=?`, repoID)
		changed = e == nil
		return e
	})
	if err != nil {
		return RepositoryTeamBinding{}, false, fmt.Errorf("store: delete repository team binding: %w", err)
	}
	return prior, changed, nil
}

// RepositoryTeamUses checks every binding for corruption and returns the paths
// using the exact source/name/version in stable order. Lookup failures fail closed.
func (db *DB) RepositoryTeamUses(ctx context.Context, source, slug, version string) ([]string, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT b.repo_id,b.source,b.slug,b.version,b.kind,b.release_digest,r.path FROM repository_team_bindings b LEFT JOIN repos r ON r.id=b.repo_id ORDER BY r.path`)
	if err != nil {
		return nil, fmt.Errorf("store: check repository team use: %w", err)
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var b RepositoryTeamBinding
		var path sql.NullString
		if err = rows.Scan(&b.RepoID, &b.Source, &b.Slug, &b.Version, &b.Kind, &b.ReleaseDigest, &path); err != nil {
			return nil, err
		}
		if err = validateRepositoryTeamBinding(b); err != nil {
			return nil, fmt.Errorf("store: invalid team binding for %s: %w", path.String, err)
		}
		if !path.Valid || path.String == "" {
			return nil, errors.New("store: repository team binding has no registered clone")
		}
		if b.Source == source && b.Slug == slug && b.Version == version {
			paths = append(paths, path.String)
		}
	}
	return paths, rows.Err()
}
