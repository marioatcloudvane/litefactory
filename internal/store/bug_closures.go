package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// BugClosureObservation excludes untrusted issue prose; state is a remote fact.
type BugClosureObservation struct {
	Kind, Endpoint, State, Reason string
	ID                            int64
	Number                        int
	At                            time.Time
}

// BugClosureOperation preserves historical confirmation even after an observed reopen.
type BugClosureOperation struct {
	SessionID                           int64
	Operation, MergeOperation, Endpoint string
	Integration                         BugMergeOperation
	Dispatched, Confirmed, Conflict     bool
	Outcome, Reason                     string
	Observations                        []BugClosureObservation
}

func bugClosureTx(ctx context.Context, tx *sql.Tx, session int64) (BugClosureOperation, error) {
	var out BugClosureOperation
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT session_id,operation,merge_operation,endpoint,binding,dispatched,confirmed,outcome,reason,conflict FROM bug_closures WHERE session_id=?`, session).Scan(&out.SessionID, &out.Operation, &out.MergeOperation, &out.Endpoint, &raw, &out.Dispatched, &out.Confirmed, &out.Outcome, &out.Reason, &out.Conflict)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(raw, &out.Integration); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT content FROM bug_closure_observations WHERE session_id=? ORDER BY sequence`, session)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var o BugClosureObservation
		if err = rows.Scan(&raw); err != nil {
			return out, err
		}
		if err = json.Unmarshal(raw, &o); err != nil {
			return out, err
		}
		out.Observations = append(out.Observations, o)
	}
	return out, rows.Err()
}

// GetBugClosure retrieves the retained closure operation and its observations.
func (d *DB) GetBugClosure(ctx context.Context, session int64) (BugClosureOperation, error) {
	var out BugClosureOperation
	err := d.withTx(ctx, func(tx *sql.Tx) error { var e error; out, e = bugClosureTx(ctx, tx, session); return e })
	return out, err
}

// EnsureBugClosure retains identity before a forge read, even if subsequent
// authorization checks block. Endpoint comes from factory forge configuration.
func (d *DB) EnsureBugClosure(ctx context.Context, session int64, operation, merge, endpoint string) (BugClosureOperation, error) {
	if operation == "" || len(operation) > 128 || merge == "" || len(merge) > 128 {
		return BugClosureOperation{}, errors.New("invalid Bug closure identity")
	}
	var out BugClosureOperation
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		old, e := bugClosureTx(ctx, tx, session)
		if e == nil {
			if old.Operation != operation || old.MergeOperation != merge || old.Endpoint != endpoint {
				return errors.New("bug closure identity conflict")
			}
			out = old
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		m, e := bugMergeTx(ctx, tx, session, merge)
		if e != nil {
			return e
		}
		id := m.Binding.Review.Evidence.Identity
		u, e := url.Parse(endpoint)
		if e != nil || u.Scheme != "https" && u.Scheme != "http" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !strings.HasSuffix(u.Path, fmt.Sprintf("/repos/%s/issues/%d", id.Repository, id.IssueNumber)) {
			return errors.New("invalid Bug closure endpoint")
		}
		raw, e := json.Marshal(m)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO bug_closures(session_id,operation,merge_operation,endpoint,binding) VALUES(?,?,?,?,?)`, session, operation, merge, endpoint, raw); e != nil {
			return e
		}
		out, e = bugClosureTx(ctx, tx, session)
		return e
	})
	return out, err
}
func bugClosureGuardTx(ctx context.Context, tx *sql.Tx, c BugClosureOperation) error {
	if c.Integration.Outcome != "confirmed" || c.Integration.Exception != "" {
		return errors.New("bug closure intent lacks confirmed authorized integration")
	}
	m, e := confirmedBugIntegrationTx(ctx, tx, c.SessionID, c.MergeOperation)
	if e != nil {
		return e
	}
	if c.Conflict || m.Binding.Review.Digest != c.Integration.Binding.Review.Digest || m.Binding.Approval.CommentID != c.Integration.Binding.Approval.CommentID {
		return errors.New("bug closure integration binding changed or issue reopened")
	}
	return nil
}

// BugClosurePrerequisites checks the confirmed authorization and integration chain.
func (d *DB) BugClosurePrerequisites(ctx context.Context, session int64) (int64, error) {
	var revision int64
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		c, e := bugClosureTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if e = bugClosureGuardTx(ctx, tx, c); e != nil {
			return e
		}
		s, e := bugExecutionTx(ctx, tx, session)
		revision = s.Revision
		return e
	})
	return revision, err
}

// ClaimBugClosure atomically reserves the remote effect under the current evidence revision.
func (d *DB) ClaimBugClosure(ctx context.Context, session, expected int64) (bool, error) {
	claimed := false
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		c, e := bugClosureTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if c.Dispatched || c.Confirmed || c.Conflict {
			return nil
		}
		if e = bugClosureGuardTx(ctx, tx, c); e != nil {
			return e
		}
		s, e := bugExecutionTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if s.Revision != expected {
			return ErrBugRevisionConflict
		}
		if len(c.Observations) == 0 {
			return errors.New("bug closure requires independent open observation")
		}
		o := c.Observations[len(c.Observations)-1]
		if o.Kind != "issue" || o.State != "open" || !validBugClosureObservation(c, o) {
			return errors.New("bug closure lacks matching open issue")
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_closures SET dispatched=1 WHERE session_id=?`, session); e != nil {
			return e
		}
		claimed = true
		return bugEventTx(ctx, tx, session, "close_dispatch", c.Operation, "")
	})
	return claimed, err
}
func validBugClosureObservation(c BugClosureOperation, o BugClosureObservation) bool {
	i := c.Integration.Binding.Review.Evidence.Identity
	return o.Endpoint == c.Endpoint && o.ID == i.IssueID && int64(o.Number) == i.IssueNumber && (o.State == "open" || o.State == "closed")
}

