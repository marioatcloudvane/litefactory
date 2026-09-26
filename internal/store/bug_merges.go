package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// BugMergeBinding snapshots the two authorizations and their full evidence chain.
type BugMergeBinding struct {
	Review   BugDeliveryReview
	Approval BugDeliveryDecision
}

// BugIntegrationObservation contains independent forge facts, never response-only proof.
type BugIntegrationObservation struct {
	At           time.Time
	PR           BugPullRequest
	MergeCommit  string
	Parents      []string
	Main         string
	MergedAt     time.Time
	RequestError string
	BindingError string
	Kind         string
}

// BugMergeOperation separates dispatch, observed integration and closure eligibility.
type BugMergeOperation struct {
	SessionID                  int64
	Operation, ReviewDigest    string
	Binding                    BugMergeBinding
	Dispatched                 bool
	Outcome, Reason, Exception string
	Observations               []BugIntegrationObservation
}

func bugMergeTx(ctx context.Context, tx *sql.Tx, session int64, operation string) (BugMergeOperation, error) {
	var out BugMergeOperation
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT session_id,operation,review_digest,binding,dispatched,outcome,reason,exception FROM bug_merges WHERE session_id=? AND operation=?`, session, operation).Scan(&out.SessionID, &out.Operation, &out.ReviewDigest, &raw, &out.Dispatched, &out.Outcome, &out.Reason, &out.Exception)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(raw, &out.Binding); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT content FROM bug_merge_observations WHERE session_id=? AND operation=? ORDER BY sequence`, session, operation)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var o BugIntegrationObservation
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

// GetBugMerge retrieves a merge operation without inferring remote success.
func (d *DB) GetBugMerge(ctx context.Context, session int64, operation string) (BugMergeOperation, error) {
	var out BugMergeOperation
	err := d.withTx(ctx, func(tx *sql.Tx) error { var e error; out, e = bugMergeTx(ctx, tx, session, operation); return e })
	return out, err
}

// bugMergeAuthorityTx excludes only this operation's own save guard while
// validating domain evidence. Removal and restoration occur in one transaction,
// so no concurrent accessor can observe an unlocked task.
func bugMergeAuthorityTx(ctx context.Context, tx *sql.Tx, m BugMergeOperation) (BugMergeBinding, error) {
	owner := "merge:" + m.Operation
	var guard string
	err := tx.QueryRowContext(ctx, `SELECT owner FROM bug_work_guards WHERE session_id=?`, m.SessionID).Scan(&guard)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return BugMergeBinding{}, err
	}
	if guard == owner {
		if _, err = tx.ExecContext(ctx, `DELETE FROM bug_work_guards WHERE session_id=? AND owner=?`, m.SessionID, owner); err != nil {
			return BugMergeBinding{}, err
		}
	}
	r, a, err := approvedBugDeliveryTx(ctx, tx, m.SessionID, m.ReviewDigest)
	if err != nil {
		return BugMergeBinding{}, err
	}
	if guard == owner {
		if _, err = tx.ExecContext(ctx, `INSERT INTO bug_work_guards(session_id,owner,kind) VALUES(?,?,'save')`, m.SessionID, owner); err != nil {
			return BugMergeBinding{}, err
		}
	}
	return BugMergeBinding{Review: r, Approval: a}, nil
}

