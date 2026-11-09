package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Bug filing states. They only move forward.
const (
	BugFilingAdmitted  = "admitted"
	BugFilingCreating  = "creating"
	BugFilingUncertain = "uncertain"
	BugFilingCreated   = "created"
	BugFilingLabelled  = "labelled"
	// BugFilingStartAdmitted is a retired state: such rows are started anew.
	BugFilingStartAdmitted     = "start_admitted"
	BugFilingDispatched        = "dispatched"
	BugFilingNotCreated        = "not_created"
	BugFilingFailedUnconfirmed = "failed_unconfirmed"
	BugFilingLabelFailed       = "label_failed"
	BugFilingStartFailed       = "start_failed"
)

// BugFilingFinal reports whether a filing state is final.
func BugFilingFinal(state string) bool {
	switch state {
	case BugFilingDispatched, BugFilingNotCreated, BugFilingFailedUnconfirmed, BugFilingLabelFailed, BugFilingStartFailed:
		return true
	}
	return false
}

// ErrBugFilingInvalid reports malformed bug filing input.
var ErrBugFilingInvalid = errors.New("store: invalid bug filing input")

// BugFiling is one confirmed bug_file proposal's filing, keyed by execution id.
type BugFiling struct {
	ID                                  int64
	ExecutionID, OwnerActionID, OwnerID string
	OwnerGeneration                     int64
	RepositoryID                        int64
	RepositoryRef                       string
	Title, Body, Marker                 string
	// Tracker is "github" or "linear". The Linear fields are frozen at
	// admission: the ticket-effect key that owns the creation UUID, the mapped
	// bug label ID and the connection destination (team/project).
	Tracker, EffectKey, LabelID, Destination string
	State                                    string
	IssueID                                  int64
	IssueNodeID                              string
	IssueNumber                              int
	IssueLink                                string
	SessionID                                int64
	AttemptAt                                time.Time
	ChecksDone                               int
	NextCheckAt                              time.Time
	CreateRetries, LabelRetries              int
	FailedStartAttempts                      int
	AdmissionTrigger                         int64
	Reason                                   string
	CreatedAt, UpdatedAt                     time.Time
}

// Bug filing trackers.
const (
	BugTrackerGitHub = "github"
	BugTrackerLinear = "linear"
)

const bugFilingColumns = `id,execution_id,owner_action_id,owner_id,owner_generation,repository_id,repository_ref,title,body,marker,tracker,effect_key,label_id,destination,state,issue_id,issue_node_id,issue_number,issue_link,session_id,attempt_at,checks_done,next_check_at,create_retries,label_retries,failed_start_attempts,admission_trigger,reason,created_at,updated_at`

func scanBugFiling(row interface{ Scan(...any) error }) (BugFiling, error) {
	var f BugFiling
	var attempt, next, created, updated string
	err := row.Scan(&f.ID, &f.ExecutionID, &f.OwnerActionID, &f.OwnerID, &f.OwnerGeneration, &f.RepositoryID, &f.RepositoryRef, &f.Title, &f.Body, &f.Marker, &f.Tracker, &f.EffectKey, &f.LabelID, &f.Destination, &f.State, &f.IssueID, &f.IssueNodeID, &f.IssueNumber, &f.IssueLink, &f.SessionID, &attempt, &f.ChecksDone, &next, &f.CreateRetries, &f.LabelRetries, &f.FailedStartAttempts, &f.AdmissionTrigger, &f.Reason, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrNotFound
	}
	if err != nil {
		return f, err
	}
	for _, p := range []struct {
		s string
		t *time.Time
	}{{attempt, &f.AttemptAt}, {next, &f.NextCheckAt}, {created, &f.CreatedAt}, {updated, &f.UpdatedAt}} {
		if *p.t, err = parseTaskTime(p.s); err != nil {
			return f, err
		}
	}
	return f, nil
}

