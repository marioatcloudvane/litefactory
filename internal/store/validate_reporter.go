package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Reporter result modes and states.
const (
	ReporterModeReport   = "report"
	ReporterModeFollowUp = "follow-up"

	ReporterPending  = "pending"
	ReporterAccepted = "accepted"
	ReporterFallback = "fallback"
)

// ErrReporterSettled refuses a change to a settled reporter result.
var ErrReporterSettled = errors.New("store: a settled reporter result never changes")

// ValidateReporterResult is one reporter attempt of a Validate run: the first
// summary of a tested commit, or the answer to one owner comment. Result holds
// the accepted summary exactly as validated; the report renders from it.
type ValidateReporterResult struct {
	ID         int64
	InstanceID int64
	Commit     string
	Mode       string
	Attempt    string
	CommentID  int64
	Question   string
	State      string
	Result     string
	Reason     string
	CreatedAt  time.Time
	FinishedAt time.Time
}

const reporterColumns = `id,instance_id,commit_sha,mode,attempt,comment_id,question,state,result,reason,created_at,finished_at`

// BeginValidateReporter returns the row of one attempt, creating it pending.
// Beginning the same report or the same owner comment again returns the
// stored row, so a replayed step never starts a second reporter.
func (db *DB) BeginValidateReporter(ctx context.Context, in ValidateReporterResult) (out ValidateReporterResult, created bool, err error) {
	if in.InstanceID < 1 || !validCommit(in.Commit) || !slices.Contains([]string{ReporterModeReport, ReporterModeFollowUp}, in.Mode) || strings.TrimSpace(in.Attempt) == "" || (in.Mode == ReporterModeFollowUp) != (in.CommentID > 0) || in.CreatedAt.IsZero() {
		return ValidateReporterResult{}, false, errors.New("store: invalid reporter attempt")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		query, args := `SELECT `+reporterColumns+` FROM validate_reporter_results WHERE instance_id=? AND commit_sha=? AND mode='report'`, []any{in.InstanceID, in.Commit}
		if in.Mode == ReporterModeFollowUp {
			query, args = `SELECT `+reporterColumns+` FROM validate_reporter_results WHERE instance_id=? AND comment_id=? AND mode='follow-up'`, []any{in.InstanceID, in.CommentID}
		}
		cur, e := scanReporter(tx.QueryRowContext(ctx, query, args...))
		if e == nil {
			out = cur
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		res, e := tx.ExecContext(ctx, `INSERT INTO validate_reporter_results(instance_id,commit_sha,mode,attempt,comment_id,question,state,created_at) VALUES(?,?,?,?,?,?,'pending',?)`,
			in.InstanceID, in.Commit, in.Mode, in.Attempt, in.CommentID, in.Question, FormatTime(in.CreatedAt))
		if e != nil {
			return e
		}
		id, e := res.LastInsertId()
		if e != nil {
			return e
		}
		out, created = in, true
		out.ID, out.State, out.Result, out.Reason = id, ReporterPending, "", ""
		return nil
	})
	if err != nil {
		return ValidateReporterResult{}, false, fmt.Errorf("store: begin reporter attempt: %w", err)
	}
	return out, created, nil
}

// FinishValidateReporter settles a pending attempt as accepted (with its
// result) or fallback (with the reason). Settling it again with the same
// values succeeds; anything else is ErrReporterSettled.
func (db *DB) FinishValidateReporter(ctx context.Context, id int64, state, result, reason string, at time.Time) error {
	if id < 1 || !slices.Contains([]string{ReporterAccepted, ReporterFallback}, state) || (state == ReporterAccepted) != (result != "") || at.IsZero() {
		return errors.New("store: invalid reporter finish")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		cur, err := scanReporter(tx.QueryRowContext(ctx, `SELECT `+reporterColumns+` FROM validate_reporter_results WHERE id=?`, id))
		if err != nil {
			return err
		}
		if cur.State != ReporterPending {
			if cur.State == state && cur.Result == result {
				return nil
			}
			return ErrReporterSettled
		}
		_, err = tx.ExecContext(ctx, `UPDATE validate_reporter_results SET state=?,result=?,reason=?,finished_at=? WHERE id=? AND state='pending'`, state, result, reason, FormatTime(at), id)
		return err
	})
}

// ListValidateReporter returns every reporter attempt of a run, oldest first.
func (db *DB) ListValidateReporter(ctx context.Context, instanceID int64) ([]ValidateReporterResult, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+reporterColumns+` FROM validate_reporter_results WHERE instance_id=? ORDER BY id`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("store: list reporter results: %w", err)
	}
	defer rows.Close()
	var out []ValidateReporterResult
	for rows.Next() {
		r, err := scanReporter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanReporter(row scanner) (ValidateReporterResult, error) {
	var r ValidateReporterResult
	var created, finished string
	if err := row.Scan(&r.ID, &r.InstanceID, &r.Commit, &r.Mode, &r.Attempt, &r.CommentID, &r.Question, &r.State, &r.Result, &r.Reason, &created, &finished); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ValidateReporterResult{}, ErrNotFound
		}
		return ValidateReporterResult{}, fmt.Errorf("store: read reporter result: %w", err)
	}
	var err error
	if r.CreatedAt, err = ParseTime(created); err != nil {
		return ValidateReporterResult{}, err
	}
	if finished != "" {
		if r.FinishedAt, err = ParseTime(finished); err != nil {
			return ValidateReporterResult{}, err
		}
	}
	return r, nil
}