// EnsureBugMerge retains immutable intent before any merge-related forge read.
func (d *DB) EnsureBugMerge(ctx context.Context, session int64, operation, digest string) (BugMergeOperation, error) {
	if session <= 0 || operation == "" || len(operation) > 128 || strings.ContainsAny(operation, "\x00\r\n") || len(digest) != 64 {
		return BugMergeOperation{}, errors.New("invalid Bug merge input")
	}
	var out BugMergeOperation
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		old, e := bugMergeTx(ctx, tx, session, operation)
		if e == nil {
			if old.ReviewDigest != digest {
				return errors.New("bug merge input identity conflict")
			}
			out = old
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		out = BugMergeOperation{SessionID: session, Operation: operation, ReviewDigest: digest, Outcome: "pending"}
		out.Binding.Review, e = bugDeliveryReviewTx(ctx, tx, session, digest)
		if e != nil {
			return e
		}
		// Preserve historical authorization, including its absence, for external
		// integration reconciliation. Intent itself grants no dispatch authority.
		var at string
		a := &out.Binding.Approval
		e = tx.QueryRowContext(ctx, `SELECT session_id,comment_id,author_id,digest,kind,body,observed_at FROM bug_delivery_replies WHERE session_id=? AND digest=? AND kind='approved'`, session, digest).Scan(&a.SessionID, &a.CommentID, &a.OwnerID, &a.Digest, &a.Kind, &a.Body, &at)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if e == nil {
			a.At, e = time.Parse(time.RFC3339Nano, at)
			if e != nil {
				return e
			}
		}
		var unresolved int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_merges WHERE session_id=? AND (dispatched=1 OR exception!='' OR outcome IN ('pending','confirmed'))`, session).Scan(&unresolved); e != nil {
			return e
		}
		if unresolved != 0 {
			return errors.New("previous Bug merge effect remains unresolved")
		}

		raw, e := json.Marshal(out.Binding)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO bug_merges(session_id,operation,review_digest,binding) VALUES(?,?,?,?)`, session, operation, digest, raw); e != nil {
			return e
		}
		return bugEventTx(ctx, tx, session, "merge_intent", operation, "")
	})
	return out, err
}

// BugMergePrerequisites revalidates current local authority without changing it.
func (d *DB) BugMergePrerequisites(ctx context.Context, session int64, operation string) (BugMergeOperation, int64, error) {
	var out BugMergeOperation
	var revision int64
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		out, e = bugMergeTx(ctx, tx, session, operation)
		if e != nil {
			return e
		}
		binding, e := bugMergeAuthorityTx(ctx, tx, out)
		if e != nil {
			return e
		}
		if binding.Review.Digest != out.Binding.Review.Digest || binding.Approval.CommentID != out.Binding.Approval.CommentID {
			return errors.New("bug merge authorization changed")
		}
		s, e := bugExecutionTx(ctx, tx, session)
		revision = s.Revision
		return e
	})
	return out, revision, err
}

