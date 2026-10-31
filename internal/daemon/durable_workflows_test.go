package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/durableworkflow"
	"github.com/cloudvane/litefactory/internal/loganalysis"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

type durableDaemonAuthor struct {
	calls   atomic.Int64
	project atomic.Int64
}

func (r *durableDaemonAuthor) Answer(_ context.Context, call standards.AuthoringCall) ([]byte, error) {
	r.calls.Add(1)
	if call.Project != nil {
		r.project.Store(call.Project.ID)
	}
	return []byte(draftWithQuestion), nil
}

type durableDaemonFixture struct {
	path    string
	db      *store.DB
	service *durableworkflow.Service
	actions *owneraction.Service
	adapter *ConversationStandards
	p       owneraction.Principal
	at      time.Time
	runner  *durableDaemonAuthor
	repo    store.Repo
}

func newDurableDaemonFixture(t *testing.T) *durableDaemonFixture {
	t.Helper()
	f := &durableDaemonFixture{path: filepath.Join(t.TempDir(), "durable.db"), at: time.Now().UTC().Truncate(time.Millisecond), runner: &durableDaemonAuthor{}}
	var err error
	f.db, err = store.Open(t.Context(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.db.Close() })
	if err = f.db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, Handle: "owner", UserID: 7, Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	owner, err := f.db.ReconcileConnectorOwner(t.Context(), 7, f.at)
	if err != nil {
		t.Fatal(err)
	}
	f.p = owneraction.Principal{OwnerID: "7", OwnerGeneration: owner.Generation, Transport: "discord", DiscordOwnerID: "17", ChannelID: "3"}
	f.repo, err = f.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	f.compose(t)
	return f
}
func (f *durableDaemonFixture) compose(t *testing.T) {
	t.Helper()
	book := &standards.Service{Store: f.db, OwnerID: 7}
	f.adapter = &ConversationStandards{DB: f.db, Book: book, Authoring: &standards.Authoring{Store: f.db, Book: book, Runner: f.runner, Projects: authorPins{}}, OwnerID: "17", Authorized: func(context.Context, messaging.StandardsRequest) error { return nil }}
	registry, err := installedWorkflows(nil)
	if err != nil {
		t.Fatal(err)
	}
	adapter := standardsWorkflowAdapter{standards: f.adapter, db: f.db}
	f.service, err = durableworkflow.New(durableworkflow.Options{DB: f.db, Registry: registry, Adapters: map[string]durableworkflow.Adapter{"standards-authoring": adapter, "log-analysis": adapter}, Now: func() time.Time { return f.at }, Protect: func(_ context.Context, s string) (string, error) { return s, nil }})
	if err != nil {
		t.Fatal(err)
	}
	f.actions = owneraction.New(f.db, f.service, func() time.Time { return f.at })
	f.actions.SetDiscord("17", []string{"3"})
	f.service.BindActions(f.actions)
}
func (f *durableDaemonFixture) queue(t *testing.T, key, project string) durableworkflow.Proposal {
	t.Helper()
	proposal, err := f.service.Propose(t.Context(), f.p, "standards-authoring", "1", project, map[string]any{"intent": "Keep relevant tests bounded"}, key)
	if err != nil {
		t.Fatal(err)
	}
	if f.runner.calls.Load() != 0 {
		t.Fatal("proposal launched author")
	}
	if err = f.actions.Available(t.Context(), proposal.Proposal.ID, "proposal-message"); err != nil {
		t.Fatal(err)
	}
	f.at = f.at.Add(5 * time.Second)
	confirmed, err := f.actions.Confirm(t.Context(), f.p, proposal.Proposal.ID)
	if err != nil || confirmed.State != "admitted" {
		t.Fatalf("confirm %+v %v", confirmed, err)
	}
	if err = f.actions.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	return proposal
}
func TestDurableWorkflowQueuedAuthoritySurvivesTickAndRestart(t *testing.T) {
	for _, project := range []string{"", "github:acme/app"} {
		name := project
		if name == "" {
			name = "independent"
		}
		t.Run(name, func(t *testing.T) {
			f := newDurableDaemonFixture(t)
			proposal := f.queue(t, "start", project)
			if err := f.actions.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			action, err := f.db.ReadOwnerAction(t.Context(), proposal.Proposal.ID)
			if err != nil || action.State != "executing" || f.runner.calls.Load() != 0 {
				t.Fatalf("queued authority settled prematurely: %+v %v", action, err)
			}
			if err = f.db.Close(); err != nil {
				t.Fatal(err)
			}
			f.db, err = store.Open(t.Context(), f.path)
			if err != nil {
				t.Fatal(err)
			}
			f.compose(t)
			if err = f.service.RecoverRunning(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err = f.actions.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			if worked, err := f.service.RunOnce(t.Context()); err != nil || !worked {
				t.Fatalf("restarted run: %v %v", worked, err)
			}
			view, err := f.service.Get(t.Context(), f.p, proposal.Job, "", 10)
			if err != nil {
				t.Fatal(err)
			}
			if view.State != "waiting_for_input" || len(view.Questions) != 1 || view.SessionRevision < 2 || view.DomainRef == "" || len(view.Items) == 0 {
				t.Fatalf("author receipt not projected: %+v", view)
			}
			if f.runner.calls.Load() != 1 || project != "" && f.runner.project.Load() != f.repo.ID || project == "" && f.runner.project.Load() != 0 {
				t.Fatalf("wrong author scope/calls: %d %d", f.runner.calls.Load(), f.runner.project.Load())
			}
			session, err := f.adapter.Authoring.Show(t.Context(), view.DomainRef, 0)
			if err != nil || session.Version != view.SessionRevision {
				t.Fatalf("domain session projection mismatch %+v %v", session, err)
			}
			if project == "" && session.Project != nil || project != "" && (session.Project == nil || session.Project.ID != f.repo.ID) {
				t.Fatalf("domain target changed: %+v", session.Project)
			}
			action, err = f.db.ReadOwnerAction(t.Context(), proposal.Proposal.ID)
			if err != nil || action.State != "succeeded" {
				t.Fatalf("attempt did not settle action: %+v %v", action, err)
			}
			if head, err := f.db.StandardsHead(t.Context()); err != nil || head != 0 {
				t.Fatalf("draft published book: %d %v", head, err)
			}
			if worked, err := f.service.RunOnce(t.Context()); err != nil || worked || f.runner.calls.Load() != 1 {
				t.Fatalf("attempt replayed %v %v", worked, err)
			}
			retry, err := f.service.Propose(t.Context(), f.p, "standards-authoring", "1", project, map[string]any{"intent": "Keep relevant tests bounded"}, "start")
			if err != nil || retry.Job != proposal.Job {
				t.Fatalf("request lost stable job: %+v %v", retry, err)
			}
		})
	}
}
func TestDurableWorkflowInstalledFixedDefinitionsRejectOverrides(t *testing.T) {
	registry, err := installedWorkflows([]config.WorkflowRegistration{{Ref: "daily", Version: "1", Adapter: "standards-authoring", Name: "Review", BindingProject: "github:acme/app", ProjectMode: "fixed", FixedProject: "github:acme/app"}, {Ref: "incident", Version: "1", Adapter: "log-analysis", Name: "Review", BindingProject: "github:acme/ops", ProjectMode: "fixed", FixedProject: "github:acme/ops"}})
	if err != nil {
		t.Fatal(err)
	}
	daily, err := registry.Get("daily", "1")
	if err != nil {
		t.Fatal(err)
	}
	incident, err := registry.Get("incident", "1")
	if err != nil {
		t.Fatal(err)
	}
	if daily.Digest == incident.Digest || daily.Adapter == incident.Adapter || daily.FixedProject == incident.FixedProject {
		t.Fatal("distinct fixed registrations collapsed")
	}
	resolved, err := durableworkflow.Resolve(daily, "", map[string]any{"intent": "Draft checks"})
	if err != nil || resolved.Project != "github:acme/app" {
		t.Fatalf("fixed target %+v %v", resolved, err)
	}
	if _, err = durableworkflow.Resolve(daily, "github:acme/ops", map[string]any{"intent": "Draft checks"}); err == nil {
		t.Fatal("cross-project override accepted")
	}
	if _, err = durableworkflow.Resolve(daily, "", map[string]any{"intent": "Draft checks", "target": map[string]any{"project": "github:acme/ops"}}); err == nil {
		t.Fatal("nested override accepted")
	}
	if _, err = registry.Get("daily", "unknown"); err == nil {
		t.Fatal("unknown version accepted")
	}
}

func TestDurableWorkflowLogRecoveryPreservesConfirmedAndUncertainFilings(t *testing.T) {
	for _, mode := range []string{"dry-run", "filed", "partial"} {
		t.Run(mode, func(t *testing.T) {
			f := newDurableDaemonFixture(t)
			var starts atomic.Int64
			configure := func() {
				f.adapter.Analyses = &ConversationAnalyses{DB: f.db, Start: func(ctx context.Context, q loganalysis.Request, dry bool) (store.LogAnalysis, error) {
					starts.Add(1)
					if dry != (mode == "dry-run") || len(q.RepositoryIDs) != 1 || q.RepositoryIDs[0] != f.repo.ID || q.StartKey == "" {
						return store.LogAnalysis{}, fmt.Errorf("wrong frozen analysis inputs: %+v dry=%v", q, dry)
					}
					if err := f.db.CheckOwnerActionEffect(ctx); err != nil {
						return store.LogAnalysis{}, err
					}
					analysis, err := f.db.CreateLogAnalysis(ctx, store.LogAnalysis{ID: "analysis-receipt", StartKey: q.StartKey, RequestDigest: "frozen-period-and-target", GrantDigest: "selected-repository", Scope: json.RawMessage(`{}`), Limits: json.RawMessage(`{}`)})
					if err != nil {
						return analysis, err
					}
					_, err = f.db.FinalizeLogAnalysis(ctx, store.LogAnalysisFinalization{AnalysisID: analysis.ID, Generation: analysis.Generation, RequestID: "report", RequestDigest: "report-digest", Report: json.RawMessage(`{"summary":"A retained finding","findings":[{"key":"confirmed","kind":"project_setup","title":"A confirmed finding","target":"github:acme/app","file":true}]}`), State: "completed", Disposition: "no_warranted_change"})
					if err != nil {
						return analysis, err
					}
					state := store.FilingFiled
					url := "https://github.com/acme/app/issues/1"
					if dry {
						state = store.FilingWouldFile
						url = ""
					}
					_, err = f.db.SaveLogAnalysisFiling(ctx, store.LogAnalysisFiling{AnalysisID: analysis.ID, FindingKey: "confirmed", Destination: "github:acme/app", State: state, Reference: func() string {
						if dry {
							return ""
						}
						return "acme/app#1"
					}(), URL: url, Title: "Finding", Body: "Evidence retained"})
					if err != nil {
						return analysis, err
					}
					if mode == "partial" {
						_, err = f.db.SaveLogAnalysisFiling(ctx, store.LogAnalysisFiling{AnalysisID: analysis.ID, FindingKey: "unresolved", Destination: "github:acme/app", State: store.FilingStarted, Title: "Ambiguous finding", Body: "Request may have reached provider"})
						if err != nil {
							return analysis, err
						}
					}
					return f.db.GetLogAnalysis(ctx, analysis.ID)
				}, Report: func(context.Context, string) (string, error) { return "Retained analysis findings", nil }}
			}
			configure()
			inputs := map[string]any{"after": f.at.Add(-time.Hour).Format(time.RFC3339), "before": f.at.Format(time.RFC3339), "dry_run": mode == "dry-run"}
			proposal, err := f.service.Propose(t.Context(), f.p, "log-analysis", "1", "github:acme/app", inputs, "analysis")
			if err != nil {
				t.Fatal(err)
			}
			if err = f.actions.Available(t.Context(), proposal.Proposal.ID, "proposal-message"); err != nil {
				t.Fatal(err)
			}
			f.at = f.at.Add(5 * time.Second)
			if _, err = f.actions.Confirm(t.Context(), f.p, proposal.Proposal.ID); err != nil {
				t.Fatal(err)
			}
			if err = f.actions.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err = f.actions.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			attempt, _, err := f.db.ClaimWorkflowAttempt(t.Context(), f.at, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			record, err := f.db.ReadOwnerAction(t.Context(), proposal.Proposal.ID)
			if err != nil {
				t.Fatal(err)
			}
			var envelope struct {
				Domain        owneraction.Facts `json:"domain"`
				DomainPayload string            `json:"domain_payload"`
			}
			if err = json.Unmarshal([]byte(record.Payload), &envelope); err != nil {
				t.Fatal(err)
			}
			envelope.Domain.Payload = envelope.DomainPayload
			effect := store.WithOwnerActionEffect(t.Context(), attempt.ExecutionID, func() time.Time { return f.at })
			effect = store.WithStandardsOwnerActionClock(effect, func() time.Time { return f.at })
			adapter := standardsWorkflowAdapter{standards: f.adapter, db: f.db}
			if _, err = adapter.Execute(effect, f.p, envelope.Domain, attempt.ExecutionID, f.at); err != nil {
				t.Fatal(err)
			}
			// Crash after the domain receipt, before the workflow result is persisted.
			if err = f.db.Close(); err != nil {
				t.Fatal(err)
			}
			f.db, err = store.Open(t.Context(), f.path)
			if err != nil {
				t.Fatal(err)
			}
			f.compose(t)
			configure()
			if err = f.service.RecoverRunning(t.Context()); err != nil {
				t.Fatal(err)
			}
			view, err := f.service.Get(t.Context(), f.p, proposal.Job, "", 10)
			if err != nil {
				t.Fatal(err)
			}
			expected := "succeeded"
			if mode == "partial" {
				expected = "uncertain"
			}
			if view.State != expected || view.DomainRef != "analysis-receipt" || starts.Load() != 1 {
				t.Fatalf("receipt recovery replayed or lost partial state: %+v starts=%d", view, starts.Load())
			}
			raw, _ := json.Marshal(view.Items)
			if mode == "dry-run" && strings.Contains(string(raw), "issue_url") {
				t.Fatalf("dry run exposed a filing: %s", raw)
			}
			if mode != "dry-run" && !strings.Contains(string(raw), "https://github.com/acme/app/issues/1") {
				t.Fatalf("confirmed issue link lost: %s", raw)
			}
			if mode == "partial" && !strings.Contains(string(raw), `"finding_key":"unresolved"`) {
				t.Fatalf("partial filing evidence lost: %+v", view.Items)
			}
			if worked, err := f.service.RunOnce(t.Context()); err != nil || worked || starts.Load() != 1 {
				t.Fatalf("recovery replayed dispatch: %v %v", worked, err)
			}
		})
	}
}

type durableDeadlineAuthor struct {
	complete func()
	deadline time.Time
}

func (r *durableDeadlineAuthor) Answer(ctx context.Context, _ standards.AuthoringCall) ([]byte, error) {
	r.deadline, _ = ctx.Deadline()
	r.complete()
	return []byte(cleanDraft), nil
}
func TestDurableWorkflowDeadlineRejectsLateAuthorOutput(t *testing.T) {
	f := newDurableDaemonFixture(t)
	runner := &durableDeadlineAuthor{complete: func() { f.at = f.at.Add(2 * time.Hour) }}
	f.adapter.Authoring.Runner = runner
	proposal := f.queue(t, "deadline", "")
	if worked, err := f.service.RunOnce(t.Context()); err != nil || !worked {
		t.Fatalf("deadline run %v %v", worked, err)
	}
	view, err := f.service.Get(t.Context(), f.p, proposal.Job, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if view.State != "uncertain" || len(view.Attempts) != 1 || runner.deadline.IsZero() {
		t.Fatalf("deadline missing or late output accepted: %+v", view)
	}
	attempt := view.Attempts[0]
	if attempt.Deadline.Sub(attempt.StartedAt) != time.Duration(view.Definition.DeadlineSeconds)*time.Second {
		t.Fatalf("persisted deadline differs from advertised deadline: %+v", attempt)
	}
	record, err := f.db.ReadOwnerAction(t.Context(), proposal.Proposal.ID)
	if err != nil || record.State != "uncertain" {
		t.Fatalf("late action authority retained: %+v %v", record, err)
	}
	sessions, err := f.adapter.Authoring.List(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range sessions {
		if session.Draft.Rule == "MUST keep unit tests under a minute" {
			t.Fatal("late author output persisted into domain draft")
		}
	}
}

func TestDurableWorkflowAuthorAnswerKeepsSessionAndRejectsOverlap(t *testing.T) {
	f := newDurableDaemonFixture(t)
	start := f.queue(t, "start-answer", "")
	if _, err := f.service.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, err := f.service.Get(t.Context(), f.p, start.Job, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	answers := durableworkflow.Continuation{Job: start.Job, SessionRevision: before.SessionRevision, Answers: map[string]string{before.Questions[0].ID: "Relevant package tests"}}
	stale := answers
	stale.SessionRevision--
	if _, err = f.service.Continue(t.Context(), f.p, stale, "stale"); err == nil {
		t.Fatal("stale author answer accepted")
	}
	first, err := f.service.Continue(t.Context(), f.p, answers, "answer-first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.service.Continue(t.Context(), f.p, answers, "answer-overlap")
	if err != nil {
		t.Fatal(err)
	}
	for _, proposal := range []durableworkflow.Proposal{first, second} {
		if proposal.Job != start.Job {
			t.Fatal("answer changed job identity")
		}
		if err = f.actions.Available(t.Context(), proposal.Proposal.ID, "answer-message"); err != nil {
			t.Fatal(err)
		}
	}
	f.at = f.at.Add(5 * time.Second)
	if _, err = f.actions.Confirm(t.Context(), f.p, first.Proposal.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.actions.Confirm(t.Context(), f.p, second.Proposal.ID); err == nil {
		t.Fatal("overlapping continuation admitted twice")
	}
	if err = f.actions.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, err := f.service.Get(t.Context(), f.p, start.Job, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if after.DomainRef != before.DomainRef || after.SessionRevision <= before.SessionRevision || len(after.Attempts) != 2 || f.runner.calls.Load() != 2 {
		t.Fatalf("answer did not advance same author session once: before %+v after %+v calls %d", before, after, f.runner.calls.Load())
	}
	if _, err = f.service.Continue(t.Context(), f.p, answers, "answered-old-revision"); err == nil {
		t.Fatal("already answered revision accepted")
	}
}

func TestDurableWorkflowInvalidAuthorTermsFailBeforeAdmission(t *testing.T) {
	f := newDurableDaemonFixture(t)
	for name, inputs := range map[string]map[string]any{
		"missing edit target": {"intent": "edit testing", "mode": "edit"},
		"unknown edit target": {"intent": "edit testing", "mode": "edit", "target": "missing"},
		"create with target":  {"intent": "create testing", "target": "existing"},
		"unknown category":    {"intent": "draft testing", "category": "missing"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, e := f.service.Propose(t.Context(), f.p, "standards-authoring", "1", "", inputs, name); e == nil {
				t.Fatal("invalid author terms admitted")
			}
		})
	}
	if f.runner.calls.Load() != 0 {
		t.Fatal("invalid proposals launched author")
	}
}

func TestDurableWorkflowLogProjectionRetainsCompleteSummary(t *testing.T) {
	f := newDurableDaemonFixture(t)
	analysis, e := f.db.CreateLogAnalysis(t.Context(), store.LogAnalysis{ID: "long-summary", RequestDigest: "request", GrantDigest: "grant", Scope: json.RawMessage(`{}`), Limits: json.RawMessage(`{}`)})
	if e != nil {
		t.Fatal(e)
	}
	summary := strings.Repeat("Retained analysis detail. ", 200)
	report, e := json.Marshal(loganalysis.Report{Summary: summary, NoChangeReason: "No new standard warranted"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.db.FinalizeLogAnalysis(t.Context(), store.LogAnalysisFinalization{AnalysisID: analysis.ID, Generation: analysis.Generation, RequestID: "report", RequestDigest: "report", Report: report, State: "completed", Disposition: "no_warranted_change"}); e != nil {
		t.Fatal(e)
	}
	a := standardsWorkflowAdapter{standards: f.adapter, db: f.db}
	out, e := a.project(t.Context(), owneraction.Facts{Invocation: owneraction.Invocation{Command: "standards-analysis"}}, messaging.StandardsResult{Status: "succeeded", References: map[string]string{"id": analysis.ID}})
	if e != nil || len(out.Items) != 1 || out.Items[0]["analysis_summary"] != summary || out.Items[0]["no_change_reason"] != "No new standard warranted" {
		t.Fatalf("long report lost: %+v %v", out, e)
	}
}
