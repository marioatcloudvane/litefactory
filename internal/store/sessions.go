package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// SessionState is the M0 lifecycle of a task.
type SessionState string

// The session states. M0 has three; LF-11's `blocked` is deliberately absent.
const (
	// SessionIdle means no run is in flight.
	SessionIdle SessionState = "idle"
	// SessionRunning means a turn is executing.
	SessionRunning SessionState = "running"
	// SessionFailed means the last run failed (S-9). M0 does not gate on this;
	// it is recorded so `logs` can explain the task at a glance.
	SessionFailed SessionState = "failed"
)

// Session is one task litefactory has touched: an issue in a registered repo,
// plus the branch, worktree and lock that belong to it.
type Session struct {
	// ID is the surrogate key.
	ID int64
	// RepoID is the registered clone this task lives in.
	RepoID int64
	// IssueNumber is the forge issue number.
	IssueNumber int
	// TaskRef is the canonical reference, github:owner/name#123. It is unique.
	TaskRef string
	// FeatureSlug is derived on the first run and frozen (AC-3.6); "" until
	// then.
	FeatureSlug string
	// Branch is lf/<issue>-<slug>, frozen alongside the slug.
	Branch string
	// WorktreePath is the absolute worktree path; "" before the first run and
	// after `task clean` (AC-3.25).
	WorktreePath string
	// State is the M0 lifecycle state.
	State SessionState
	// LockOwner is the daemon instance id currently holding the per-task lock,
	// "" when the lock is free.
	LockOwner string
	// LockAcquiredAt is when the current holder took the lock.
	LockAcquiredAt time.Time
	// NeedsPoll marks a session the daemon owes another look at, independently
	// of anything the forge's timestamp-indexed listings can still see. See
	// [DB.SetSessionNeedsPoll].
	NeedsPoll bool
	// CreatedAt and UpdatedAt are bookkeeping.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TaskRef builds the canonical task reference, github:owner/name#123. It is
// the identity litefactory uses in comment markers, log paths and prompts, so
// there is exactly one function that spells it.
func TaskRef(forge, owner, name string, issueNumber int) string {
	return forge + ":" + owner + "/" + name + "#" + strconv.Itoa(issueNumber)
}

// GetOrCreateSession returns the session for an issue in repo, creating it on
// first sight.
//
// The task ref is derived here rather than passed in, so every session in the
// database carries the same canonical form. A session that already exists is
// returned untouched — including its task ref, which stays stable even if the
// repository is later renamed on the forge, because the branch and worktree it
// names already exist on disk.
//
// Creating a session does not bootstrap its watermark. That is a separate,
// deliberate step: see [DB.BootstrapWatermark] and AC-2.10.
func (db *DB) GetOrCreateSession(ctx context.Context, repo Repo, issueNumber int) (Session, error) {
	if repo.ID == 0 {
		return Session{}, errors.New("store: get or create session: repo is not registered")
	}

	var session Session
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		existing, err := scanSession(tx.QueryRowContext(ctx,
			`SELECT `+sessionColumns+` FROM sessions WHERE repo_id = ? AND issue_number = ?`,
			repo.ID, issueNumber))
		switch {
		case err == nil:
			session = existing
			return nil
		case !errors.Is(err, ErrNotFound):
			return err
		}

		now := FormatTime(nowUTC())
		created, err := scanSession(tx.QueryRowContext(ctx, `
INSERT INTO sessions (repo_id, issue_number, task_ref, state, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING `+sessionColumns,
			repo.ID, issueNumber, TaskRef(repo.Forge, repo.Owner, repo.Name, issueNumber),
			string(SessionIdle), now, now))
		if err != nil {
			return err
		}
		session = created
		return nil
	})
	if err != nil {
		return Session{}, fmt.Errorf("store: get or create session for %s#%d: %w", repo.Slug(), issueNumber, err)
	}
	return session, nil
}

// GetSession reads a session by id, returning [ErrNotFound] when there is none.
func (db *DB) GetSession(ctx context.Context, id int64) (Session, error) {
	session, err := scanSession(db.sql.QueryRowContext(ctx,
		`SELECT `+sessionColumns+` FROM sessions WHERE id = ?`, id))
	if err != nil {
		return Session{}, fmt.Errorf("store: read session %d: %w", id, err)
	}
	return session, nil
}

// GetSessionByTaskRef reads a session by its canonical task ref, returning
// [ErrNotFound] when the task has never been seen.
func (db *DB) GetSessionByTaskRef(ctx context.Context, taskRef string) (Session, error) {
	session, err := scanSession(db.sql.QueryRowContext(ctx,
		`SELECT `+sessionColumns+` FROM sessions WHERE task_ref = ?`, taskRef))
	if err != nil {
		return Session{}, fmt.Errorf("store: read session %s: %w", taskRef, err)
	}
	return session, nil
}

