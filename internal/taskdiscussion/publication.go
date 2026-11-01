package taskdiscussion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func validateSummary(v Summary) error {
	for _, text := range []string{v.Question, v.Conclusion, v.AlternativesOrUncertainty, v.EvidenceBasis, v.Attribution} {
		if !valid(text, 8192) {
			return errors.New("summary requires bounded question, conclusion, alternatives or uncertainty, evidence and attribution")
		}
	}
	return nil
}

// Publish freezes a structured conclusion under separate publication consent.
func (s *Service) Publish(ctx context.Context, p Principal, r PublicationRequest) (PublicationView, error) {
	if s.publisher == nil {
		return PublicationView{}, errors.New("discussion publication unavailable")
	}
	if !valid(r.TaskRef, 512) || !valid(r.ConversationRef, 128) || !valid(r.ConclusionID, 128) || len(r.SourceJobRef) > 128 {
		return PublicationView{}, store.ErrConnectorInvalid
	}
	if err := validateSummary(r.Summary); err != nil {
		return PublicationView{}, err
	}
	scope, err := s.authorize(ctx, p, r.TaskRef, PublicationScope)
	if err != nil {
		return PublicationView{}, err
	}
	if _, err = s.db.ReadDiscussion(ctx, scope, r.ConversationRef); err != nil {
		return PublicationView{}, err
	}
	raw, _ := json.Marshal(r.Summary)
	rendered := fmt.Sprintf("Discussion conclusion\n\nQuestion: %s\n\nConclusion: %s\n\nAlternatives or uncertainty: %s\n\nEvidence basis (client supplied): %s\n\nDecision attribution: %s\n\nThis discussion record does not change the task's stage or approvals. Only the supplied summary was reviewed; no full chat transcript was supplied.", r.Summary.Question, r.Summary.Conclusion, r.Summary.AlternativesOrUncertainty, r.Summary.EvidenceBasis, r.Summary.Attribution)
	if r.SourceJobRef == "" {
		rendered += "\n\nEvidence freshness: unverifiable (client-supplied ordinary-chat basis)."
	}
	if r.SourceJobRef != "" {
		a, e := s.db.ReadAdvisory(ctx, scope, r.SourceJobRef)
		if e != nil || a.ConversationID != r.ConversationRef {
			return PublicationView{}, store.ErrConnectorInvalid
		}
		var b Basis
		_ = json.Unmarshal([]byte(a.Basis), &b)
		freshness := s.freshness(ctx, b, a.Operation)
		rendered += "\n\nRecorded advisory evidence: " + freshness + "; revision " + b.Revision
	}
	rendered, err = s.protect(ctx, rendered)
	if err != nil {
		return PublicationView{}, err
	}
	c, err := s.db.ReserveDiscussionConclusion(ctx, store.DiscussionConclusion{DiscussionScope: scope, ID: identity("publication-"), ConversationID: r.ConversationRef, ConclusionKey: r.ConclusionID, Digest: hash(r), Summary: string(raw), Rendered: rendered, SourceJob: r.SourceJobRef, GrantID: p.GrantID, ConsentRevision: principalJSON(p), CreatedAt: s.now()})
	if err != nil {
		return PublicationView{}, err
	}
	// Dispatch is separate from admission; clients can cancel pending work.
	return publicationView(c), nil
}
func publicationView(c store.DiscussionConclusion) PublicationView {
	state := c.State
	if state == "sending" {
		state = "uncertain"
	}
	actions := []string{"discussion_summary_get"}
	if state == "pending" {
		actions = append(actions, "cancel_pending")
	}
	return PublicationView{PublicationRef: c.ID, State: state, Reason: c.Reason, CommentURL: c.CommentURL, NextActions: actions}
}

