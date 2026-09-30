package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/bugrepair"
)

// BugExhaustionContent is the retained audit snapshot for one repair-budget
// exhaustion: the sequential number, how much history stood behind it, and a
// digest of that history so a later dispute can confirm what was shown.
type BugExhaustionContent struct {
	N             int    `json:"n"`
	AttemptsSoFar int    `json:"attempts_so_far"`
	GrantsSoFar   int    `json:"grants_so_far"`
	HistoryDigest string `json:"history_digest"`
}

// bugExhaustionDecisionContent is one bug_events exhaustion_decision row:
// exactly one answer to exactly one exhaustion.
type bugExhaustionDecisionContent struct {
	Decision     string `json:"decision"`
	CommentID    int64  `json:"comment_id"`
	AuthorID     int64  `json:"author_id"`
	Instructions string `json:"instructions,omitempty"`
}

// bugGrantDecision is one recorded grant decision, keyed by the exhaustion
// reference it answered.
type bugGrantDecision struct {
	Reference    string
	CommentID    int64
	AuthorID     int64
	Instructions string
}

// bugGrantDecisionsTx lists every grant decision recorded for a bug. Ordinals
// above three are capped at 3+len(this list); ReserveBugRepair also checks
// that a reservation's grant_reference names one of them and is unused.
func bugGrantDecisionsTx(ctx context.Context, tx *sql.Tx, session int64) ([]bugGrantDecision, error) {
	rows, err := tx.QueryContext(ctx, `SELECT reference,content FROM bug_events WHERE session_id=? AND kind='exhaustion_decision' ORDER BY id`, session)
	if err != nil {
		return nil, err
	}
	var out []bugGrantDecision
	for rows.Next() {
		var reference string
		var content []byte
		if err = rows.Scan(&reference, &content); err != nil {
			_ = rows.Close()
			return nil, err
		}
		var c bugExhaustionDecisionContent
		if err = json.Unmarshal(content, &c); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if c.Decision == "grant" {
			out = append(out, bugGrantDecision{Reference: reference, CommentID: c.CommentID, AuthorID: c.AuthorID, Instructions: c.Instructions})
		}
	}
	return out, errors.Join(rows.Err(), rows.Close())
}

// bugExhaustionReferencesTx lists every exhaustion reference recorded for a
// bug, oldest first; its length is the next exhaustion's sequential number.
func bugExhaustionReferencesTx(ctx context.Context, tx *sql.Tx, session int64) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT reference FROM bug_events WHERE session_id=? AND kind='exhaustion' ORDER BY id`, session)
	if err != nil {
		return nil, err
	}
	var out []string
	for rows.Next() {
		var reference string
		if err = rows.Scan(&reference); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, reference)
	}
	return out, errors.Join(rows.Err(), rows.Close())
}

// RecordBugExhaustion appends the sequential exhaustion audit row for one
// exhausted repair-budget attempt outcome (source node attempt, never the
// analysis-pass limit). A replay for the exact same source attempt returns
// its already-recorded number; recording a distinct exhaustion while an
// earlier one still has no decision is refused (AC-003).
func (d *DB) RecordBugExhaustion(ctx context.Context, session, attemptID int64) (int, error) {
	if session <= 0 || attemptID <= 0 {
		return 0, errors.New("store: invalid Bug exhaustion identity")
	}
	var n int
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		if _, e := bugIdentityTx(ctx, tx, session); e != nil {
			return e
		}
		var existing []byte
		e := tx.QueryRowContext(ctx, `SELECT content FROM bug_events WHERE session_id=? AND kind='exhaustion' AND attempt_id=?`, session, attemptID).Scan(&existing)
		if e == nil {
			var c BugExhaustionContent
			if e = json.Unmarshal(existing, &c); e != nil {
				return e
			}
			n = c.N
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		references, e := bugExhaustionReferencesTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if len(references) > 0 {
			var decided int
			if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_events WHERE session_id=? AND kind='exhaustion_decision' AND reference=?`, session, references[len(references)-1]).Scan(&decided); e != nil {
				return e
			}
			if decided == 0 {
				return errors.New("store: an earlier Bug exhaustion is still undecided")
			}
		}
		attempts, e := bugRepairHistoryTx(ctx, tx, session)
		if e != nil {
			return e
		}
		grants, e := bugGrantDecisionsTx(ctx, tx, session)
		if e != nil {
			return e
		}
		digestSource, e := json.Marshal(attempts)
		if e != nil {
			return e
		}
		n = len(references) + 1
		content := BugExhaustionContent{N: n, AttemptsSoFar: len(attempts), GrantsSoFar: len(grants), HistoryDigest: submissionDigest(digestSource)}
		return bugEventJSONFullTx(ctx, tx, session, "exhaustion", fmt.Sprintf("exhaustion:%d", n), attemptID, "", content)
	})
	return n, err
}