// AdmitBugFiling inserts the filing for its execution id when none exists and
// returns the stored row either way. A replay never changes the frozen content.
// created reports whether this call inserted it.
func (db *DB) AdmitBugFiling(ctx context.Context, f BugFiling, at time.Time) (out BugFiling, created bool, err error) {
	if f.ExecutionID == "" || f.OwnerActionID == "" || f.OwnerID == "" || f.RepositoryID < 1 || f.RepositoryRef == "" || f.Title == "" || f.Body == "" {
		return BugFiling{}, false, ErrBugFilingInvalid
	}
	switch f.Tracker {
	case "", BugTrackerGitHub:
		f.Tracker = BugTrackerGitHub
		if f.Marker == "" {
			return BugFiling{}, false, ErrBugFilingInvalid
		}
	case BugTrackerLinear:
		if f.Marker != "" || f.EffectKey == "" || f.LabelID == "" || f.Destination == "" {
			return BugFiling{}, false, ErrBugFilingInvalid
		}
	default:
		return BugFiling{}, false, ErrBugFilingInvalid
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		existing, e := scanBugFiling(tx.QueryRowContext(ctx, `SELECT `+bugFilingColumns+` FROM bug_filings WHERE execution_id=?`, f.ExecutionID))
		if e == nil {
			out = existing
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		stamp := taskTime(at)
		out, e = scanBugFiling(tx.QueryRowContext(ctx, `INSERT INTO bug_filings(execution_id,owner_action_id,owner_id,owner_generation,repository_id,repository_ref,title,body,marker,tracker,effect_key,label_id,destination,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'admitted',?,?) RETURNING `+bugFilingColumns,
			f.ExecutionID, f.OwnerActionID, f.OwnerID, f.OwnerGeneration, f.RepositoryID, f.RepositoryRef, f.Title, f.Body, f.Marker, f.Tracker, f.EffectKey, f.LabelID, f.Destination, stamp, stamp))
		created = e == nil
		return e
	})
	if err != nil {
		return BugFiling{}, false, fmt.Errorf("store: admit bug filing: %w", err)
	}
	return out, created, nil
}

// BugFilingByExecution reads the filing of one owner-action execution.
func (db *DB) BugFilingByExecution(ctx context.Context, executionID string) (BugFiling, error) {
	return scanBugFiling(db.sql.QueryRowContext(ctx, `SELECT `+bugFilingColumns+` FROM bug_filings WHERE execution_id=?`, executionID))
}

// ReadBugFiling reads one filing.
func (db *DB) ReadBugFiling(ctx context.Context, id int64) (BugFiling, error) {
	return scanBugFiling(db.sql.QueryRowContext(ctx, `SELECT `+bugFilingColumns+` FROM bug_filings WHERE id=?`, id))
}

// ActiveBugFilings lists filings that are not final, oldest first.
func (db *DB) ActiveBugFilings(ctx context.Context, limit int) ([]BugFiling, error) {
	if limit < 1 {
		limit = 100
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT `+bugFilingColumns+` FROM bug_filings WHERE state IN ('admitted','creating','uncertain','created','labelled','start_admitted') ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: active bug filings: %w", err)
	}
	defer rows.Close()
	var out []BugFiling
	for rows.Next() {
		f, err := scanBugFiling(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SaveBugFiling writes every mutable field when the filing is still in
// fromState. Frozen content and an issue identity once set never change. The
// commit is the durable boundary; it reports whether the row changed.
func (db *DB) SaveBugFiling(ctx context.Context, f BugFiling, fromState string, at time.Time) (bool, error) {
	res, err := db.sql.ExecContext(ctx, `UPDATE bug_filings SET state=?,
 issue_id=CASE WHEN issue_id=0 THEN ? ELSE issue_id END,
 issue_node_id=CASE WHEN issue_node_id='' THEN ? ELSE issue_node_id END,
 issue_number=CASE WHEN issue_number=0 THEN ? ELSE issue_number END,
 issue_link=CASE WHEN issue_link='' THEN ? ELSE issue_link END,
 session_id=CASE WHEN session_id=0 THEN ? ELSE session_id END,
 attempt_at=?,checks_done=?,next_check_at=?,create_retries=?,label_retries=?,failed_start_attempts=?,
 admission_trigger=CASE WHEN admission_trigger=0 THEN ? ELSE admission_trigger END,
 reason=?,updated_at=? WHERE id=? AND state=?`,
		f.State, f.IssueID, f.IssueNodeID, f.IssueNumber, f.IssueLink, f.SessionID, taskTime(f.AttemptAt), f.ChecksDone, taskTime(f.NextCheckAt), f.CreateRetries, f.LabelRetries, f.FailedStartAttempts, f.AdmissionTrigger, f.Reason, taskTime(at), f.ID, fromState)
	if err != nil {
		return false, fmt.Errorf("store: save bug filing: %w", err)
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
