package daemon_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// TestJudgeChallengesBugAnalysisWithoutTouchingBugWork: on a Bug issue parked
// for its owner, /judge challenges the saved analysis and publishes an
// advisory report. The Bug graph stays exactly where it was: the command and
// the factory's report are no owner answer, wake nothing and block nothing.
func TestJudgeChallengesBugAnalysisWithoutTouchingBugWork(t *testing.T) {
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
	f.issueWithLabels("Merge step fails on a deferred discovery", "bug")
	f.clock.advance(time.Minute)
	f.settle()
	ctx := t.Context()
	session, err := f.db.GetOrCreateSession(ctx, f.repo, testIssue)
	if err != nil {
		t.Fatal(err)
	}
	repo := gittest.New(t)
	if err = f.db.SetSessionWorktree(ctx, session.ID, repo.Dir); err != nil {
		t.Fatal(err)
	}
	service.Git, err = git.Resolve(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// A standalone Bug task whose analyst saved a diagnosis and whose graph
	// now waits for the owner at its review stop.
	if _, err = f.db.GetTaskWorkflowAdmission(ctx, session.ID); errors.Is(err, store.ErrNotFound) {
		if _, err = f.db.AdmitWorkflow(ctx, store.TaskWorkflowAdmission{SessionID: session.ID, Version: 1, Family: "standalone", Workflow: "bug"}); err != nil {
			t.Fatal(err)
		}
	} else if err != nil {
		t.Fatal(err)
	}
	segment, err := f.db.GetOrCreateSegment(ctx, session.ID, store.FirstSegmentOrdinal, "bug-analyst", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Workflow: "bug", Agent: store.AgentClaude, TriggerCommentID: 700, LogPath: func(int) string { return "/logs/bug-analyst.log" }})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := f.db.BindResultAttempt(ctx, store.ResultAttempt{Workflow: "bug", Task: session.TaskRef, WorkContext: "graph#bug:bug-analyst:analysis:1", Attempt: "analyst-1", RunID: run.ID, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := `{"meta":{"workflow":"bug","mode":""},"result":{"outcome":"completed","comment":"Diagnosis complete.","payload":{"bug":{"response":"done","analysis":{"expected":"merge retries","actual":"merge fails","cause":"a deferral is returned as a hard error","remedy":"return retry","criteria":[{"id":"c1","requirement":"a deferred merge returns retry"}]}}}}}`
	if _, err = f.db.RecordResult(ctx, store.ResultRecord{Binding: binding, RequestID: "analysis-1", Payload: []byte(`{}`), Diagnostics: []byte(`{"valid":true}`), Snapshot: []byte(snapshot), Retired: []byte(`{}`), Accepted: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.FinishRun(ctx, run.ID, store.RunOutcome{Status: store.RunSucceeded}); err != nil {
		t.Fatal(err)
	}
	at := f.clock.Now()
	definition, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "builtin", Workflow: "bug", ProfileVersion: 1, Path: "bug/bug.dot", Digest: strings.Repeat("b", 64), CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := f.db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "review", StartedAt: at, NextDueAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.db.ParkGraphNode(ctx, store.GraphStop{Key: "bug-review-stop", InstanceID: instance.ID, Origin: "review", Cause: "review", Reason: "Approve the analysis?", Candidate: []byte(`{"outcome":"incomplete"}`), At: at, AllowRunless: true}); err != nil {
		t.Fatal(err)
	}
	parked, err := f.db.GetGraphInstance(ctx, instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	runsBefore := len(f.runs())

	source, err := f.db.ResolveJudgeSource(ctx, session.TaskRef, "")
	if err != nil || source.Label != "Bug analysis rev 1" {
		t.Fatalf("Bug analysis not a judge source: %+v %v", source, err)
	}

	f.forge.addComment(testIssue, 880, owner(), "/judge is the cause proven?")
	f.clock.advance(time.Minute)
	f.settle()
	f.clock.advance(time.Minute)
	f.settle()

	if runner.calls.Load() != 1 || !runner.receipted.Load() {
		bodies, _, _ := f.forge.snapshot()
		t.Fatalf("judge did not run on the Bug issue: calls=%d; posted: %v; logs: %s", runner.calls.Load(), bodies, f.logs)
	}
	bodies, _, _ := f.forge.snapshot()
	found := false
	for _, body := range bodies {
		if strings.Contains(body, "Bug analysis rev 1") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no advisory report naming the Bug analysis: %v", bodies)
	}

	requests, err := f.db.PendingJudgeRequests(ctx)
	if err != nil || len(requests) != 1 || requests[0].Source.Reference != source.Reference || len(requests[0].Report) == 0 {
		t.Fatalf("durable judge request did not pin the Bug analysis: %+v %v", requests, err)
	}

	// The Bug graph did not move, wake or lose its owner wait.
	after, err := f.db.GetGraphInstance(ctx, instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != parked.Status || after.CurrentNode != parked.CurrentNode || !after.NextDueAt.Equal(parked.NextDueAt) {
		t.Fatalf("judge moved the Bug graph: before %+v after %+v", parked, after)
	}
	wait, err := f.db.GetGraphOwnerWait(ctx, instance.ID)
	if err != nil || wait.State != "waiting" || wait.ReplyID != 0 {
		t.Fatalf("judge answered the Bug owner wait: %+v %v", wait, err)
	}
	attempts, err := f.db.ListGraphNodeAttempts(ctx, instance.ID)
	if err != nil || len(attempts) != 0 {
		t.Fatalf("judge started Bug graph work: %+v %v", attempts, err)
	}
	if _, err = f.db.LatestGraphDispatch(ctx, instance.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("judge queued a Bug graph dispatch: %v", err)
	}
	due, err := f.db.ListDueGraphInstances(ctx, f.clock.Now().Add(365*24*time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range due {
		if d.ID == instance.ID {
			t.Fatal("judge made the parked Bug graph due")
		}
	}
	if got := len(f.runs()); got != runsBefore || len(f.turns.calls()) != 0 {
		t.Fatalf("judge started a run: runs %d -> %d, turns %d", runsBefore, got, len(f.turns.calls()))
	}
	analyses, err := f.db.ListJudgeSources(ctx, session.TaskRef)
	if err != nil || len(analyses) != 1 {
		t.Fatalf("judging changed the saved sources: %+v %v", analyses, err)
	}
}