// BugRepairHistoryAttempt is one lifetime repair reservation's record for the
// exhaustion question and the granted attempt's fixer prompt.
type BugRepairHistoryAttempt struct {
	Ordinal           int
	Cause             string
	GrantReference    string
	GrantInstructions string
	Summary           string
	FellShort         string
	State             string
}

// BugRepairHistoryExhaustion is one earlier exhaustion and its decision, or an
// empty Decision when a decision has not been recorded yet.
type BugRepairHistoryExhaustion struct {
	N            int
	Decision     string
	Instructions string
}

// BugRepairHistory is the bounded read shared by the exhaustion question and
// the granted attempt's fixer prompt (AC-011).
type BugRepairHistory struct {
	Attempts    []BugRepairHistoryAttempt
	Exhaustions []BugRepairHistoryExhaustion
}

// BugRepairHistory reads every lifetime repair attempt and every exhaustion
// recorded for a bug, oldest first.
func (d *DB) BugRepairHistory(ctx context.Context, session int64) (BugRepairHistory, error) {
	var out BugRepairHistory
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		attempts, e := bugRepairHistoryTx(ctx, tx, session)
		if e != nil {
			return e
		}
		exhaustions, e := bugExhaustionHistoryTx(ctx, tx, session)
		if e != nil {
			return e
		}
		out = BugRepairHistory{Attempts: attempts, Exhaustions: exhaustions}
		return nil
	})
	return out, err
}

