package connector

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/taskdiscussion"
)

type advisoryCaptureFunc func(context.Context, string, string) (taskdiscussion.Basis, error)

func (f advisoryCaptureFunc) Capture(ctx context.Context, task, operation string) (taskdiscussion.Basis, error) {
	return f(ctx, task, operation)
}

type advisoryRunnerFunc func(context.Context, taskdiscussion.RunRequest) (taskdiscussion.Answer, error)

func (f advisoryRunnerFunc) Run(ctx context.Context, r taskdiscussion.RunRequest) (taskdiscussion.Answer, error) {
	return f(ctx, r)
}
func (advisoryRunnerFunc) Recover(context.Context, taskdiscussion.RunRequest) (taskdiscussion.Answer, bool, error) {
	return taskdiscussion.Answer{}, false, nil
}

type advisoryServiceFixture struct {
	f       *connectorFixture
	target  store.ConnectorGrantTarget
	p       owneraction.Principal
	client  connectorTestClient
	service *taskdiscussion.Service
}

func newAdvisoryServiceFixture(t *testing.T, scope string, runner taskdiscussion.Runner, publisher taskdiscussion.Publisher) advisoryServiceFixture {
	t.Helper()
	f := newConnectorFixture(t, nil)
	target := seedDiscussionTarget(t, f.db)
	seedAdvisoryReview(t, f.db, target.TaskRef)
	client := registerConnectorClient(t, f.server(t), "service boundary client", "none")
	_, grantID := issueDiscussionTokens(t, f, client, target, scope)
	owner, err := f.db.ConnectorOwner(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	p := owneraction.Principal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "mcp", ClientID: client.ClientID, GrantID: grantID}
	if runner == nil {
		runner = &advisoryJourneyRunner{}
	}
	service, err := taskdiscussion.New(taskdiscussion.Options{DB: f.db, Now: func() time.Time { return f.now }, Capture: advisoryJourneyCapture{f.db}, Runner: runner, Publisher: publisher, Protect: func(_ context.Context, s string) (string, error) { return s, nil }, Deadline: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	return advisoryServiceFixture{f, target, p, client, service}
}
func (f advisoryServiceFixture) request(key string) taskdiscussion.Request {
	return taskdiscussion.Request{TaskRef: f.target.TaskRef, Question: "Explain the saved choice.", ClientRequestKey: key, Operation: "ask"}
}

func TestTaskAdvisoryAuthorityRetryAndRevocation(t *testing.T) {
	var calls atomic.Int32
	fixture := newAdvisoryServiceFixture(t, AdvisoryScope, advisoryRunnerFunc(func(context.Context, taskdiscussion.RunRequest) (taskdiscussion.Answer, error) {
		calls.Add(1)
		return taskdiscussion.Answer{Answer: "Private saved answer", Sources: []string{"saved-plan"}, Gaps: []string{}}, nil
	}), nil)
	svc, p := fixture.service, fixture.p
	r := fixture.request("one")
	admitted, err := svc.Ask(t.Context(), p, r)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := svc.Ask(t.Context(), p, r)
	if err != nil || replay.JobRef != admitted.JobRef {
		t.Fatalf("retry replaced work: %+v %v", replay, err)
	}
	changed := r
	changed.Question = "Different question"
	if _, err = svc.Ask(t.Context(), p, changed); !errors.Is(err, store.ErrWorkflowConflict) {
		t.Fatalf("changed request key = %v", err)
	}
	foreign := p
	foreign.OwnerGeneration++
	if _, err = svc.Get(t.Context(), foreign, admitted.JobRef, ""); err == nil {
		t.Fatal("different owner generation read a private answer")
	}
	foreign = p
	foreign.OwnerID = "99"
	if _, err = svc.Get(t.Context(), foreign, admitted.JobRef, ""); err == nil {
		t.Fatal("different owner read a private answer")
	}
	foreignRepo := registerTestRepo(t, fixture.f.db, "other", "private")
	foreignSession, err := fixture.f.db.GetOrCreateSession(t.Context(), foreignRepo, 7)
	if err != nil {
		t.Fatal(err)
	}
	seedAdvisoryReview(t, fixture.f.db, foreignSession.TaskRef)
	crossRepository := r
	crossRepository.TaskRef = foreignSession.TaskRef
	crossRepository.ClientRequestKey = "cross-repository"
	if _, err = svc.Ask(t.Context(), p, crossRepository); err == nil {
		t.Fatal("repository-scoped advisory grant crossed repository boundary")
	}
	// A second canonical task in the same repository still cannot reuse a conversation.
	repo, err := fixture.f.db.GetRepo(t.Context(), fixture.target.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := fixture.f.db.GetOrCreateSession(t.Context(), repo, 8)
	if err != nil {
		t.Fatal(err)
	}
	seedAdvisoryReview(t, fixture.f.db, session.TaskRef)
	crossTask := r
	crossTask.TaskRef = session.TaskRef
	crossTask.ClientRequestKey = "cross-task"
	crossTask.ConversationRef = admitted.ConversationRef
	crossTask.ConversationRevision = admitted.ConversationRevision
	if _, err = svc.Ask(t.Context(), p, crossTask); err == nil {
		t.Fatal("conversation crossed task boundary")
	}
	if err = fixture.f.db.RevokeConnectorGrant(t.Context(), 42, p.GrantID, fixture.f.now); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Get(t.Context(), p, admitted.JobRef, ""); err == nil {
		t.Fatal("revoked grant read advisory work")
	}
	if _, err = svc.Ask(t.Context(), p, fixture.request("revoked")); err == nil {
		t.Fatal("revoked grant admitted work")
	}
	if ran, err := svc.RunOnce(t.Context()); err != nil || !ran {
		t.Fatalf("revoked dispatch: %v %v", ran, err)
	}
	if calls.Load() != 0 {
		t.Fatal("revoked execution reached the provider")
	}
	// A current grant can recover the same owner's stopped work; it cannot revive it.
	_, replacement := issueDiscussionTokens(t, fixture.f, fixture.client, fixture.target, AdvisoryScope)
	p.GrantID = replacement
	recovered, err := svc.Get(t.Context(), p, admitted.JobRef, "")
	if err != nil || recovered.State != "failed" {
		t.Fatalf("replacement grant revived stopped work: %+v %v", recovered, err)
	}
}

func TestTaskAdvisoryCancellationFence(t *testing.T) {
	t.Run("stop-before-provider-launch", func(t *testing.T) {
		var calls atomic.Int32
		runner := advisoryRunnerFunc(func(context.Context, taskdiscussion.RunRequest) (taskdiscussion.Answer, error) {
			calls.Add(1)
			return taskdiscussion.Answer{Answer: "unexpected invocation"}, nil
		})
		fixture := newAdvisoryServiceFixture(t, AdvisoryScope, runner, nil)
		admitted, err := fixture.service.Ask(t.Context(), fixture.p, fixture.request("stop-before-launch"))
		if err != nil {
			t.Fatal(err)
		}
		attempt, job, err := fixture.f.db.ClaimWorkflowAttempt(t.Context(), fixture.f.now, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		requested, err := fixture.service.Cancel(t.Context(), fixture.p, admitted.JobRef)
		if err != nil || requested.State != "stop_requested" {
			t.Fatalf("stop before launch = %+v %v", requested, err)
		}
		if err = fixture.service.ExecuteClaim(t.Context(), attempt, job); err != nil {
			t.Fatal(err)
		}
		terminal, err := fixture.service.Get(t.Context(), fixture.p, admitted.JobRef, "")
		if err != nil || terminal.State != "cancelled" {
			t.Fatalf("unlaunched stop was not settled: %+v %v", terminal, err)
		}
		if calls.Load() != 0 {
			t.Fatal("stopped claim reached provider")
		}
		if ran, err := fixture.service.RunOnce(t.Context()); err != nil || ran {
			t.Fatalf("cancelled claim retained worker capacity: %v %v", ran, err)
		}
	})
	t.Run("queued", func(t *testing.T) {
		fixture := newAdvisoryServiceFixture(t, AdvisoryScope, nil, nil)
		admitted, err := fixture.service.Ask(t.Context(), fixture.p, fixture.request("queued"))
		if err != nil {
			t.Fatal(err)
		}
		cancelled, err := fixture.service.Cancel(t.Context(), fixture.p, admitted.JobRef)
		if err != nil || cancelled.State != "cancelled" || cancelled.CompletionWon {
			t.Fatalf("queued cancel: %+v %v", cancelled, err)
		}
		if ran, err := fixture.service.RunOnce(t.Context()); err != nil || ran {
			t.Fatalf("cancelled queue executed: %v %v", ran, err)
		}
	})
	for _, succeeds := range []bool{false, true} {
		name := "confirmed-stop"
		if succeeds {
			name = "completion-wins"
		}
		t.Run(name, func(t *testing.T) {
			started := make(chan struct{})
			runner := advisoryRunnerFunc(func(ctx context.Context, _ taskdiscussion.RunRequest) (taskdiscussion.Answer, error) {
				close(started)
				<-ctx.Done()
				if succeeds {
					return taskdiscussion.Answer{Answer: "Completed despite the stop race", Sources: []string{}, Gaps: []string{}}, nil
				}
				return taskdiscussion.Answer{}, ctx.Err()
			})
			fixture := newAdvisoryServiceFixture(t, AdvisoryScope, runner, nil)
			admitted, err := fixture.service.Ask(t.Context(), fixture.p, fixture.request(name))
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { _, err := fixture.service.RunOnce(t.Context()); done <- err }()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("runner never started")
			}
			requested, err := fixture.service.Cancel(t.Context(), fixture.p, admitted.JobRef)
			if err != nil || requested.State != "stop_requested" {
				t.Fatalf("running stop reported final cancellation early: %+v %v", requested, err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("worker did not account for provider termination")
			}
			terminal, err := fixture.service.Cancel(t.Context(), fixture.p, admitted.JobRef)
			if err != nil {
				t.Fatal(err)
			}
			want := "cancelled"
			if succeeds {
				want = "succeeded"
			}
			if terminal.State != want || terminal.CompletionWon != succeeds {
				t.Fatalf("terminal stop outcome: %+v", terminal)
			}
		})
	}
}

type advisoryPublicationBoundary struct {
	posts, recoveries int
	found             bool
}

func (p *advisoryPublicationBoundary) Publish(context.Context, store.DiscussionConclusion) (string, error) {
	p.posts++
	return "", errors.New("lost create-comment response")
}
func (p *advisoryPublicationBoundary) Recover(context.Context, store.DiscussionConclusion) (string, bool, error) {
	p.recoveries++
	if p.found {
		return "https://github.com/acme/api/issues/7#issuecomment-91", true, nil
	}
	return "", false, nil
}

func TestTaskAdvisorySummaryUncertaintyDoesNotRepost(t *testing.T) {
	publisher := &advisoryPublicationBoundary{}
	fixture := newAdvisoryServiceFixture(t, AdvisoryScope+" "+DiscussionPublishScope, nil, publisher)
	svc, p := fixture.service, fixture.p
	conversation, err := svc.Open(t.Context(), p, fixture.target.TaskRef, "ordinary-chat")
	if err != nil {
		t.Fatal(err)
	}
	request := taskdiscussion.PublicationRequest{TaskRef: fixture.target.TaskRef, ConversationRef: conversation.ConversationRef, ConclusionID: "settled-conclusion", Summary: taskdiscussion.Summary{Question: "Why separate analysis?", Conclusion: "Preserve pending review.", AlternativesOrUncertainty: "No owner decision was made.", EvidenceBasis: "Client supplied saved review.", Attribution: "Agent recommendation."}}
	publication, err := svc.Publish(t.Context(), p, request)
	if err != nil {
		t.Fatal(err)
	}
	scope := store.DiscussionScope{OwnerID: 42, OwnerGeneration: p.OwnerGeneration, RepositoryRef: fixture.target.RepositoryRef, TaskRef: fixture.target.TaskRef}
	record, err := fixture.f.db.ReadDiscussionConclusion(t.Context(), scope, publication.PublicationRef)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.DispatchPublication(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	uncertain, err := svc.Publication(t.Context(), p, publication.PublicationRef, false)
	if err != nil || uncertain.State != "uncertain" {
		t.Fatalf("lost response = %+v %v", uncertain, err)
	}
	replay, err := svc.Publish(t.Context(), p, request)
	if err != nil || replay.PublicationRef != publication.PublicationRef {
		t.Fatalf("summary retry replaced identity: %+v %v", replay, err)
	}
	record, err = fixture.f.db.ReadDiscussionConclusion(t.Context(), scope, publication.PublicationRef)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.DispatchPublication(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	if publisher.posts != 1 || publisher.recoveries != 1 {
		t.Fatalf("uncertain dispatch reposted: %+v", publisher)
	}
	publisher.found = true
	if err = svc.DispatchPublication(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	delivered, err := svc.Publication(t.Context(), p, publication.PublicationRef, false)
	if err != nil || delivered.State != "delivered" || delivered.CommentURL == "" {
		t.Fatalf("trusted receipt not recovered: %+v %v", delivered, err)
	}
	request.Summary.Conclusion = "Changed conclusion"
	if _, err = svc.Publish(t.Context(), p, request); !errors.Is(err, store.ErrWorkflowConflict) {
		t.Fatalf("conclusion identity accepted changed bytes: %v", err)
	}
}

func TestTaskAdvisoryPublicationConsentAndPendingCancellation(t *testing.T) {
	publisher := &advisoryPublicationBoundary{}
	fixture := newAdvisoryServiceFixture(t, AdvisoryScope, nil, publisher)
	svc, p := fixture.service, fixture.p
	conversation, err := svc.Open(t.Context(), p, fixture.target.TaskRef, "chat-without-paid-run")
	if err != nil {
		t.Fatal(err)
	}
	request := taskdiscussion.PublicationRequest{TaskRef: fixture.target.TaskRef, ConversationRef: conversation.ConversationRef, ConclusionID: "summary-only", Summary: taskdiscussion.Summary{Question: "What should be retained?", Conclusion: "A useful answer.", AlternativesOrUncertainty: "Open question remains.", EvidenceBasis: "Client supplied evidence.", Attribution: "Agent recommendation."}}
	if _, err = svc.Publish(t.Context(), p, request); err == nil {
		t.Fatal("execution consent authorized publication")
	}
	_, grantID := issueDiscussionTokens(t, fixture.f, fixture.client, fixture.target, DiscussionPublishScope)
	p.GrantID = grantID
	if _, err = svc.Ask(t.Context(), p, fixture.request("not-paid")); err == nil {
		t.Fatal("publication consent authorized paid advisory work")
	}
	publication, err := svc.Publish(t.Context(), p, request)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := svc.Publication(t.Context(), p, publication.PublicationRef, true)
	if err != nil || cancelled.State != "failed" || cancelled.Reason == "" {
		t.Fatalf("pending publication cancellation = %+v %v", cancelled, err)
	}
	scope := store.DiscussionScope{OwnerID: 42, OwnerGeneration: p.OwnerGeneration, RepositoryRef: fixture.target.RepositoryRef, TaskRef: fixture.target.TaskRef}
	record, err := fixture.f.db.ReadDiscussionConclusion(t.Context(), scope, publication.PublicationRef)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.DispatchPublication(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	if publisher.posts != 0 {
		t.Fatal("cancelled summary was posted")
	}
	request.ConclusionID = "revoked-before-send"
	pending, err := svc.Publish(t.Context(), p, request)
	if err != nil {
		t.Fatal(err)
	}
	if err = fixture.f.db.RevokeConnectorGrant(t.Context(), 42, p.GrantID, fixture.f.now); err != nil {
		t.Fatal(err)
	}
	record, err = fixture.f.db.ReadDiscussionConclusion(t.Context(), scope, pending.PublicationRef)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.DispatchPublication(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	if publisher.posts != 0 {
		t.Fatal("revoked publication reached GitHub")
	}
	if _, err = svc.Publication(t.Context(), p, pending.PublicationRef, false); err == nil {
		t.Fatal("revoked publication consent read private delivery status")
	}
	_, replacement := issueDiscussionTokens(t, fixture.f, fixture.client, fixture.target, DiscussionPublishScope)
	p.GrantID = replacement
	stopped, err := svc.Publication(t.Context(), p, pending.PublicationRef, false)
	if err != nil || stopped.State != "failed" {
		t.Fatalf("replacement grant revived revoked publication: %+v %v", stopped, err)
	}
}

func TestTaskAdvisoryResultPaging(t *testing.T) {
	full := strings.Repeat("界word\n", 10000)
	fixture := newAdvisoryServiceFixture(t, AdvisoryScope, advisoryRunnerFunc(func(context.Context, taskdiscussion.RunRequest) (taskdiscussion.Answer, error) {
		return taskdiscussion.Answer{Answer: full, Sources: []string{"saved-plan:immutable"}, Gaps: []string{"original_session_unavailable"}}, nil
	}), nil)
	admitted, err := fixture.service.Ask(t.Context(), fixture.p, fixture.request("long-answer"))
	if err != nil {
		t.Fatal(err)
	}
	if ran, err := fixture.service.RunOnce(t.Context()); err != nil || !ran {
		t.Fatalf("worker: %v %v", ran, err)
	}
	var received strings.Builder
	cursor := ""
	var firstCursor string
	sources := []string{}
	for pageNumber := 0; pageNumber < 30; pageNumber++ {
		page, err := fixture.service.Get(t.Context(), fixture.p, admitted.JobRef, cursor)
		if err != nil {
			t.Fatal(err)
		}
		if page.State != "succeeded" || page.Answer == nil || page.ResultPageOffset != received.Len() {
			t.Fatalf("bad result page: %+v", page)
		}
		result, err := actionResult(page)
		if err != nil {
			t.Fatalf("page exceeds connector budget: %v", err)
		}
		encoded, err := json.Marshal(result)
		if err != nil || len(encoded) > DiscussionMaxResponseBytes {
			t.Fatalf("unbounded page: %d %v", len(encoded), err)
		}
		received.WriteString(page.Answer.Answer)
		sources = append(sources, page.Answer.Sources...)
		if pageNumber == 0 {
			firstCursor = page.NextCursor
		}
		if page.ResultComplete {
			if page.NextCursor != "" {
				t.Fatal("complete result has continuation")
			}
			break
		}
		if page.NextCursor == "" {
			t.Fatal("partial result has no continuation")
		}
		cursor = page.NextCursor
	}
	if received.String() != full || len(sources) != 1 {
		t.Fatalf("pagination lost answer bytes or evidence: %d/%d bytes, %v", received.Len(), len(full), sources)
	}
	if firstCursor == "" {
		t.Fatal("large result did not page")
	}
	if _, err := fixture.service.Get(t.Context(), fixture.p, admitted.JobRef, firstCursor+"x"); err == nil {
		t.Fatal("tampered continuation accepted")
	}
	_, replacement := issueDiscussionTokens(t, fixture.f, fixture.client, fixture.target, AdvisoryScope)
	otherGrant := fixture.p
	otherGrant.GrantID = replacement
	if _, err := fixture.service.Get(t.Context(), otherGrant, admitted.JobRef, firstCursor); err == nil {
		t.Fatal("continuation crossed grant boundary")
	}
	if _, err := fixture.service.Get(t.Context(), otherGrant, admitted.JobRef, ""); err != nil {
		t.Fatalf("current grant cannot restart result read: %v", err)
	}
}

func TestTaskAdvisoryCaptureRace(t *testing.T) {
	fixture := newAdvisoryServiceFixture(t, AdvisoryScope, nil, nil)
	calls := 0
	capture := advisoryCaptureFunc(func(ctx context.Context, task, operation string) (taskdiscussion.Basis, error) {
		basis, err := (advisoryJourneyCapture{fixture.f.db}).Capture(ctx, task, operation)
		calls++
		if calls > 1 {
			basis.Revision = "changed-during-capture"
		}
		return basis, err
	})
	service, err := taskdiscussion.New(taskdiscussion.Options{DB: fixture.f.db, Now: func() time.Time { return fixture.f.now }, Capture: capture, Runner: &advisoryJourneyRunner{}, Protect: func(_ context.Context, value string) (string, error) { return value, nil }, Deadline: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Ask(t.Context(), fixture.p, fixture.request("capture-race")); !errors.Is(err, taskdiscussion.ErrBasisChanged) {
		t.Fatalf("captured mixed target: %v", err)
	}
	if ran, err := service.RunOnce(t.Context()); err != nil || ran {
		t.Fatalf("capture race queued provider execution: %v %v", ran, err)
	}
}

func TestTaskAdvisoryCapturedDeadlineBoundsProviderAndClaim(t *testing.T) {
	fixture := newAdvisoryServiceFixture(t, AdvisoryScope, nil, nil)
	capture := advisoryCaptureFunc(func(ctx context.Context, task, operation string) (taskdiscussion.Basis, error) {
		basis, err := (advisoryJourneyCapture{fixture.f.db}).Capture(ctx, task, operation)
		basis.Guidance = json.RawMessage(`{"settings":{"deadline":5000000000}}`)
		return basis, err
	})
	runner := advisoryRunnerFunc(func(ctx context.Context, request taskdiscussion.RunRequest) (taskdiscussion.Answer, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 5*time.Second {
			return taskdiscussion.Answer{}, errors.New("provider context does not honor the captured five-second deadline")
		}
		attempts, err := fixture.f.db.WorkflowRecentAttempts(ctx, request.JobID, 1)
		if err != nil {
			return taskdiscussion.Answer{}, err
		}
		if len(attempts) != 1 || attempts[0].Deadline.Sub(attempts[0].StartedAt) != 5*time.Second {
			return taskdiscussion.Answer{}, errors.New("durable claim does not retain the captured five-second deadline")
		}
		return taskdiscussion.Answer{Answer: "The captured provider budget is enforced.", Sources: []string{"captured-settings"}, Gaps: []string{}}, nil
	})
	service, err := taskdiscussion.New(taskdiscussion.Options{DB: fixture.f.db, Now: func() time.Time { return fixture.f.now }, Capture: capture, Runner: runner, Protect: func(_ context.Context, value string) (string, error) { return value, nil }, Deadline: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := service.Ask(t.Context(), fixture.p, fixture.request("captured-deadline"))
	if err != nil {
		t.Fatal(err)
	}
	if ran, err := service.RunOnce(t.Context()); err != nil || !ran {
		t.Fatalf("worker: %v %v", ran, err)
	}
	saved, err := service.Get(t.Context(), fixture.p, admitted.JobRef, "")
	if err != nil || saved.State != "succeeded" {
		t.Fatalf("captured deadline was not enforced: %+v %v", saved, err)
	}
}
