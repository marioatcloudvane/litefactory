package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func bugTestResolutionGuardTx(ctx context.Context, tx *sql.Tx, session, call int64) error {
	c, e := scanBugTest(tx.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_test_calls WHERE id=?`, call))
	if e != nil {
		return e
	}
	if c.Request.SessionID != session || c.Outcome != "blocked" || !c.Consumed || c.Cancelled {
		return errors.New("bug resolved testing requires a consumed infrastructure block")
	}
	outcome, _, e := bugTestEvidenceTx(ctx, tx, c)
	if e != nil {
		return e
	}
	if outcome != "blocked" {
		return errors.New("bug child state is not a confirmed block")
	}
	var unsafe int
	for _, phase := range []string{"regression", "focused", "full"} {
		b := c.PhaseBinding(phase)
		var n int
		e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_check_phases WHERE session_id=? AND operation=? AND (cleanup=0 OR unreliable=1)`, session, b.Operation).Scan(&n)
		if e != nil {
			return e
		}
		unsafe += n
	}
	if unsafe != 0 {
		return errors.New("bug child cleanup or known flakiness prevents fresh testing")
	}
	return nil
}

// RecordBugTestResolution records the owner's resolved infrastructure cause. It
// cannot waive known flakiness, uncertain cleanup, checks or either approval.
func (d *DB) RecordBugTestResolution(ctx context.Context, session, call int64, in GraphOwnerReply) error {
	prefix := fmt.Sprintf("/retry-bug-tests %d ", call)
	if in.Mode != "" || in.OwnerID <= 0 || in.AuthorID != in.OwnerID || in.CommentID <= 0 || in.SourceRef != in.TaskRef || in.At.IsZero() || !strings.HasPrefix(in.Body, prefix) || strings.TrimSpace(strings.TrimPrefix(in.Body, prefix)) == "" || len(in.Body) > 65536 {
		return errors.New("invalid Bug test resolution")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		identity, e := bugIdentityTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if identity.OwnerID != in.OwnerID || in.TaskRef != fmt.Sprintf("github:%s#%d", identity.Repository, identity.IssueNumber) {
			return errors.New("bug test resolution task/owner mismatch")
		}
		var oldCall, author int64
		var body string
		e = tx.QueryRowContext(ctx, `SELECT call_id,author_id,body FROM bug_test_resolutions WHERE session_id=? AND comment_id=?`, session, in.CommentID).Scan(&oldCall, &author, &body)
		if e == nil {
			if oldCall != call || author != in.AuthorID || body != in.Body {
				return errors.New("bug test resolution replay conflict")
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if e = bugTestResolutionGuardTx(ctx, tx, session, call); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO bug_test_resolutions(session_id,call_id,comment_id,author_id,body,resolution) VALUES(?,?,?,?,?,?)`, session, call, in.CommentID, in.AuthorID, in.Body, strings.TrimSpace(strings.TrimPrefix(in.Body, prefix)))
		return e
	})
}

// BugTestResolution reads the retained source for a fresh child generation.
func (d *DB) BugTestResolution(ctx context.Context, session, call int64) (string, error) {
	var s string
	e := d.sql.QueryRowContext(ctx, `SELECT resolution FROM bug_test_resolutions WHERE session_id=? AND call_id=?`, session, call).Scan(&s)
	if errors.Is(e, sql.ErrNoRows) {
		e = ErrNotFound
	}
	return s, e
}
