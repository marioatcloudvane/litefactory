package turn

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

const implementCompletedJSON = `{"version":1,"response":"completed","result":{"outcome":"completed","comment":"The health endpoint is built and its tests pass.","payload":{"implement":{"stories":[]}}}}`

const implementBlockedJSON = `{"version":1,"response":"blocked","result":{"outcome":"blocked","comment":"Built what could be built.","reason":"The payment API key is not available to this story.","payload":{"implement":{"stories":[]}}}}`

const implementIncompleteJSON = `{"version":1,"response":"incomplete","result":{"outcome":"incomplete","comment":"Part of it is built.","reason":"The second endpoint is not done.","payload":{"implement":{"stories":[]}}}}`

const implementTaskParent = "github:acme/acme-api#900"

// assembledImplementFixture is a turn fixture whose task is the worker of a
// running story of a managed parent, with the result route and reporter wired the
// way a launched run has them.
func assembledImplementFixture(t *testing.T, runner Runner) (*fixture, *fakeForge, Request) {
	t.Helper()
	f := newFixture(t, runner)
	authority, err := NewAttemptAuthority(t.Context(), f.db)
	mustProgress(t, err)
	t.Cleanup(func() { mustProgress(t, authority.Close(context.WithoutCancel(t.Context()))) })
	f.opts.MCP = &MCPRuntime{Authority: authority, Paths: config.NewPaths(launchFixtureDirectory(t)), Executable: "/factory/litefactory", Now: time.Now}
	f.opts.Tiers.ModelByTier = map[config.Kind]map[config.Tier]string{"claude": {"balanced": "test-model"}}
	forge := newFakeForge(t)
	f.opts.MCP.Comments = forge.client
	f.opts.MCP.Redactor = redact.New(redact.Options{Environ: []string{}})
	f.opts.MCP.BotID = 9999
	f.opts.ResearchReporter = newReporter(t, f, forge, nil)
	f.opts.SubIssueCreator = forge.client
	return f, forge, f.request()
}

// seedImplementStory makes the fixture's task the worker of a running story:
// an approved managed parent, a ledger row and the worker session.
func seedImplementStory(t *testing.T, f *fixture) {
	t.Helper()
	ctx := t.Context()
	mustProgress(t, f.db.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: implementTaskParent, Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}))
	story := store.ManagedStory{ParentTask: implementTaskParent, Generation: 1, Key: "story-001", IssueNumber: f.session.IssueNumber, IssueID: 4242, State: "planning"}
	mustProgress(t, f.db.PutManagedStory(ctx, story))
	mustProgress(t, f.db.SetManagedFeatureReady(ctx, implementTaskParent, 1, true))
	mustProgress(t, f.db.ApproveManagedStories(ctx, implementTaskParent, 1, []store.ApprovedStoryRevision{{ManagedStory: story, ParentID: 9, Title: "Health endpoint", Body: "As an operator I want a health endpoint.", Hash: strings.Repeat("ab", 32), FetchedAt: implementClock}}))
	mustProgress(t, f.db.DeliverStoryScopeNotice(ctx, implementTaskParent, 1, "story-001", "freeze"))
	mustProgress(t, f.db.ActivateManagedScope(ctx, implementTaskParent, 1))
	mustProgress(t, f.db.EnsureImplementStories(ctx, implementTaskParent, 1, []store.ImplementStorySeed{{Key: "story-001", Position: 0, Branch: "lf/story-001"}}, implementClock))
	row, err := f.db.GetImplementStory(ctx, implementTaskParent, 1, "story-001")
	mustProgress(t, err)
	_, err = f.db.TransitionImplementStory(ctx, store.ImplementTransition{Parent: implementTaskParent, Generation: 1, Key: "story-001", From: store.ImplementWaiting, To: store.ImplementRunning, ExpectedRevision: row.Revision, At: implementClock})
	mustProgress(t, err)
	mustProgress(t, f.db.SetImplementWorkerSession(ctx, implementTaskParent, 1, "story-001", f.session.ID, implementClock))
}

func implementWorkerRow(t *testing.T, f *fixture) store.ImplementStory {
	t.Helper()
	row, err := f.db.GetImplementStory(t.Context(), implementTaskParent, 1, "story-001")
	mustProgress(t, err)
	return row
}

