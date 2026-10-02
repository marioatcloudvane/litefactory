package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

func (p BugParentPolicy) publishReview(ctx context.Context, r store.BugDeliveryReview) (store.BugDeliveryReview, error) {
	if p.Reporter == nil || p.Reporter.botUserID != r.BotID {
		return r, errors.New("bug review publisher identity mismatch")
	}
	session, e := p.Reporter.db.GetSession(ctx, r.SessionID)
	if e != nil {
		return r, e
	}
	// The review digest itself is a unique stable marker included in the exact
	// immutable body. Reconciliation also requires numeric bot and exact bytes.
	intent := store.ReportDelivery{Key: r.DeliveryKey, Version: 1, SessionID: r.SessionID, Task: session.TaskRef, Destination: session.TaskRef, TriggerNamespace: "bug-review", Purpose: "delivery-review", BotUserID: r.BotID, Body: r.Body, Marker: "review `" + r.Digest + "`"}
	saved, e := p.Reporter.db.EnsureReportDelivery(ctx, intent)
	if e != nil {
		return r, e
	}
	if _, e = p.Reporter.dispatchDelivery(ctx, saved); e != nil {
		return r, e
	}
	return p.Reporter.db.ConfirmBugDeliveryReview(ctx, r.SessionID, r.Digest)
}
func (h bugParentActivity) publishDelivery(ctx context.Context, opts Options, req ActivityRequest, i store.GraphInstance) (ActivityResult, error) {
	var source BugPROutput
	a, e := bugParentOutput(ctx, opts.DB, i.ID, "pr", &source)
	if e != nil {
		return ActivityResult{}, e
	}
	if source.Review == nil || (source.Outcome != "review-ready" && source.Outcome != "clarify") {
		return ActivityResult{}, errors.New("bug delivery wait lacks review outcome")
	}
	r, e := opts.DB.ConfirmBugDeliveryReview(ctx, i.SessionID, source.Review.Digest)
	if e != nil {
		return ActivityResult{}, e
	}
	body, e := json.Marshal(r)
	if e != nil {
		return ActivityResult{}, e
	}
	if e = opts.DB.RetainGraphComment(ctx, store.GraphCommentIntent{Key: r.DeliveryKey, InstanceID: i.ID, SourceAttemptID: a.ID, Node: req.Node, Kind: "review", Candidate: body}); e != nil {
		return ActivityResult{}, e
	}
	e = opts.DB.RecordBugLifecycleDisposition(ctx, i.SessionID, i.ID, req.Node, "delivery-review", r.Digest)
	return ActivityResult{Outcome: "published", Kind: store.GraphOutputJSON, Output: body}, e
}
func (h bugParentActivity) publishNotice(ctx context.Context, opts Options, req ActivityRequest, i store.GraphInstance) (ActivityResult, error) {
	attempts, e := opts.DB.ListGraphNodeAttempts(ctx, i.ID)
	if e != nil {
		return ActivityResult{}, e
	}
	var source store.GraphNodeAttempt
	for _, a := range attempts {
		if a.ID != req.NodeAttemptID && a.ID > source.ID && (a.Status == store.GraphSucceeded || a.Status == store.GraphFailed) {
			source = a
		}
	}
	if source.ID == 0 {
		return ActivityResult{}, errors.New("bug notice lacks actual source")
	}
	// Only the repair-budget exhaustion (the attempt node's own outcome) asks a
	// person; the unrelated 8-pass analysis limit keeps today's plain notice
	// (AC-002). bug.dot is unchanged: this is the report node's existing
	// notice path choosing a question instead of a notice, not a new node.
	if source.Node == "attempt" && source.Outcome == "exhausted" {
		return h.publishExhaustionQuestion(ctx, opts, req, i, source)
	}
	kind := "notice"
	if source.Status == store.GraphFailed {
		kind = "failure"
	}
	values, e := opts.DB.ListGraphOutputsBounded(ctx, source.ID, 1<<20, 16)
	if e != nil {
		return ActivityResult{}, e
	}
	payloads := make([]string, len(values))
	for n, v := range values {
		payloads[n] = string(v.Payload)
	}
	body := bugNoticeComment(source.Node, source.Outcome, payloads, i.ID, source.ID)
	event := fmt.Sprintf("attempt:%d", source.ID)
	if e = h.p.PublishEvent(ctx, i.SessionID, source.ID, event, kind, body); e != nil {
		return ActivityResult{}, e
	}
	// Closure remains observed even if the final report or another required effect
	// is pending. Only the store's complete chain can mark the lifecycle complete.
	if source.Node == "close" && source.Outcome == "confirmed" {
		e = opts.DB.RecordBugLifecycleDisposition(ctx, i.SessionID, i.ID, req.Node, "pending-effect", event)
	} else {
		disposition := "blocked"
		if source.Outcome == "pending" {
			disposition = "pending-effect"
		}
		if source.Outcome == "exhausted" {
			disposition = "exhausted"
		}
		if source.Outcome == "cancelled" {
			disposition = "cancelled"
		}
		e = opts.DB.RecordBugLifecycleDisposition(ctx, i.SessionID, i.ID, req.Node, disposition, event)
	}
	return ActivityResult{Outcome: "published"}, e
}

