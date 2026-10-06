package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrRightsDuplicate is returned when an active grant or an open reviewer
// assignment with the same scope already exists.
var ErrRightsDuplicate = errors.New("store: an active record with this scope already exists")

// RightsGrant is one delegated right. Rows are never deleted; a revoked grant
// keeps its row with RevokedAt set.
type RightsGrant struct {
	ID           int64     `json:"id"`
	RepoID       int64     `json:"repo_id"`
	SubjectID    int64     `json:"subject_id"`
	SubjectLogin string    `json:"subject_login"`
	Workflow     string    `json:"workflow"`
	Stage        string    `json:"stage"`
	Action       string    `json:"action"`
	TaskRef      string    `json:"task_ref,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitzero"`
	GrantedBy    int64     `json:"granted_by"`
	CreatedAt    time.Time `json:"created_at"`
	RevokedAt    time.Time `json:"revoked_at,omitzero"`
	RevokedBy    int64     `json:"revoked_by,omitempty"`
}

// ReviewerAssignment asks one reviewer to review a task at a stage. It grants
// no right.
type ReviewerAssignment struct {
	ID            int64     `json:"id"`
	RepoID        int64     `json:"repo_id"`
	TaskRef       string    `json:"task_ref"`
	Workflow      string    `json:"workflow"`
	Stage         string    `json:"stage"`
	ReviewerID    int64     `json:"reviewer_id"`
	ReviewerLogin string    `json:"reviewer_login"`
	AssignedBy    int64     `json:"assigned_by"`
	CreatedAt     time.Time `json:"created_at"`
	RemovedAt     time.Time `json:"removed_at,omitzero"`
	RemovedBy     int64     `json:"removed_by,omitempty"`
}

// RightsHistoryEntry is one append-only history row.
type RightsHistoryEntry struct {
	Sequence int64
	RepoID   int64
	At       time.Time
	ActorID  int64
	Kind     string
	RecordID int64
	Snapshot string
}

const grantColumns = `id,repo_id,subject_id,subject_login,workflow,stage,action,task_ref,expires_at,granted_by,created_at,revoked_at,revoked_by`
const assignmentColumns = `id,repo_id,task_ref,workflow,stage,reviewer_id,reviewer_login,assigned_by,created_at,removed_at,removed_by`

type rowScanner interface{ Scan(...any) error }

func scanGrant(r rowScanner) (RightsGrant, error) {
	var g RightsGrant
	var expires, created, revoked sql.NullString
	var revokedBy sql.NullInt64
	if err := r.Scan(&g.ID, &g.RepoID, &g.SubjectID, &g.SubjectLogin, &g.Workflow, &g.Stage, &g.Action, &g.TaskRef, &expires, &g.GrantedBy, &created, &revoked, &revokedBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RightsGrant{}, ErrNotFound
		}
		return RightsGrant{}, err
	}
	var err error
	if g.ExpiresAt, err = scanTime(expires); err != nil {
		return RightsGrant{}, err
	}
	if g.CreatedAt, err = scanTime(created); err != nil {
		return RightsGrant{}, err
	}
	if g.RevokedAt, err = scanTime(revoked); err != nil {
		return RightsGrant{}, err
	}
	g.RevokedBy = scanID(revokedBy)
	return g, nil
}

func scanAssignment(r rowScanner) (ReviewerAssignment, error) {
	var a ReviewerAssignment
	var created, removed sql.NullString
	var removedBy sql.NullInt64
	if err := r.Scan(&a.ID, &a.RepoID, &a.TaskRef, &a.Workflow, &a.Stage, &a.ReviewerID, &a.ReviewerLogin, &a.AssignedBy, &created, &removed, &removedBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ReviewerAssignment{}, ErrNotFound
		}
		return ReviewerAssignment{}, err
	}
	var err error
	if a.CreatedAt, err = scanTime(created); err != nil {
		return ReviewerAssignment{}, err
	}
	if a.RemovedAt, err = scanTime(removed); err != nil {
		return ReviewerAssignment{}, err
	}
	a.RemovedBy = scanID(removedBy)
	return a, nil
}

func appendRightsHistory(ctx context.Context, tx *sql.Tx, repoID int64, at time.Time, actor int64, kind string, recordID int64, snapshot any) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode rights snapshot: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO rights_history(repo_id,at,actor_id,kind,record_id,snapshot_json) VALUES(?,?,?,?,?,?)`,
		repoID, FormatTime(at), actor, kind, recordID, string(data))
	if err != nil {
		return fmt.Errorf("append rights history: %w", err)
	}
	return nil
}

func requireRepo(ctx context.Context, tx *sql.Tx, repoID int64) error {
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM repos WHERE id=?`, repoID).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("store: repo %d: %w", repoID, ErrNotFound)
		}
		return err
	}
	return nil
}

