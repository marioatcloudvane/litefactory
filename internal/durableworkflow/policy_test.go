package durableworkflow

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestInstalledAdapterNeedsFixedPolicyAndUsesSharedLifecycle(t *testing.T) {
	adapter := &workerAdapter{execute: func(context.Context) (Output, error) {
		return Output{State: "succeeded", Summary: "Custom result", Items: []map[string]any{{"check": "bounded"}}}, nil
	}}
	base, p, _ := workerFixture(t, adapter)
	d := BuiltinDefinitions()[1]
	d.Ref = "installed-check"
	d.Adapter = "installed-check"
	d.Inputs = object(map[string]Field{"check": {Type: "string"}}, "check")
	registry, e := NewRegistry([]Definition{d})
	if e != nil {
		t.Fatal(e)
	}
	options := Options{DB: base.db, Registry: registry, Adapters: map[string]Adapter{d.Adapter: adapter}, Now: base.now, Protect: base.protect}
	if _, e = New(options); e == nil {
		t.Fatal("unclassified adapter installed")
	}
	options.Policies = map[string]AdapterPolicy{d.Adapter: {Kind: "author"}}
	svc, e := New(options)
	if e != nil {
		t.Fatal(e)
	}
	at := svc.now()
	svc.now = func() time.Time { return at }
	actions := owneraction.New(base.db, svc.WrapDomain(nil), svc.now)
	actions.SetDiscord("owner-discord", []string{"channel"})
	svc.BindActions(actions)
	proposal, e := svc.Propose(t.Context(), p, d.Ref, d.Version, "", map[string]any{"check": "bounded"}, "custom")
	if e != nil {
		t.Fatal(e)
	}
	if adapter.calls.Load() != 0 {
		t.Fatal("proposal executed custom adapter")
	}
	if e = actions.Available(t.Context(), proposal.Proposal.ID, "message"); e != nil {
		t.Fatal(e)
	}
	at = at.Add(5 * time.Second)
	confirmed, e := actions.Confirm(t.Context(), p, proposal.Proposal.ID)
	if e != nil || confirmed.State != "admitted" {
		t.Fatalf("confirmation %+v %v", confirmed, e)
	}
	if _, e = actions.Step(t.Context(), proposal.Proposal.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.RunOnce(t.Context()); e != nil {
		t.Fatal(e)
	}
	view, e := svc.Get(t.Context(), p, proposal.Job, "", 1)
	if e != nil || view.State != "succeeded" || adapter.calls.Load() != 1 {
		t.Fatalf("custom result %+v %v", view, e)
	}
	options.Policies = map[string]AdapterPolicy{"standards-authoring": {Kind: "analysis"}, d.Adapter: {Kind: "author"}}
	if _, e = New(options); e == nil {
		t.Fatal("built-in classification overridden")
	}
}
func TestSharedActionCannotSupplyAnUnvalidatedWorkflowSnapshot(t *testing.T) {
	s, p, _ := workerFixture(t, &workerAdapter{})
	d, e := s.registry.Get("standards-authoring", "1")
	if e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(frozen{Definition: d, Resolved: Resolved{Inputs: map[string]any{"unknown": "invalid"}}, Job: "injected"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.actions.Propose(t.Context(), p, owneraction.Invocation{Command: "workflow-author", Arguments: map[string]string{"snapshot": string(raw)}}, "injected")
	if e == nil {
		t.Fatal("generic action route accepted an unvalidated workflow snapshot")
	}
}
func TestResolvePreservesExactFractionalInstants(t *testing.T) {
	d := BuiltinDefinitions()[0]
	r, e := Resolve(d, "github:example/app", map[string]any{"after": "2026-10-30T00:00:00.123456789Z", "before": "2026-10-31T00:00:00.987654321Z", "dry_run": true})
	if e != nil {
		t.Fatal(e)
	}
	after, ok := r.Inputs["after"].(string)
	if !ok || !strings.Contains(after, ".123456789") {
		t.Fatal("period precision lost")
	}
}

func TestNoticeRechecksOriginalGrantAndConfiguredDestination(t *testing.T) {
	adapter := &workerAdapter{execute: func(context.Context) (Output, error) {
		return Output{State: "succeeded", Summary: "Retained result"}, nil
	}}
	s, _, _ := workerFixture(t, adapter)
	at := serviceClock(t, s)
	p := serviceGrant(t, s, "notice-owner", "standards:management", nil, nil)
	destination := "channel"
	s.noticeDestination = func(context.Context, owneraction.Principal, Definition, Resolved) (string, error) {
		return destination, nil
	}
	proposal, e := s.Propose(t.Context(), p, "standards-authoring", "1", "", map[string]any{"intent": "draft testing"}, "notice")
	if e != nil {
		t.Fatal(e)
	}
	serviceConfirm(t, s, p, proposal, at)
	if _, e = s.actions.Step(t.Context(), proposal.Proposal.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.RunOnce(t.Context()); e != nil {
		t.Fatal(e)
	}
	reply, e := s.db.ClaimChatNotice(t.Context(), "discord", s.now())
	if e != nil {
		t.Fatal(e)
	}
	if e = s.AuthorizeNotice(t.Context(), reply.MessageID, "channel"); e != nil {
		t.Fatal(e)
	}
	destination = "changed"
	if e = s.AuthorizeNotice(t.Context(), reply.MessageID, "channel"); e == nil {
		t.Fatal("removed destination remained authorized")
	}
	destination = "channel"
	if e = s.db.RevokeConnectorGrant(t.Context(), 42, p.GrantID, s.now()); e != nil {
		t.Fatal(e)
	}
	if e = s.AuthorizeNotice(t.Context(), reply.MessageID, "channel"); e == nil {
		t.Fatal("revoked original grant authorized delivery")
	}
}
func TestResultCursorCannotBeEditedOrTransferred(t *testing.T) {
	adapter := &workerAdapter{execute: func(context.Context) (Output, error) {
		return Output{State: "succeeded", Summary: "saved", Items: []map[string]any{{"part": "one"}, {"part": "two"}}}, nil
	}}
	s, p, queue := workerFixture(t, adapter)
	one := queue("one")
	if _, e := s.RunOnce(t.Context()); e != nil {
		t.Fatal(e)
	}
	two := queue("two")
	if _, e := s.RunOnce(t.Context()); e != nil {
		t.Fatal(e)
	}
	page, e := s.Get(t.Context(), p, one.Job, "", 1)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Get(t.Context(), p, two.Job, page.NextCursor, 1); e == nil {
		t.Fatal("cursor transferred between jobs")
	}
	raw, sig, ok := strings.Cut(page.NextCursor, ".")
	if !ok {
		t.Fatal("cursor lacks authentication")
	}
	decoded, e := base64.RawURLEncoding.DecodeString(raw)
	if e != nil {
		t.Fatal(e)
	}
	changed := strings.Replace(string(decoded), one.Job, two.Job, 1)
	tampered := base64.RawURLEncoding.EncodeToString([]byte(changed)) + "." + sig
	if _, e = s.Get(t.Context(), p, two.Job, tampered, 1); e == nil {
		t.Fatal("edited cursor accepted")
	}
}

func TestFailedProtectionPreservesAlreadySafePartialResult(t *testing.T) {
	adapter := &workerAdapter{execute: func(context.Context) (Output, error) { return Output{State: "succeeded", Summary: "safe"}, nil }}
	s, _, queue := workerFixture(t, adapter)
	proposal := queue("protection")
	a, j, e := s.db.ClaimWorkflowAttempt(t.Context(), s.now(), time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	j.Result = `{"state":"waiting_for_input","summary":"previous","domain_ref":"author-safe","session_revision":2,"items":[{"draft":"safe draft"}],"questions":[{"id":"q","text":"Which test?"}]}`
	s.protect = func(context.Context, string) (string, error) { return "", errors.New("protection unavailable") }
	if e = s.finish(t.Context(), a, j, Output{State: "failed", Summary: "current attempt failed"}, false); e != nil {
		t.Fatal(e)
	}
	saved, e := s.db.ReadWorkflowJob(t.Context(), proposal.Job)
	if e != nil {
		t.Fatal(e)
	}
	if saved.State != "uncertain" || saved.DomainRef != "author-safe" || !strings.Contains(saved.Result, "safe draft") || !strings.Contains(saved.Questions, "Which test?") {
		t.Fatalf("partial result lost: %+v", saved)
	}
}

func TestProjectFilterUsesDefinitionBindingSeparatelyFromFixedTarget(t *testing.T) {
	s, _, _ := workerFixture(t, &workerAdapter{})
	serviceClock(t, s)
	var coverage store.ConnectorCoverage
	for _, name := range []string{"definition", "target"} {
		r, e := s.db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(t.TempDir(), name), Forge: "github", Owner: "example", Name: name})
		if e != nil {
			t.Fatal(e)
		}
		coverage.Repositories = append(coverage.Repositories, store.ConnectorRepository{RepoID: r.ID, RepositoryRef: "github:example/" + name})
	}
	p := serviceGrant(t, s, "binding-filter", "standards:management task:discussion", &coverage, nil)
	d := BuiltinDefinitions()[1]
	d.Ref = "bound-review"
	d.BindingProject = "github:example/definition"
	d.ProjectMode = ProjectFixed
	d.FixedProject = "github:example/target"
	var e error
	s.registry, e = NewRegistry([]Definition{d})
	if e != nil {
		t.Fatal(e)
	}
	page, e := s.List(t.Context(), p, d.BindingProject, "", 1)
	if e != nil || len(page.Workflows) != 1 || page.Workflows[0].FixedProject != d.FixedProject {
		t.Fatalf("binding filter lost distinct execution target: %+v %v", page, e)
	}
	page, e = s.List(t.Context(), p, d.FixedProject, "", 1)
	if e != nil || len(page.Workflows) != 0 {
		t.Fatalf("definition ownership replaced by target filter: %+v %v", page, e)
	}
}
