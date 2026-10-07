package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// States and sources of a Validate entry.
const (
	ValidateEntryRequested = "requested"
	ValidateEntryAdmitted  = "admitted"
	ValidateEntryRefused   = "refused"

	ValidateSourceOwner = "owner"
	ValidateSourceAuto  = "auto"
)

// ValidateEntry is one request to start Validate for one Implement result.
type ValidateEntry struct {
	ID         int64
	SessionID  int64
	Commit     string
	Branch     string
	Source     string
	CommentID  int64
	AuthorID   int64
	Note       string
	State      string
	Reason     string
	InstanceID int64
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

const validateEntryColumns = `id,session_id,commit_sha,branch,source,comment_id,author_id,note,state,reason,instance_id,created_at,updated_at`

func scanValidateEntry(row interface{ Scan(...any) error }) (ValidateEntry, error) {
	var e ValidateEntry
	var created, updated string
	err := row.Scan(&e.ID, &e.SessionID, &e.Commit, &e.Branch, &e.Source, &e.CommentID, &e.AuthorID, &e.Note, &e.State, &e.Reason, &e.InstanceID, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	if err != nil {
		return e, err
	}
	if e.CreatedAt, err = ParseTime(created); err != nil {
		return e, err
	}
	e.UpdatedAt, err = ParseTime(updated)
	return e, err
}

// EnsureValidateEntry saves a start request before admission is attempted and
// returns the entry for its key. A key that already has an entry keeps it
// unchanged, except that a refused entry is reopened as requested by a new
// start. created is true when this call wrote or reopened the entry.
func (db *DB) EnsureValidateEntry(ctx context.Context, in ValidateEntry, at time.Time) (entry ValidateEntry, created bool, err error) {
	if in.SessionID < 1 || (len(in.Commit) != 40 && len(in.Commit) != 64) || strings.TrimSpace(in.Branch) == "" || at.IsZero() ||
		(in.Source != ValidateSourceOwner && in.Source != ValidateSourceAuto) || (in.Source == ValidateSourceOwner && in.CommentID < 1) {
		return entry, false, errors.New("store: invalid validate entry")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		stamp := FormatTime(at)
		res, e := tx.ExecContext(ctx, `INSERT OR IGNORE INTO validate_entries(session_id,commit_sha,branch,source,comment_id,author_id,note,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,'requested',?,?)`,
			in.SessionID, in.Commit, in.Branch, in.Source, in.CommentID, in.AuthorID, in.Note, stamp, stamp)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		created = n > 0
		entry, e = scanValidateEntry(tx.QueryRowContext(ctx, `SELECT `+validateEntryColumns+` FROM validate_entries WHERE session_id=? AND commit_sha=?`, in.SessionID, in.Commit))
		if e != nil {
			return e
		}
		if entry.State != ValidateEntryRefused {
			return nil
		}
		if _, e = tx.ExecContext(ctx, `UPDATE validate_entries SET state='requested',reason='',branch=?,source=?,comment_id=?,author_id=?,note=?,updated_at=? WHERE id=? AND state='refused'`,
			in.Branch, in.Source, in.CommentID, in.AuthorID, in.Note, stamp, entry.ID); e != nil {
			return e
		}
		created = true
		entry, e = scanValidateEntry(tx.QueryRowContext(ctx, `SELECT `+validateEntryColumns+` FROM validate_entries WHERE id=?`, entry.ID))
		return e
	})
	if err != nil {
		return ValidateEntry{}, false, fmt.Errorf("store: ensure validate entry: %w", err)
	}
	return entry, created, nil
}

// GetValidateEntry reads the entry of one parent session and commit.
func (db *DB) GetValidateEntry(ctx context.Context, sessionID int64, commit string) (ValidateEntry, error) {
	return scanValidateEntry(db.sql.QueryRowContext(ctx, `SELECT `+validateEntryColumns+` FROM validate_entries WHERE session_id=? AND commit_sha=?`, sessionID, commit))
}

// ListValidateEntries lists the entries of one parent session, oldest first.
func (db *DB) ListValidateEntries(ctx context.Context, sessionID int64) ([]ValidateEntry, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+validateEntryColumns+` FROM validate_entries WHERE session_id=? ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ValidateEntry
	for rows.Next() {
		e, err := scanValidateEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// RefuseValidateEntry records why a start was refused. An admitted entry is
// never refused.
func (db *DB) RefuseValidateEntry(ctx context.Context, id int64, reason string, at time.Time) error {
	reason = strings.TrimSpace(reason)
	if id < 1 || reason == "" || at.IsZero() {
		return errors.New("store: invalid validate entry refusal")
	}
	res, err := db.sql.ExecContext(ctx, `UPDATE validate_entries SET state='refused',reason=?,updated_at=? WHERE id=? AND state IN ('requested','refused')`, reason, FormatTime(at), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AdmitValidateEntry records the Validate instance of an entry. Repeating it
// with the same instance changes nothing; a different instance is refused.
func (db *DB) AdmitValidateEntry(ctx context.Context, id, instanceID int64, at time.Time) error {
	if id < 1 || instanceID < 1 || at.IsZero() {
		return errors.New("store: invalid validate entry admission")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		e, err := scanValidateEntry(tx.QueryRowContext(ctx, `SELECT `+validateEntryColumns+` FROM validate_entries WHERE id=?`, id))
		if err != nil {
			return err
		}
		if e.State == ValidateEntryAdmitted {
			if e.InstanceID != instanceID {
				return fmt.Errorf("store: validate entry %d is already admitted to instance %d", id, e.InstanceID)
			}
			return nil
		}
		_, err = tx.ExecContext(ctx, `UPDATE validate_entries SET state='admitted',reason='',instance_id=?,updated_at=? WHERE id=?`, instanceID, FormatTime(at), id)
		return err
	})
}

// ListFinishedImplementParents lists the managed parent sessions whose latest
// Implement coordinator instance reached its finished terminal. Story workers
// have Implement instances of their own and are not parents.
func (db *DB) ListFinishedImplementParents(ctx context.Context) ([]int64, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT i.session_id FROM graph_instances i
JOIN graph_definitions d ON d.id=i.definition_id AND d.mode='implement'
JOIN sessions s ON s.id=i.session_id
WHERE i.status=? AND s.task_ref IN (SELECT parent_task FROM managed_features)
 AND s.task_ref NOT IN (SELECT parent_task FROM managed_parent_closures)
 AND i.id=(SELECT MAX(i2.id) FROM graph_instances i2 JOIN graph_definitions d2 ON d2.id=i2.definition_id AND d2.mode='implement' WHERE i2.session_id=i.session_id)
ORDER BY i.session_id`, GraphSucceeded)
	if err != nil {
		return nil, fmt.Errorf("store: list finished implement parents: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ValidateLaunchedBy reports whether the trigger already launched the instance,
// so a replay of the very start that admitted it can be told from a new start.
func (db *DB) ValidateLaunchedBy(ctx context.Context, sessionID, triggerID, instanceID int64) (bool, error) {
	var n int
	err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM research_launch_definitions WHERE session_id=? AND trigger_id=? AND instance_id=?`, sessionID, triggerID, instanceID).Scan(&n)
	return n > 0, err
}

// ReopenValidateEntry lets an explicit owner start run again on an entry whose
// Validate instance ended without finishing (failed or cancelled). The entry is
// requested again with the new start's source; the next admission links a
// successor to the ended instance. An entry in any other state is unchanged.
func (db *DB) ReopenValidateEntry(ctx context.Context, in ValidateEntry, at time.Time) (ValidateEntry, error) {
	if in.ID < 1 || in.Source != ValidateSourceOwner || in.CommentID < 1 || at.IsZero() {
		return ValidateEntry{}, errors.New("store: invalid validate entry reopen")
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `UPDATE validate_entries SET state='requested',reason='',instance_id=0,source=?,comment_id=?,author_id=?,note=?,updated_at=?
WHERE id=? AND state='admitted' AND instance_id IN (SELECT id FROM graph_instances WHERE status IN ('failed','cancelled'))`,
			in.Source, in.CommentID, in.AuthorID, in.Note, FormatTime(at), in.ID)
		return e
	})
	if err != nil {
		return ValidateEntry{}, fmt.Errorf("store: reopen validate entry: %w", err)
	}
	return scanValidateEntry(db.sql.QueryRowContext(ctx, `SELECT `+validateEntryColumns+` FROM validate_entries WHERE id=?`, in.ID))
}