func TestImplementWorkerCompletedIsCommittedPushedAndOnlyThenDone(t *testing.T) {
	var prompt string
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		prompt = req.Prompt
		mustProgress(t, os.WriteFile(filepath.Join(req.WorktreePath, "health.go"), []byte("package health\n"), 0o600))
		submitLiveResult(t, req, implementCompletedJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, _, req := assembledImplementFixture(t, runner)
	seedImplementStory(t, f)
	out, err := Execute(t.Context(), f.opts, req)
	if err != nil || !out.Succeeded() || !out.ResearchGraph {
		t.Fatalf("%+v %v", out, err)
	}
	row := implementWorkerRow(t, f)
	if row.State != store.ImplementDone || row.Attempts != 0 {
		t.Fatalf("an accepted completed answer saved: %+v", row)
	}
	if !out.Pushed || out.CommitSHA == "" {
		t.Fatalf("the story branch was committed and pushed before the ledger moved: %+v", out)
	}
	if remote := f.repo.Git("ls-remote", "origin", "refs/heads/"+out.Branch); !strings.Contains(remote, out.CommitSHA) && !strings.Contains(remote, out.SHAAfter) {
		t.Fatalf("the remote branch %q does not hold the commit %s: %q", out.Branch, out.CommitSHA, remote)
	}
	for _, want := range []string{"Health endpoint", "As an operator I want a health endpoint.", `"attempt":1`} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the worker was not given %q", want)
		}
	}
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
	mustProgress(t, err)
	if instance.Status != store.GraphSucceeded || instance.CurrentNode != "worker_done" {
		t.Fatalf("%+v", instance)
	}
}

func TestImplementWorkerBlockedAndIncompleteParkAtOnceWithTheirOwnReason(t *testing.T) {
	for name, tc := range map[string]struct{ json, reason string }{
		"blocked":    {implementBlockedJSON, "payment API key"},
		"incomplete": {implementIncompleteJSON, "second endpoint"},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
				calls++
				submitLiveResult(t, req, tc.json)
				return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
			})
			f, _, req := assembledImplementFixture(t, runner)
			seedImplementStory(t, f)
			if _, err := Execute(t.Context(), f.opts, req); err != nil {
				t.Fatal(err)
			}
			row := implementWorkerRow(t, f)
			if calls != 1 || row.State != store.ImplementParked || row.ParkOrigin != store.ImplementOriginWorker || !strings.Contains(row.Reason, tc.reason) || row.Attempts != 0 {
				t.Fatalf("an honest %s answer parks at once without repairs: calls=%d %+v", name, calls, row)
			}
		})
	}
}

func TestImplementWorkerRepairsTwiceThenParksOnTheThirdFailure(t *testing.T) {
	var prompts []string
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		prompts = append(prompts, req.Prompt)
		return &agent.RunResult{Outcome: agent.OutcomeFailed, Reason: "the agent crashed", FinishedAt: time.Now()}, nil
	})
	f, _, req := assembledImplementFixture(t, runner)
	seedImplementStory(t, f)
	// Attempt 1 fails and the graph parks for the repair; each owed stage is
	// one dispatch, as the daemon runs them.
	if _, err := Execute(t.Context(), f.opts, req); err != nil {
		t.Fatal(err)
	}
	for n := 2; n <= store.MaxImplementAttempts; n++ {
		dispatch, ok, err := f.db.NextFreshGraphDispatch(t.Context(), f.session.ID)
		if err != nil || !ok {
			t.Fatalf("attempt %d is owed a dispatch: %v %v", n, ok, err)
		}
		reserved, ok, err := f.db.ReserveGraphDispatch(t.Context(), dispatch.ID, dispatch.DueAt)
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
		repair := req
		repair.Trigger.CommentID, repair.Trigger.Body = reserved.TriggerID(), "Continue the workflow at its worker_agent stage."
		if _, err := Execute(t.Context(), f.opts, repair); err != nil {
			t.Fatal(err)
		}
	}
	if len(prompts) != 3 {
		t.Fatalf("the first attempt and two repairs ran: %d", len(prompts))
	}
	for i, prompt := range prompts[1:] {
		if !strings.Contains(prompt, "previous_failure") || !strings.Contains(prompt, "the agent crashed") || !strings.Contains(prompt, `"attempt":`+string(rune('2'+i))) {
			t.Errorf("repair %d does not carry the earlier failure:\n%s", i+1, prompt[max(0, len(prompt)-1500):])
		}
	}
	row := implementWorkerRow(t, f)
	if row.State != store.ImplementParked || row.Attempts != store.MaxImplementAttempts || !strings.Contains(row.Reason, "the agent crashed") {
		t.Fatalf("the third failure parks the story with a plain reason: %+v", row)
	}
}