// ClaimBugMerge records one possible remote mutation and excludes repair/check/save work.
func (d *DB) ClaimBugMerge(ctx context.Context, session int64, operation string, expected int64) (bool, error) {
	claimed := false
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		m, e := bugMergeTx(ctx, tx, session, operation)
		if e != nil {
			return e
		}
		if m.Dispatched || m.Outcome != "pending" || m.Exception != "" {
			return nil
		}
		current, e := bugMergeAuthorityTx(ctx, tx, m)
		if e != nil {
			return e
		}
		if current.Approval.CommentID != m.Binding.Approval.CommentID || current.Review.Digest != m.Binding.Review.Digest {
			return errors.New("bug merge intent does not bind current approval")
		}

		s, e := bugExecutionTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if s.Revision != expected {
			return ErrBugRevisionConflict
		}
		locked, e := claimBugWorkTx(ctx, tx, session, "merge:"+operation, "save")
		if e != nil {
			return e
		}
		if !locked {
			return ErrBugWorkActive
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_merges SET dispatched=1 WHERE session_id=? AND operation=?`, session, operation); e != nil {
			return e
		}
		claimed = true
		return bugEventTx(ctx, tx, session, "merge_dispatch", operation, "")
	})
	return claimed, err
}

func validBugMergeSHA(s string) bool {
	return len(s) == 40 && strings.Trim(s, "0123456789abcdef") == ""
}

// BugIntegrationProof derives confirmation and exceptions from independent facts.
func BugIntegrationProof(m BugMergeOperation, o BugIntegrationObservation) (string, string) {
	r := m.Binding.Review
	p := o.PR
	want := r.PR
	t := r.Evidence.Child.Request.Binding.TargetCommit
	if p.ID != want.ID || p.Number != want.Number || p.CreatorID != want.CreatorID || p.HeadRepo != want.HeadRepo || p.BaseRepo != want.BaseRepo || p.Branch != want.Branch || p.Target != "main" || p.Head != want.Head {
		return "blocked", "remote PR differs from approved identity, head or target"
	}
	if !p.Merged {
		return "pending", "merge has not been independently observed"
	}
	if p.State != "closed" || !validBugMergeSHA(o.MergeCommit) {
		return "pending", "merged PR lacks complete commit observations"
	}
	if o.MergedAt.IsZero() || m.Binding.Approval.At.IsZero() || r.Evidence.Approval.ApprovedAt.IsZero() || !m.Binding.Approval.At.Before(o.MergedAt) || !r.Evidence.Approval.ApprovedAt.Before(o.MergedAt) {
		return "blocked", "both exact approvals must precede observed integration"
	}
	if len(o.Parents) == 0 || o.Main == "" {
		return "pending", "merge commit or main observation is missing"
	}
	if len(o.Parents) != 2 || o.Parents[0] != t || o.Parents[1] != want.Head {
		return "blocked", "integration used unexpected target or merge shape"
	}
	if o.Main != o.MergeCommit {
		return "blocked", "main moved away from merge commit during confirmation"
	}
	if o.BindingError != "" {
		return "blocked", o.BindingError
	}
	if m.Exception != "" {
		return "blocked", m.Exception
	}
	return "confirmed", ""
}

// ObserveBugMerge appends facts and derives confirmation atomically. Exceptions
// are sticky; subsequent ancestry or newer observations cannot waive them.
func (d *DB) ObserveBugMerge(ctx context.Context, session int64, operation string, o BugIntegrationObservation) (BugMergeOperation, error) {
	if o.At.IsZero() || len(o.RequestError) > 8192 || len(o.BindingError) > 8192 || len(o.Parents) > 64 {
		return BugMergeOperation{}, errors.New("invalid Bug merge observation")
	}
	switch o.Kind {
	case "request", "request_refused", "precheck", "integration", "unavailable":
	default:
		return BugMergeOperation{}, errors.New("invalid Bug merge observation kind")
	}
	var out BugMergeOperation
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		m, e := bugMergeTx(ctx, tx, session, operation)
		if e != nil {
			return e
		}
		raw, e := json.Marshal(o)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO bug_merge_observations(session_id,operation,sequence,content) VALUES(?,?,?,?)`, session, operation, len(m.Observations)+1, raw); e != nil {
			return e
		}
		outcome, reason := m.Outcome, m.Reason
		exception := m.Exception
		if m.Outcome != "confirmed" || o.Kind == "integration" {
			if o.Kind == "request_refused" {
				outcome, reason = "failed", o.RequestError
			}
			if o.Kind == "integration" {
				outcome, reason = BugIntegrationProof(m, o)
				if m.Outcome == "failed" && !o.PR.Merged {
					outcome, reason = "failed", m.Reason
				}
				if m.Outcome == "confirmed" && outcome == "pending" {
					outcome, reason = "confirmed", m.Reason
				}
				if outcome == "confirmed" {
					if _, e = bugMergeAuthorityTx(ctx, tx, m); e != nil {
						outcome, reason = "blocked", "integration observed but current authorization is no longer applicable"
					}
				}
				if outcome == "blocked" && o.PR.Merged {
					exception = reason
				}
			} else if o.Kind == "precheck" && o.Main != "" && o.Main != m.Binding.Review.Evidence.Child.Request.Binding.TargetCommit {
				outcome, reason = "stale", "main changed before merge dispatch"
			}
			if o.RequestError != "" {
				reason = o.RequestError
			}
			if exception != "" {
				outcome = "blocked"
			}
			if _, e = tx.ExecContext(ctx, `UPDATE bug_merges SET outcome=?,reason=?,exception=? WHERE session_id=? AND operation=?`, outcome, reason, exception, session, operation); e != nil {
				return e
			}
			knownMerged := o.PR.Merged && o.PR.ID == m.Binding.Review.PR.ID && o.PR.Number == m.Binding.Review.PR.Number && o.PR.BaseRepo == m.Binding.Review.PR.BaseRepo && o.PR.HeadRepo == m.Binding.Review.PR.HeadRepo
			if outcome == "confirmed" || outcome == "failed" || outcome == "blocked" && knownMerged {
				if _, e = tx.ExecContext(ctx, `DELETE FROM bug_work_guards WHERE session_id=? AND owner=?`, session, "merge:"+operation); e != nil {
					return e
				}
			}
		}
		out, e = bugMergeTx(ctx, tx, session, operation)
		return e
	})
	return out, err
}

