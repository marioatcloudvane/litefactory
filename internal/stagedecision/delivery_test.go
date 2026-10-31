package stagedecision

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/taskdiscussion"
)

type decisionDeliveryProbe struct {
	posts, reconciles int
	body, url         string
	failure           error
	found             bool
}

func (p *decisionDeliveryProbe) Publish(_ context.Context, c store.DiscussionConclusion) (string, error) {
	p.posts++
	p.body = c.Rendered
	return p.url, p.failure
}
func (p *decisionDeliveryProbe) Recover(context.Context, store.DiscussionConclusion) (string, bool, error) {
	p.reconciles++
	return p.url, p.found, nil
}
func admitDecision(t *testing.T, f *decisionFixture) string {
	t.Helper()
	p := f.propose(t, "revise_task", f.request())
	f.now = f.now.Add(5 * time.Second)
	_, err := f.actions.Confirm(t.Context(), f.principal, p.ID)
	decisionCheck(t, err)
	p, err = f.actions.Step(t.Context(), p.ID)
	decisionCheck(t, err)
	if p.State != "succeeded" {
		t.Fatalf("decision was not recorded: %+v", p)
	}
	return p.ID
}
func TestDecisionLostSummaryReplyRecoversWithoutAnotherPost(t *testing.T) {
	f := newDecisionFixture(t)
	p := &decisionDeliveryProbe{failure: errors.New("lost reply")}
	f.service.publisher = p
	id := admitDecision(t, f)
	before, err := f.service.Get(t.Context(), f.principal, id)
	decisionCheck(t, err)
	decisionCheck(t, f.service.PublishPending(t.Context()))
	lost, err := f.service.Get(t.Context(), f.principal, id)
	decisionCheck(t, err)
	if lost.DeliveryState != "uncertain" || lost.Transition == nil || lost.Transition.State != "applied" || p.posts != 1 || p.body != before.Summary {
		t.Fatalf("lost reply changed decision or reviewed bytes: %+v posts=%d", lost, p.posts)
	}
	// A complete negative lookup cannot prove an earlier POST did not take effect.
	for range 2 {
		f.now = f.now.Add(time.Minute)
		decisionCheck(t, f.service.PublishPending(t.Context()))
	}
	if p.posts != 1 || p.reconciles != 2 {
		t.Fatalf("blind retry: posts=%d reconciles=%d", p.posts, p.reconciles)
	}
	p.found = true
	p.url = "https://github.com/acme/app/issues/7#issuecomment-500"
	f.now = f.now.Add(time.Minute)
	decisionCheck(t, f.service.PublishPending(t.Context()))
	got, err := f.service.Get(t.Context(), f.principal, id)
	decisionCheck(t, err)
	if got.DeliveryState != "delivered" || got.CommentURL != p.url || got.Summary != before.Summary || got.Transition.State != "applied" {
		t.Fatalf("receipt recovery: %+v", got)
	}
}
func TestDecisionSummaryCancellationAndRevocationPreserveTransition(t *testing.T) {
	for _, mode := range []string{"cancel", "revoke"} {
		t.Run(mode, func(t *testing.T) {
			f := newDecisionFixture(t)
			p := &decisionDeliveryProbe{url: "https://github.com/acme/app/issues/7#issuecomment-500"}
			f.service.publisher = p
			id := admitDecision(t, f)
			if mode == "cancel" {
				v, err := f.service.Cancel(t.Context(), f.principal, id, "proposal")
				decisionCheck(t, err)
				if !strings.Contains(v.Cancellation, "Too late") || v.Transition.State != "applied" {
					t.Fatalf("applied cancellation: %+v", v)
				}
				v, err = f.service.Cancel(t.Context(), f.principal, id, "summary")
				decisionCheck(t, err)
				if v.DeliveryState != "failed" || v.Transition.State != "applied" {
					t.Fatalf("summary cancellation: %+v", v)
				}
			} else {
				decisionCheck(t, f.db.RevokeConnectorGrant(t.Context(), 42, f.principal.GrantID, f.now))
			}
			decisionCheck(t, f.service.PublishPending(t.Context()))
			if p.posts != 0 {
				t.Fatal("cancelled or revoked publication was sent")
			}
			receipt, err := f.db.GetStageDecisionReceipt(t.Context(), "owner-action:"+id)
			decisionCheck(t, err)
			if receipt.State != "applied" {
				t.Fatalf("delivery failure erased transition: %+v", receipt)
			}
		})
	}
}
func TestDecisionDefinitivelyUnsentSummaryRespectsRetryTime(t *testing.T) {
	f := newDecisionFixture(t)
	p := &decisionDeliveryProbe{failure: &taskdiscussion.PublicationDeferred{RetryAfter: time.Minute}}
	f.service.publisher = p
	id := admitDecision(t, f)
	decisionCheck(t, f.service.PublishPending(t.Context()))
	decisionCheck(t, f.service.PublishPending(t.Context()))
	if p.posts != 1 {
		t.Fatal("retried before provider retry time")
	}
	p.failure = nil
	p.url = "https://github.com/acme/app/issues/7#issuecomment-501"
	f.now = f.now.Add(time.Minute)
	decisionCheck(t, f.service.PublishPending(t.Context()))
	got, err := f.service.Get(t.Context(), f.principal, id)
	decisionCheck(t, err)
	if p.posts != 2 || got.DeliveryState != "delivered" {
		t.Fatalf("definitely unsent retry: %+v posts=%d", got, p.posts)
	}
}
func TestDecisionMixedIntentAndProtectedChangeCannotApproveUnseenWork(t *testing.T) {
	f := newDecisionFixture(t)
	p := f.propose(t, "approve_stage", f.request())
	if p.Facts.Invocation.Command != "revise_task" || p.Tier != "conversational" {
		t.Fatalf("mixed request approved unseen work: %+v", p)
	}
	f.service.protect = func(_ context.Context, text string) (string, error) {
		return strings.ReplaceAll(text, "Keep error context", "[protected]"), nil
	}
	r := f.request()
	r.ClientRequestKey = "protected"
	if _, err := f.service.Propose(t.Context(), f.principal, "revise_task", r); err == nil {
		t.Fatal("changed protected guidance was silently executed")
	}
}