func TestImplementAdmissionLeavesOtherWorkOnTheLegacyPath(t *testing.T) {
	f, _, req := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Fatal("no agent runs at admission")
		return nil, nil
	}))
	for name, r := range map[string]Request{"another mode": {Mode: agent.ModePlan}, "bug": {Workflow: agent.WorkflowBug}} {
		if _, handled, err := admitImplementExecution(t.Context(), f.opts, r, Result{}); err != nil || handled {
			t.Fatalf("%s handled=%v err=%v", name, handled, err)
		}
	}
	// An Implement task with no managed stories and no story ledger is not a graph job.
	ref, err := naming.ParseRef(f.session.TaskRef)
	mustProgress(t, err)
	result := Result{Ref: ref}
	if _, handled, err := admitImplementExecution(t.Context(), f.opts, req, result); err != nil || handled {
		t.Fatalf("an unmanaged Implement task keeps the legacy path: handled=%v err=%v", handled, err)
	}
}

func TestImplementAdmissionRefusesAGraphContinuation(t *testing.T) {
	f, _, req := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Fatal("no agent runs at admission")
		return nil, nil
	}))
	seedImplementStory(t, f)
	ref, err := naming.ParseRef(f.session.TaskRef)
	mustProgress(t, err)
	result := Result{Ref: ref, SessionID: f.session.ID}
	req.Continuation = &store.Continuation{}
	if _, handled, err := admitImplementExecution(t.Context(), f.opts, req, result); err != nil || handled {
		t.Fatalf("a continuation of a run with no graph history stays on the legacy path: handled=%v err=%v", handled, err)
	}
}

// seedImplementParent approves a managed feature for the fixture's own task and
// gives it its feature branch, so the task is the parent the coordinator runs on.
func seedImplementParent(t *testing.T, f *fixture, stories ...store.ManagedStory) string {
	t.Helper()
	ctx := t.Context()
	parent := f.session.TaskRef
	mustProgress(t, f.db.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: parent, Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}))
	var revisions []store.ApprovedStoryRevision
	for _, story := range stories {
		story.ParentTask, story.Generation, story.State = parent, 1, "planning"
		mustProgress(t, f.db.PutManagedStory(ctx, story))
		revisions = append(revisions, store.ApprovedStoryRevision{ManagedStory: story, ParentID: 9, Title: "Title of " + story.Key, Body: "Body of " + story.Key, Hash: strings.Repeat("cd", 32), FetchedAt: implementClock})
	}
	mustProgress(t, f.db.SetManagedFeatureReady(ctx, parent, 1, true))
	mustProgress(t, f.db.ApproveManagedStories(ctx, parent, 1, revisions))
	for _, story := range stories {
		mustProgress(t, f.db.DeliverStoryScopeNotice(ctx, parent, 1, story.Key, "freeze"))
	}
	mustProgress(t, f.db.ActivateManagedScope(ctx, parent, 1))
	_, err := f.db.FreezeSessionSlug(ctx, f.session.ID, "feature", "lf/123-feature")
	mustProgress(t, err)
	return parent
}

func implementStatesOf(t *testing.T, f *fixture, parent string) map[string]store.ImplementStory {
	t.Helper()
	rows, err := f.db.ListImplementStories(t.Context(), parent, 1)
	mustProgress(t, err)
	out := map[string]store.ImplementStory{}
	for _, r := range rows {
		out[r.Key] = r
	}
	return out
}