func bugExhaustionHistoryTx(ctx context.Context, tx *sql.Tx, session int64) ([]BugRepairHistoryExhaustion, error) {
	rows, err := tx.QueryContext(ctx, `SELECT reference,content FROM bug_events WHERE session_id=? AND kind='exhaustion' ORDER BY id`, session)
	if err != nil {
		return nil, err
	}
	type entry struct {
		reference string
		content   BugExhaustionContent
	}
	var list []entry
	for rows.Next() {
		var reference string
		var raw []byte
		if err = rows.Scan(&reference, &raw); err != nil {
			_ = rows.Close()
			return nil, err
		}
		var c BugExhaustionContent
		if err = json.Unmarshal(raw, &c); err != nil {
			_ = rows.Close()
			return nil, err
		}
		list = append(list, entry{reference, c})
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	out := make([]BugRepairHistoryExhaustion, len(list))
	for i, e := range list {
		out[i] = BugRepairHistoryExhaustion{N: e.content.N}
		content, de := bugEventByReferenceTx(ctx, tx, session, "exhaustion_decision", e.reference)
		if de == nil {
			var d bugExhaustionDecisionContent
			if de = json.Unmarshal(content, &d); de != nil {
				return nil, de
			}
			out[i].Decision, out[i].Instructions = d.Decision, d.Instructions
		} else if !errors.Is(de, ErrNotFound) {
			return nil, de
		}
	}
	return out, nil
}

// bugRepairHistoryTx reads every bug_repairs row for a bug in ordinal order,
// deriving each attempt's fixer summary (from its retained completion
// evidence) and why it fell short: a failed project check, the fixer's own
// reshape/blocked/failed reason, or -- when a later reservation exists for
// review feedback -- that review's text.
func bugRepairHistoryTx(ctx context.Context, tx *sql.Tx, session int64) ([]BugRepairHistoryAttempt, error) {
	rows, err := tx.QueryContext(ctx, `SELECT ordinal,cause,evidence,state,reason,COALESCE(test_outcome,''),test_reason,completion_evidence,COALESCE(grant_reference,'') FROM bug_repairs WHERE session_id=? ORDER BY ordinal`, session)
	if err != nil {
		return nil, err
	}
	type raw struct {
		ordinal                        int
		cause, evidence, state, reason string
		testOutcome, testReason        string
		completion                     []byte
		grantReference                 string
	}
	var list []raw
	for rows.Next() {
		var r raw
		if err = rows.Scan(&r.ordinal, &r.cause, &r.evidence, &r.state, &r.reason, &r.testOutcome, &r.testReason, &r.completion, &r.grantReference); err != nil {
			_ = rows.Close()
			return nil, err
		}
		list = append(list, r)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	out := make([]BugRepairHistoryAttempt, len(list))
	for i, r := range list {
		a := BugRepairHistoryAttempt{Ordinal: r.ordinal, Cause: r.cause, State: r.state, GrantReference: r.grantReference}
		if len(r.completion) > 0 {
			var e bugrepair.Evidence
			if json.Unmarshal(r.completion, &e) == nil {
				a.Summary = e.Summary
			}
		}
		switch {
		case r.testOutcome == "failed":
			a.FellShort = strings.TrimSpace("Project checks failed: " + r.testReason)
		case r.state == "reshape":
			a.FellShort = strings.TrimSpace("Fixer asked to reshape: " + r.reason)
		case r.state == "failed":
			a.FellShort = strings.TrimSpace("Repair failed: " + r.reason)
		case r.state == "blocked":
			a.FellShort = strings.TrimSpace("Repair blocked: " + r.reason)
		case i+1 < len(list) && list[i+1].cause == "pr_feedback":
			if commentID, pe := strconv.ParseInt(list[i+1].evidence, 10, 64); pe == nil {
				content, ce := bugEventByReferenceTx(ctx, tx, session, "delivery_reply", strconv.FormatInt(commentID, 10))
				if ce == nil {
					var reply bugDeliveryReplyEventContent
					if json.Unmarshal(content, &reply) == nil {
						a.FellShort = strings.TrimSpace("Owner asked for another repair after review: " + reply.Body)
					}
				} else if !errors.Is(ce, ErrNotFound) {
					return nil, ce
				}
			}
		}
		if r.grantReference != "" {
			content, ge := bugEventByReferenceTx(ctx, tx, session, "exhaustion_decision", r.grantReference)
			if ge == nil {
				var decision bugExhaustionDecisionContent
				if json.Unmarshal(content, &decision) == nil {
					a.GrantInstructions = decision.Instructions
				}
			} else if !errors.Is(ge, ErrNotFound) {
				return nil, ge
			}
		}
		out[i] = a
	}
	return out, nil
}

// BugExhaustionQuestionComment resolves the exact posted comment id for one
// exhaustion's question. It only succeeds once the question has actually
// posted and confirmed, which is also when the bug's disposition becomes
// "exhausted" (AC-012): a caller reads this only after observing that
// disposition, so ErrNotFound here means an inconsistent read, not a race.
func (d *DB) BugExhaustionQuestionComment(ctx context.Context, session int64, n int) (int64, error) {
	var key string
	reference := fmt.Sprintf("exhaustion:%d", n)
	err := d.sql.QueryRowContext(ctx, `SELECT delivery_key FROM bug_events WHERE session_id=? AND kind='report' AND reference=?`, session, reference).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	var completed bool
	var comment int64
	if err = d.sql.QueryRowContext(ctx, `SELECT completed,COALESCE(comment_id,0) FROM report_deliveries WHERE delivery_key=?`, key).Scan(&completed, &comment); err != nil {
		return 0, err
	}
	if !completed || comment <= 0 {
		return 0, ErrNotFound
	}
	return comment, nil
}

// BugExhaustionSourceAttempt resolves the real attempt node whose exhausted
// outcome raised one exhaustion question, so a decision's own acknowledgement
// (for example a stop's notice) can name a real source attempt instead of
// inventing one that RetainBugLifecycleReport can never join against.
func (d *DB) BugExhaustionSourceAttempt(ctx context.Context, session int64, n int) (int64, error) {
	var attempt int64
	err := d.sql.QueryRowContext(ctx, `SELECT attempt_id FROM bug_events WHERE session_id=? AND kind='exhaustion' AND reference=?`, session, fmt.Sprintf("exhaustion:%d", n)).Scan(&attempt)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return attempt, err
}

// BugExhaustedReservationEvidence recovers the evidence bound to the exact
// reservation attempt an exhaustion refused (by grantReference, exhaustion:<n>),
// so a granted attempt's re-entry into the attempt node can recompute the same
// review-feedback cause the refused reservation would have used, instead of
// treating the grant's own continuation reference as that evidence.
func (d *DB) BugExhaustedReservationEvidence(ctx context.Context, session int64, grantReference string) (string, error) {
	var attempt int64
	err := d.sql.QueryRowContext(ctx, `SELECT attempt_id FROM bug_events WHERE session_id=? AND kind='exhaustion' AND reference=?`, session, grantReference).Scan(&attempt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	reference := fmt.Sprintf("repair-outcome:repair:%d:%d", session, attempt)
	var content []byte
	err = d.sql.QueryRowContext(ctx, `SELECT content FROM bug_events WHERE session_id=? AND kind='failure' AND reference=?`, session, reference).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	var c bugFailureEventContent
	if err = json.Unmarshal(content, &c); err != nil {
		return "", err
	}
	if len(c.Evidence) == 0 {
		return "", ErrNotFound
	}
	return c.Evidence[0], nil
}

// BugExhaustionGrantInstructions reads one grant's owner-authored instructions
// by its exhaustion:<n> reference, for the granted attempt's fixer prompt
// (AC-009). It returns ErrNotFound when the reference names no grant.
func (d *DB) BugExhaustionGrantInstructions(ctx context.Context, session int64, reference string) (string, error) {
	var instructions string
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		content, e := bugEventByReferenceTx(ctx, tx, session, "exhaustion_decision", reference)
		if e != nil {
			return e
		}
		var decision bugExhaustionDecisionContent
		if e = json.Unmarshal(content, &decision); e != nil {
			return e
		}
		if decision.Decision != "grant" {
			return ErrNotFound
		}
		instructions = decision.Instructions
		return nil
	})
	return instructions, err
}

// BugExhaustionDecisionRequest is one caller-parsed owner command answering an
// open exhaustion.
type BugExhaustionDecisionRequest struct {
	SessionID           int64
	N                   int
	Decision            string // "grant" or "stop"
	CommentID, AuthorID int64
	Instructions        string
	ExpectedRevision    int64
	At                  time.Time
}

// BugExhaustionOutcome is the recorded, replay-stable result of one decision.
// Successor is the zero value for a stop.
type BugExhaustionOutcome struct {
	N            int
	Decision     string
	CommentID    int64
	AuthorID     int64
	Instructions string
	Successor    GraphInstance
}

// DecideBugExhaustion records exactly one decision per exhaustion and, in the
// same transaction, either claims the budget-grant continuation or sets the
// terminal owner-stopped disposition, so a crash between the two never
// happens (AC-005, AC-006, AC-010, AC-012). An identical replay of the same
// comment returns the recorded result; a different comment for the same n, or
// an n that is not the bug's current open exhaustion, is refused.
func (d *DB) DecideBugExhaustion(ctx context.Context, in BugExhaustionDecisionRequest) (BugExhaustionOutcome, error) {
	if in.SessionID <= 0 || in.N <= 0 || in.CommentID <= 0 || in.AuthorID <= 0 || in.At.IsZero() {
		return BugExhaustionOutcome{}, errors.New("store: invalid Bug exhaustion decision")
	}
	switch in.Decision {
	case "grant":
		if len(in.Instructions) > 16384 {
			return BugExhaustionOutcome{}, errors.New("store: Bug grant instructions exceed bound")
		}
	case "stop":
		if in.Instructions != "" {
			return BugExhaustionOutcome{}, errors.New("store: a Bug stop decision carries no instructions")
		}
	default:
		return BugExhaustionOutcome{}, errors.New("store: unknown Bug exhaustion decision")
	}
	reference := fmt.Sprintf("exhaustion:%d", in.N)
	var out BugExhaustionOutcome
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var exists int
		if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_events WHERE session_id=? AND kind='exhaustion' AND reference=?`, in.SessionID, reference).Scan(&exists); e != nil {
			return e
		}
		if exists == 0 {
			return errors.New("store: unknown Bug exhaustion")
		}
		var total int
		if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_events WHERE session_id=? AND kind='exhaustion'`, in.SessionID).Scan(&total); e != nil {
			return e
		}
		if in.N != total {
			return errors.New("store: that Bug exhaustion is not the bug's current open one")
		}
		identity, e := bugIdentityTx(ctx, tx, in.SessionID)
		if e != nil {
			return e
		}
		if identity.OwnerID != in.AuthorID {
			return errors.New("store: a Bug exhaustion decision requires the configured owner")
		}
		var existing []byte
		e = tx.QueryRowContext(ctx, `SELECT content FROM bug_events WHERE session_id=? AND kind='exhaustion_decision' AND reference=?`, in.SessionID, reference).Scan(&existing)
		if e == nil {
			var old bugExhaustionDecisionContent
			if e = json.Unmarshal(existing, &old); e != nil {
				return e
			}
			if old.Decision != in.Decision || old.CommentID != in.CommentID || old.AuthorID != in.AuthorID || old.Instructions != in.Instructions {
				return errors.New("store: that Bug exhaustion is already decided")
			}
			out = BugExhaustionOutcome{N: in.N, Decision: old.Decision, CommentID: old.CommentID, AuthorID: old.AuthorID, Instructions: old.Instructions}
			if old.Decision != "grant" {
				return nil
			}
			content, e := bugEventByReferenceTx(ctx, tx, in.SessionID, "continuation", fmt.Sprintf("owner-reply:%d", old.CommentID))
			if e != nil {
				return e
			}
			var c bugContinuationEventContent
			if e = json.Unmarshal(content, &c); e != nil {
				return e
			}
			out.Successor, e = scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, c.Successor))
			return e
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if e = bugEventJSONTx(ctx, tx, in.SessionID, "exhaustion_decision", reference, bugExhaustionDecisionContent{Decision: in.Decision, CommentID: in.CommentID, AuthorID: in.AuthorID, Instructions: in.Instructions}); e != nil {
			return e
		}
		switch in.Decision {
		case "grant":
			successor, e := claimBugContinuationTx(ctx, tx, BugParentContinuation{SessionID: in.SessionID, Operation: fmt.Sprintf("owner-reply:%d", in.CommentID), Cause: "budget-grant", Reference: reference, ExpectedRevision: in.ExpectedRevision, At: in.At})
			if e != nil {
				return e
			}
			out = BugExhaustionOutcome{N: in.N, Decision: in.Decision, CommentID: in.CommentID, AuthorID: in.AuthorID, Instructions: in.Instructions, Successor: successor}
		case "stop":
			l, e := bugLifecycleTx(ctx, tx, in.SessionID)
			if e != nil {
				return e
			}
			if l.Revision != in.ExpectedRevision {
				return ErrBugRevisionConflict
			}
			if e = bugRecordLifecycleDispositionTx(ctx, tx, in.SessionID, l.ParentInstance, l.Node, "owner-stopped", reference); e != nil {
				return e
			}
			out = BugExhaustionOutcome{N: in.N, Decision: in.Decision, CommentID: in.CommentID, AuthorID: in.AuthorID}
		}
		return nil
	})
	return out, err
}
