package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// BugPRIntent is the sole immutable task PR provenance and dispatch ledger.
type BugPRIntent struct {
	SessionID                 int64
	Operation, Branch, Marker string
	CreatorID                 int64
	Dispatched, Cancelled     bool
	PR                        *BugPullRequest
}

func bugPRIntentTx(ctx context.Context, tx *sql.Tx, session int64) (BugPRIntent, error) {
	var i BugPRIntent
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT session_id,operation,branch,creator_id,marker,dispatched,cancelled,pr FROM bug_pr_intents WHERE session_id=?`, session).Scan(&i.SessionID, &i.Operation, &i.Branch, &i.CreatorID, &i.Marker, &i.Dispatched, &i.Cancelled, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return i, ErrNotFound
	}
	if err != nil {
		return i, err
	}
	if len(raw) > 0 {
		var p BugPullRequest
		if err = json.Unmarshal(raw, &p); err != nil {
			return i, err
		}
		i.PR = &p
	}
	return i, nil
}

// GetBugPRIntent reads the retained request even when creation is uncertain.
func (d *DB) GetBugPRIntent(ctx context.Context, session int64) (BugPRIntent, error) {
	var i BugPRIntent
	err := d.withTx(ctx, func(tx *sql.Tx) error { var e error; i, e = bugPRIntentTx(ctx, tx, session); return e })
	return i, err
}

// EnsureBugPRIntent retains the sole task provenance before any remote effect.
// A different call generation may reuse this PR, but never replace its identity.
func (d *DB) EnsureBugPRIntent(ctx context.Context, i BugPRIntent, callID, expected int64) (BugPRIntent, error) {
	if i.SessionID <= 0 || i.Operation == "" || len(i.Operation) > 128 || i.CreatorID <= 0 || i.Branch == "" || i.Branch == "main" || strings.ContainsAny(i.Branch, "\x00\r\n ") || i.Dispatched || i.Cancelled || i.PR != nil {
		return i, errors.New("invalid Bug PR intent")
	}
	i.Marker = fmt.Sprintf("<!-- lf:bug-pr session=%d operation=%s -->", i.SessionID, submissionDigest([]byte(i.Operation)))
	var out BugPRIntent
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var branch string
		if e := tx.QueryRowContext(ctx, `SELECT COALESCE(branch,'') FROM sessions WHERE id=?`, i.SessionID).Scan(&branch); e != nil {
			return e
		}
		if branch == "" || branch != i.Branch {
			return errors.New("bug PR branch differs from frozen task branch")
		}
		old, e := bugPRIntentTx(ctx, tx, i.SessionID)
		if e == nil {
			if old.Operation != i.Operation || old.Branch != i.Branch || old.CreatorID != i.CreatorID {
				return errors.New("bug PR immutable intent conflict")
			}
			out = old
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		if _, e = bugDeliveryEvidenceTx(ctx, tx, i.SessionID, callID); e != nil {
			return e
		}
		state, e := bugExecutionTx(ctx, tx, i.SessionID)
		if e != nil {
			return e
		}
		if state.Revision != expected {
			return ErrBugRevisionConflict
		}
		raw, e := json.Marshal(i)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO bug_pr_intents(session_id,operation,input_digest,branch,creator_id,marker) VALUES(?,?,?,?,?,?)`, i.SessionID, i.Operation, submissionDigest(raw), i.Branch, i.CreatorID, i.Marker); e != nil {
			return e
		}
		out = i
		return bugEventTx(ctx, tx, i.SessionID, "pr_intent", i.Operation, "")
	})
	return out, err
}

// ClaimBugPRCreate is a one-shot dispatch. Empty subsequent listings cannot
// reset this claim; uncertain requests remain pending for independent readback.
func (d *DB) ClaimBugPRCreate(ctx context.Context, session, callID, expected int64) (bool, error) {
	claimed := false
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		i, e := bugPRIntentTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if i.Cancelled || i.Dispatched || i.PR != nil {
			return nil
		}
		if _, e = bugDeliveryEvidenceTx(ctx, tx, session, callID); e != nil {
			return e
		}
		s, e := bugExecutionTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if s.Revision != expected {
			return ErrBugRevisionConflict
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_pr_intents SET dispatched=1 WHERE session_id=?`, session); e != nil {
			return e
		}
		claimed = true
		return bugEventTx(ctx, tx, session, "pr_dispatch", i.Operation, "")
	})
	return claimed, err
}

// BugPRObservation retains errors and returned identity before validation. The
// first numeric PR is sticky even when its other observed fields conflict.
type BugPRObservation struct {
	Operation, Reason string
	PR                *BugPullRequest
	At                time.Time
}

// ObserveBugPR appends a bounded remote fact without replacing numeric identity.
func (d *DB) ObserveBugPR(ctx context.Context, session int64, o BugPRObservation) error {
	if o.Operation == "" || len(o.Operation) > 256 || o.At.IsZero() || len(o.Reason) > 8192 {
		return errors.New("invalid Bug PR observation")
	}
	raw, err := json.Marshal(o)
	if err != nil {
		return err
	}
	digest := submissionDigest(raw)
	return d.withTx(ctx, func(tx *sql.Tx) error {
		var old string
		e := tx.QueryRowContext(ctx, `SELECT input_digest FROM bug_pr_observations WHERE session_id=? AND operation=?`, session, o.Operation).Scan(&old)
		if e == nil {
			if old != digest {
				return errors.New("bug PR observation replay conflict")
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		i, e := bugPRIntentTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if o.PR != nil && o.PR.ID > 0 && o.PR.Number > 0 {
			if i.PR != nil && (i.PR.ID != o.PR.ID || i.PR.Number != o.PR.Number) {
				return errors.New("bug PR identity cannot be replaced")
			}
			if i.PR == nil {
				p, e := json.Marshal(o.PR)
				if e != nil {
					return e
				}
				if _, e = tx.ExecContext(ctx, `UPDATE bug_pr_intents SET pr=? WHERE session_id=?`, p, session); e != nil {
					return e
				}
			}
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO bug_pr_observations(session_id,operation,input_digest,content) VALUES(?,?,?,?)`, session, o.Operation, digest, raw)
		return e
	})
}

// CancelBugDelivery suppresses new delivery effects without erasing prior effects.
func (d *DB) CancelBugDelivery(ctx context.Context, session int64) error {
	return d.withTx(ctx, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(ctx, `UPDATE bug_pr_intents SET cancelled=1 WHERE session_id=?`, session)
		if e != nil {
			return e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return ErrNotFound
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_delivery_reviews SET active=0,reason='cancelled' WHERE session_id=? AND active=1`, session); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE bug_execution SET revision=revision+1 WHERE session_id=?`, session)
		return e
	})
}
