package stagedecision

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/taskdiscussion"
)

func (s *Service) ensureDelivery(ctx context.Context, p owneraction.Principal, v frozen, key string) error {
	return s.db.EnsureStageDecisionDelivery(ctx, store.StageDecisionDelivery{ExecutionID: key, ProposalID: strings.TrimPrefix(key, "owner-action:"), TaskRef: v.Target.TaskRef, Rendered: v.Rendered, Digest: digest(v.Rendered), Principal: actor(p)}, s.now())
}

// Get retrieves the exact proposal and known receipt without executing or confirming.
func (s *Service) Get(ctx context.Context, p owneraction.Principal, id string) (View, error) {
	if !s.Ready() {
		return View{}, owneraction.ErrUnavailable
	}
	proposal, err := s.actions.Inspect(ctx, p, id)
	if err != nil {
		return View{}, err
	}
	if !isDecision(proposal.Facts.Invocation) {
		return View{}, store.ErrNotFound
	}
	v, err := decode(proposal.Facts)
	if err != nil {
		return View{}, err
	}
	if _, err = s.authorize(ctx, p, v.Target.TaskRef); err != nil {
		return View{}, err
	}
	omitted := max(0, len(proposal.Attempts)-10)
	proposal.Attempts = append([]owneraction.Attempt(nil), proposal.Attempts[omitted:]...)
	out := View{OmittedAttempts: omitted, DecisionRef: id, Proposal: proposal, Summary: v.Rendered, RevisionProgress: "not_admitted", DeliveryState: "not_requested", NextActions: []string{"decision_get", "action_inspect"}}
	receipt, err := s.db.GetStageDecisionReceipt(ctx, proposal.ExecutionID)
	found := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return View{}, err
	}
	if found {
		out.Transition = &receipt
		out.RevisionProgress = receipt.RevisionProgress
		if receipt.State == "applied" && receipt.Operation == "revise_task" {
			progress, e := s.db.StageDecisionRevisionProgress(ctx, proposal.ExecutionID)
			if e != nil {
				return View{}, e
			}
			out.RevisionProgress = progress
		}
	}
	delivery, err := s.db.ReadStageDecisionDelivery(ctx, proposal.ExecutionID)
	switch {
	case err == nil:
		out.DeliveryState = delivery.State
		if delivery.State == "sending" {
			out.DeliveryState = "uncertain"
		}
		out.DeliveryReason = delivery.Reason
		out.CommentURL = delivery.CommentURL
		if delivery.State == "pending" {
			out.NextActions = append(out.NextActions, "decision_cancel:summary")
		}
	case !errors.Is(err, store.ErrNotFound):
		return View{}, err
	case found && receipt.State == "applied":
		out.DeliveryState = "pending"
		out.DeliveryReason = "Transition recorded; summary dispatch record is being recovered."
	}
	if proposal.State == "awaiting_confirmation" {
		out.NextActions = append(out.NextActions, "action_confirm", "decision_cancel:proposal")
	}
	if proposal.State == "awaiting_discord" {
		out.NextActions = append(out.NextActions, "discord_owner_reaction", "decision_cancel:proposal")
	}
	if proposal.State == "uncertain" || proposal.State == "executing" {
		if !found {
			out.RevisionProgress = "Transition outcome unresolved; reconcile before another proposal."
		}
		out.NextActions = append(out.NextActions, "receipt_reconciliation")
	}
	return out, nil
}

// Cancel keeps cancellation of a proposal separate from its unsent summary.
func (s *Service) Cancel(ctx context.Context, p owneraction.Principal, id, target string) (View, error) {
	v, err := s.Get(ctx, p, id)
	if err != nil {
		return View{}, err
	}
	switch target {
	case "proposal":
		if v.Transition != nil && v.Transition.State == "applied" {
			v.Cancellation = "Too late: the decision took effect; cancellation cannot undo it."
			return v, nil
		}
		if v.Proposal.State != "awaiting_confirmation" && v.Proposal.State != "awaiting_discord" {
			v.Cancellation = "Too late to cancel the proposal; inspect its recorded or unresolved transition."
			return v, nil
		}
		cancelled, e := s.actions.Cancel(ctx, p, id)
		if e != nil {
			return View{}, e
		}
		v, err = s.Get(ctx, p, id)
		if err != nil {
			return View{}, err
		}
		if cancelled.State == "cancelled" && (v.Transition == nil || v.Transition.State != "applied") {
			v.Cancellation = "Pending proposal cancelled; no transition was authorized."
		} else {
			v.Cancellation = "Cancellation was too late; inspect the recorded or unresolved transition. The decision is retained."
		}
		return v, nil
	case "summary":
		d, e := s.db.ReadStageDecisionDelivery(ctx, v.Proposal.ExecutionID)
		if e != nil {
			return View{}, e
		}
		if d.State != "pending" {
			switch d.State {
			case "failed":
				v.Cancellation = "Summary is already not published: " + d.Reason + ". The decision is retained."
			case "delivered":
				v.Cancellation = "Summary was already delivered; the recorded decision cannot be undone."
			default:
				v.Cancellation = "Summary cancellation is too late after a possible send; receipt reconciliation remains available. The decision is retained."
			}
			return v, nil
		}
		if err = s.db.AdvanceStageDecisionDelivery(ctx, d.ExecutionID, "pending", "failed", "cancelled before publication", "", time.Time{}, s.now()); err != nil {
			if errors.Is(err, store.ErrWorkflowRevision) {
				v, err = s.Get(ctx, p, id)
				v.Cancellation = "Summary dispatch won the cancellation race; the decision is retained."
				return v, err
			}
			return View{}, err
		}
		v, err = s.Get(ctx, p, id)
		v.Cancellation = "Unsent summary cancelled. The recorded decision and its workflow remain in effect."
		return v, err
	default:
		return View{}, store.ErrConnectorInvalid
	}
}

