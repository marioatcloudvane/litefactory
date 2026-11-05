package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ErrBaseChangeViaAdd is returned by [DB.RegisterRepo] when a registration asks
// for a base branch other than the stored one. `repo base` is the only way.
var ErrBaseChangeViaAdd = errors.New("store: base branch of a registered repository can only change with repo base")

// BaseChangeViaAddError is the [ErrBaseChangeViaAdd] refusal with the base
// that was stored when the guarded write re-read the row.
type BaseChangeViaAddError struct{ Stored string }

func (e *BaseChangeViaAddError) Error() string {
	return fmt.Sprintf("%v (stored %q)", ErrBaseChangeViaAdd, e.Stored)
}

// Is reports the refusal as [ErrBaseChangeViaAdd].
func (e *BaseChangeViaAddError) Is(target error) bool { return target == ErrBaseChangeViaAdd }

// OpenWorkItem is one piece of executing work that blocks a base change.
type OpenWorkItem struct {
	// Issue is the task reference of the session the work belongs to.
	Issue string
	// Kind is "run", "step" or "queued step".
	Kind string
}

// OpenWorkError refuses a base change while work is executing.
type OpenWorkError struct{ Items []OpenWorkItem }

func (e *OpenWorkError) Error() string {
	parts := make([]string, len(e.Items))
	for i, item := range e.Items {
		parts[i] = item.Kind + " of " + item.Issue
	}
	return "store: work is executing: " + strings.Join(parts, ", ")
}

// RegisterRepo is the guarded registration write. In one transaction it
// re-reads the row by path: a new path is inserted; an existing path keeps the
// stored base when explicitBase is false and refuses a different explicit base
// with a [*BaseChangeViaAddError] naming the stored base, writing nothing. LastFetchAt is never touched.
func (db *DB) RegisterRepo(ctx context.Context, r Repo, explicitBase bool) (Repo, error) {
	if r.Path == "" {
		return Repo{}, errors.New("store: register repo: path is required")
	}
	var stored Repo
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		current, err := scanRepo(tx.QueryRowContext(ctx, `SELECT `+repoColumns+` FROM repos WHERE path=?`, r.Path))
		switch {
		case errors.Is(err, ErrNotFound):
		case err != nil:
			return err
		case explicitBase && r.BaseBranch != current.BaseBranch:
			return &BaseChangeViaAddError{Stored: current.BaseBranch}
		default:
			r.BaseBranch = current.BaseBranch
		}
		now := FormatTime(nowUTC())
		stored, err = scanRepo(tx.QueryRowContext(ctx, `
INSERT INTO repos (path, forge, owner, name, remote_url, base_branch, feature_prefix, worktree_root, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (path) DO UPDATE SET
    forge=excluded.forge, owner=excluded.owner, name=excluded.name, remote_url=excluded.remote_url,
    base_branch=excluded.base_branch, feature_prefix=excluded.feature_prefix,
    worktree_root=excluded.worktree_root, updated_at=excluded.updated_at
RETURNING `+repoColumns,
			r.Path, r.Forge, r.Owner, r.Name, r.RemoteURL, r.BaseBranch, r.FeaturePrefix, r.WorktreeRoot, now, now))
		return err
	})
	if err != nil {
		if errors.Is(err, ErrBaseChangeViaAdd) {
			return Repo{}, err
		}
		return Repo{}, fmt.Errorf("store: register repo %s: %w", r.Path, err)
	}
	return stored, nil
}

// RepoBaseChange reports what [DB.ChangeRepoBase] found and wrote.
type RepoBaseChange struct {
	Repo Repo
	// OldRecordBase and OldConnectionBase are the bases before the change; the
	// connection one is empty when there was no current code connection.
	OldRecordBase, OldConnectionBase string
	// NoChange is true when record and connection already had the base.
	NoChange bool
	// RecordWritten and ConnectionWritten name the sides that were stale.
	RecordWritten, ConnectionWritten bool
	// PinnedSessions counts unpinned sessions that kept their old base.
	PinnedSessions int
}

// ChangeRepoBase changes the base branch of a repository in one transaction:
// open-work check, legacy base pins, record update, new current code-connection
// revision. Any failure leaves nothing changed.
func (db *DB) ChangeRepoBase(ctx context.Context, repoID int64, base string) (RepoBaseChange, error) {
	var out RepoBaseChange
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		repo, err := scanRepo(tx.QueryRowContext(ctx, `SELECT `+repoColumns+` FROM repos WHERE id=?`, repoID))
		if err != nil {
			return err
		}
		out.OldRecordBase = repo.BaseBranch
		code, err := currentConnection(ctx, tx, repoID, CodeConnection)
		haveCode := err == nil
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		out.OldConnectionBase = code.BaseBranch
		out.RecordWritten = repo.BaseBranch != base
		out.ConnectionWritten = !haveCode || code.BaseBranch != base
		if !out.RecordWritten && !out.ConnectionWritten {
			out.NoChange = true
			out.Repo = repo
			return nil
		}
		if items, err := openWork(ctx, tx, repoID); err != nil {
			return err
		} else if len(items) > 0 {
			return &OpenWorkError{Items: items}
		}
		now := FormatTime(nowUTC())
		res, err := tx.ExecContext(ctx, `
INSERT INTO session_legacy_base_pins(session_id, repo_id, base_branch, created_at)
SELECT s.id, s.repo_id, ?, ? FROM sessions s
WHERE s.repo_id=?
  AND NOT EXISTS (SELECT 1 FROM session_execution_snapshots x WHERE x.session_id=s.id)
  AND NOT EXISTS (SELECT 1 FROM session_legacy_base_pins p WHERE p.session_id=s.id)`,
			repo.BaseBranch, now, repoID)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err == nil {
			out.PinnedSessions = int(n)
		}
		if out.RecordWritten {
			if _, err = tx.ExecContext(ctx, `UPDATE repos SET base_branch=?, updated_at=? WHERE id=?`, base, now, repoID); err != nil {
				return err
			}
			repo.BaseBranch = base
		}
		if out.ConnectionWritten {
			if !haveCode {
				return errors.New("store: repository has no code connection to update")
			}
			v := code
			v.BaseBranch = base
			if _, err = setConnection(ctx, tx, v, code.Revision); err != nil {
				return err
			}
		}
		out.Repo, err = scanRepo(tx.QueryRowContext(ctx, `SELECT `+repoColumns+` FROM repos WHERE id=?`, repoID))
		return err
	})
	if err != nil {
		var open *OpenWorkError
		if errors.As(err, &open) {
			return RepoBaseChange{}, err
		}
		return RepoBaseChange{}, fmt.Errorf("store: change base of repo %d: %w", repoID, err)
	}
	return out, nil
}

func openWork(ctx context.Context, tx *sql.Tx, repoID int64) ([]OpenWorkItem, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT s.task_ref, 'run' FROM runs r JOIN sessions s ON s.id=r.session_id WHERE s.repo_id=? AND r.status IN ('prelaunch','running')
UNION ALL
SELECT s.task_ref, 'step' FROM graph_node_attempts a JOIN graph_instances i ON i.id=a.instance_id JOIN sessions s ON s.id=i.session_id WHERE s.repo_id=? AND a.status IN ('pending','running')
UNION ALL
SELECT s.task_ref, 'queued step' FROM graph_dispatches d JOIN sessions s ON s.id=d.session_id WHERE s.repo_id=? AND d.status IN ('pending','reserved','running')
ORDER BY 1, 2`, repoID, repoID, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []OpenWorkItem
	for rows.Next() {
		var it OpenWorkItem
		if err := rows.Scan(&it.Issue, &it.Kind); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}