// Factory Bug filing states.
const (
	FactoryBugIntent = "intent"
	FactoryBugFiled  = "filed"
	FactoryBugFailed = "failed"
)

// ValidateFactoryBug is one owner-confirmed upstream factory Bug for one item.
type ValidateFactoryBug struct {
	ID         int64
	InstanceID int64
	Commit     string
	Item       string
	CommentID  int64
	AuthorID   int64
	Marker     string
	State      string
	Reference  string
	URL        string
	Error      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

const factoryBugColumns = `id,instance_id,commit_sha,item,comment_id,author_id,marker,state,reference,url,error,created_at,updated_at`

// PutValidateFactoryBugIntent records the owner's confirmation before the
// remote call. A failed filing is put back to intent by a new confirmation; a
// filed one is returned unchanged.
func (db *DB) PutValidateFactoryBugIntent(ctx context.Context, in ValidateFactoryBug) (ValidateFactoryBug, error) {
	if in.InstanceID < 1 || !validCommit(in.Commit) || in.Item == "" || in.CommentID < 1 || in.AuthorID < 1 || in.Marker == "" || in.CreatedAt.IsZero() {
		return ValidateFactoryBug{}, errors.New("store: invalid factory Bug intent")
	}
	var out ValidateFactoryBug
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		cur, e := scanFactoryBug(tx.QueryRowContext(ctx, `SELECT `+factoryBugColumns+` FROM validate_factory_bugs WHERE instance_id=? AND commit_sha=? AND item=?`, in.InstanceID, in.Commit, in.Item))
		switch {
		case e == nil && cur.State == FactoryBugFailed && cur.CommentID != in.CommentID:
			if _, e = tx.ExecContext(ctx, `UPDATE validate_factory_bugs SET state='intent',comment_id=?,author_id=?,error='',updated_at=? WHERE id=?`, in.CommentID, in.AuthorID, FormatTime(in.CreatedAt), cur.ID); e != nil {
				return e
			}
			cur.State, cur.CommentID, cur.AuthorID, cur.Error, cur.UpdatedAt = FactoryBugIntent, in.CommentID, in.AuthorID, "", in.CreatedAt
			out = cur
			return nil
		case e == nil:
			out = cur
			return nil
		case !errors.Is(e, ErrNotFound):
			return e
		}
		res, e := tx.ExecContext(ctx, `INSERT INTO validate_factory_bugs(instance_id,commit_sha,item,comment_id,author_id,marker,state,created_at,updated_at) VALUES(?,?,?,?,?,?,'intent',?,?)`,
			in.InstanceID, in.Commit, in.Item, in.CommentID, in.AuthorID, in.Marker, FormatTime(in.CreatedAt), FormatTime(in.CreatedAt))
		if e != nil {
			return e
		}
		id, e := res.LastInsertId()
		if e != nil {
			return e
		}
		out = in
		out.ID, out.State, out.UpdatedAt = id, FactoryBugIntent, in.CreatedAt
		return nil
	})
	if err != nil {
		return ValidateFactoryBug{}, fmt.Errorf("store: record factory Bug intent: %w", err)
	}
	return out, nil
}

// FinishValidateFactoryBug records the filing result of an intent.
func (db *DB) FinishValidateFactoryBug(ctx context.Context, id int64, state, reference, url, failure string, at time.Time) error {
	if id < 1 || !slices.Contains([]string{FactoryBugFiled, FactoryBugFailed}, state) || at.IsZero() || (state == FactoryBugFiled) != (reference != "") {
		return errors.New("store: invalid factory Bug result")
	}
	_, err := db.sql.ExecContext(ctx, `UPDATE validate_factory_bugs SET state=?,reference=?,url=?,error=?,updated_at=? WHERE id=? AND state='intent'`, state, reference, url, failure, FormatTime(at), id)
	if err != nil {
		return fmt.Errorf("store: finish factory Bug: %w", err)
	}
	return nil
}

// ListValidateFactoryBugs returns the factory Bug rows of one run and commit.
func (db *DB) ListValidateFactoryBugs(ctx context.Context, instanceID int64, commit string) ([]ValidateFactoryBug, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+factoryBugColumns+` FROM validate_factory_bugs WHERE instance_id=? AND commit_sha=? ORDER BY id`, instanceID, commit)
	if err != nil {
		return nil, fmt.Errorf("store: list factory Bugs: %w", err)
	}
	defer rows.Close()
	var out []ValidateFactoryBug
	for rows.Next() {
		r, err := scanFactoryBug(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanFactoryBug(row scanner) (ValidateFactoryBug, error) {
	var r ValidateFactoryBug
	var created, updated string
	if err := row.Scan(&r.ID, &r.InstanceID, &r.Commit, &r.Item, &r.CommentID, &r.AuthorID, &r.Marker, &r.State, &r.Reference, &r.URL, &r.Error, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ValidateFactoryBug{}, ErrNotFound
		}
		return ValidateFactoryBug{}, fmt.Errorf("store: read factory Bug: %w", err)
	}
	var err error
	if r.CreatedAt, err = ParseTime(created); err != nil {
		return ValidateFactoryBug{}, err
	}
	if r.UpdatedAt, err = ParseTime(updated); err != nil {
		return ValidateFactoryBug{}, err
	}
	return r, nil
}