// FindSessionByIssue resolves a bare issue number, the way `litefactory logs
// 123` is invoked.
//
// When owner and name are both non-empty they narrow the search to that
// repository, which is what `--repo owner/name` does. Otherwise every
// registered repository is searched, and a number that matches more than one
// returns an [*AmbiguousIssueError] carrying the candidates to print (AC-5.2).
// No match returns [ErrNotFound] (AC-5.3).
func (db *DB) FindSessionByIssue(ctx context.Context, issueNumber int, owner, name string) (Session, error) {
	query := `
SELECT ` + prefixedSessionColumns + `, repos.forge, repos.owner, repos.name
FROM sessions
JOIN repos ON repos.id = sessions.repo_id
WHERE sessions.issue_number = ?`
	args := []any{issueNumber}
	if owner != "" && name != "" {
		query += ` AND repos.owner = ? AND repos.name = ?`
		args = append(args, owner, name)
	}
	query += ` ORDER BY repos.owner, repos.name`

	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return Session{}, fmt.Errorf("store: find session for issue %d: %w", issueNumber, err)
	}
	defer rows.Close()

	var (
		sessions   []Session
		candidates []IssueCandidate
	)
	for rows.Next() {
		var forge, repoOwner, repoName string
		session, err := scanSessionWith(rows, &forge, &repoOwner, &repoName)
		if err != nil {
			return Session{}, fmt.Errorf("store: find session for issue %d: %w", issueNumber, err)
		}
		sessions = append(sessions, session)
		candidates = append(candidates, IssueCandidate{
			RepoID:  session.RepoID,
			Forge:   forge,
			Owner:   repoOwner,
			Name:    repoName,
			TaskRef: session.TaskRef,
		})
	}
	if err := rows.Err(); err != nil {
		return Session{}, fmt.Errorf("store: find session for issue %d: %w", issueNumber, err)
	}

	switch len(sessions) {
	case 0:
		return Session{}, fmt.Errorf("store: no task for issue %d: %w", issueNumber, ErrNotFound)
	case 1:
		return sessions[0], nil
	default:
		return Session{}, &AmbiguousIssueError{IssueNumber: issueNumber, Candidates: candidates}
	}
}