// PublishPending serializes through durable possible-send claims. Lost replies
// can only be reconciled; absence of a remote receipt never permits another POST.
func (s *Service) PublishPending(ctx context.Context) error {
	records, err := s.db.PendingStageDecisionDeliveries(ctx, s.now())
	if err != nil {
		return err
	}
	for _, d := range records {
		if err = s.dispatch(ctx, d); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) dispatch(ctx context.Context, d store.StageDecisionDelivery) error {
	owner, err := s.authorize(ctx, owneraction.Principal{OwnerID: d.Principal.OwnerID, OwnerGeneration: d.Principal.OwnerGeneration, Transport: d.Principal.Transport, ClientID: d.Principal.ClientID, GrantID: d.Principal.GrantID}, d.TaskRef)
	if err != nil {
		if d.State == "pending" {
			return s.db.AdvanceStageDecisionDelivery(ctx, d.ExecutionID, "pending", "failed", "steering authority revoked before publication", "", time.Time{}, s.now())
		}
		// Reconciliation uses trusted remote receipts and does not publish private text.
	}
	c := store.DiscussionConclusion{DiscussionScope: owner, ID: "decision-" + d.ProposalID, Rendered: d.Rendered, Digest: d.Digest}
	// A revoked grant must not prevent trusted receipt reconciliation of a possible send.
	c.TaskRef = d.TaskRef
	if d.State == "sending" || d.State == "uncertain" {
		url, found, e := s.publisher.Recover(ctx, c)
		if e == nil && found {
			return s.db.AdvanceStageDecisionDelivery(ctx, d.ExecutionID, d.State, "delivered", "", url, time.Time{}, s.now())
		}
		delay := 30 * time.Second
		var deferError *taskdiscussion.PublicationDeferred
		if errors.As(e, &deferError) {
			delay = max(time.Second, deferError.RetryAfter)
		}
		return errors.Join(e, s.db.AdvanceStageDecisionDelivery(ctx, d.ExecutionID, d.State, "uncertain", "A possible send is unresolved; reconciliation will not repost.", "", s.now().Add(delay), s.now()))
	}
	if d.State != "pending" {
		return nil
	}
	if err = s.db.AdvanceStageDecisionDelivery(ctx, d.ExecutionID, "pending", "sending", "", "", time.Time{}, s.now()); err != nil {
		if errors.Is(err, store.ErrWorkflowRevision) {
			return nil
		}
		return err
	}
	url, err := s.publisher.Publish(ctx, c)
	if err == nil && url != "" {
		return s.db.AdvanceStageDecisionDelivery(ctx, d.ExecutionID, "sending", "delivered", "", url, time.Time{}, s.now())
	}
	var rejected *PublicationRejected
	if errors.As(err, &rejected) {
		return s.db.AdvanceStageDecisionDelivery(ctx, d.ExecutionID, "sending", "failed", rejected.Error(), "", time.Time{}, s.now())
	}
	var refused *taskdiscussion.PublicationDeferred
	if errors.As(err, &refused) {
		return s.db.AdvanceStageDecisionDelivery(ctx, d.ExecutionID, "sending", "pending", "provider refused before send", "", s.now().Add(max(time.Second, refused.RetryAfter)), s.now())
	}
	return s.db.AdvanceStageDecisionDelivery(ctx, d.ExecutionID, "sending", "uncertain", "GitHub reply unconfirmed; reconcile before any retry.", "", s.now().Add(30*time.Second), s.now())
}
