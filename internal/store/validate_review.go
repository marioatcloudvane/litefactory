package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

// Reviewer assessment states. Pending is unfinished; every other state is final.
const (
	ReviewPending   = "pending"
	ReviewAccepted  = "accepted"
	ReviewFailed    = "failed"
	ReviewMalformed = "malformed"
)

// ErrReviewFinished refuses a change to a finished reviewer assessment.
var ErrReviewFinished = errors.New("store: finished reviewer assessment cannot change")

// ValidateReview is the accepted result of one reviewer for one tested commit.
// Findings holds the role's assessment per story exactly as the validated
// submission carried it; it is empty unless the state is accepted.
type ValidateReview struct {
	ID         int64
	InstanceID int64
	Role       string
	Commit     string
	TreeDigest string
	SuiteRunID int64
	Attempt    string
	State      string
	Reason     string
	Findings   json.RawMessage
	StartedAt  time.Time
	FinishedAt time.Time
}

const reviewColumns = `id,instance_id,role,commit_sha,tree_digest,suite_run_id,attempt,state,reason,findings,started_at,finished_at`

// BeginValidateReview records a pending reviewer row, or returns the row that
// already exists for the instance, role and commit. created reports whether
// this call made it. A reviewer is therefore started at most once per commit.
func (db *DB) BeginValidateReview(ctx context.Context, in ValidateReview) (out ValidateReview, created bool, err error) {
	if in.InstanceID < 1 || !slices.Contains([]string{"acceptance", "code", "architecture"}, in.Role) || len(in.Commit) != 40 && len(in.Commit) != 64 || in.StartedAt.IsZero() {
		return ValidateReview{}, false, errors.New("store: invalid reviewer assessment")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		cur, e := scanReview(tx.QueryRowContext(ctx, `SELECT `+reviewColumns+` FROM validate_reviews WHERE instance_id=? AND role=? AND commit_sha=?`, in.InstanceID, in.Role, in.Commit))
		if e == nil {
			out = cur
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		res, e := tx.ExecContext(ctx, `INSERT INTO validate_reviews(instance_id,role,commit_sha,tree_digest,suite_run_id,attempt,state,started_at) VALUES(?,?,?,?,?,?,'pending',?)`,
			in.InstanceID, in.Role, in.Commit, in.TreeDigest, in.SuiteRunID, in.Attempt, FormatTime(in.StartedAt))
		if e != nil {
			return e
		}
		id, e := res.LastInsertId()
		if e != nil {
			return e
		}
		out, created = in, true
		out.ID, out.State, out.Findings = id, ReviewPending, json.RawMessage("[]")
		return nil
	})
	if err != nil {
		return ValidateReview{}, false, fmt.Errorf("store: begin reviewer assessment: %w", err)
	}
	return out, created, nil
}

// FinishValidateReview makes a pending row final. Finishing a finished row with
// the same state and findings succeeds; anything else is ErrReviewFinished.
func (db *DB) FinishValidateReview(ctx context.Context, id int64, state, reason string, findings json.RawMessage, at time.Time) error {
	if id < 1 || !slices.Contains([]string{ReviewAccepted, ReviewFailed, ReviewMalformed}, state) || at.IsZero() {
		return errors.New("store: invalid reviewer assessment finish")
	}
	if state != ReviewAccepted {
		findings = nil
	}
	if len(findings) == 0 {
		findings = json.RawMessage("[]")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		cur, err := scanReview(tx.QueryRowContext(ctx, `SELECT `+reviewColumns+` FROM validate_reviews WHERE id=?`, id))
		if err != nil {
			return err
		}
		if cur.State != ReviewPending {
			if cur.State == state && string(cur.Findings) == string(findings) {
				return nil
			}
			return ErrReviewFinished
		}
		_, err = tx.ExecContext(ctx, `UPDATE validate_reviews SET state=?,reason=?,findings=?,finished_at=? WHERE id=? AND state='pending'`, state, reason, string(findings), FormatTime(at), id)
		return err
	})
}

// ListValidateReviews returns the reviewer rows of an instance on one commit.
func (db *DB) ListValidateReviews(ctx context.Context, instanceID int64, commit string) ([]ValidateReview, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+reviewColumns+` FROM validate_reviews WHERE instance_id=? AND commit_sha=? ORDER BY id`, instanceID, commit)
	if err != nil {
		return nil, fmt.Errorf("store: list reviewer assessments: %w", err)
	}
	defer rows.Close()
	var out []ValidateReview
	for rows.Next() {
		r, err := scanReview(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanReview(row scanner) (ValidateReview, error) {
	var r ValidateReview
	var findings, started, finished string
	if err := row.Scan(&r.ID, &r.InstanceID, &r.Role, &r.Commit, &r.TreeDigest, &r.SuiteRunID, &r.Attempt, &r.State, &r.Reason, &findings, &started, &finished); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ValidateReview{}, ErrNotFound
		}
		return ValidateReview{}, fmt.Errorf("store: read reviewer assessment: %w", err)
	}
	r.Findings = json.RawMessage(findings)
	var err error
	if r.StartedAt, err = ParseTime(started); err != nil {
		return ValidateReview{}, err
	}
	if finished != "" {
		if r.FinishedAt, err = ParseTime(finished); err != nil {
			return ValidateReview{}, err
		}
	}
	return r, nil
}