func TestImplementCoordinatorStartsStoriesRestsAsksOnceAndResumesOnTheReply(t *testing.T) {
	f, forge, req := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Error("the coordinator never writes code")
		return nil, nil
	}))
	parent := seedImplementParent(t, f,
		store.ManagedStory{Key: "story-001", IssueNumber: 201, IssueID: 7001},
		store.ManagedStory{Key: "story-002", IssueNumber: 202, IssueID: 7002},
		store.ManagedStory{Key: "story-003", IssueNumber: 203, IssueID: 7003, Dependencies: []string{"story-001"}})

	// First run: merge finds nothing, ready starts the two independent stories,
	// and the coordinator rests while they build.
	out, err := Execute(t.Context(), f.opts, req)
	if err != nil || !out.ResearchGraph {
		t.Fatalf("%+v %v", out, err)
	}
	rows := implementStatesOf(t, f, parent)
	if rows["story-001"].State != store.ImplementRunning || rows["story-002"].State != store.ImplementRunning || rows["story-003"].State != store.ImplementWaiting {
		t.Fatalf("only the stories that are ready start: %+v", rows)
	}
	if rows["story-001"].Branch == "" || !strings.HasPrefix(rows["story-001"].Branch, "lf/201-") {
		t.Fatalf("a story builds on the branch of its own task: %q", rows["story-001"].Branch)
	}
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
	mustProgress(t, err)
	if instance.Status != store.GraphRunning || instance.CurrentNode != "await" {
		t.Fatalf("the coordinator rests at await: %+v", instance)
	}
	if _, ok, err := f.db.NextFreshGraphDispatch(t.Context(), f.session.ID); err != nil || ok {
		t.Fatalf("it holds no run slot and is not owed a run yet: %v %v", ok, err)
	}

	// Both workers park. Their wakes leave exactly one dispatch.
	for _, key := range []string{"story-001", "story-002"} {
		row := rows[key]
		if _, err := f.db.TransitionImplementStory(t.Context(), store.ImplementTransition{Parent: parent, Generation: 1, Key: key, From: store.ImplementRunning, To: store.ImplementParked, ExpectedRevision: row.Revision, Reason: "The worker stopped with a blocked answer: " + key + " needs a decision.", At: implementClock}); err != nil {
			t.Fatal(err)
		}
		mustProgress(t, wakeImplementParent(t.Context(), f.db, parent, time.Now()))
	}
	pending, err := f.db.ListDueGraphDispatches(t.Context(), time.Now().Add(time.Hour), 50)
	mustProgress(t, err)
	open := 0
	for _, d := range pending {
		if d.InstanceID == instance.ID {
			open++
		}
	}
	if open != 1 {
		t.Fatalf("a story change queues exactly one parent dispatch, however many wakes occur: %d", open)
	}

	// Second run, from the dispatch: nothing is left to start, so the hand-off is
	// written and the owner gets exactly one question that lists every parked story.
	dispatch, ok, err := f.db.NextFreshGraphDispatch(t.Context(), f.session.ID)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	reserved, ok, err := f.db.ReserveGraphDispatch(t.Context(), dispatch.ID, dispatch.DueAt)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	resume := req
	resume.Trigger.CommentID, resume.Trigger.Body = reserved.TriggerID(), "Continue the workflow at its await stage."
	if out, err = Execute(t.Context(), f.opts, resume); err != nil {
		t.Fatalf("%+v %v", out, err)
	}
	wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	if err != nil || wait.State != "waiting" {
		t.Fatalf("the owner has one open question: %+v %v", wait, err)
	}
	delivery, err := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
	mustProgress(t, err)
	var question string
	for _, post := range forge.all() {
		if strings.Contains(post.body, "need your decision") {
			question = post.body
		}
	}
	if question == "" || !strings.Contains(question, "story-001") || !strings.Contains(question, "story-002") || strings.Contains(question, "story-003") {
		t.Fatalf("one question lists every parked story with its key and reason:\n%s", question)
	}
	questions := 0
	for _, post := range forge.all() {
		if strings.Contains(post.body, "need your decision") {
			questions++
		}
	}
	if questions != 1 {
		t.Fatalf("exactly one question: %d", questions)
	}

	// The owner answers one story by key.
	reply := "story-002: please use the v2 payment API."
	owner := int64(77)
	_, err = f.db.ClaimGraphOwnerReply(t.Context(), store.GraphOwnerReply{InstanceID: instance.ID, OwnerID: owner, AuthorID: owner, CommentID: delivery.CommentID + 100, QuestionCommentID: delivery.CommentID, TaskRef: parent, SourceRef: parent, Mode: "implement", Workflow: "implement", DeliveryKey: wait.DeliveryKey, Body: reply, At: time.Now()})
	mustProgress(t, err)
	answer := req
	answer.Trigger.CommentID, answer.Trigger.Body = delivery.CommentID+100, reply
	if out, err = Execute(t.Context(), f.opts, answer); err != nil {
		t.Fatalf("%+v %v", out, err)
	}
	rows = implementStatesOf(t, f, parent)
	if got := rows["story-002"]; got.State != store.ImplementRunning || got.Attempts != 0 || !strings.Contains(got.Guidance, "v2 payment API") || got.Branch != rows["story-002"].Branch {
		t.Fatalf("the named story restarted through ready with the reply as guidance: %+v", got)
	}
	if got := rows["story-001"]; got.State != store.ImplementParked {
		t.Fatalf("a story the reply does not name stays parked: %+v", got)
	}
	instance, err = f.db.GetGraphInstance(t.Context(), instance.ID)
	mustProgress(t, err)
	if instance.Status != store.GraphRunning || instance.CurrentNode != "await" {
		t.Fatalf("the coordinator rests again while the restarted story builds: %+v", instance)
	}
}

