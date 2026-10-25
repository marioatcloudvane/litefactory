package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ValidateReturn retains the exact reviewed work and the owner's explanation.
// Selection is saved before any story is reopened and is immutable on replay.
type ValidateReturn struct {
	InstanceID                                       int64
	Parent                                           string
	Generation                                       int64
	Commit, ReportRevision                           string
	CommentID, AuthorID                              int64
	Explanation, Context, Selection, SelectionSource string
	Applied                                          bool
	CreatedAt                                        time.Time
}

const validateReturnColumns = `instance_id,parent,generation,commit_sha,report_revision,comment_id,author_id,explanation,context,selection,selection_source,applied,created_at`

func scanValidateReturn(row scanner) (ValidateReturn, error) {
	var r ValidateReturn
	var at string
	err := row.Scan(&r.InstanceID, &r.Parent, &r.Generation, &r.Commit, &r.ReportRevision, &r.CommentID, &r.AuthorID, &r.Explanation, &r.Context, &r.Selection, &r.SelectionSource, &r.Applied, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	r.CreatedAt, err = ParseTime(at)
	return r, err
}

// GetValidateReturn reads one immutable owner return.
func (db *DB) GetValidateReturn(ctx context.Context, id int64) (ValidateReturn, error) {
	return scanValidateReturn(db.sql.QueryRowContext(ctx, `SELECT `+validateReturnColumns+` FROM validate_returns WHERE instance_id=?`, id))
}

// LatestValidateReturn reads the newest return in the frozen generation.
func (db *DB) LatestValidateReturn(ctx context.Context, parent string, generation int64) (ValidateReturn, error) {
	return scanValidateReturn(db.sql.QueryRowContext(ctx, `SELECT `+validateReturnColumns+` FROM validate_returns WHERE parent=? AND generation=? ORDER BY instance_id DESC LIMIT 1`, parent, generation))
}

// PutValidateReturn retains exact owner input and rejects edited replays.
func (db *DB) PutValidateReturn(ctx context.Context, in ValidateReturn) (ValidateReturn, error) {
	if in.InstanceID < 1 || in.Parent == "" || in.Generation < 1 || in.CommentID < 1 || in.AuthorID < 1 || in.Explanation == "" || in.Context == "" || in.CreatedAt.IsZero() {
		return ValidateReturn{}, errors.New("store: invalid reviewed return")
	}
	_, err := db.sql.ExecContext(ctx, `INSERT OR IGNORE INTO validate_returns(instance_id,parent,generation,commit_sha,report_revision,comment_id,author_id,explanation,context,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, in.InstanceID, in.Parent, in.Generation, in.Commit, in.ReportRevision, in.CommentID, in.AuthorID, in.Explanation, in.Context, FormatTime(in.CreatedAt))
	if err != nil {
		return ValidateReturn{}, err
	}
	out, err := db.GetValidateReturn(ctx, in.InstanceID)
	if err == nil && (out.Parent != in.Parent || out.Generation != in.Generation || out.CommentID != in.CommentID || out.AuthorID != in.AuthorID || out.Context != in.Context || out.Explanation != in.Explanation || out.Commit != in.Commit || out.ReportRevision != in.ReportRevision) {
		return ValidateReturn{}, errors.New("store: reviewed return already retained with different input")
	}
	return out, err
}

// SelectValidateReturn saves the factory-validated selection once.
func (db *DB) SelectValidateReturn(ctx context.Context, id int64, selection, source string) (ValidateReturn, error) {
	if selection == "" || source == "" {
		return ValidateReturn{}, errors.New("store: empty return selection")
	}
	_, err := db.sql.ExecContext(ctx, `UPDATE validate_returns SET selection=?,selection_source=? WHERE instance_id=? AND selection=''`, selection, source, id)
	if err != nil {
		return ValidateReturn{}, err
	}
	out, err := db.GetValidateReturn(ctx, id)
	if err == nil && (out.Selection != selection || out.SelectionSource != source) {
		return out, fmt.Errorf("store: return selection already retained")
	}
	return out, err
}

// ApplyValidateReturn marks a saved selection as reopened for dispatch.
func (db *DB) ApplyValidateReturn(ctx context.Context, id int64) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE validate_returns SET applied=1 WHERE instance_id=? AND selection<>''`, id)
	return err
}