// ConfirmedBugIntegration is the closure handoff. It introduces no fresh remote
// target-tip rule and cannot turn a receipt or externally merged PR into authority.
func (d *DB) ConfirmedBugIntegration(ctx context.Context, session int64, operation string) (BugMergeOperation, error) {
	var out BugMergeOperation
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		out, e = confirmedBugIntegrationTx(ctx, tx, session, operation)
		return e
	})
	return out, err
}
func confirmedBugIntegrationTx(ctx context.Context, tx *sql.Tx, session int64, operation string) (BugMergeOperation, error) {
	var out BugMergeOperation
	err := func() error {
		var e error
		out, e = bugMergeTx(ctx, tx, session, operation)
		if e != nil {
			return e
		}
		if out.Outcome != "confirmed" || out.Exception != "" {
			return errors.New("bug integration is not confirmed without exceptions")
		}
		if _, e = bugMergeAuthorityTx(ctx, tx, out); e != nil {
			return e
		}
		for _, o := range out.Observations {
			if o.Kind == "integration" {
				if status, _ := BugIntegrationProof(out, o); status == "confirmed" {
					return nil
				}
			}
		}
		return errors.New("bug integration proof is missing")
	}()
	return out, err
}

// StopBugMerge retains pre-dispatch refusal without hiding dispatched uncertainty.
func (d *DB) StopBugMerge(ctx context.Context, session int64, operation, outcome, reason string) (BugMergeOperation, error) {
	if (outcome != "stale" && outcome != "blocked" && outcome != "failed") || reason == "" || len(reason) > 8192 {
		return BugMergeOperation{}, errors.New("invalid Bug merge stop")
	}
	var out BugMergeOperation
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		m, e := bugMergeTx(ctx, tx, session, operation)
		if e != nil {
			return e
		}
		if m.Dispatched || m.Outcome == "confirmed" {
			out = m
			return nil
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_merges SET outcome=?,reason=? WHERE session_id=? AND operation=?`, outcome, reason, session, operation); e != nil {
			return e
		}
		if e = bugEventTx(ctx, tx, session, "merge_stop", operation, reason); e != nil {
			return e
		}
		out, e = bugMergeTx(ctx, tx, session, operation)
		return e
	})
	return out, err
}

// PrepareBugMergeRetry admits an explicit factory continuation only after a
// retained definitive refusal and independent open-PR reconciliation. It never
// starts a mutation and cannot resolve a transport timeout as nonexecution.
func (d *DB) PrepareBugMergeRetry(ctx context.Context, session int64, operation, continuation, evidence string, expected int64) error {
	if continuation == "" || len(continuation) > 128 || evidence == "" || len(evidence) > 8192 {
		return errors.New("invalid Bug merge retry continuation")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		var old string
		e := tx.QueryRowContext(ctx, `SELECT evidence FROM bug_merge_retries WHERE session_id=? AND operation=? AND continuation=?`, session, operation, continuation).Scan(&old)
		if e == nil {
			if old != evidence {
				return errors.New("bug merge retry identity conflict")
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		m, e := bugMergeTx(ctx, tx, session, operation)
		if e != nil {
			return e
		}
		if m.Outcome != "failed" || !m.Dispatched || m.Exception != "" {
			return errors.New("bug merge retry requires a resolved definitive refusal")
		}
		refused, reconciled := false, false
		for _, o := range m.Observations {
			if o.Kind == "request" {
				refused, reconciled = false, false
			}
			if o.Kind == "request_refused" {
				refused, reconciled = true, false
			}
			if o.Kind == "integration" && refused && !o.PR.Merged && o.PR.State == "open" && o.PR.ID == m.Binding.Review.PR.ID {
				reconciled = true
			}
		}
		if !refused || !reconciled {
			return errors.New("bug merge retry lacks independent refusal reconciliation")
		}
		b, e := bugMergeAuthorityTx(ctx, tx, m)
		if e != nil {
			return e
		}
		if b.Approval.CommentID != m.Binding.Approval.CommentID {
			return errors.New("bug merge retry authorization changed")
		}
		s, e := bugExecutionTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if s.Revision != expected {
			return ErrBugRevisionConflict
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO bug_merge_retries(session_id,operation,continuation,evidence) VALUES(?,?,?,?)`, session, operation, continuation, evidence); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_merges SET dispatched=0,outcome='pending',reason='' WHERE session_id=? AND operation=?`, session, operation); e != nil {
			return e
		}
		return bugEventTx(ctx, tx, session, "merge_retry", continuation, evidence)
	})
}