// A story issue planned as its own task (#475) keeps its own Plan row in
// planning. The worker is admitted by the parent's approval, never by that row.
func TestImplementWorkerIgnoresTheStoryIssuesOwnPlanRow(t *testing.T) {
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		mustProgress(t, os.WriteFile(filepath.Join(req.WorktreePath, "health.go"), []byte("package health\n"), 0o600))
		submitLiveResult(t, req, implementCompletedJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, _, req := assembledImplementFixture(t, runner)
	seedImplementStory(t, f)
	mustProgress(t, f.db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: f.session.TaskRef, Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}))
	out, err := Execute(t.Context(), f.opts, req)
	if err != nil || !out.Succeeded() {
		t.Fatalf("the worker was refused by its story issue's own Plan row: %+v %v", out, err)
	}
	if row := implementWorkerRow(t, f); row.State != store.ImplementDone {
		t.Fatalf("%+v", row)
	}
}

// A story branch cut before a dependency was merged (here: created from the base
// branch during Plan) is brought up to date with its parent's branch, where
// finished stories are merged, before the worker runs.
func TestImplementWorkerSyncsWithItsParentsBranch(t *testing.T) {
	var sawDependency bool
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		_, err := os.Stat(filepath.Join(req.WorktreePath, "dependency.go"))
		sawDependency = err == nil
		submitLiveResult(t, req, implementCompletedJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, _, req := assembledImplementFixture(t, runner)
	seedImplementStory(t, f)
	f.opts.AutoSyncBase = true
	ctx := t.Context()

	// The story's own branch already exists on origin, cut from the base branch.
	f.repo.Git("push", "--quiet", "origin", "HEAD:refs/heads/"+testBranch)
	_, err := f.db.FreezeSessionSlug(ctx, f.session.ID, testSlug, testBranch)
	mustProgress(t, err)

	// The parent's branch carries a story merged after that.
	parent := f.otherSession(900)
	const parentBranch = "lf/900-feature"
	_, err = f.db.FreezeSessionSlug(ctx, parent.ID, "feature", parentBranch)
	mustProgress(t, err)
	f.repo.Git("checkout", "--quiet", "-b", parentBranch)
	f.repo.WriteFile("dependency.go", "package api // merged story\n")
	f.repo.Commit("merge story-000")
	f.repo.Git("push", "--quiet", "origin", parentBranch)
	f.repo.Git("checkout", "--quiet", "-")

	out, err := Execute(ctx, f.opts, req)
	if err != nil || !out.Succeeded() {
		t.Fatalf("%+v %v", out, err)
	}
	if !sawDependency {
		t.Fatal("the worker ran without the work merged into its parent's branch")
	}
}

func TestImplementWorkerStartupFailureSettlesAllocatedRun(t *testing.T) {
	calls := 0
	f, _, req := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		calls++
		return nil, nil
	}))
	seedImplementStory(t, f)
	// The runtime validates structurally, but cannot open an attempt route.
	mustProgress(t, f.opts.MCP.Authority.Close(t.Context()))
	out, err := Execute(t.Context(), f.opts, req)
	if err == nil || calls != 0 {
		t.Fatalf("startup error=%v runner calls=%d", err, calls)
	}
	instance, readErr := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
	mustProgress(t, readErr)
	row := implementWorkerRow(t, f)
	if instance.Status != store.GraphFailed || row.Attempts != 0 || row.LastFailure == "" {
		t.Fatalf("instance=%+v story=%+v", instance, row)
	}
	run, exists, readErr := f.db.GetRunByTrigger(t.Context(), f.session.ID, req.Trigger.CommentID)
	mustProgress(t, readErr)
	if !exists || run.Status != store.RunFailed || run.Error == "" || out.Run.ID != run.ID {
		t.Fatalf("allocated run was not settled: %+v out=%+v", run, out)
	}
	row, readErr = f.db.TransitionImplementStory(t.Context(), store.ImplementTransition{Parent: row.Parent, Generation: row.Generation, Key: row.Key, From: store.ImplementRunning, To: store.ImplementParked, ExpectedRevision: row.Revision, Reason: row.LastFailure, At: time.Now()})
	mustProgress(t, readErr)
	candidate := implementQuestionCandidate([]store.ImplementStory{row})
	if len(candidate.Questions) != 1 || !strings.Contains(candidate.Questions[0].Text, row.Key) || !strings.Contains(candidate.Questions[0].Text, sentence(row.LastFailure)) {
		t.Fatalf("parked reason absent from coordinator: %+v", candidate)
	}
}