// publishExhaustionQuestion turns a spent repair budget into a question
// instead of a one-way notice: it records the sequential exhaustion event with
// its bounded history digest, then posts that history and the two owner
// commands, and finally records the "exhausted" disposition the owner's reply
// resumes from (AC-001, AC-003, AC-011).
func (h bugParentActivity) publishExhaustionQuestion(ctx context.Context, opts Options, req ActivityRequest, i store.GraphInstance, source store.GraphNodeAttempt) (ActivityResult, error) {
	n, e := opts.DB.RecordBugExhaustion(ctx, i.SessionID, source.ID)
	if e != nil {
		return ActivityResult{}, e
	}
	history, e := opts.DB.BugRepairHistory(ctx, i.SessionID)
	if e != nil {
		return ActivityResult{}, e
	}
	event := fmt.Sprintf("exhaustion:%d", n)
	if e = h.p.PublishEvent(ctx, i.SessionID, source.ID, event, "question", bugExhaustionQuestionComment(n, history)); e != nil {
		return ActivityResult{}, e
	}
	e = opts.DB.RecordBugLifecycleDisposition(ctx, i.SessionID, i.ID, req.Node, "exhausted", event)
	return ActivityResult{Outcome: "published"}, e
}

// PublishEvent also handles failures before dispatch without forging an attempt.
// A retained delivery resumes its exact bytes and identity, never business work.
func (p BugParentPolicy) PublishEvent(ctx context.Context, session, attempt int64, event, kind, body string) error {
	if p.Reporter == nil {
		return errors.New("bug failure reporter missing")
	}
	db := p.Reporter.db
	s, e := db.GetSession(ctx, session)
	if e != nil {
		return e
	}
	ref, e := naming.ParseRef(s.TaskRef)
	if e != nil {
		return e
	}
	intent := p.Reporter.deliveryIntent(ref, ref, session, 0, 0, "bug-event:"+event)
	rendered, e := p.Reporter.renderer.RenderReportDelivery(ctx, body, intent.Marker)
	if e != nil {
		return e
	}
	if rendered.Truncated() {
		return errors.New("bug required report evidence exceeds bound")
	}
	intent.Body = rendered.String()
	// Only the delivery key, source attempt and reported flag are retained; the
	// body itself is regenerated above on every call (including a post-crash
	// retry), and report_deliveries' delivery-key dedup prevents a duplicate post.
	if e = db.RetainBugLifecycleReport(ctx, store.BugLifecycleReport{SessionID: session, AttemptID: attempt, Event: event, Kind: kind, DeliveryKey: intent.Key}); e != nil {
		return e
	}
	saved, e := db.EnsureReportDelivery(ctx, intent)
	if e != nil {
		return e
	}
	if _, e = p.Reporter.dispatchDelivery(ctx, saved); e != nil {
		return e
	}
	return db.ConfirmBugLifecycleReport(ctx, session, event)
}