// Validate outcome kinds.
const (
	ValidateAssessed   = "assessed"
	ValidateHandedBack = "handed_back"
	ValidateBlocked    = "blocked"
)

// ValidateOutcome is the end of Validate's part of one run on its final commit.
// Assessment is the joined assessment per story; Handback is set only for a
// handed-back outcome. It never says accepted or approved.
type ValidateOutcome struct {
	ID         int64
	InstanceID int64
	Commit     string
	Outcome    string
	Reason     string
	Assessment json.RawMessage
	Handback   json.RawMessage
	CreatedAt  time.Time
}

// PutValidateOutcome records the outcome once per instance and final commit. A
// second call with the same outcome returns the stored row; a different one is
// refused, because a recorded outcome never changes.
func (db *DB) PutValidateOutcome(ctx context.Context, in ValidateOutcome) (ValidateOutcome, error) {
	if in.InstanceID < 1 || len(in.Commit) != 40 && len(in.Commit) != 64 || !slices.Contains([]string{ValidateAssessed, ValidateHandedBack, ValidateBlocked}, in.Outcome) || in.CreatedAt.IsZero() {
		return ValidateOutcome{}, errors.New("store: invalid validate outcome")
	}
	if len(in.Assessment) == 0 {
		in.Assessment = json.RawMessage("[]")
	}
	var out ValidateOutcome
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		cur, e := scanOutcome(tx.QueryRowContext(ctx, `SELECT `+outcomeColumns+` FROM validate_outcomes WHERE instance_id=? AND commit_sha=?`, in.InstanceID, in.Commit))
		if e == nil {
			if cur.Outcome != in.Outcome {
				return errors.New("a different outcome is already recorded for this commit")
			}
			out = cur
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		res, e := tx.ExecContext(ctx, `INSERT INTO validate_outcomes(instance_id,commit_sha,outcome,reason,assessment,handback,created_at) VALUES(?,?,?,?,?,?,?)`,
			in.InstanceID, in.Commit, in.Outcome, in.Reason, string(in.Assessment), string(in.Handback), FormatTime(in.CreatedAt))
		if e != nil {
			return e
		}
		id, e := res.LastInsertId()
		if e != nil {
			return e
		}
		out = in
		out.ID = id
		return nil
	})
	if err != nil {
		return ValidateOutcome{}, fmt.Errorf("store: record validate outcome: %w", err)
	}
	return out, nil
}

const outcomeColumns = `id,instance_id,commit_sha,outcome,reason,assessment,handback,created_at`

// GetValidateOutcome returns the outcome of an instance on a commit, or ErrNotFound.
func (db *DB) GetValidateOutcome(ctx context.Context, instanceID int64, commit string) (ValidateOutcome, error) {
	return scanOutcome(db.sql.QueryRowContext(ctx, `SELECT `+outcomeColumns+` FROM validate_outcomes WHERE instance_id=? AND commit_sha=?`, instanceID, commit))
}

// LatestValidateOutcome returns the newest outcome of an instance, or ErrNotFound.
func (db *DB) LatestValidateOutcome(ctx context.Context, instanceID int64) (ValidateOutcome, error) {
	return scanOutcome(db.sql.QueryRowContext(ctx, `SELECT `+outcomeColumns+` FROM validate_outcomes WHERE instance_id=? ORDER BY id DESC LIMIT 1`, instanceID))
}

func scanOutcome(row scanner) (ValidateOutcome, error) {
	var o ValidateOutcome
	var assessment, handback, created string
	if err := row.Scan(&o.ID, &o.InstanceID, &o.Commit, &o.Outcome, &o.Reason, &assessment, &handback, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ValidateOutcome{}, ErrNotFound
		}
		return ValidateOutcome{}, fmt.Errorf("store: read validate outcome: %w", err)
	}
	o.Assessment = json.RawMessage(assessment)
	if handback != "" {
		o.Handback = json.RawMessage(handback)
	}
	var err error
	if o.CreatedAt, err = ParseTime(created); err != nil {
		return ValidateOutcome{}, err
	}
	return o, nil
}