// FreezeSessionSlug records the feature slug and branch on the first run and
// leaves them alone on every run after that (AC-3.6), so the branch a task
// works on never moves under it. The session as stored — with whichever slug
// won — is returned, and that is the value the caller must use.
func (db *DB) FreezeSessionSlug(ctx context.Context, sessionID int64, slug, branch string) (Session, error) {
	if slug == "" || branch == "" {
		return Session{}, errors.New("store: freeze session slug: slug and branch are required")
	}

	var session Session
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
UPDATE sessions
SET feature_slug = ?, branch = ?, updated_at = ?
WHERE id = ? AND (feature_slug IS NULL OR feature_slug = '')`,
			slug, branch, FormatTime(nowUTC()), sessionID)
		if err != nil {
			return err
		}
		session, err = scanSession(tx.QueryRowContext(ctx,
			`SELECT `+sessionColumns+` FROM sessions WHERE id = ?`, sessionID))
		return err
	})
	if err != nil {
		return Session{}, fmt.Errorf("store: freeze slug on session %d: %w", sessionID, err)
	}
	return session, nil
}

// SetSessionWorktree records the worktree path for a task. Passing "" clears
// it, which is what `task clean` does after removing the worktree (AC-3.25).
func (db *DB) SetSessionWorktree(ctx context.Context, sessionID int64, worktreePath string) error {
	res, err := db.sql.ExecContext(ctx,
		`UPDATE sessions SET worktree_path = ?, updated_at = ? WHERE id = ?`,
		nullString(worktreePath), FormatTime(nowUTC()), sessionID)
	if err != nil {
		return fmt.Errorf("store: set worktree on session %d: %w", sessionID, err)
	}
	return requireAffected(res, fmt.Sprintf("store: set worktree on session %d", sessionID))
}

// SetSessionState moves a session between the M0 states.
func (db *DB) SetSessionState(ctx context.Context, sessionID int64, state SessionState) error {
	if !state.valid() {
		return fmt.Errorf("store: set session state: unknown state %q", state)
	}
	res, err := db.sql.ExecContext(ctx,
		`UPDATE sessions SET state = ?, updated_at = ? WHERE id = ?`,
		string(state), FormatTime(nowUTC()), sessionID)
	if err != nil {
		return fmt.Errorf("store: set state on session %d: %w", sessionID, err)
	}
	return requireAffected(res, fmt.Sprintf("store: set state on session %d", sessionID))
}

func (s SessionState) valid() bool {
	switch s {
	case SessionIdle, SessionRunning, SessionFailed:
		return true
	default:
		return false
	}
}

// AcquireSessionLock claims the per-task lock for instanceID, reporting
// whether it got it.
//
// The claim is a single conditional UPDATE, so two daemons racing for the same
// task cannot both win: at most one UPDATE matches the free row. A caller that
// is refused must leave the triggering comment unconsumed and retry on a later
// tick (AC-3.1) — there is no queue at M0.
//
// A lock left behind by a daemon that was killed is not stolen here; it is
// cleared at startup by [DB.ReconcileOrphans] (AC-3.2).
func (db *DB) AcquireSessionLock(ctx context.Context, sessionID int64, instanceID string) (bool, error) {
	if instanceID == "" {
		return false, errors.New("store: acquire session lock: instance id is required")
	}
	now := nowUTC()
	res, err := db.sql.ExecContext(ctx, `
UPDATE sessions
SET lock_owner = ?, lock_acquired_at = ?, updated_at = ?
WHERE id = ? AND lock_owner IS NULL`,
		instanceID, FormatTime(now), FormatTime(now), sessionID)
	if err != nil {
		return false, fmt.Errorf("store: acquire lock on session %d: %w", sessionID, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: acquire lock on session %d: %w", sessionID, err)
	}
	if affected == 0 {
		// Either another instance holds it, or the session does not exist.
		// Distinguish the two, so a bad session id is never read as "busy".
		if _, err := db.GetSession(ctx, sessionID); err != nil {
			return false, err
		}
		return false, nil
	}
	return true, nil
}

// ReleaseSessionLock gives up the lock, but only if instanceID still holds it.
//
// Releasing a lock that is free, or that another instance has since reclaimed,
// is a no-op and not an error: this call belongs in a defer that has to run on
// every exit path including a panic (AC-3.3), and a defer that can fail
// spuriously is a defer people start ignoring.
func (db *DB) ReleaseSessionLock(ctx context.Context, sessionID int64, instanceID string) error {
	if instanceID == "" {
		return errors.New("store: release session lock: instance id is required")
	}
	_, err := db.sql.ExecContext(ctx, `
UPDATE sessions
SET lock_owner = NULL, lock_acquired_at = NULL, updated_at = ?
WHERE id = ? AND lock_owner = ?`,
		FormatTime(nowUTC()), sessionID, instanceID)
	if err != nil {
		return fmt.Errorf("store: release lock on session %d: %w", sessionID, err)
	}
	return nil
}

// SetSessionNeedsPoll marks a session as one the daemon still owes a look at,
// whatever the forge's listings can still see.
//
// A concurrent tick cannot hold the poll cursor back to cover work it has
// handed to a worker — holding it for one busy session pins discovery for
// every other repository (AC-PAR-5.1). Nor can the cursor re-find the work
// later: both forge listings are timestamp-indexed on timestamps that do not
// move while a run is in flight (the issue's updated_at, the comment's
// created_at), so once the cursor passes T neither ever re-finds a comment
// written at T (AC-PAR-5.2). This flag is that re-discovery axis, and it is in
// the database rather than in memory because a restart must not be able to
// lose it after the cursor has already moved on — that would be permanent
// silence on the thread, which AC-4.1 forbids.
//
// The protocol has one write site and one clear rule: the tick sets the flag
// for every session it takes out of the eligible set — the ones it dispatches,
// before the worker starts, and the ones it defers for want of a free slot —
// and only
// [DB.ClearSessionNeedsPoll] on a fully drained, error-free turn takes it back
// off. Setting it on a session that already carries it is a no-op, so the call
// needs no read first. A session that does not exist is [ErrNotFound].
func (db *DB) SetSessionNeedsPoll(ctx context.Context, sessionID int64) error {
	return db.setSessionNeedsPoll(ctx, sessionID, true)
}

// ClearSessionNeedsPoll takes the backlog flag off a session.
//
// Only a fully drained, error-free turn may call this. Every other exit —
// a killed daemon, a worker abandoned at shutdown, a drain that stopped on an
// unconsumed comment, a lock lost to a second process, an error mid-turn —
// must leave the flag standing, because a stale-true flag is self-healing (the
// next tick re-lists the session, finds nothing above its watermark and clears
// it) while a stale-false flag is silence. That asymmetry is the same posture
// as [DB.AdvanceWatermark]'s never-rewind: a bad outcome leaves the row
// pointing at more work, never less.
//
// Clearing a session that is not flagged is not an error; the row still
// matches. A session that does not exist is [ErrNotFound].
func (db *DB) ClearSessionNeedsPoll(ctx context.Context, sessionID int64) error {
	return db.setSessionNeedsPoll(ctx, sessionID, false)
}

func (db *DB) setSessionNeedsPoll(ctx context.Context, sessionID int64, needsPoll bool) error {
	verb := "clear"
	if needsPoll {
		verb = "set"
	}
	res, err := db.sql.ExecContext(ctx,
		`UPDATE sessions SET needs_poll = ?, updated_at = ? WHERE id = ?`,
		needsPoll, FormatTime(nowUTC()), sessionID)
	if err != nil {
		return fmt.Errorf("store: %s needs_poll on session %d: %w", verb, sessionID, err)
	}
	return requireAffected(res, fmt.Sprintf("store: %s needs_poll on session %d", verb, sessionID))
}

// ListBacklogSessions returns every session still carrying the backlog flag,
// ordered by id.
//
// This is the local half of discovery, and it is deliberately cross-repo: the
// backlog is a property of the session set as a whole, not of whichever
// repository the tick happens to be listing, which is what keeps one busy repo
// from starving another. It reads a partial index covering exactly the flagged
// rows, so it costs the size of the backlog rather than the size of the table
// and — unlike the forge listings it is unioned with — costs no API quota at
// all.
//
// It returns sessions, not issues. The caller fetches the forge issue for a
// backlogged session lazily, only once the dispatcher has actually selected it
// to run, which caps the extra API cost at max_concurrent_runs issue fetches
// per tick however deep the backlog has grown. Fetching eagerly for every row
// this returns would make that cost grow with backlog depth, which is the
// shape this bound exists to rule out.
func (db *DB) ListBacklogSessions(ctx context.Context) ([]Session, error) {
	rows, err := db.sql.QueryContext(ctx,
		`SELECT `+sessionColumns+` FROM sessions WHERE needs_poll = 1 ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list backlog sessions: %w", err)
	}
	defer rows.Close()

	var out []Session
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list backlog sessions: %w", err)
		}
		out = append(out, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list backlog sessions: %w", err)
	}
	return out, nil
}