func TestDecisionRateLimitedUnsentSummaryCanStillBeCancelled(t *testing.T) {
	f := newDecisionFixture(t)
	probe := &decisionDeliveryProbe{failure: &taskdiscussion.PublicationDeferred{RetryAfter: time.Hour}}
	f.service.publisher = probe
	id := admitDecision(t, f)
	decisionCheck(t, f.service.PublishPending(t.Context()))
	v, err := f.service.Cancel(t.Context(), f.principal, id, "summary")
	decisionCheck(t, err)
	if v.DeliveryState != "failed" || !strings.Contains(v.Cancellation, "Unsent summary cancelled") || v.Transition.State != "applied" {
		t.Fatalf("paced unsent cancellation: %+v", v)
	}
	f.now = f.now.Add(30 * time.Minute)
	decisionCheck(t, f.service.PublishPending(t.Context()))
	if probe.posts != 1 {
		t.Fatal("cancelled paced publication retried")
	}
}

func TestDecisionRevisionProgressIsObservedWithoutChangingSummary(t *testing.T) {
	f := newDecisionFixture(t)
	id := admitDecision(t, f)
	before, err := f.service.Get(t.Context(), f.principal, id)
	decisionCheck(t, err)
	dispatch, err := f.db.LatestGraphDispatch(t.Context(), f.instance.ID)
	decisionCheck(t, err)
	_, reserved, err := f.db.ReserveGraphDispatch(t.Context(), dispatch.ID, f.now)
	decisionCheck(t, err)
	if !reserved {
		t.Fatal("continuation was not reserved")
	}
	running, err := f.service.Get(t.Context(), f.principal, id)
	decisionCheck(t, err)
	if !strings.Contains(running.RevisionProgress, "running") || running.Summary != before.Summary || running.Transition.State != "applied" {
		t.Fatalf("dynamic progress: %+v", running)
	}
	decisionCheck(t, f.db.CancelGraphDispatch(t.Context(), dispatch.ID, "continuation cannot start", f.now))
	stopped, err := f.service.Get(t.Context(), f.principal, id)
	decisionCheck(t, err)
	if !strings.Contains(stopped.RevisionProgress, "stopped") || stopped.Summary != before.Summary || stopped.Transition.State != "applied" {
		t.Fatalf("stopped progress erased decision: %+v", stopped)
	}
}

func TestDecisionRejectsUndisplayablePreviewBeforeSavingProposal(t *testing.T) {
	f := newDecisionFixture(t)
	r := f.request()
	r.Summary.Conclusion = strings.Repeat("x", 8192)
	if _, err := f.service.Propose(t.Context(), f.principal, "revise_task", r); !errors.Is(err, ErrPreviewTooLarge) {
		t.Fatalf("oversized preview: %v", err)
	}
	if _, err := f.db.OwnerActionByRequest(t.Context(), actor(f.principal), r.ClientRequestKey); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("undisplayable proposal persisted: %v", err)
	}
}
func TestDecisionStatusBoundsConfirmationHistoryWithoutChangingPreview(t *testing.T) {
	f := newDecisionFixture(t)
	p := f.propose(t, "revise_task", f.request())
	for range 24 {
		_, err := f.actions.Confirm(t.Context(), f.principal, p.ID)
		decisionCheck(t, err)
	}
	v, err := f.service.Get(t.Context(), f.principal, p.ID)
	decisionCheck(t, err)
	if len(v.Proposal.Attempts) != 10 || v.OmittedAttempts != 14 || v.Proposal.Facts.Summary != p.Facts.Summary {
		t.Fatalf("unbounded or changed status: %+v", v)
	}
}