// Publication retrieves delivery status and may cancel an undispatched summary.
func (s *Service) Publication(ctx context.Context, p Principal, id string, cancel bool) (PublicationView, error) {
	owner, err := strconv.ParseInt(p.OwnerID, 10, 64)
	if err != nil {
		return PublicationView{}, store.ErrConnectorOwner
	}
	scope, err := s.db.ConclusionTarget(ctx, owner, p.OwnerGeneration, id)
	if err != nil {
		return PublicationView{}, err
	}
	if _, err = s.authorize(ctx, p, scope.TaskRef, PublicationScope); err != nil {
		return PublicationView{}, err
	}
	c, err := s.db.ReadDiscussionConclusion(ctx, scope, id)
	if err != nil {
		return PublicationView{}, err
	}
	if cancel && c.State == "pending" {
		err = s.db.UpdateDiscussionConclusion(ctx, scope, id, "pending", "failed", "cancelled before publication", "", s.now())
		if err != nil && !errors.Is(err, store.ErrWorkflowRevision) {
			return PublicationView{}, err
		}
		c, err = s.db.ReadDiscussionConclusion(ctx, scope, id)
	}
	return publicationView(c), err
}

// DispatchPublication retains possible-send state before calling the publisher.
// Reconciliation never starts another POST after an ambiguous outcome.
func (s *Service) DispatchPublication(ctx context.Context, c store.DiscussionConclusion) error {
	if s.publisher == nil {
		return errors.New("discussion publication unavailable")
	}
	if c.State == "sending" || c.State == "uncertain" {
		url, found, err := s.publisher.Recover(ctx, c)
		if err != nil || !found {
			delay := 30 * time.Second
			var deferred *PublicationDeferred
			if errors.As(err, &deferred) {
				delay = max(deferred.RetryAfter, time.Second)
			}
			pacingErr := s.db.DelayDiscussionReconciliation(ctx, c.DiscussionScope, c.ID, c.State, s.now().Add(delay), s.now())
			return errors.Join(err, pacingErr)
		}
		return s.db.UpdateDiscussionConclusion(ctx, c.DiscussionScope, c.ID, c.State, "delivered", "", url, s.now())
	}
	if c.State != "pending" || !c.NextAttemptAt.IsZero() && s.now().Before(c.NextAttemptAt) {
		return nil
	}
	var p Principal
	if err := json.Unmarshal([]byte(c.ConsentRevision), &p); err != nil {
		return err
	}
	if _, err := s.authorize(ctx, p, c.TaskRef, PublicationScope); err != nil {
		return s.db.UpdateDiscussionConclusion(ctx, c.DiscussionScope, c.ID, "pending", "failed", "publication consent revoked before dispatch", "", s.now())
	}
	if err := s.db.ClaimDiscussionPublication(ctx, c, s.now()); err != nil {
		return err
	}
	url, err := s.publisher.Publish(ctx, c)
	if err != nil {
		var deferred *PublicationDeferred
		if errors.As(err, &deferred) {
			delay := max(deferred.RetryAfter, time.Second)
			return s.db.UpdateDiscussionConclusionRetry(context.WithoutCancel(ctx), c.DiscussionScope, c.ID, "sending", "provider rate limited; publication remains queued", s.now().Add(delay), s.now())
		}
		save := context.WithoutCancel(ctx)
		return s.db.UpdateDiscussionConclusion(save, c.DiscussionScope, c.ID, "sending", "uncertain", "delivery is unconfirmed; receipt reconciliation required", "", s.now())
	}
	return s.db.UpdateDiscussionConclusion(context.WithoutCancel(ctx), c.DiscussionScope, c.ID, "sending", "delivered", "", url, s.now())
}

// PublishPending handles a bounded batch of retained sends and receipt recovery.
func (s *Service) PublishPending(ctx context.Context) error {
	records, err := s.db.ListPendingDiscussionConclusions(ctx, s.now(), 1)
	if err != nil {
		return err
	}
	var failures []error
	for _, c := range records {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e := s.DispatchPublication(ctx, c); e != nil {
			failures = append(failures, e)
		}
	}
	return errors.Join(failures...)
}

// PublicationDeferred proves the provider refused the POST before creating a comment.
// Only this typed boundary permits a paced retry of retained publication bytes.
type PublicationDeferred struct{ RetryAfter time.Duration }

func (e *PublicationDeferred) Error() string { return "publication rate limited before send" }
