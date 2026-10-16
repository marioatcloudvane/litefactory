package daemon_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

type controlledJudge struct {
	calls                           atomic.Int32
	preflight, submitted, receipted atomic.Bool
}

func (r *controlledJudge) RunJudge(ctx context.Context, q agent.JudgeRequest) (agent.JudgeRunResult, error) {
	r.calls.Add(1)
	if err := q.Validate(); err != nil {
		return agent.JudgeRunResult{}, err
	}
	client := submission.Client{DescriptorPath: q.Invocation.MCPLaunch.DescriptorPath()}
	invoke := func(name string, input any) (submission.Response, error) {
		data, err := json.Marshal(input)
		if err != nil {
			return submission.Response{}, err
		}
		response, err := client.Call(ctx, name, data)
		if err == nil && response.Error != "" {
			err = fmt.Errorf("%s: %s", name, response.Error)
		}
		return response, err
	}
	listed, err := invoke("evidence_list", struct{}{})
	if err != nil {
		return agent.JudgeRunResult{}, err
	}
	var evidence []agentresult.JudgeEvidence
	if err = json.Unmarshal(listed.Result, &evidence); err != nil {
		return agent.JudgeRunResult{}, err
	}
	coverage := []string{}
	gaps := []string{}
	for _, e := range evidence {
		if e.Available {
			coverage = append(coverage, e.ID)
		} else if e.Essential {
			gaps = append(gaps, e.Gap)
		}
	}
	if _, err = invoke("evidence_read", map[string]any{"id": "source", "start": 1, "count": 200}); err != nil {
		return agent.JudgeRunResult{}, err
	}
	report := agentresult.JudgeReport{Version: 1, Verdict: "no_material_concerns", Summary: "The controlled assessment reviewed the captured saved Plan.", Coverage: coverage, Uncertainty: []string{}, Findings: []agentresult.JudgeFinding{}}
	if len(gaps) > 0 {
		report.Verdict = "unable_to_assess"
		report.Uncertainty = gaps
	}
	input := agentresult.JudgeInput{Report: report}
	checked, err := invoke("communication_preflight", input)
	if err != nil {
		return agent.JudgeRunResult{}, err
	}
	var diagnostics agentresult.Diagnostics
	if err = json.Unmarshal(checked.Result, &diagnostics); err != nil {
		return agent.JudgeRunResult{}, err
	}
	if !diagnostics.Valid {
		return agent.JudgeRunResult{}, fmt.Errorf("invalid preflight: %s", checked.Result)
	}
	r.preflight.Store(true)
	submitted, err := invoke("communication_submit", input)
	if err != nil {
		return agent.JudgeRunResult{}, err
	}
	r.submitted.Store(true)
	var receipt store.ResultReceipt
	if err = json.Unmarshal(submitted.Result, &receipt); err != nil {
		return agent.JudgeRunResult{}, err
	}
	if receipt.InputState != "accepted" {
		return agent.JudgeRunResult{}, fmt.Errorf("result was not accepted: %s", submitted.Result)
	}
	recovered, err := invoke("communication_receipt", map[string]string{"request_id": submitted.RequestID})
	if err != nil {
		return agent.JudgeRunResult{}, err
	}
	var recoveredReceipt store.ResultReceipt
	if err = json.Unmarshal(recovered.Result, &recoveredReceipt); err != nil {
		return agent.JudgeRunResult{}, err
	}
	if recoveredReceipt.InputState != "accepted" {
		return agent.JudgeRunResult{}, fmt.Errorf("receipt not accepted: %s", recovered.Result)
	}
	r.receipted.Store(true)
	return agent.JudgeRunResult{ObservedModel: q.Invocation.Model, ProviderVersion: "controlled-fixture"}, nil
}