func insertGrant(ctx context.Context, tx *sql.Tx, g RightsGrant) (RightsGrant, error) {
	res, err := tx.ExecContext(ctx, `INSERT INTO rights_grants(repo_id,subject_id,subject_login,workflow,stage,action,task_ref,expires_at,granted_by,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		g.RepoID, g.SubjectID, g.SubjectLogin, g.Workflow, g.Stage, g.Action, g.TaskRef, nullTime(g.ExpiresAt), g.GrantedBy, FormatTime(g.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return RightsGrant{}, ErrRightsDuplicate
		}
		return RightsGrant{}, fmt.Errorf("insert rights grant: %w", err)
	}
	if g.ID, err = res.LastInsertId(); err != nil {
		return RightsGrant{}, err
	}
	return scanGrant(tx.QueryRowContext(ctx, `SELECT `+grantColumns+` FROM rights_grants WHERE id=?`, g.ID))
}

// CreateRightsGrant stores a grant and its history row in one transaction.
// GrantedBy is the actor and CreatedAt the change time.
func (db *DB) CreateRightsGrant(ctx context.Context, g RightsGrant) (out RightsGrant, err error) {
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		if err := requireRepo(ctx, tx, g.RepoID); err != nil {
			return err
		}
		stored, err := insertGrant(ctx, tx, g)
		if err != nil {
			return err
		}
		out = stored
		return appendRightsHistory(ctx, tx, g.RepoID, g.CreatedAt, g.GrantedBy, "grant", stored.ID, stored)
	})
	if err != nil {
		return RightsGrant{}, fmt.Errorf("store: create rights grant: %w", err)
	}
	return out, nil
}

// RevokeRightsGrant closes an active grant and appends its history row. It
// returns ErrNotFound when the grant is unknown, in another repository or
// already revoked.
func (db *DB) RevokeRightsGrant(ctx context.Context, repoID, id, actor int64, at time.Time) (out RightsGrant, err error) {
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE rights_grants SET revoked_at=?,revoked_by=? WHERE id=? AND repo_id=? AND revoked_at IS NULL`, FormatTime(at), actor, id, repoID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		if out, err = scanGrant(tx.QueryRowContext(ctx, `SELECT `+grantColumns+` FROM rights_grants WHERE id=?`, id)); err != nil {
			return err
		}
		return appendRightsHistory(ctx, tx, repoID, at, actor, "revoke", id, out)
	})
	if err != nil {
		return RightsGrant{}, fmt.Errorf("store: revoke rights grant %d: %w", id, err)
	}
	return out, nil
}