// ObserveBugClosure atomically retains the observation and current outcome.
func (d *DB) ObserveBugClosure(ctx context.Context, session int64, o BugClosureObservation) (BugClosureOperation, error) {
	if o.At.IsZero() || len(o.Reason) > 8192 || len(o.Endpoint) > 8192 {
		return BugClosureOperation{}, errors.New("invalid Bug closure observation")
	}
	switch o.Kind {
	case "issue", "request", "request_refused", "unavailable", "blocked":
	default:
		return BugClosureOperation{}, errors.New("invalid Bug closure observation kind")
	}
	var out BugClosureOperation
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		c, e := bugClosureTx(ctx, tx, session)
		if e != nil {
			return e
		}
		raw, e := json.Marshal(o)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO bug_closure_observations(session_id,sequence,content) VALUES(?,?,?)`, session, len(c.Observations)+1, raw); e != nil {
			return e
		}
		outcome, reason := c.Outcome, c.Reason
		confirmed, conflict := c.Confirmed, c.Conflict
		switch o.Kind {
		case "request_refused":
			outcome, reason = "failed", o.Reason
		case "request", "unavailable":
			outcome, reason = "pending", o.Reason
		case "blocked":
			outcome, reason = "blocked", o.Reason
		case "issue":
			if !validBugClosureObservation(c, o) {
				outcome, reason = "blocked", "issue observation does not match recorded repository and numeric identity"
				break
			}
			hadClosed := c.Confirmed
			for _, old := range c.Observations {
				if old.Kind == "issue" && old.State == "closed" && validBugClosureObservation(c, old) {
					hadClosed = true
				}
			}
			if o.State == "open" && hadClosed {
				conflict = true
				outcome, reason = "pending", "issue reopened after an observed closure; operator reconciliation required"
				break
			}
			if e = bugClosureGuardTx(ctx, tx, c); e != nil {
				outcome, reason = "blocked", "authorized integration is not currently applicable"
				break
			}
			if o.State == "closed" {
				confirmed = true
				outcome, reason = "confirmed", ""
			} else if c.Outcome != "failed" {
				outcome, reason = "pending", "issue remains open"
			}
		}
		if conflict {
			outcome, reason = "pending", "issue reopened after an observed closure; operator reconciliation required"
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_closures SET confirmed=?,conflict=?,outcome=?,reason=? WHERE session_id=?`, confirmed, conflict, outcome, reason, session); e != nil {
			return e
		}
		out, e = bugClosureTx(ctx, tx, session)
		return e
	})
	return out, err
}

// ConfirmedBugClosure supplies lifecycle evidence without repeating merge or close.
func (d *DB) ConfirmedBugClosure(ctx context.Context, session int64) (BugClosureOperation, error) {
	var out BugClosureOperation
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		out, e = bugClosureTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if !out.Confirmed || out.Conflict || out.Outcome != "confirmed" {
			return errors.New("bug closure is not confirmed")
		}
		return bugClosureGuardTx(ctx, tx, out)
	})
	return out, err
}

// PrepareBugClosureRetry records an explicit continuation after definitive
// refusal and a matching open read, never from open state or a timeout alone.
func (d *DB) PrepareBugClosureRetry(ctx context.Context, session int64, continuation, evidence string, expected int64) error {
	if continuation == "" || len(continuation) > 128 || evidence == "" || len(evidence) > 8192 {
		return errors.New("invalid Bug closure retry")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		var old string
		e := tx.QueryRowContext(ctx, `SELECT evidence FROM bug_closure_retries WHERE session_id=? AND continuation=?`, session, continuation).Scan(&old)
		if e == nil {
			if old != evidence {
				return errors.New("bug closure retry conflict")
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		c, e := bugClosureTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if c.Outcome != "failed" || !c.Dispatched || c.Confirmed || c.Conflict {
			return errors.New("bug closure retry requires definitive nonapplication")
		}
		refused, reconciled := false, false
		for _, o := range c.Observations {
			if o.Kind == "request" {
				refused, reconciled = false, false
			}
			if o.Kind == "request_refused" {
				refused, reconciled = true, false
			}
			if o.Kind == "issue" && o.State == "open" && validBugClosureObservation(c, o) && refused {
				reconciled = true
			}
		}
		if !refused || !reconciled {
			return errors.New("bug closure refusal is not reconciled")
		}
		if e = bugClosureGuardTx(ctx, tx, c); e != nil {
			return e
		}
		s, e := bugExecutionTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if s.Revision != expected {
			return ErrBugRevisionConflict
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO bug_closure_retries(session_id,continuation,evidence) VALUES(?,?,?)`, session, continuation, evidence); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE bug_closures SET dispatched=0,outcome='pending',reason='' WHERE session_id=?`, session)
		return e
	})
}