func TestImplementWorkerCancelledPreparationKeepsOwedInstance(t *testing.T) {
	f, _, req := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Error("cancelled preparation launched a runner")
		return nil, nil
	}))
	seedImplementStory(t, f)
	f.opts.CloneGate = cancelledImplementGate{}
	_, err := Execute(t.Context(), f.opts, req)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
	instance, readErr := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
	mustProgress(t, readErr)
	row := implementWorkerRow(t, f)
	if instance.Status != store.GraphRunning || row.LastFailure != "" || row.Attempts != 0 {
		t.Fatalf("cancellation spent infrastructure budget: %+v %+v", instance, row)
	}
}

type cancelledImplementGate struct{}

func (cancelledImplementGate) Acquire(context.Context, string) (func(), error) {
	return nil, context.Canceled
}

func TestImplementWorkerPrepareFailureSettlesPrelaunchRun(t *testing.T) {
	calls := 0
	f, _, req := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		calls++
		return nil, nil
	}))
	seedImplementStory(t, f)
	// Request admission accepts nonempty text; prompt assembly rejects blank
	// instructions after allocation, before prepare returns its preparation.
	req.Command.Text = " "
	out, err := Execute(t.Context(), f.opts, req)
	if err == nil || !strings.Contains(err.Error(), "instruction is empty") || calls != 0 {
		t.Fatalf("error=%v calls=%d", err, calls)
	}
	run, exists, readErr := f.db.GetRunByTrigger(t.Context(), f.session.ID, req.Trigger.CommentID)
	mustProgress(t, readErr)
	instance, readErr := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
	mustProgress(t, readErr)
	row := implementWorkerRow(t, f)
	if !exists || run.Status != store.RunFailed || out.Run.ID != run.ID || instance.Status != store.GraphFailed || row.Attempts != 0 || !strings.Contains(row.LastFailure, "instruction is empty") {
		t.Fatalf("run=%+v instance=%+v story=%+v", run, instance, row)
	}
}