// sessionColumns is the column list every session read shares, in the order
// scanSession expects.
const sessionColumns = `
    id, repo_id, issue_number, task_ref, feature_slug, branch, worktree_path,
    state, lock_owner, lock_acquired_at, created_at, updated_at, needs_poll`

// prefixedSessionColumns is sessionColumns qualified for a join.
const prefixedSessionColumns = `
    sessions.id, sessions.repo_id, sessions.issue_number, sessions.task_ref,
    sessions.feature_slug, sessions.branch, sessions.worktree_path,
    sessions.state, sessions.lock_owner, sessions.lock_acquired_at,
    sessions.created_at, sessions.updated_at, sessions.needs_poll`

func scanSession(s scanner) (Session, error) {
	return scanSessionWith(s)
}

// scanSessionWith scans a session row plus any extra destinations appended to
// the select list, which is how the issue lookup reads repo columns in the
// same pass.
func scanSessionWith(s scanner, extra ...any) (Session, error) {
	var (
		session      Session
		state        string
		slug         sql.NullString
		branch       sql.NullString
		worktreePath sql.NullString
		lockOwner    sql.NullString
		lockAt       sql.NullString
		createdAt    sql.NullString
		updatedAt    sql.NullString
	)
	dest := []any{
		&session.ID, &session.RepoID, &session.IssueNumber, &session.TaskRef,
		&slug, &branch, &worktreePath, &state, &lockOwner, &lockAt,
		&createdAt, &updatedAt, &session.NeedsPoll,
	}
	dest = append(dest, extra...)

	if err := s.Scan(dest...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Session{}, ErrNotFound
		}
		return Session{}, err
	}
	session.State = SessionState(state)
	session.FeatureSlug = slug.String
	session.Branch = branch.String
	session.WorktreePath = worktreePath.String
	session.LockOwner = lockOwner.String

	for _, field := range []struct {
		src sql.NullString
		dst *time.Time
	}{
		{lockAt, &session.LockAcquiredAt},
		{createdAt, &session.CreatedAt},
		{updatedAt, &session.UpdatedAt},
	} {
		t, err := scanTime(field.src)
		if err != nil {
			return Session{}, err
		}
		*field.dst = t
	}
	return session, nil
}