// TestJudgeOwnerBareCommandDurableAdvisory exercises the admitted owner command,
// immutable source, shared result protocol and requesting-ticket publication.
func TestJudgeOwnerBareCommandDurableAdvisory(t *testing.T) {
	w := newWebhookFixture(t)
	f := w.fixture
	w.clock.mu.Lock()
	w.clock.now = time.Now().UTC().Add(time.Minute)
	w.clock.mu.Unlock()
	origin := recoveryProvider(t, f)
	configureRecoverySource(t, w, origin)
	cfg, err := config.Decode(strings.NewReader("agents:\n  claude: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	runner := &controlledJudge{}
	brokerHome, err := os.MkdirTemp("/tmp", "lf-jtest-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(brokerHome) })
	brokerHome, err = filepath.EvalSymlinks(brokerHome)
	if err != nil {
		t.Fatal(err)
	}
	service := &turn.JudgeService{DB: f.db, Config: cfg, Providers: map[agent.Kind]agent.JudgeRunner{agent.Kind("claude"): runner}, Paths: config.NewPaths(brokerHome), Executable: "/usr/bin/false", Redactor: redact.New(redact.Options{Logger: f.logger})}
	f.daemon = newJudgeDaemon(t, w, origin, service)
	f.plantCursor()
	f.issue("Review the saved Plan")
	f.clock.advance(time.Minute)
	f.settle()
	session, err := f.db.GetOrCreateSession(t.Context(), f.repo, testIssue)
	if err != nil {
		t.Fatal(err)
	}
	repo := gittest.New(t)
	if err = f.db.SetSessionWorktree(t.Context(), session.ID, repo.Dir); err != nil {
		t.Fatal(err)
	}
	service.Git, err = git.Resolve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	state := planbrief.PlanState{SharedRevision: "saved-shared", Stories: []planbrief.PlanStoryState{{Key: "story-001", Stage: "story", Candidate: "saved-candidate", Draft: planbrief.PlanDraft{Key: "story-001", Name: "Saved proposal", UserStory: planbrief.UserStoryContent{Actor: "An owner", Want: "to inspect saved work", Benefit: "to find gaps"}}}}}
	if _, err = f.db.SavePlanWorkflowState(t.Context(), session.TaskRef, 0, state); err != nil {
		t.Fatal(err)
	}
	source, err := f.db.ResolveJudgeSource(t.Context(), session.TaskRef, "")
	if err != nil {
		t.Fatal(err)
	}
	f.forge.addComment(testIssue, 880, owner(), "/judge")
	f.clock.advance(time.Minute)
	f.settle()
	if runner.calls.Load() != 1 || !runner.preflight.Load() || !runner.submitted.Load() || !runner.receipted.Load() {
		bodies, _, _ := f.forge.snapshot()
		t.Fatalf("judge protocol calls=%d preflight=%t submit=%t receipt=%t; reports: %v", runner.calls.Load(), runner.preflight.Load(), runner.submitted.Load(), runner.receipted.Load(), bodies)
	}
	requests, err := f.db.PendingJudgeRequests(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || requests[0].Source.Reference != source.Reference || len(requests[0].Report) == 0 {
		t.Fatalf("durable source/report: %+v", requests)
	}
	bodies, _, _ := f.forge.snapshot()
	found := false
	for i, body := range bodies {
		if strings.Contains(body, source.Reference) && strings.Contains(body, "Advisory challenge") {
			found = true
			f.forge.mu.Lock()
			destination := f.forge.postedIssues[i]
			f.forge.mu.Unlock()
			if !strings.Contains(body, "Verdict: no material concerns") {
				t.Fatalf("main path did not complete an assessment: %s", body)
			}
			if destination != testIssue {
				t.Fatalf("report destination=%d", destination)
			}
		}
	}
	if !found {
		t.Fatalf("no source-identified advisory report: %v; logs: %s", bodies, f.logs)
	}
	if len(f.turns.calls()) != 0 {
		t.Fatal("judge entered workflow writer")
	}
	// A restarted daemon reconciles the saved report instead of launching a model.
	f.daemon = newJudgeDaemon(t, w, origin, service)
	f.clock.advance(time.Minute)
	f.settle()
	if runner.calls.Load() != 1 {
		t.Fatal("restart reran saved assessment")
	}
	current, _, err := f.db.GetPlanWorkflowState(t.Context(), session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	if current.Stories[0].Candidate != "saved-candidate" {
		t.Fatal("judging changed saved Plan")
	}
	// A refused setting shows the accepted form.
	f.forge.addComment(testIssue, 883, owner(), "/judge --unknown x")
	f.clock.advance(time.Minute)
	f.settle()
	bodies, _, _ = f.forge.snapshot()
	usage := false
	for _, body := range bodies {
		if strings.Contains(body, "unknown judge setting") && strings.Contains(body, "Usage: /judge") {
			usage = true
		}
	}
	if !usage || runner.calls.Load() != 1 {
		t.Fatalf("parse refusal without usage line (calls=%d): %v", runner.calls.Load(), bodies)
	}
}

func TestJudgeNonOwnerDoesNotLaunchAdvisory(t *testing.T) {
	w := newWebhookFixture(t)
	f := w.fixture
	w.clock.mu.Lock()
	w.clock.now = time.Now().UTC().Add(time.Minute)
	w.clock.mu.Unlock()
	origin := recoveryProvider(t, f)
	configureRecoverySource(t, w, origin)
	cfg, err := config.Decode(strings.NewReader("agents:\n  claude: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	runner := &controlledJudge{}
	brokerHome, err := os.MkdirTemp("/tmp", "lf-jtest-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(brokerHome) })
	brokerHome, err = filepath.EvalSymlinks(brokerHome)
	if err != nil {
		t.Fatal(err)
	}
	service := &turn.JudgeService{DB: f.db, Config: cfg, Providers: map[agent.Kind]agent.JudgeRunner{agent.Kind("claude"): runner}, Paths: config.NewPaths(brokerHome), Executable: "/usr/bin/false", Redactor: redact.New(redact.Options{Logger: f.logger})}
	f.daemon = newJudgeDaemon(t, w, origin, service)
	f.plantCursor()
	f.issue("Owner-only saved challenge")
	f.clock.advance(time.Minute)
	f.settle()
	f.forge.addComment(testIssue, 881, fakeUser{ID: ownerID + 100, Login: "visitor", Type: "User"}, "/judge")
	f.clock.advance(time.Minute)
	f.settle()
	if runner.calls.Load() != 0 {
		t.Fatal("non-owner launched judge")
	}
	requests, err := f.db.PendingJudgeRequests(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 0 {
		t.Fatal("non-owner created durable judge authority")
	}
}

func newJudgeDaemon(t *testing.T, w *webhookFixture, origin ticket.Origin, service *turn.JudgeService) *daemon.Daemon {
	t.Helper()
	connections := projectconnection.NewFactory(projectconnection.Options{
		Resolver: secrets.Resolver{Keychain: w.spy.lookup}, Logger: w.logger, ExpectedActorID: botID,
	})
	identities := w.filter.Identities()
	identities.TicketOrigin = origin
	filter, err := turn.NewFilter(w.db, identities, w.logger)
	if err != nil {
		t.Fatal(err)
	}
	version, err := w.db.CurrentConnection(t.Context(), w.repo.ID, store.TicketConnection)
	if err != nil {
		t.Fatal(err)
	}
	clients, err := connections.Clients(t.Context(), version, botID)
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := turn.NewRoutedGitHubTicketWorkflow(w.db, clients.Tickets, botID, daemon.ProjectTicketSelector(w.db, connections))
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Logger: w.logger}), Limit: github.CommentSizeLimit, Logger: w.logger})
	if err != nil {
		t.Fatal(err)
	}
	reporter, err := turn.NewReporter(turn.ReporterOptions{
		DB: w.db, Forge: workflow, Renderer: renderer, Logger: w.logger,
		BotUserID: botID, StoryLockDir: w.paths.StoryProgressLocksDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	opts := daemon.Options{
		Judge:        service,
		DefaultAgent: agent.Kind(store.AgentClaude), ToolProfiles: config.DefaultToolProfiles().Permissions,
		Agents: turn.RunnerSet{agent.Kind(store.AgentClaude): nil}, Classifiers: map[agent.Kind]agent.Classifier{agent.Kind(store.AgentClaude): fakeModeClassifier{}},
		DB: w.db, Forge: clients.Code, Tickets: clients.Tickets, Filter: filter, Turn: w.turns, Reporter: reporter,
		Connections: connections, InstanceID: "judge-controlled-test", Interval: testInterval,
		DiscoveryInterval: time.Minute, Ticks: w.ticks, Logger: w.logger,
		Now: w.clock.Now, ShutdownGrace: 500 * time.Millisecond,
	}
	d, err := daemon.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestJudgeAmbiguousPublicationRemainsPendingWithoutRerun(t *testing.T) {
	w := newWebhookFixture(t)
	f := w.fixture
	w.clock.mu.Lock()
	w.clock.now = time.Now().UTC().Add(time.Minute)
	w.clock.mu.Unlock()
	origin := recoveryProvider(t, f)
	configureRecoverySource(t, w, origin)
	cfg, err := config.Decode(strings.NewReader("agents:\n  claude: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	runner := &controlledJudge{}
	brokerHome, err := os.MkdirTemp("/tmp", "lf-jtest-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(brokerHome) })
	brokerHome, err = filepath.EvalSymlinks(brokerHome)
	if err != nil {
		t.Fatal(err)
	}
	service := &turn.JudgeService{DB: f.db, Config: cfg, Providers: map[agent.Kind]agent.JudgeRunner{agent.Kind("claude"): runner}, Paths: config.NewPaths(brokerHome), Executable: "/usr/bin/false", Redactor: redact.New(redact.Options{Logger: f.logger})}
	f.daemon = newJudgeDaemon(t, w, origin, service)
	f.plantCursor()
	f.issue("Review the saved Plan")
	f.clock.advance(time.Minute)
	f.settle()
	session, err := f.db.GetOrCreateSession(t.Context(), f.repo, testIssue)
	if err != nil {
		t.Fatal(err)
	}
	repo := gittest.New(t)
	if err = f.db.SetSessionWorktree(t.Context(), session.ID, repo.Dir); err != nil {
		t.Fatal(err)
	}
	service.Git, err = git.Resolve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	state := planbrief.PlanState{SharedRevision: "saved-shared", Stories: []planbrief.PlanStoryState{{Key: "story-001", Stage: "story", Candidate: "saved-candidate", Draft: planbrief.PlanDraft{Key: "story-001", Name: "Saved proposal", UserStory: planbrief.UserStoryContent{Actor: "An owner", Want: "to inspect saved work", Benefit: "to find gaps"}}}}}
	if _, err = f.db.SavePlanWorkflowState(t.Context(), session.TaskRef, 0, state); err != nil {
		t.Fatal(err)
	}
	_, err = f.db.ResolveJudgeSource(t.Context(), session.TaskRef, "")
	if err != nil {
		t.Fatal(err)
	}

	f.forge.mu.Lock()
	f.forge.onPostComment = func(w http.ResponseWriter, _ int) bool {
		http.Error(w, `{"message":"uncertain remote publication"}`, http.StatusBadGateway)
		return true
	}
	f.forge.mu.Unlock()
	f.forge.addComment(testIssue, 882, owner(), "/judge")
	f.clock.advance(time.Minute)
	f.settle()
	if runner.calls.Load() != 1 {
		requests, _ := f.db.PendingJudgeRequests(t.Context())
		t.Fatalf("assessment launches=%d requests=%+v", runner.calls.Load(), requests)
	}
	requests, err := f.db.PendingJudgeRequests(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || len(requests[0].Report) == 0 {
		t.Fatalf("report was not saved before publication: %+v", requests)
	}
	saved := append([]byte(nil), requests[0].Report...)
	pending, found, err := f.db.FindPendingReportDelivery(t.Context(), session.ID, 882)
	if err != nil || !found || !pending.PossiblySent || pending.Completed {
		t.Fatalf("uncertain delivery=%+v found=%t err=%v", pending, found, err)
	}
	// A fresh daemon retains uncertainty and reuses the report instead of rerunning.
	f.daemon = newJudgeDaemon(t, w, origin, service)
	f.clock.advance(time.Hour)
	f.clock.advance(time.Minute)
	f.settle()
	if runner.calls.Load() != 1 {
		t.Fatal("uncertain publication reran assessment")
	}
	recovered, err := f.db.GetJudgeRequest(t.Context(), requests[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(saved, recovered.Report) {
		t.Fatal("publication recovery rewrote saved report")
	}
	pending, found, err = f.db.FindPendingReportDelivery(t.Context(), session.ID, 882)
	if err != nil || !found || pending.Completed {
		t.Fatalf("ambiguous publication incorrectly completed: %+v %t %v", pending, found, err)
	}
}