func TestImplementCoordinatorResumesBeforeQuestionWhileSiblingRuns(t *testing.T) {
	for _, reply := range []string{"story-001: use the v2 API", "Try again with the v2 API"} {
		t.Run(reply, func(t *testing.T) {
			f, forge, req := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
				t.Fatal("the coordinator must not launch an agent")
				return nil, nil
			}))
			parent := seedImplementParent(t, f,
				store.ManagedStory{Key: "story-001", IssueNumber: 201, IssueID: 7001},
				store.ManagedStory{Key: "story-002", IssueNumber: 202, IssueID: 7002},
				store.ManagedStory{Key: "story-003", IssueNumber: 203, IssueID: 7003})
			_, err := Execute(t.Context(), f.opts, req)
			mustProgress(t, err)
			rows := implementStatesOf(t, f, parent)
			for _, key := range []string{"story-001", "story-003"} {
				_, err = f.db.RecordImplementFailure(t.Context(), parent, 1, key, "build failed", implementClock)
				mustProgress(t, err)
				row, e := f.db.GetImplementStory(t.Context(), parent, 1, key)
				mustProgress(t, e)
				_, err = f.db.TransitionImplementStory(t.Context(), store.ImplementTransition{Parent: parent, Generation: 1, Key: key, From: store.ImplementRunning, To: store.ImplementParked, ExpectedRevision: row.Revision, Reason: "needs guidance", At: implementClock})
				mustProgress(t, err)
			}
			answer := req
			answer.Trigger.CommentID++
			answer.Trigger.Body = reply
			answer.Command = ParseCommand(reply)
			_, err = Execute(t.Context(), f.opts, answer)
			mustProgress(t, err)
			after := implementStatesOf(t, f, parent)
			got := after["story-001"]
			if got.State != store.ImplementRunning || got.Attempts != 0 || got.Guidance != reply || got.Branch != rows["story-001"].Branch || got.WorkerSession != rows["story-001"].WorkerSession {
				t.Fatalf("parked story did not restart with retained work: %+v", got)
			}
			if got := after["story-002"]; got.Revision != rows["story-002"].Revision || got.State != store.ImplementRunning {
				t.Fatalf("running sibling changed: %+v", got)
			}
			want := store.ImplementParked
			if !strings.Contains(reply, "story-001") {
				want = store.ImplementRunning
			}
			if after["story-003"].State != want {
				t.Fatalf("selection changed: %+v", after["story-003"])
			}
			instance, e := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
			mustProgress(t, e)
			if _, e = f.db.GetGraphOwnerWait(t.Context(), instance.ID); !errors.Is(e, store.ErrNotFound) {
				t.Fatalf("early reply required a question: %v", e)
			}
			for _, post := range forge.all() {
				if strings.Contains(post.body, "need your decision") {
					t.Fatal("unexpected owner question")
				}
			}
			// A consumed successful input cannot reset a subsequently parked story.
			_, err = f.db.AdvanceWatermark(t.Context(), f.session.ID, answer.Trigger.CommentID)
			mustProgress(t, err)
			_, err = f.db.TransitionImplementStory(t.Context(), store.ImplementTransition{Parent: parent, Generation: 1, Key: got.Key, From: store.ImplementRunning, To: store.ImplementParked, ExpectedRevision: got.Revision, Reason: "another failure", At: implementClock})
			mustProgress(t, err)
			_, err = Execute(t.Context(), f.opts, answer)
			mustProgress(t, err)
			if got := implementStatesOf(t, f, parent)["story-001"]; got.State != store.ImplementParked {
				t.Fatalf("replay restarted the story: %+v", got)
			}
		})
	}
}

func TestImplementEarlyReplyRetriesAfterGuidanceWasApplied(t *testing.T) {
	f, _, req := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Fatal("the coordinator must not launch an agent")
		return nil, nil
	}))
	parent := seedImplementParent(t, f,
		store.ManagedStory{Key: "story-001", IssueNumber: 201, IssueID: 7001},
		store.ManagedStory{Key: "story-002", IssueNumber: 202, IssueID: 7002})
	_, err := Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	row := implementStatesOf(t, f, parent)["story-001"]
	_, err = f.db.TransitionImplementStory(t.Context(), store.ImplementTransition{Parent: parent, Generation: 1, Key: row.Key, From: store.ImplementRunning, To: store.ImplementParked, ExpectedRevision: row.Revision, Reason: "needs guidance", At: implementClock})
	mustProgress(t, err)
	req.Trigger.CommentID++
	req.Trigger.Body = "story-001: retry with the v2 API"
	req.Command = ParseCommand(req.Trigger.Body)
	redactor := f.opts.MCP.Redactor
	f.opts.MCP.Redactor = nil // Fail coordinator binding after applying guidance.
	_, err = Execute(t.Context(), f.opts, req)
	if err == nil || errors.Is(err, ErrImplementReplyNotApplied) {
		t.Fatalf("expected an execution failure: %v", err)
	}
	row = implementStatesOf(t, f, parent)["story-001"]
	if row.State != store.ImplementWaiting || row.Guidance != req.Trigger.Body {
		t.Fatalf("failure did not occur after guidance application: %+v", row)
	}
	f.opts.MCP.Redactor = redactor
	_, err = Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	row = implementStatesOf(t, f, parent)["story-001"]
	if row.State != store.ImplementRunning || row.Guidance != req.Trigger.Body {
		t.Fatalf("retry failed to schedule applied guidance: %+v", row)
	}
	// Also replay a successful runless coordinator input before consumption.
	_, err = Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	if got := implementStatesOf(t, f, parent)["story-001"]; got.Revision != row.Revision {
		t.Fatalf("successful reply was applied twice: %+v", got)
	}
}