// ListRightsGrants lists a repository's grants by id; activeOnly drops revoked
// ones. Expiry is not applied here: it belongs to the check.
func (db *DB) ListRightsGrants(ctx context.Context, repoID int64, activeOnly bool) ([]RightsGrant, error) {
	q := `SELECT ` + grantColumns + ` FROM rights_grants WHERE repo_id=?`
	if activeOnly {
		q += ` AND revoked_at IS NULL`
	}
	rows, err := db.sql.QueryContext(ctx, q+` ORDER BY id`, repoID)
	if err != nil {
		return nil, fmt.Errorf("store: list rights grants: %w", err)
	}
	defer rows.Close()
	var out []RightsGrant
	for rows.Next() {
		g, err := scanGrant(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list rights grants: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// MatchingRightsGrants returns the unrevoked grants of one person for one
// exact workflow, stage and action, in any task scope.
func (db *DB) MatchingRightsGrants(ctx context.Context, repoID, subjectID int64, workflow, stage, action string) ([]RightsGrant, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+grantColumns+` FROM rights_grants WHERE repo_id=? AND subject_id=? AND workflow=? AND stage=? AND action=? AND revoked_at IS NULL ORDER BY id`,
		repoID, subjectID, workflow, stage, action)
	if err != nil {
		return nil, fmt.Errorf("store: match rights grants: %w", err)
	}
	defer rows.Close()
	var out []RightsGrant
	for rows.Next() {
		g, err := scanGrant(rows)
		if err != nil {
			return nil, fmt.Errorf("store: match rights grants: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func insertAssignment(ctx context.Context, tx *sql.Tx, a ReviewerAssignment) (ReviewerAssignment, error) {
	res, err := tx.ExecContext(ctx, `INSERT INTO reviewer_assignments(repo_id,task_ref,workflow,stage,reviewer_id,reviewer_login,assigned_by,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		a.RepoID, a.TaskRef, a.Workflow, a.Stage, a.ReviewerID, a.ReviewerLogin, a.AssignedBy, FormatTime(a.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return ReviewerAssignment{}, ErrRightsDuplicate
		}
		return ReviewerAssignment{}, fmt.Errorf("insert reviewer assignment: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ReviewerAssignment{}, err
	}
	return scanAssignment(tx.QueryRowContext(ctx, `SELECT `+assignmentColumns+` FROM reviewer_assignments WHERE id=?`, id))
}

func closeAssignment(ctx context.Context, tx *sql.Tx, repoID, id, actor int64, at time.Time) (ReviewerAssignment, error) {
	res, err := tx.ExecContext(ctx, `UPDATE reviewer_assignments SET removed_at=?,removed_by=? WHERE id=? AND repo_id=? AND removed_at IS NULL`, FormatTime(at), actor, id, repoID)
	if err != nil {
		return ReviewerAssignment{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ReviewerAssignment{}, ErrNotFound
	}
	a, err := scanAssignment(tx.QueryRowContext(ctx, `SELECT `+assignmentColumns+` FROM reviewer_assignments WHERE id=?`, id))
	if err != nil {
		return ReviewerAssignment{}, err
	}
	return a, appendRightsHistory(ctx, tx, repoID, at, actor, "remove", id, a)
}

// CreateReviewerAssignment stores an assignment and its history row.
// AssignedBy is the actor and CreatedAt the change time.
func (db *DB) CreateReviewerAssignment(ctx context.Context, a ReviewerAssignment) (ReviewerAssignment, error) {
	out, err := db.ReplaceReviewerAssignment(ctx, a, false)
	return out, err
}

// ReplaceReviewerAssignment closes every open assignment of the same
// repository, task and stage, then stores the new one, all in one transaction.
// Each closed assignment and the new one write one history row each. With
// replace false it closes nothing.
func (db *DB) ReplaceReviewerAssignment(ctx context.Context, a ReviewerAssignment, replace bool) (out ReviewerAssignment, err error) {
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		if err := requireRepo(ctx, tx, a.RepoID); err != nil {
			return err
		}
		if replace {
			ids, err := openAssignmentIDs(ctx, tx, a.RepoID, a.TaskRef, a.Workflow, a.Stage)
			if err != nil {
				return err
			}
			for _, id := range ids {
				if _, err := closeAssignment(ctx, tx, a.RepoID, id, a.AssignedBy, a.CreatedAt); err != nil {
					return err
				}
			}
		}
		stored, err := insertAssignment(ctx, tx, a)
		if err != nil {
			return err
		}
		out = stored
		return appendRightsHistory(ctx, tx, a.RepoID, a.CreatedAt, a.AssignedBy, "assign", stored.ID, stored)
	})
	if err != nil {
		return ReviewerAssignment{}, fmt.Errorf("store: assign reviewer: %w", err)
	}
	return out, nil
}

func openAssignmentIDs(ctx context.Context, tx *sql.Tx, repoID int64, taskRef, workflow, stage string) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM reviewer_assignments WHERE repo_id=? AND task_ref=? AND workflow=? AND stage=? AND removed_at IS NULL ORDER BY id`, repoID, taskRef, workflow, stage)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// RemoveReviewerAssignment closes an open assignment and appends its history
// row. It returns ErrNotFound when none is open under that id.
func (db *DB) RemoveReviewerAssignment(ctx context.Context, repoID, id, actor int64, at time.Time) (out ReviewerAssignment, err error) {
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		out, err = closeAssignment(ctx, tx, repoID, id, actor, at)
		return err
	})
	if err != nil {
		return ReviewerAssignment{}, fmt.Errorf("store: remove reviewer assignment %d: %w", id, err)
	}
	return out, nil
}

// ListReviewerAssignments lists a repository's assignments by id, optionally
// for one task and without removed ones.
func (db *DB) ListReviewerAssignments(ctx context.Context, repoID int64, taskRef string, openOnly bool) ([]ReviewerAssignment, error) {
	q := `SELECT ` + assignmentColumns + ` FROM reviewer_assignments WHERE repo_id=?`
	args := []any{repoID}
	if taskRef != "" {
		q += ` AND task_ref=?`
		args = append(args, taskRef)
	}
	if openOnly {
		q += ` AND removed_at IS NULL`
	}
	rows, err := db.sql.QueryContext(ctx, q+` ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list reviewer assignments: %w", err)
	}
	defer rows.Close()
	var out []ReviewerAssignment
	for rows.Next() {
		a, err := scanAssignment(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list reviewer assignments: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListRightsHistory returns a repository's history in sequence order.
func (db *DB) ListRightsHistory(ctx context.Context, repoID int64) ([]RightsHistoryEntry, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT sequence,repo_id,at,actor_id,kind,record_id,snapshot_json FROM rights_history WHERE repo_id=? ORDER BY sequence`, repoID)
	if err != nil {
		return nil, fmt.Errorf("store: list rights history: %w", err)
	}
	defer rows.Close()
	var out []RightsHistoryEntry
	for rows.Next() {
		var e RightsHistoryEntry
		var at string
		if err := rows.Scan(&e.Sequence, &e.RepoID, &at, &e.ActorID, &e.Kind, &e.RecordID, &e.Snapshot); err != nil {
			return nil, fmt.Errorf("store: list rights history: %w", err)
		}
		if e.At, err = ParseTime(at); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
