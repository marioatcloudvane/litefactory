package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// bugStartTriggerBase reserves the factory-private run identities of the Bug
// starts a confirmed bug filing hands off, below the factory work-id band that
// begins at bugStartTriggerEnd. Each identity is the band plus one start
// attempt id, so it is never a forge comment id and never another band.
const (
	bugStartTriggerBase int64 = 1 << 55
	bugStartTriggerEnd  int64 = 1 << 56
)

// IsBugStartTrigger reports whether an identity belongs to a bug filing start attempt.
func IsBugStartTrigger(id int64) bool {
	return id > bugStartTriggerBase && id < bugStartTriggerEnd
}

// BugStartTrigger returns the private run identity of one start attempt.
func BugStartTrigger(attemptID int64) int64 { return bugStartTriggerBase + attemptID }

// BugStartAttemptID inverts BugStartTrigger.
func BugStartAttemptID(trigger int64) (int64, bool) {
	if !IsBugStartTrigger(trigger) {
		return 0, false
	}
	return trigger - bugStartTriggerBase, true
}

// Bug filing states. They only move forward.
const (
	BugFilingAdmitted          = "admitted"
	BugFilingCreating          = "creating"
	BugFilingUncertain         = "uncertain"
	BugFilingCreated           = "created"
	BugFilingLabelled          = "labelled"
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

// BugStartFailureLimit bounds failed start hand-offs before the Bug admission.
const BugStartFailureLimit = 3

// Start attempt states and outcomes reuse the brainstorm launch vocabulary.
const (
	BugStartPending   = BrainstormLaunchPending
	BugStartReserved  = BrainstormLaunchReserved
	BugStartRunning   = BrainstormLaunchRunning
	BugStartSucceeded = BrainstormLaunchSucceeded
	BugStartFailed    = BrainstormLaunchFailed

	BugStartOutcomeReplayed = BrainstormLaunchOutcomeReplayed
	BugStartOutcomeDeferred = BrainstormLaunchOutcomeDeferred
	BugStartOutcomeFailed   = BrainstormLaunchOutcomeFailed
	BugStartOutcomeStarted  = BrainstormLaunchOutcomeStarted
)

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

// BugFilingStartAttempt is one private Bug start hand-off for a filing.
type BugFilingStartAttempt struct {
	ID                   int64
	FilingID             int64
	Ordinal              int
	SessionID            int64
	State                string
	RunID                int64
	Outcome, Reason      string
	CreatedAt, UpdatedAt time.Time
}

// TriggerID is the attempt's private run identity.
func (a BugFilingStartAttempt) TriggerID() int64 { return BugStartTrigger(a.ID) }

// Open reports whether the attempt still counts against the one-open rule.
func (a BugFilingStartAttempt) Open() bool {
	return a.State == BugStartPending || a.State == BugStartReserved || a.State == BugStartRunning
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

const bugStartAttemptColumns = `id,filing_id,ordinal,session_id,state,run_id,outcome,reason,created_at,updated_at`

func scanBugStartAttempt(row interface{ Scan(...any) error }) (BugFilingStartAttempt, error) {
	var a BugFilingStartAttempt
	var created, updated string
	err := row.Scan(&a.ID, &a.FilingID, &a.Ordinal, &a.SessionID, &a.State, &a.RunID, &a.Outcome, &a.Reason, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	a.CreatedAt, _ = ParseTime(created)
	a.UpdatedAt, _ = ParseTime(updated)
	return a, nil
}

// BugFilingStartAttempts lists a filing's start attempts in ordinal order.
func (db *DB) BugFilingStartAttempts(ctx context.Context, filingID int64) ([]BugFilingStartAttempt, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+bugStartAttemptColumns+` FROM bug_filing_start_attempts WHERE filing_id=? ORDER BY ordinal`, filingID)
	if err != nil {
		return nil, fmt.Errorf("store: bug start attempts: %w", err)
	}
	defer rows.Close()
	var out []BugFilingStartAttempt
	for rows.Next() {
		a, err := scanBugStartAttempt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ReadBugFilingStartAttempt reads one start attempt.
func (db *DB) ReadBugFilingStartAttempt(ctx context.Context, id int64) (BugFilingStartAttempt, error) {
	return scanBugStartAttempt(db.sql.QueryRowContext(ctx, `SELECT `+bugStartAttemptColumns+` FROM bug_filing_start_attempts WHERE id=?`, id))
}

// OpenBugFilingStartAttempt returns the filing's open attempt, or opens a new
// pending one with the next ordinal. created is false when one was open.
func (db *DB) OpenBugFilingStartAttempt(ctx context.Context, filingID, sessionID int64, at time.Time) (a BugFilingStartAttempt, created bool, err error) {
	if filingID < 1 || sessionID < 1 {
		return a, false, errors.New("store: open bug start attempt: filing and session are required")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		open, e := scanBugStartAttempt(tx.QueryRowContext(ctx, `SELECT `+bugStartAttemptColumns+` FROM bug_filing_start_attempts WHERE filing_id=? AND state IN ('pending','reserved','running')`, filingID))
		if e == nil {
			a = open
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		var next int
		if e = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(ordinal),0)+1 FROM bug_filing_start_attempts WHERE filing_id=?`, filingID).Scan(&next); e != nil {
			return e
		}
		stamp := FormatTime(at.UTC())
		a, e = scanBugStartAttempt(tx.QueryRowContext(ctx, `INSERT INTO bug_filing_start_attempts(filing_id,ordinal,session_id,state,created_at,updated_at) VALUES(?,?,?,'pending',?,?) RETURNING `+bugStartAttemptColumns, filingID, next, sessionID, stamp, stamp))
		created = e == nil
		return e
	})
	if err != nil {
		return BugFilingStartAttempt{}, false, fmt.Errorf("store: open bug start attempt: %w", err)
	}
	return a, created, nil
}

// MoveBugFilingStartAttempt changes an attempt's state when it is still in
// from. Closed attempts never reopen. It reports whether the row changed.
func (db *DB) MoveBugFilingStartAttempt(ctx context.Context, id int64, from, to, outcome string, runID int64, reason string, at time.Time) (bool, error) {
	allowed := map[string][]string{
		BugStartPending:  {BugStartReserved},
		BugStartReserved: {BugStartRunning, BugStartPending, BugStartSucceeded, BugStartFailed},
		BugStartRunning:  {BugStartPending, BugStartSucceeded, BugStartFailed},
	}
	ok := false
	for _, s := range allowed[from] {
		ok = ok || s == to
	}
	if !ok {
		return false, fmt.Errorf("store: bug start attempt cannot move from %s to %s", from, to)
	}
	res, err := db.sql.ExecContext(ctx, `UPDATE bug_filing_start_attempts SET state=?,outcome=CASE WHEN ?='' THEN outcome ELSE ? END,run_id=CASE WHEN ?>0 THEN ? ELSE run_id END,reason=?,updated_at=? WHERE id=? AND state=?`,
		to, outcome, outcome, runID, runID, reason, FormatTime(at.UTC()), id, from)
	if err != nil {
		return false, fmt.Errorf("store: move bug start attempt: %w", err)
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
