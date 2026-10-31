package durableworkflow

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

func serviceGrant(t *testing.T, s *Service, id, scope string, read, actions *store.ConnectorCoverage) owneraction.Principal {
	t.Helper()
	ctx := t.Context()
	now := s.now()
	owner, err := s.db.ConnectorOwner(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	const resource = "https://example.test/litefactory/mcp"
	c := store.ConnectorClient{ID: id, Name: id, AuthMethod: "none", Redirects: []string{"https://client.test/callback"}, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err = s.db.RegisterConnectorClient(ctx, c, now); err != nil {
		t.Fatal(err)
	}
	b := store.ConnectorBrowser{ID: id, ClientID: id, RedirectURI: c.Redirects[0], Resource: resource, Scope: scope, PKCEChallenge: "challenge", NonceDigest: "nonce", UpstreamStateDigest: "upstream", OwnerGeneration: owner.Generation, ExpiresAt: now.Add(10 * time.Minute)}
	if err = s.db.CreateConnectorBrowser(ctx, b, 42, now); err != nil {
		t.Fatal(err)
	}
	if err = s.db.VerifyConnectorBrowser(ctx, id, "nonce", "upstream", 42, now); err != nil {
		t.Fatal(err)
	}
	g := store.ConnectorGrant{ID: id, ClientID: id, Resource: resource, Scope: scope, OwnerID: 42, OwnerGeneration: owner.Generation, CreatedAt: now, ExpiresAt: now.Add(time.Hour), Coverage: read, ActionCoverage: actions}
	code := store.ConnectorCode{Digest: id + "code", GrantID: id, ClientID: id, RedirectURI: b.RedirectURI, Resource: resource, Scope: scope, PKCEChallenge: "challenge", ExpiresAt: now.Add(time.Minute)}
	if err = s.db.ConsentConnectorBrowser(ctx, id, "nonce", 42, now, g, code, true); err != nil {
		t.Fatal(err)
	}
	return owneraction.Principal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "mcp", ClientID: id, GrantID: id}
}
func serviceClock(t *testing.T, s *Service) *time.Time {
	t.Helper()
	at := s.now()
	s.now = func() time.Time { return at }
	s.actions = owneraction.New(s.db, s, func() time.Time { return at })
	s.actions.SetDiscord("owner-discord", []string{"channel"})
	return &at
}
func serviceConfirm(t *testing.T, s *Service, p owneraction.Principal, proposal Proposal, at *time.Time) {
	t.Helper()
	*at = at.Add(6 * time.Second)
	confirmed, err := s.actions.Confirm(t.Context(), p, proposal.Proposal.ID)
	if err != nil || confirmed.State != "admitted" {
		t.Fatalf("confirm %+v %v", confirmed, err)
	}
}
func TestServiceOwnerRequestIdentityAndOriginalConfirmation(t *testing.T) {
	s, _, _ := workerFixture(t, &workerAdapter{})
	at := serviceClock(t, s)
	first := serviceGrant(t, s, "first", "standards:management", nil, nil)
	second := serviceGrant(t, s, "second", "standards:management", nil, nil)
	inputs := map[string]any{"intent": "draft testing"}
	one, err := s.Propose(t.Context(), first, "standards-authoring", "1", "", inputs, "same-key")
	if err != nil {
		t.Fatal(err)
	}
	two, err := s.Propose(t.Context(), second, "standards-authoring", "1", "", inputs, "same-key")
	if err != nil || two.Job != one.Job || two.Proposal.ID != one.Proposal.ID {
		t.Fatalf("cross-client retry %+v %v", two, err)
	}
	if _, err = s.Propose(t.Context(), second, "standards-authoring", "1", "", map[string]any{"intent": "changed"}, "same-key"); !errors.Is(err, owneraction.ErrRequestConflict) {
		t.Fatalf("changed inputs: %v", err)
	}
	*at = at.Add(6 * time.Second)
	other, err := s.actions.Confirm(t.Context(), second, one.Proposal.ID)
	if err == nil && other.State == "admitted" {
		t.Fatal("second client adopted confirmation")
	}
	original, err := s.actions.Confirm(t.Context(), first, one.Proposal.ID)
	if err != nil || original.State != "admitted" {
		t.Fatalf("original confirmation %+v %v", original, err)
	}
	wrong := first
	wrong.OwnerGeneration++
	if _, err = s.Propose(t.Context(), wrong, "standards-authoring", "1", "", inputs, "same-key"); err == nil {
		t.Fatal("different owner generation reused operation")
	}
}
func TestServiceBindingAndTargetPermissionsBeforePagination(t *testing.T) {
	s, _, _ := workerFixture(t, &workerAdapter{})
	serviceClock(t, s)
	var repos []store.Repo
	for _, name := range []string{"visible", "hidden"} {
		r, err := s.db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(t.TempDir(), name), Forge: "github", Owner: "example", Name: name})
		if err != nil {
			t.Fatal(err)
		}
		repos = append(repos, r)
	}
	coverage := &store.ConnectorCoverage{Repositories: []store.ConnectorRepository{{RepoID: repos[0].ID, RepositoryRef: "github:example/visible"}}}
	p := serviceGrant(t, s, "limited", "standards:management task:discussion", coverage, nil)
	definitions := BuiltinDefinitions()
	base := definitions[1]
	hidden := base
	hidden.Ref = "a-hidden"
	hidden.BindingProject = "github:example/hidden"
	bound := base
	bound.Ref = "b-bound"
	bound.BindingProject = "github:example/visible"
	s.registry, _ = NewRegistry([]Definition{hidden, bound, base})
	page, err := s.List(t.Context(), p, "", "", 1)
	if err != nil || len(page.Workflows) != 1 || page.Workflows[0].Ref != "b-bound" || page.NextCursor == "" {
		t.Fatalf("filtered first page %+v %v", page, err)
	}
	next, err := s.List(t.Context(), p, "", page.NextCursor, 1)
	if err != nil || len(next.Workflows) != 1 || next.Workflows[0].Ref != "standards-authoring" || next.NextCursor != "" {
		t.Fatalf("filtered second page %+v %v", next, err)
	}
	if _, err = s.Describe(t.Context(), p, "a-hidden", "1", "github:example/visible"); err == nil {
		t.Fatal("hidden definition binding disclosed")
	}
	if _, err = s.Propose(t.Context(), p, "b-bound", "1", "github:example/hidden", map[string]any{"intent": "draft"}, "hidden-target"); err == nil {
		t.Fatal("authorized binding allowed hidden target")
	}
	if _, err = s.Describe(t.Context(), p, "standards-authoring", "1", ""); err != nil {
		t.Fatalf("explicit independent authority: %v", err)
	}
}
func TestServiceRevocationStopsReadsProposalConfirmationAndQueuedExecution(t *testing.T) {
	adapter := &workerAdapter{execute: func(context.Context) (Output, error) { return Output{State: "succeeded", Summary: "done"}, nil }}
	s, _, _ := workerFixture(t, adapter)
	at := serviceClock(t, s)
	p := serviceGrant(t, s, "revoked", "standards:management", nil, nil)
	start, err := s.Propose(t.Context(), p, "standards-authoring", "1", "", map[string]any{"intent": "draft"}, "queued")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := s.Propose(t.Context(), p, "standards-authoring", "1", "", map[string]any{"intent": "another"}, "pending")
	if err != nil {
		t.Fatal(err)
	}
	serviceConfirm(t, s, p, start, at)
	if _, err = s.actions.Step(t.Context(), start.Proposal.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.db.RevokeConnectorGrant(t.Context(), 42, p.GrantID, s.now()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(t.Context(), p, start.Job, "", 1); err == nil {
		t.Fatal("revoked grant read job")
	}
	if _, err = s.Propose(t.Context(), p, "standards-authoring", "1", "", map[string]any{"intent": "draft"}, "new"); err == nil {
		t.Fatal("revoked grant proposed")
	}
	confirmed, err := s.actions.Confirm(t.Context(), p, pending.Proposal.ID)
	if err == nil && confirmed.State == "admitted" {
		t.Fatal("revoked grant confirmed")
	}
	if _, err = s.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if adapter.calls.Load() != 0 {
		t.Fatal("revoked queued execution launched adapter")
	}
	job, err := s.db.ReadWorkflowJob(t.Context(), start.Job)
	if err != nil || job.State != "failed" {
		t.Fatalf("revoked job %+v %v", job, err)
	}
}
func TestServiceConcurrentAnswersAdmitOnlyOneRevision(t *testing.T) {
	a := &workerAdapter{execute: func(context.Context) (Output, error) {
		return Output{State: "waiting_for_input", Summary: "question", DomainRef: "session", SessionRevision: 1, Questions: []Question{{ID: "q", Text: "Which behavior?"}}}, nil
	}}
	s, p, queue := workerFixture(t, a)
	start := queue("start")
	if _, err := s.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Use MCP grants for independently confirmable proposals and a controlled clock.
	at := serviceClock(t, s)
	p = serviceGrant(t, s, "answers", "standards:management", nil, nil)
	gate := make(chan struct{})
	results := make(chan Proposal, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, key := range []string{"answer-one", "answer-two"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			r, e := s.Continue(t.Context(), p, Continuation{Job: start.Job, SessionRevision: 1, Answers: map[string]string{"q": "bounded tests"}}, key)
			results <- r
			errs <- e
		}()
	}
	close(gate)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	*at = at.Add(6 * time.Second)
	admitted := 0
	for proposal := range results {
		confirmed, err := s.actions.Confirm(t.Context(), p, proposal.Proposal.ID)
		if err == nil && confirmed.State == "admitted" {
			admitted++
		}
	}
	if admitted != 1 {
		t.Fatalf("overlapping answers admitted %d attempts", admitted)
	}
	if _, err := s.Continue(t.Context(), p, Continuation{Job: start.Job, SessionRevision: 0, Answers: map[string]string{"q": "stale"}}, "stale"); err == nil {
		t.Fatal("stale revision accepted")
	}
}

func TestServiceContinuationRejectsChangedInstalledDefinition(t *testing.T) {
	adapter := &workerAdapter{execute: func(context.Context) (Output, error) {
		return Output{State: "waiting_for_input", Summary: "question", DomainRef: "session", SessionRevision: 1, Questions: []Question{{ID: "q", Text: "Which behavior?"}}}, nil
	}}
	s, p, queue := workerFixture(t, adapter)
	start := queue("start")
	if _, err := s.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	definitions := BuiltinDefinitions()
	definitions[1].Purpose = "Changed installed contract"
	registry, err := NewRegistry(definitions)
	if err != nil {
		t.Fatal(err)
	}
	s.registry = registry
	if _, err = s.Continue(t.Context(), p, Continuation{Job: start.Job, SessionRevision: 1, Answers: map[string]string{"q": "answer"}}, "changed-definition"); err == nil {
		t.Fatal("continuation adopted changed installed definition")
	}
}

func TestServiceNoticeDestinationIsFrozenBeforeExecution(t *testing.T) {
	adapter := &workerAdapter{execute: func(context.Context) (Output, error) { return Output{State: "succeeded", Summary: "saved"}, nil }}
	s, p, queue := workerFixture(t, adapter)
	destination := "original-channel"
	s.noticeDestination = func(context.Context, owneraction.Principal, Definition, Resolved) (string, error) {
		return destination, nil
	}
	start := queue("notice-frozen")
	request, err := s.db.WorkflowRequestByProposal(t.Context(), start.Proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot frozen
	if err = json.Unmarshal([]byte(request.Inputs), &snapshot); err != nil || snapshot.NoticeDestination != "original-channel" {
		t.Fatalf("missing frozen notice destination: %+v %v", snapshot, err)
	}
	destination = "replacement-channel"
	if _, err := s.Propose(t.Context(), p, "standards-authoring", "1", "", map[string]any{"intent": "draft testing"}, "notice-frozen"); !errors.Is(err, owneraction.ErrRequestConflict) {
		t.Fatalf("changed destination did not conflict: %v", err)
	}
	if _, err := s.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	notices, err := s.db.WorkflowNotices(t.Context(), start.Job)
	if err != nil || len(notices) != 1 || notices[0].Destination != "" || notices[0].State != "unavailable" {
		t.Fatalf("frozen destination %+v %v", notices, err)
	}
}
func TestServiceConsumedContinuationRetryReturnsOriginalOperation(t *testing.T) {
	adapter := &workerAdapter{execute: func(context.Context) (Output, error) {
		return Output{State: "waiting_for_input", Summary: "question", DomainRef: "session", SessionRevision: 1, Questions: []Question{{ID: "q", Text: "Which behavior?"}}}, nil
	}}
	s, _, queue := workerFixture(t, adapter)
	start := queue("start")
	if _, err := s.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	at := serviceClock(t, s)
	p := serviceGrant(t, s, "retry-answer", "standards:management", nil, nil)
	input := Continuation{Job: start.Job, SessionRevision: 1, Answers: map[string]string{"q": "bounded tests"}}
	proposed, err := s.Continue(t.Context(), p, input, "answer")
	if err != nil {
		t.Fatal(err)
	}
	serviceConfirm(t, s, p, proposed, at)
	// Admission has consumed the waiting revision, even before the provider runs.
	replay, err := s.Continue(t.Context(), p, input, "answer")
	if err != nil || replay.Job != proposed.Job || replay.Proposal.ID != proposed.Proposal.ID {
		t.Fatalf("consumed retry %+v %v", replay, err)
	}
	input.Answers = map[string]string{"q": "changed"}
	if _, err = s.Continue(t.Context(), p, input, "answer"); !errors.Is(err, owneraction.ErrRequestConflict) {
		t.Fatalf("changed consumed answer: %v", err)
	}
}