func TestImplementUnusedReplyRemainsARefusalBeforeDeliveryIsRecorded(t *testing.T) {
	f, _, req := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Fatal("the coordinator must not launch an agent")
		return nil, nil
	}))
	parent := seedImplementParent(t, f,
		store.ManagedStory{Key: "story-001", IssueNumber: 201, IssueID: 7001},
		store.ManagedStory{Key: "story-002", IssueNumber: 202, IssueID: 7002})
	_, err := Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	row := implementStatesOf(t, f, parent)["story-001"]
	_, err = f.db.TransitionImplementStory(t.Context(), store.ImplementTransition{Parent: parent, Generation: 1, Key: row.Key, From: store.ImplementRunning, To: store.ImplementParked, ExpectedRevision: row.Revision, Reason: "needs guidance", At: implementClock})
	mustProgress(t, err)
	req.Trigger.CommentID++
	req.Trigger.Body = "story-002: retry the running story"
	req.Command = ParseCommand(req.Trigger.Body)
	for range 2 {
		_, err = Execute(t.Context(), f.opts, req)
		if !errors.Is(err, ErrImplementReplyNotApplied) {
			t.Fatalf("unused input lost its refusal on retry: %v", err)
		}
		if exists, e := f.db.HasWorkflowLaunch(t.Context(), f.session.ID, req.Trigger.CommentID); e != nil || exists {
			t.Fatalf("unused input acquired a launch: %v %v", exists, e)
		}
		if got := implementStatesOf(t, f, parent)["story-001"]; got.State != store.ImplementParked {
			t.Fatalf("unused reply changed a story: %+v", got)
		}
	}
}

func TestImplementWorkerPublishesFullRetainedOwnerQuestion(t *testing.T) {
	reason := strings.Repeat("Retained reason ", 130) + testBotToken + strings.Repeat(" remaining reason ", 30) + "OWNER_QUESTION_SUFFIX"
	var raw agentresult.ResearchNode
	mustProgress(t, json.Unmarshal([]byte(implementBlockedJSON), &raw))
	raw.Result.Reason = reason
	raw.Result.Comment = "Built what could be built. First complete question? Second complete question?"
	encoded, err := json.Marshal(raw)
	mustProgress(t, err)
	f, forge, req := assembledImplementFixture(t, lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		submitLiveResult(t, req, string(encoded))
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, FinishedAt: time.Now()}, nil
	}))
	seedImplementStory(t, f)
	_, err = Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	row := implementWorkerRow(t, f)
	if row.State != store.ImplementParked || strings.Contains(row.Reason, "OWNER_QUESTION_SUFFIX") {
		t.Fatal("fixture did not cross the ledger clipping boundary")
	}
	posts := forge.all()
	if len(posts) != 1 {
		t.Fatalf("got %d comments", len(posts))
	}
	body := html.UnescapeString(posts[0].body)
	safeReason, err := f.opts.ResearchReporter.renderer.RenderReportDelivery(t.Context(), reason, "")
	mustProgress(t, err)
	if strings.Contains(body, testBotToken) || strings.Contains(row.Reason, "ghp_") {
		t.Fatal("clipped reason leaked a credential")
	}
	for _, want := range []string{safeReason.String(), "Built what could be built.", "First complete question?", "Second complete question?"} {
		if !strings.Contains(body, want) {
			t.Fatalf("full retained comment lacks %q", want)
		}
	}
	parent, err := naming.ParseRef(row.Parent)
	mustProgress(t, err)
	a := ImplementAuthority{Parent: parent, Generation: 1, Stories: []ImplementStory{{Key: row.Key, Name: "Health endpoint", IssueNumber: f.session.IssueNumber}}}
	notes, err := implementParkNotes(t.Context(), f.db, a, []store.ImplementStory{row})
	mustProgress(t, err)
	table, _ := renderImplementHandoffNotes(a, []store.ImplementStory{row}, nil, notes)
	if !strings.Contains(table, "| Blocked |") || !strings.Contains(table, forgePostURL(posts[0])) {
		t.Fatal(table)
	}
	if forge.attemptCount() != 1 {
		t.Fatal("reading status posted another comment")
	}
}
