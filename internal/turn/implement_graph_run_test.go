package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
	"github.com/cloudvane/litefactory/internal/teamsource"
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
	// Existing runtime regressions exercise supported pinned worker graphs,
	// including their automatic preparation and conflict-resolution behavior.
	f.opts.WorkflowSource = func(ctx context.Context, repo store.Repo, workflow agent.Workflow, specs graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		if workflow != agent.WorkflowImplement {
			return fixtureWorkflowSource(ctx, repo, workflow, specs)
		}
		return fixtureInstalled(teamsource.CaptureWorkflow(ctx, os.DirFS(filepath.Join("..", "..", "teams", "go-development", "v1.0.2", "implement")), "implement.dot", "go-development", agent.ModeImplement, specs))
	}
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

// enableWorkerSyncFixture selects the editable canonical gate rather than the
// released graph used by compatibility fixtures.
func enableWorkerSyncFixture(f *fixture) {
	f.opts.WorkflowSource = func(ctx context.Context, repo store.Repo, workflow agent.Workflow, specs graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		if workflow != agent.WorkflowImplement {
			return fixtureWorkflowSource(ctx, repo, workflow, specs)
		}
		return fixtureInstalled(teamsource.CaptureImplement(ctx, specs))
	}
}

// Earlier captured worker gates retain their strict conflict routing.
func enableStrictWorkerSyncFixture(t *testing.T, f *fixture) {
	t.Helper()
	source := os.DirFS(filepath.Join("..", "teamsource", "implement"))
	files := fstest.MapFS{}
	mustProgress(t, fs.WalkDir(source, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := fs.ReadFile(source, name)
		if err != nil {
			return err
		}
		if name == "implement.dot" {
			shape := strings.ReplaceAll(string(raw), ",retry,deferred", ",retry")
			shape = strings.ReplaceAll(shape, ` sync_start          -> merge               [lf_on="deferred"];`, "")
			shape = strings.ReplaceAll(shape, ` sync_before_worker -> worker_agent [lf_on="deferred"];`, "")
			raw = []byte(shape)
		}
		files[name] = &fstest.MapFile{Data: raw, Mode: 0644}
		return nil
	}))
	f.opts.WorkflowSource = func(ctx context.Context, repo store.Repo, workflow agent.Workflow, specs graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		if workflow != agent.WorkflowImplement {
			return fixtureWorkflowSource(ctx, repo, workflow, specs)
		}
		return fixtureInstalled(teamsource.CaptureWorkflow(ctx, files, "implement.dot", "go-development", agent.ModeImplement, specs))
	}
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

// implementWorkerResultOutcomes lists the outcomes worker_result returned in
// the story's latest Implement instance, in order.
func implementWorkerResultOutcomes(t *testing.T, f *fixture) []string {
	t.Helper()
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
	mustProgress(t, err)
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
	mustProgress(t, err)
	var outcomes []string
	for _, a := range attempts {
		if a.Node == "worker_result" {
			outcomes = append(outcomes, a.Outcome)
		}
	}
	return outcomes
}

// runOwedImplementStages runs every owed graph dispatch of the worker, one per
// Execute, as the daemon does, and returns how many ran.
func runOwedImplementStages(t *testing.T, f *fixture, req Request) int {
	t.Helper()
	ran := 0
	for ; ran < 10; ran++ {
		dispatch, ok, err := f.db.NextFreshGraphDispatch(t.Context(), f.session.ID)
		mustProgress(t, err)
		if !ok {
			return ran
		}
		reserved, ok, err := f.db.ReserveGraphDispatch(t.Context(), dispatch.ID, dispatch.DueAt)
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
		next := req
		next.Trigger.CommentID, next.Trigger.Body = reserved.TriggerID(), "Continue the workflow."
		if _, err := Execute(t.Context(), f.opts, next); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("the worker kept owing dispatches")
	return ran
}

func TestImplementWorkerBlockedParksAtOnceWithItsOwnReason(t *testing.T) {
	calls := 0
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		submitLiveResult(t, req, implementBlockedJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, _, req := assembledImplementFixture(t, runner)
	seedImplementStory(t, f)
	if _, err := Execute(t.Context(), f.opts, req); err != nil {
		t.Fatal(err)
	}
	runOwedImplementStages(t, f, req)
	row := implementWorkerRow(t, f)
	if calls != 1 || row.State != store.ImplementParked || row.ParkOrigin != store.ImplementOriginWorker || !strings.Contains(row.Reason, "payment API key") || row.Attempts != 0 {
		t.Fatalf("an honest blocked answer parks at once without repairs: calls=%d %+v", calls, row)
	}
	if got := implementWorkerResultOutcomes(t, f); len(got) != 1 || got[0] != "blocked" {
		t.Fatalf("worker_result outcomes = %v, want [blocked]", got)
	}
}

func TestImplementWorkerIncompleteRetriesWithItsReasonThenParksWithItsOwnWords(t *testing.T) {
	var prompts []string
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		prompts = append(prompts, req.Prompt)
		submitLiveResult(t, req, strings.Replace(implementIncompleteJSON, "The second endpoint is not done.", fmt.Sprintf("Attempt %d left the second endpoint undone.", len(prompts)), 1))
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, _, req := assembledImplementFixture(t, runner)
	seedImplementStory(t, f)
	if _, err := Execute(t.Context(), f.opts, req); err != nil {
		t.Fatal(err)
	}
	row := implementWorkerRow(t, f)
	if row.State != store.ImplementRunning || row.Attempts != 1 || !strings.Contains(row.LastFailure, "Attempt 1 left the second endpoint undone.") {
		t.Fatalf("an incomplete answer counts one attempt with its reason: %+v", row)
	}
	runOwedImplementStages(t, f, req)
	if len(prompts) != store.MaxImplementAttempts {
		t.Fatalf("the first attempt and two repairs ran: %d", len(prompts))
	}
	for i, prompt := range prompts[1:] {
		want := fmt.Sprintf("Attempt %d left the second endpoint undone.", i+1)
		if !strings.Contains(prompt, "previous_failure") || !strings.Contains(prompt, want) || !strings.Contains(prompt, fmt.Sprintf(`"attempt":%d`, i+2)) {
			t.Errorf("repair %d does not carry the earlier worker's reason %q:\n%s", i+1, want, prompt[max(0, len(prompt)-1500):])
		}
	}
	if got := implementWorkerResultOutcomes(t, f); len(got) != 3 || got[0] != "incomplete" || got[2] != "incomplete" {
		t.Fatalf("worker_result outcomes = %v, want three incomplete", got)
	}
	row = implementWorkerRow(t, f)
	if row.State != store.ImplementParked || row.Attempts != store.MaxImplementAttempts || !strings.Contains(row.Reason, "The worker stopped with an incomplete answer: Attempt 3 left the second endpoint undone.") || strings.Contains(row.Reason, "attempt(s) in a row") {
		t.Fatalf("the spent budget parks with the last worker's own words: %+v", row)
	}
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
	mustProgress(t, err)
	if instance.Status != store.GraphSucceeded || instance.CurrentNode != "worker_parked" {
		t.Fatalf("three attempts and the park fit within the node limits: %+v", instance)
	}
}

// releasedStoppedImplement returns the catalogue's go-development implement
// package with its graph in the earlier completed,stopped result shape, which
// running instances captured from older releases still use.
func releasedStoppedImplement(t *testing.T) fstest.MapFS {
	t.Helper()
	var download string
	for _, d := range catalogueDownloads(t) {
		if strings.HasPrefix(d, "go-development/") {
			download = d
		}
	}
	if download == "" {
		t.Fatal("the catalogue ships no go-development release")
	}
	dir := os.DirFS(filepath.Join("..", "..", "teams", filepath.FromSlash(download), "implement"))
	files := fstest.MapFS{}
	err := fs.WalkDir(dir, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := fs.ReadFile(dir, name)
		if err != nil {
			return err
		}
		files[name] = &fstest.MapFile{Data: raw, Mode: 0o644}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	graph, ok := files["implement.dot"]
	if !ok {
		t.Fatal("the released implement package has no graph")
	}
	shape := string(graph.Data)
	if !strings.Contains(shape, `lf_outcomes="completed,stopped"`) {
		shape = strings.Replace(shape, `lf_outcomes="completed,incomplete,blocked"`, `lf_outcomes="completed,stopped"`, 1)
		shape = regexp.MustCompile(`(?m)^[ \t]*worker_result[ \t]*->[ \t]*budget[ \t]*\[lf_on="incomplete"\];\n`).ReplaceAllString(shape, "")
		shape = regexp.MustCompile(`(worker_result[ \t]*->[ \t]*park_story[ \t]*)\[lf_on="blocked"\]`).ReplaceAllString(shape, `${1}[lf_on="stopped"]`)
	}
	if !strings.Contains(shape, `lf_outcomes="completed,stopped"`) || !strings.Contains(shape, `[lf_on="stopped"]`) || strings.Contains(shape, `[lf_on="incomplete"]`) {
		t.Fatal("the released implement graph has no derivable stopped shape")
	}
	graph.Data = []byte(shape)
	return files
}

// TestImplementReleasedGraphStillParksStoppedAtOnce runs the catalogue's released
// implement package in the earlier completed,stopped shape: incomplete and
// blocked answers both return stopped and park at once without counting an attempt.
func TestImplementReleasedGraphStillParksStoppedAtOnce(t *testing.T) {
	released := releasedStoppedImplement(t)
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
			f.opts.WorkflowSource = func(ctx context.Context, _ store.Repo, _ agent.Workflow, specs graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
				return fixtureInstalled(teamsource.CaptureWorkflow(ctx, released, "implement.dot", "go-development", agent.ModeImplement, specs))
			}
			seedImplementStory(t, f)
			if _, err := Execute(t.Context(), f.opts, req); err != nil {
				t.Fatal(err)
			}
			runOwedImplementStages(t, f, req)
			row := implementWorkerRow(t, f)
			if calls != 1 || row.State != store.ImplementParked || row.ParkOrigin != store.ImplementOriginWorker || !strings.Contains(row.Reason, tc.reason) || row.Attempts != 0 {
				t.Fatalf("the released graph parks an honest %s answer at once: calls=%d %+v", name, calls, row)
			}
			if got := implementWorkerResultOutcomes(t, f); len(got) != 1 || got[0] != "stopped" {
				t.Fatalf("worker_result outcomes = %v, want [stopped]", got)
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
	// The feature branch exists on the remote, as Plan leaves it: the
	// coordinator's base sync brings main into it before any story starts.
	f.repo.Git("push", "--quiet", "origin", "HEAD:refs/heads/lf/123-feature")
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

const implementConflictBlockedJSON = `{"version":1,"response":"blocked","result":{"outcome":"blocked","comment":"The merge could not be resolved.","reason":"The conflict in shared.go needs a decision on which handler wins.","payload":{"implement":{"stories":[]}}}}`

// seedStoryParentConflict gives the worker's story branch and its parent's
// branch two different edits of shared.go and returns the parent's tip.
func seedStoryParentConflict(t *testing.T, f *fixture) string {
	t.Helper()
	ctx := t.Context()
	f.opts.AutoSyncBase = true
	f.opts.BlockRunOnConflict = true

	f.repo.Git("checkout", "--quiet", "--detach")
	f.repo.WriteFile("shared.go", "package api // story\n")
	f.repo.Commit("story edits shared.go")
	f.repo.Git("push", "--quiet", "origin", "HEAD:refs/heads/"+testBranch)
	f.repo.Git("checkout", "--quiet", "-")
	_, err := f.db.FreezeSessionSlug(ctx, f.session.ID, testSlug, testBranch)
	mustProgress(t, err)

	parent := f.otherSession(900)
	const parentBranch = "lf/900-feature"
	_, err = f.db.FreezeSessionSlug(ctx, parent.ID, "feature", parentBranch)
	mustProgress(t, err)
	f.repo.Git("checkout", "--quiet", "-b", parentBranch)
	f.repo.WriteFile("shared.go", "package api // merged sibling\n")
	f.repo.Commit("merge a sibling story")
	f.repo.Git("push", "--quiet", "origin", parentBranch)
	tip := f.repo.Git("rev-parse", "HEAD")
	f.repo.Git("checkout", "--quiet", "-")
	return tip
}

// A story whose branch conflicts with its parent's branch is not refused: the
// worker starts on the started merge, resolves it, and the save concludes it.
func TestImplementWorkerResolvesAConflictWithItsParentsBranch(t *testing.T) {
	var prompt, mergeHead string
	var f *fixture
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		prompt = req.Prompt
		mergeHead = f.repo.GitIn(req.WorktreePath, "rev-parse", "MERGE_HEAD")
		mustProgress(t, os.WriteFile(filepath.Join(req.WorktreePath, "shared.go"), []byte("package api // story and merged sibling\n"), 0o600))
		submitLiveResult(t, req, implementCompletedJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, _, req := assembledImplementFixture(t, runner)
	seedImplementStory(t, f)
	parentTip := seedStoryParentConflict(t, f)

	out, err := Execute(t.Context(), f.opts, req)
	if err != nil || !out.Succeeded() {
		t.Fatalf("the conflicted story ran instead of being refused: %+v %v", out, err)
	}
	if mergeHead != parentTip {
		t.Fatalf("MERGE_HEAD = %q, want the parent's tip %s", mergeHead, parentTip)
	}
	for _, want := range []string{"## Merge conflict to resolve", `"lf/900-feature"`, `- "shared.go"`, "Do not commit"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the worker prompt lacks %q", want)
		}
	}
	if row := implementWorkerRow(t, f); row.State != store.ImplementDone {
		t.Fatalf("the resolved story is done: %+v", row)
	}
	remote := strings.Fields(f.repo.Git("ls-remote", "origin", "refs/heads/"+testBranch))
	if len(remote) != 2 {
		t.Fatalf("story branch missing on origin: %v", remote)
	}
	f.repo.Git("fetch", "--quiet", "origin")
	parents := strings.Fields(f.repo.Git("rev-list", "--parents", "-n", "1", remote[0]))
	if len(parents) != 3 || parents[2] != parentTip {
		t.Fatalf("the story head is not a merge of the parent's tip: %v", parents)
	}
	if got := f.repo.Git("show", remote[0]+":shared.go"); got != "package api // story and merged sibling" {
		t.Fatalf("shared.go = %q", got)
	}
}

// A story the merge step restarted after a conflict (guidance_question holds
// the merge reason, guidance is empty) gets the conflicting paths in its
// worker prompt: preparation starts the merge of its parent's branch.
func TestImplementWorkerRestartedByTheMergeStepSeesTheConflictInItsPrompt(t *testing.T) {
	var prompt string
	var f *fixture
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		prompt = req.Prompt
		mustProgress(t, os.WriteFile(filepath.Join(req.WorktreePath, "shared.go"), []byte("package api // story and merged sibling\n"), 0o600))
		submitLiveResult(t, req, implementCompletedJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	var req Request
	f, _, req = assembledImplementFixture(t, runner)
	seedImplementStory(t, f)
	ctx := t.Context()
	const reason = "the story conflicts with the work already merged in shared.go; its worker is started again to resolve the conflict"
	row := implementWorkerRow(t, f)
	for _, move := range []struct{ from, to, reason string }{
		{store.ImplementRunning, store.ImplementDone, ""},
		{store.ImplementDone, store.ImplementParked, reason},
		{store.ImplementParked, store.ImplementWaiting, ""},
		{store.ImplementWaiting, store.ImplementRunning, ""},
	} {
		var err error
		row, err = f.db.TransitionImplementStory(ctx, store.ImplementTransition{Parent: implementTaskParent, Generation: 1, Key: "story-001", From: move.from, To: move.to, ExpectedRevision: row.Revision, Reason: move.reason, At: implementClock})
		mustProgress(t, err)
	}
	mustProgress(t, f.db.SetImplementWorkerSession(ctx, implementTaskParent, 1, "story-001", f.session.ID, implementClock))
	if row = implementWorkerRow(t, f); row.GuidanceQuestion != reason || row.Guidance != "" {
		t.Fatalf("seeded row = %+v, want the merge step's restart", row)
	}
	seedStoryParentConflict(t, f)

	out, err := Execute(t.Context(), f.opts, req)
	if err != nil || !out.Succeeded() {
		t.Fatalf("%+v %v", out, err)
	}
	for _, want := range []string{"## Merge conflict to resolve", `- "shared.go"`} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the restarted worker's prompt lacks %q", want)
		}
	}
}

// A worker that cannot resolve the conflict parks the story naming the file;
// the owner's restart starts the worker again on the merge still in progress,
// with the reply as guidance, instead of failing the turn.
func TestImplementWorkerUnresolvedConflictParksAndTheOwnerRestartRuns(t *testing.T) {
	var prompts []string
	var f *fixture
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		prompts = append(prompts, req.Prompt)
		if len(prompts) == 1 {
			submitLiveResult(t, req, implementConflictBlockedJSON)
		} else {
			mustProgress(t, os.WriteFile(filepath.Join(req.WorktreePath, "shared.go"), []byte("package api // the story's handler wins\n"), 0o600))
			submitLiveResult(t, req, implementCompletedJSON)
		}
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, _, req := assembledImplementFixture(t, runner)
	seedImplementStory(t, f)
	parentTip := seedStoryParentConflict(t, f)

	if _, err := Execute(t.Context(), f.opts, req); err != nil {
		t.Fatal(err)
	}
	row := implementWorkerRow(t, f)
	if row.State != store.ImplementParked || row.ParkOrigin != store.ImplementOriginWorker || !strings.Contains(row.Reason, "shared.go") {
		t.Fatalf("the unresolved conflict parks the story naming the file: %+v", row)
	}
	sess, err := f.db.GetSessionByTaskRef(t.Context(), f.session.TaskRef)
	mustProgress(t, err)
	if got := f.repo.GitIn(sess.WorktreePath, "rev-parse", "MERGE_HEAD"); got != parentTip {
		t.Fatalf("the unresolved merge stays in progress, uncommitted: MERGE_HEAD=%q", got)
	}
	if status := f.repo.GitIn(sess.WorktreePath, "status", "--porcelain"); !strings.Contains(status, "shared.go") {
		t.Fatalf("the conflict stays in the worktree: %q", status)
	}

	// The owner's reply resumes it (as resumeStories does) and ready restarts it.
	const reply = "story-001: keep the story's handler."
	row, err = f.db.TransitionImplementStory(t.Context(), store.ImplementTransition{Parent: implementTaskParent, Generation: 1, Key: "story-001", From: store.ImplementParked, To: store.ImplementWaiting, ExpectedRevision: row.Revision, Guidance: reply, At: implementClock})
	mustProgress(t, err)
	_, err = f.db.TransitionImplementStory(t.Context(), store.ImplementTransition{Parent: implementTaskParent, Generation: 1, Key: "story-001", From: store.ImplementWaiting, To: store.ImplementRunning, ExpectedRevision: row.Revision, At: implementClock})
	mustProgress(t, err)
	mustProgress(t, f.db.SetImplementWorkerSession(t.Context(), implementTaskParent, 1, "story-001", f.session.ID, implementClock))

	restart := req
	restart.Trigger.CommentID, restart.Trigger.Body = req.Trigger.CommentID+1000, "Start the story worker."
	out, err := Execute(t.Context(), f.opts, restart)
	if err != nil || !out.Succeeded() {
		t.Fatalf("the restarted worker ran instead of failing the turn: %+v %v", out, err)
	}
	if len(prompts) != 2 {
		t.Fatalf("the worker ran twice: %d", len(prompts))
	}
	for _, want := range []string{"## Merge conflict to resolve", `- "shared.go"`, "keep the story's handler"} {
		if !strings.Contains(prompts[1], want) {
			t.Errorf("the restarted worker prompt lacks %q", want)
		}
	}
	if row := implementWorkerRow(t, f); row.State != store.ImplementDone || row.LastFailure != "" {
		t.Fatalf("the restarted story is done: %+v", row)
	}
	if parents := strings.Fields(f.repo.GitIn(sess.WorktreePath, "rev-list", "--parents", "-n", "1", "HEAD")); len(parents) != 3 || parents[2] != parentTip {
		t.Fatalf("the restarted worker's save concluded the merge: %v", parents)
	}
}

// mergeImplementStories seeds the ledger of the fixture's parent and moves each
// story to merged with its own merge commit, one minute apart in key order.
func mergeImplementStories(t *testing.T, f *fixture, parent string, commits map[string]string, keys ...string) {
	t.Helper()
	ctx := t.Context()
	var seeds []store.ImplementStorySeed
	for i, key := range keys {
		seeds = append(seeds, store.ImplementStorySeed{Key: key, Position: i, Branch: "lf/" + key})
	}
	mustProgress(t, f.db.EnsureImplementStories(ctx, parent, 1, seeds, implementClock))
	for i, key := range keys {
		commits[key] = advanceRemote(t, f, "lf/123-feature", key+".txt", "round one\n")
		f.repo.Git("push", "--quiet", "origin", commits[key]+":refs/heads/lf/"+key)
		at := implementClock.Add(time.Duration(i+1) * time.Minute)
		for _, move := range [][2]string{{store.ImplementWaiting, store.ImplementRunning}, {store.ImplementRunning, store.ImplementDone}, {store.ImplementDone, store.ImplementMerged}} {
			row, err := f.db.GetImplementStory(ctx, parent, 1, key)
			mustProgress(t, err)
			in := store.ImplementTransition{Parent: parent, Generation: 1, Key: key, From: move[0], To: move[1], ExpectedRevision: row.Revision, At: at}
			if move[1] == store.ImplementMerged {
				in.MergeCommit = commits[key]
			}
			_, err = f.db.TransitionImplementStory(ctx, in)
			mustProgress(t, err)
		}
	}
}

func implementCompletionReplies(forge *fakeForge) []string {
	var out []string
	for _, post := range forge.all() {
		if strings.Contains(post.body, "Implement is finished") {
			out = append(out, post.body)
		}
	}
	return out
}

// The last merge of a generation with nothing parked ends in one completion
// reply on the parent; a replay of report_done posts nothing new (#624).
func TestImplementCoordinatorReportsCompletionOnceWhenEveryStoryIsMerged(t *testing.T) {
	f, forge, req := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Error("the coordinator never writes code")
		return nil, nil
	}))
	parent := seedImplementParent(t, f,
		store.ManagedStory{Key: "story-001", IssueNumber: 201, IssueID: 7001},
		store.ManagedStory{Key: "story-002", IssueNumber: 202, IssueID: 7002})
	commits := map[string]string{"story-001": "1111111111aaaaaaaaaa", "story-002": "2222222222bbbbbbbbbb"}
	mergeImplementStories(t, f, parent, commits, "story-001", "story-002")

	if out, err := Execute(t.Context(), f.opts, req); err != nil || !out.ResearchGraph {
		t.Fatalf("%+v %v", out, err)
	}
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
	mustProgress(t, err)
	if instance.CurrentNode != "finished" {
		t.Fatalf("the coordinator finishes after the completion reply: %+v", instance)
	}
	replies := implementCompletionReplies(forge)
	if len(replies) != 1 {
		t.Fatalf("exactly one completion reply, got %d: %q", len(replies), replies)
	}
	reply := replies[0]
	for _, want := range []string{"story-001", "story-002", commits["story-001"], commits["story-002"], "lf/123-feature", "now at commit `" + commits["story-002"] + "`", ValidateStartCommand, "all 2 stories are merged"} {
		if !strings.Contains(reply, want) {
			t.Errorf("the completion reply does not say %q:\n%s", want, reply)
		}
	}
	if strings.Contains(reply, "need your decision") {
		t.Errorf("the completion reply is not a question:\n%s", reply)
	}
	t.Logf("sample completion reply:\n%s", reply)
	if _, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID); err == nil {
		t.Error("a completion reply opens no owner wait")
	}

	// A replay of the step, as after a restart, reads the same merge attempt and
	// posts nothing new under the same delivery key.
	attempt, err := f.db.LatestGraphNodeAttempt(t.Context(), instance.ID, "report_done")
	mustProgress(t, err)
	var first struct {
		Posted      bool   `json:"posted"`
		DeliveryKey string `json:"delivery_key"`
	}
	outputs, err := f.db.ListGraphOutputs(t.Context(), attempt.ID)
	mustProgress(t, err)
	if len(outputs) != 1 {
		t.Fatalf("report_done records one output: %+v", outputs)
	}
	mustProgress(t, json.Unmarshal(outputs[0].Payload, &first))
	if !first.Posted || first.DeliveryKey == "" {
		t.Fatalf("report_done records its delivery: %s", outputs[0].Payload)
	}
	rt := &implementRuntime{opts: f.opts, req: req, parentRef: mustParseRef(t, parent), generation: 1}
	before := len(forge.all())
	res, err := rt.reportDone(t.Context(), f.opts, ActivityRequest{InstanceID: instance.ID, Node: "report_done", Attrs: map[string]string{"lf_source": "merge"}})
	if err != nil || res.Outcome != "published" {
		t.Fatalf("%+v %v", res, err)
	}
	var again struct {
		DeliveryKey string `json:"delivery_key"`
	}
	mustProgress(t, json.Unmarshal(res.Output, &again))
	if again.DeliveryKey != first.DeliveryKey || len(forge.all()) != before || len(implementCompletionReplies(forge)) != 1 {
		t.Fatalf("a replay posts again: key %q vs %q, posts %d vs %d", again.DeliveryKey, first.DeliveryKey, len(forge.all()), before)
	}
}

// A generation with a parked story asks its question and posts no completion
// reply; report_done itself posts nothing unless every story is merged.
func TestImplementCompletionReplyIsNotPostedWhileAStoryIsParked(t *testing.T) {
	f, forge, req := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Error("the coordinator never writes code")
		return nil, nil
	}))
	parent := seedImplementParent(t, f,
		store.ManagedStory{Key: "story-001", IssueNumber: 201, IssueID: 7001},
		store.ManagedStory{Key: "story-002", IssueNumber: 202, IssueID: 7002})
	mergeImplementStories(t, f, parent, map[string]string{"story-001": "1111111111aaaaaaaaaa"}, "story-001")
	mustProgress(t, f.db.EnsureImplementStories(t.Context(), parent, 1, []store.ImplementStorySeed{{Key: "story-001", Position: 0, Branch: "lf/story-001"}, {Key: "story-002", Position: 1, Branch: "lf/story-002"}}, implementClock))
	for _, move := range [][2]string{{store.ImplementWaiting, store.ImplementRunning}, {store.ImplementRunning, store.ImplementParked}} {
		row, err := f.db.GetImplementStory(t.Context(), parent, 1, "story-002")
		mustProgress(t, err)
		in := store.ImplementTransition{Parent: parent, Generation: 1, Key: "story-002", From: move[0], To: move[1], ExpectedRevision: row.Revision, At: implementClock}
		if move[1] == store.ImplementParked {
			in.Reason = "The worker stopped with a blocked answer: story-002 needs a decision."
		}
		_, err = f.db.TransitionImplementStory(t.Context(), in)
		mustProgress(t, err)
	}
	if out, err := Execute(t.Context(), f.opts, req); err != nil {
		t.Fatalf("%+v %v", out, err)
	}
	if got := implementCompletionReplies(forge); len(got) != 0 {
		t.Fatalf("a parked story gets no completion reply: %q", got)
	}
	questions := 0
	for _, post := range forge.all() {
		if strings.Contains(post.body, "1 story is stopped and needs your decision") {
			questions++
		}
	}
	if questions != 1 {
		t.Fatalf("the stopped-stories question is posted as before: %d", questions)
	}
	rt := &implementRuntime{opts: f.opts, req: req, parentRef: mustParseRef(t, parent), generation: 1}
	before := len(forge.all())
	res, err := rt.reportDone(t.Context(), f.opts, ActivityRequest{Node: "report_done", Attrs: map[string]string{"lf_source": "merge"}})
	if err != nil || res.Outcome != "published" || !strings.Contains(string(res.Output), `"posted":false`) || len(forge.all()) != before {
		t.Fatalf("report_done posts nothing while a story is parked: %+v %v", res, err)
	}
}

func mustParseRef(t *testing.T, ref string) naming.Ref {
	t.Helper()
	parsed, err := naming.ParseRef(ref)
	mustProgress(t, err)
	return parsed
}

// implementTrail lists node:outcome for every finished step of the parent's
// latest Implement instance, in order.
func implementTrail(t *testing.T, f *fixture) []string {
	t.Helper()
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
	mustProgress(t, err)
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
	mustProgress(t, err)
	var trail []string
	for _, a := range attempts {
		trail = append(trail, a.Node+":"+a.Outcome)
	}
	return trail
}

func trailIndex(trail []string, step string) int {
	for i, s := range trail {
		if s == step {
			return i
		}
	}
	return -1
}

// advanceRemote commits one file on top of a remote branch and pushes it,
// leaving the clone's checkout where it was. It returns the new tip.
func advanceRemote(t *testing.T, f *fixture, branch, file, content string) string {
	t.Helper()
	f.repo.Git("fetch", "--quiet", "origin")
	f.repo.Git("checkout", "--quiet", "--detach", "origin/"+branch)
	f.repo.WriteFile(file, content)
	f.repo.Commit("change " + file + " on " + branch)
	f.repo.Git("push", "--quiet", "origin", "HEAD:refs/heads/"+branch)
	tip := f.repo.Git("rev-parse", "HEAD")
	f.repo.Git("checkout", "--quiet", gittest.DefaultBranch)
	return tip
}

// remoteHolds reports whether the remote branch contains commit.
func remoteHolds(t *testing.T, f *fixture, branch, commit string) bool {
	t.Helper()
	f.repo.Git("fetch", "--quiet", "origin")
	return f.repo.Git("merge-base", commit, "origin/"+branch) == commit
}

// c1: with main ahead of the feature branch, the coordinator merges main into
// the feature branch before ready starts any story.
func TestImplementCoordinatorMergesMainBeforeTheFirstStoryStarts(t *testing.T) {
	f, _, req := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Error("a clean base sync needs no agent")
		return nil, nil
	}))
	parent := seedImplementParent(t, f, store.ManagedStory{Key: "story-001", IssueNumber: 201, IssueID: 7001})
	fix := advanceRemote(t, f, gittest.DefaultBranch, "fix.txt", "fixed on main\n")
	if remoteHolds(t, f, "lf/123-feature", fix) {
		t.Fatal("the feature branch must start behind main")
	}

	if out, err := Execute(t.Context(), f.opts, req); err != nil || !out.ResearchGraph {
		t.Fatalf("%+v %v", out, err)
	}
	trail := implementTrail(t, f)
	synced, started := trailIndex(trail, "sync_start:synced"), trailIndex(trail, "ready:started")
	if synced < 0 || started < 0 || synced > started {
		t.Fatalf("main is merged before the first story starts: %v", trail)
	}
	if !remoteHolds(t, f, "lf/123-feature", fix) {
		t.Fatal("the feature branch does not hold main's fix")
	}
	if rows := implementStatesOf(t, f, parent); rows["story-001"].State != store.ImplementRunning {
		t.Fatalf("the story starts after the sync: %+v", rows)
	}
}

// c2: main that moves while the stories build is merged again before the
// completion reply.
func TestImplementCoordinatorMergesMainAgainBeforeReportingDone(t *testing.T) {
	f, forge, req := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Error("a clean base sync needs no agent")
		return nil, nil
	}))
	parent := seedImplementParent(t, f, store.ManagedStory{Key: "story-001", IssueNumber: 201, IssueID: 7001})
	if _, err := Execute(t.Context(), f.opts, req); err != nil {
		t.Fatal(err)
	}
	if trail := implementTrail(t, f); trailIndex(trail, "sync_start:current") < 0 {
		t.Fatalf("an up-to-date feature branch is current: %v", trail)
	}

	// While the story builds, main moves on; then the story is merged.
	fix := advanceRemote(t, f, gittest.DefaultBranch, "fix.txt", "fixed on main\n")
	for _, move := range [][2]string{{store.ImplementRunning, store.ImplementDone}, {store.ImplementDone, store.ImplementMerged}} {
		row := implementStatesOf(t, f, parent)["story-001"]
		in := store.ImplementTransition{Parent: parent, Generation: 1, Key: "story-001", From: move[0], To: move[1], ExpectedRevision: row.Revision, At: implementClock}
		if move[1] == store.ImplementMerged {
			in.MergeCommit = f.repo.Git("rev-parse", "origin/lf/123-feature")
			f.repo.Git("push", "--quiet", "origin", in.MergeCommit+":refs/heads/"+row.Branch)
		}
		_, err := f.db.TransitionImplementStory(t.Context(), in)
		mustProgress(t, err)
	}
	mustProgress(t, wakeImplementParent(t.Context(), f.db, parent, time.Now()))
	runOwedImplementStages(t, f, req)

	trail := implementTrail(t, f)
	synced, reported := trailIndex(trail, "sync_done:synced"), trailIndex(trail, "report_done:published")
	if synced < 0 || reported < 0 || synced > reported {
		t.Fatalf("main is merged again before the completion reply: %v", trail)
	}
	if !remoteHolds(t, f, "lf/123-feature", fix) {
		t.Fatal("the reported branch does not hold main's fix")
	}
	if replies := implementCompletionReplies(forge); len(replies) != 1 || !strings.Contains(replies[0], "now at commit `"+f.repo.Git("rev-parse", "origin/lf/123-feature")+"`") {
		t.Fatalf("one completion reply naming the final synced head: %q", replies)
	}
}

const syncResolvedJSON = `{"version":1,"response":"completed","result":{"outcome":"completed","comment":"Resolved the conflict in the shared handler; the package tests pass.","payload":{"implement":{"stories":[]}}}}`

const syncResolverBlockedJSON = `{"version":1,"response":"blocked","result":{"outcome":"blocked","comment":"The conflict could not be resolved.","reason":"shared.go: main and the merged stories want two different handlers.","payload":{"implement":{"stories":[]}}}}`

// seedMainFeatureConflict gives the feature branch and main two different edits
// of shared.go and returns main's tip.
func seedMainFeatureConflict(t *testing.T, f *fixture) string {
	t.Helper()
	f.opts.AutoSyncBase = true
	f.opts.BlockRunOnConflict = true
	advanceRemote(t, f, "lf/123-feature", "shared.go", "package api // merged stories\n")
	return advanceRemote(t, f, gittest.DefaultBranch, "shared.go", "package api // main\n")
}

// c4/c6: a conflict with main goes to the resolver, which works on the merge in
// the parent's worktree; the factory saves its completed answer and the same
// sync runs again and finds main in.
func TestImplementCoordinatorResolvesAConflictWithMainThroughTheResolver(t *testing.T) {
	var prompt, mergeHead, unmerged string
	var f *fixture
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		prompt = req.Prompt
		// #1164: preparation, not the agent, starts the conflicted merge.
		mergeHead = f.repo.GitIn(req.WorktreePath, "rev-parse", "MERGE_HEAD")
		unmerged = f.repo.GitIn(req.WorktreePath, "diff", "--name-only", "--diff-filter=U")
		mustProgress(t, os.WriteFile(filepath.Join(req.WorktreePath, "shared.go"), []byte("package api // main and merged stories\n"), 0o600))
		submitLiveResult(t, req, syncResolvedJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, _, req := assembledImplementFixture(t, runner)
	parent := seedImplementParent(t, f, store.ManagedStory{Key: "story-001", IssueNumber: 201, IssueID: 7001})
	mainTip := seedMainFeatureConflict(t, f)

	out, err := Execute(t.Context(), f.opts, req)
	if err != nil || !out.ResearchGraph {
		t.Fatalf("%+v %v", out, err)
	}
	// The coordinator rests before the resolver; its stage is owed at once.
	runOwedImplementStages(t, f, req)
	trail := implementTrail(t, f)
	want := []string{"select_path:coordinator", "sync_start:conflict", "sync_start_resolve:success", "sync_start_resolved:resolved", "sync_start:current", "merge:none", "ready:started"}
	for i, step := range want {
		if i >= len(trail) || trail[i] != step {
			t.Fatalf("trail = %v, want it to begin %v", trail, want)
		}
	}
	if mergeHead != mainTip || unmerged != "shared.go" {
		t.Fatalf("MERGE_HEAD = %q with unmerged %q, want main's tip %s with shared.go unmerged", mergeHead, unmerged, mainTip)
	}
	for _, want := range []string{"## Merge conflict to resolve", `- "shared.go"`, "Do not commit"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the resolver prompt lacks %q", want)
		}
	}
	if !remoteHolds(t, f, "lf/123-feature", mainTip) {
		t.Fatal("the resolved merge was not pushed to the feature branch")
	}
	if got := f.repo.Git("show", "origin/lf/123-feature:shared.go"); got != "package api // main and merged stories" {
		t.Fatalf("shared.go = %q", got)
	}
	if rows := implementStatesOf(t, f, parent); rows["story-001"].State != store.ImplementRunning {
		t.Fatalf("the story starts once main is in: %+v", rows)
	}
}

// c7: a resolver that cannot settle the conflict answers blocked; only then is
// the owner asked, naming the files, and nothing is pushed.
func TestImplementCoordinatorBlockedResolverAsksTheOwnerAndPushesNothing(t *testing.T) {
	var f *fixture
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		submitLiveResult(t, req, syncResolverBlockedJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledImplementFixture(t, runner)
	parent := seedImplementParent(t, f, store.ManagedStory{Key: "story-001", IssueNumber: 201, IssueID: 7001})
	mainTip := seedMainFeatureConflict(t, f)
	f.repo.Git("fetch", "--quiet", "origin")
	before := f.repo.Git("rev-parse", "origin/lf/123-feature")

	if _, err := Execute(t.Context(), f.opts, req); err != nil {
		t.Fatal(err)
	}
	runOwedImplementStages(t, f, req)
	trail := implementTrail(t, f)
	if trailIndex(trail, "sync_start_resolved:blocked") < 0 || trailIndex(trail, "ready:started") >= 0 {
		t.Fatalf("a blocked resolver stops before any story starts: %v", trail)
	}
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
	mustProgress(t, err)
	if instance.CurrentNode != "waiting" {
		t.Fatalf("the coordinator rests at the owner wait: %+v", instance)
	}
	wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	if err != nil || wait.State != "waiting" {
		t.Fatalf("the owner has one open question: %+v %v", wait, err)
	}
	var questions []string
	for _, post := range forge.all() {
		if strings.Contains(post.body, "could not settle") {
			questions = append(questions, post.body)
		}
	}
	if len(questions) != 1 || !strings.Contains(questions[0], "shared.go") || !strings.Contains(questions[0], "two different handlers") {
		t.Fatalf("one question names the conflicting file and the resolver's reason: %q", questions)
	}
	// #1142: a resolver that sends no questions gets the factory's own q1, as before.
	if !strings.Contains(html.UnescapeString(questions[0]), "- **Conflict in `shared.go`**: Shared.go: main and the merged stories want two different handlers.\n") || strings.Contains(questions[0], "**q1**") {
		t.Fatalf("the fallback question is unchanged: %q", questions[0])
	}
	f.repo.Git("fetch", "--quiet", "origin")
	if after := f.repo.Git("rev-parse", "origin/lf/123-feature"); after != before {
		t.Fatalf("nothing is pushed: feature moved from %s to %s", before, after)
	}
	sess, err := f.db.GetSessionByTaskRef(t.Context(), parent)
	mustProgress(t, err)
	if got := f.repo.GitIn(sess.WorktreePath, "rev-parse", "MERGE_HEAD"); got != mainTip {
		t.Fatalf("the unresolved merge stays in progress, uncommitted: MERGE_HEAD=%q", got)
	}
}

const syncResolverAsksJSON = `{"version":1,"response":"blocked","result":{"outcome":"blocked","comment":"The conflict could not be resolved.","reason":"shared.go: main and the merged stories want two different handlers.","questions":[{"id":"q1","audience":"technical","text":"Which handler should the feature branch keep?","options":[{"id":"a","text":"Keep the feature branch's handler"},{"id":"b","text":"Take main's handler"},{"id":"c","text":"Combine both behind one handler"}],"recommended_option":"c"}],"payload":{"implement":{"stories":[]}}}}`

// #1142: a blocked resolver may ask its own questions. The owner sees each with
// its ID, options and recommendation instead of the factory's q1, and the owner's
// answer reaches the resolver's next run as guidance.
func TestImplementCoordinatorResolverQuestionsReachTheOwnerWithTheirOptions(t *testing.T) {
	var prompts []string
	var f *fixture
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		prompts = append(prompts, req.Prompt)
		if len(prompts) == 1 {
			submitLiveResult(t, req, syncResolverAsksJSON)
		} else {
			mustProgress(t, os.WriteFile(filepath.Join(req.WorktreePath, "shared.go"), []byte("package api // main and merged stories\n"), 0o600))
			submitLiveResult(t, req, syncResolvedJSON)
		}
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	var forge *fakeForge
	var req Request
	f, forge, req = assembledImplementFixture(t, runner)
	parent := seedImplementParent(t, f, store.ManagedStory{Key: "story-001", IssueNumber: 201, IssueID: 7001})
	mainTip := seedMainFeatureConflict(t, f)
	if _, err := Execute(t.Context(), f.opts, req); err != nil {
		t.Fatal(err)
	}
	runOwedImplementStages(t, f, req)
	if trail := implementTrail(t, f); trailIndex(trail, "sync_start_resolved:blocked") < 0 {
		t.Fatalf("the asking resolver stops at the owner wait: %v", trail)
	}
	var questions []string
	for _, post := range forge.all() {
		if strings.Contains(post.body, "could not settle") {
			questions = append(questions, html.UnescapeString(post.body))
		}
	}
	if len(questions) != 1 {
		t.Fatalf("one owner question: %q", questions)
	}
	for _, want := range []string{"The conflict is in `shared.go`. Shared.go: main and the merged stories want two different handlers.", "- **q1** Which handler should the feature branch keep?", "\n  - `a` Keep the feature branch's handler\n", "\n  - `b` Take main's handler\n", "\n  - `c` Combine both behind one handler (recommended)"} {
		if !strings.Contains(questions[0], want) {
			t.Errorf("the question lacks %q:\n%s", want, questions[0])
		}
	}
	if strings.Contains(questions[0], "Conflict in") || strings.Count(questions[0], "(recommended)") != 1 {
		t.Fatalf("the resolver's questions replace the fallback: %s", questions[0])
	}

	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
	mustProgress(t, err)
	wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	if err != nil || wait.State != "waiting" {
		t.Fatalf("the owner has one open question: %+v %v", wait, err)
	}
	asked, err := f.db.LatestGraphDispatch(t.Context(), instance.ID)
	mustProgress(t, err)
	mustProgress(t, f.db.FinishGraphDispatch(t.Context(), asked.ID, time.Now()))
	delivery, err := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
	mustProgress(t, err)
	const reply = "q1 b"
	_, err = f.db.ClaimGraphOwnerReply(t.Context(), store.GraphOwnerReply{InstanceID: instance.ID, OwnerID: 77, AuthorID: 77, CommentID: delivery.CommentID + 100, QuestionCommentID: delivery.CommentID, TaskRef: parent, SourceRef: parent, Mode: "implement", Workflow: "implement", DeliveryKey: wait.DeliveryKey, Body: reply, At: time.Now()})
	mustProgress(t, err)
	answer := req
	answer.Trigger.CommentID, answer.Trigger.Body = delivery.CommentID+100, reply
	if _, err := Execute(t.Context(), f.opts, answer); err != nil {
		t.Fatal(err)
	}
	runOwedImplementStages(t, f, req)
	if len(prompts) != 2 || !strings.Contains(prompts[1], reply) {
		t.Fatalf("the resolver runs again with the owner's answer as guidance: %d runs", len(prompts))
	}
	if !remoteHolds(t, f, "lf/123-feature", mainTip) {
		t.Fatal("the resolved merge was not pushed to the feature branch")
	}
}

const syncResolverIncompleteJSON = `{"version":1,"response":"incomplete","result":{"outcome":"incomplete","comment":"The conflict is not resolved yet.","reason":"No merge is in progress in my worktree.","payload":{"implement":{"stories":[]}}}}`

// implementPostsContaining returns the bodies of the forge's posts that contain text.
func implementPostsContaining(forge *fakeForge, text string) []string {
	var out []string
	for _, post := range forge.all() {
		if body := html.UnescapeString(post.body); strings.Contains(body, text) {
			out = append(out, body)
		}
	}
	return out
}

// #1164: a resolver whose worktree holds no merge in progress and no conflicted
// file had nothing to resolve. Its incomplete answer reaches the owner as one
// question instead of another identical sync.
func TestImplementCoordinatorResolverWithNothingToResolveAsksTheOwner(t *testing.T) {
	calls := 0
	var f *fixture
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		f.repo.GitIn(req.WorktreePath, "merge", "--abort")
		submitLiveResult(t, req, syncResolverIncompleteJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	var forge *fakeForge
	var req Request
	f, forge, req = assembledImplementFixture(t, runner)
	seedImplementParent(t, f, store.ManagedStory{Key: "story-001", IssueNumber: 201, IssueID: 7001})
	seedMainFeatureConflict(t, f)

	if _, err := Execute(t.Context(), f.opts, req); err != nil {
		t.Fatal(err)
	}
	runOwedImplementStages(t, f, req)
	trail := implementTrail(t, f)
	if calls != 1 || trailIndex(trail, "sync_start_resolved:blocked") < 0 || trailIndex(trail, "sync_start_resolved:retry") >= 0 {
		t.Fatalf("nothing to resolve goes to the owner, not another sync: %d resolver runs, trail %v", calls, trail)
	}
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
	mustProgress(t, err)
	wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	if instance.CurrentNode != "waiting" || err != nil || wait.State != "waiting" {
		t.Fatalf("the coordinator rests at one open owner question: %+v %+v %v", instance, wait, err)
	}
	questions := implementPostsContaining(forge, "could not settle")
	if len(questions) != 1 || !strings.Contains(questions[0], "no merge in progress") {
		t.Fatalf("one question says there was nothing to resolve: %q", questions)
	}
}

// #1164: a coordinator step whose dispatch limit is exhausted stops the
// instance and tells the owner once, on the parent, what stopped and how to
// resume.
func TestImplementCoordinatorExhaustedStepTellsTheOwnerOnce(t *testing.T) {
	calls := 0
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		submitLiveResult(t, req, syncResolverIncompleteJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledImplementFixture(t, runner)
	seedImplementParent(t, f, store.ManagedStory{Key: "story-001", IssueNumber: 201, IssueID: 7001})
	seedMainFeatureConflict(t, f)

	var failures []error
	var last Request
	if _, err := Execute(t.Context(), f.opts, req); err != nil {
		failures = append(failures, err)
	}
	for range 20 {
		dispatch, ok, err := f.db.NextFreshGraphDispatch(t.Context(), f.session.ID)
		mustProgress(t, err)
		if !ok {
			break
		}
		reserved, ok, err := f.db.ReserveGraphDispatch(t.Context(), dispatch.ID, dispatch.DueAt)
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
		last = req
		last.Trigger.CommentID, last.Trigger.Body = reserved.TriggerID(), "Continue the workflow."
		if _, err := Execute(t.Context(), f.opts, last); err != nil {
			failures = append(failures, err)
		}
	}
	var limit *GraphLimitError
	if len(failures) != 1 || !errors.As(failures[0], &limit) || limit.Node != "sync_start_resolve" {
		t.Fatalf("the resolver's exhausted limit stops the coordinator: %d resolver runs, %v", calls, failures)
	}
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
	mustProgress(t, err)
	if instance.Status != store.GraphFailed {
		t.Fatalf("the instance stops: %+v", instance)
	}
	// A repeat of the report finds the one retained comment.
	rt := &implementRuntime{opts: f.opts}
	mustProgress(t, rt.reportExhausted(t.Context(), instance.ID, limit))
	stops := implementPostsContaining(forge, "Implement stopped at step `sync_start_resolve`")
	if len(stops) != 1 || !strings.Contains(stops[0], "dispatch limit 3 exhausted") || !strings.Contains(stops[0], "Reply on this issue to resume") {
		t.Fatalf("one owner comment says what stopped and how to resume: %q", stops)
	}
}

// #1142: a story worker still cannot ask; its result with questions is refused
// at submit.
func TestImplementWorkerResultWithQuestionsIsStillRefused(t *testing.T) {
	var refused bool
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		ask := `{"result":{"outcome":"blocked","comment":"Built what could be built.","reason":"The payment API key is not available.","questions":[{"id":"q1","audience":"technical","text":"Which key?","options":[{"id":"a","text":"Test key"},{"id":"b","text":"Live key"}],"recommended_option":"a"}],"payload":{"implement":{"stories":[]}}}}`
		got := mcpDecodeSubmission(t, mcpToolCall(t, submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}, "communication_submit", []byte(ask)))
		for _, d := range got.Validation.Diagnostics.Diagnostics {
			refused = refused || (!got.Validation.Valid && d.Path == "$.result.questions" && d.Code == "authorization")
		}
		submitLiveResult(t, req, implementBlockedJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, _, req := assembledImplementFixture(t, runner)
	seedImplementStory(t, f)
	if _, err := Execute(t.Context(), f.opts, req); err != nil {
		t.Fatal(err)
	}
	if !refused {
		t.Fatal("a story worker's result with questions was accepted")
	}
}

// c6 at the end: main that conflicts once the stories are merged goes to the
// final sync's resolver, and the completion reply follows the resolved merge.
func TestImplementCoordinatorResolvesAConflictWithMainBeforeReportingDone(t *testing.T) {
	resolves := 0
	var f *fixture
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		resolves++
		mustProgress(t, os.WriteFile(filepath.Join(req.WorktreePath, "shared.go"), []byte("package api // main and merged stories\n"), 0o600))
		submitLiveResult(t, req, syncResolvedJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	var forge *fakeForge
	var req Request
	f, forge, req = assembledImplementFixture(t, runner)
	parent := seedImplementParent(t, f, store.ManagedStory{Key: "story-001", IssueNumber: 201, IssueID: 7001})
	if _, err := Execute(t.Context(), f.opts, req); err != nil {
		t.Fatal(err)
	}
	mainTip := seedMainFeatureConflict(t, f)
	for _, move := range [][2]string{{store.ImplementRunning, store.ImplementDone}, {store.ImplementDone, store.ImplementMerged}} {
		row := implementStatesOf(t, f, parent)["story-001"]
		in := store.ImplementTransition{Parent: parent, Generation: 1, Key: "story-001", From: move[0], To: move[1], ExpectedRevision: row.Revision, At: implementClock}
		if move[1] == store.ImplementMerged {
			in.MergeCommit = f.repo.Git("rev-parse", "origin/lf/123-feature")
			f.repo.Git("push", "--quiet", "origin", in.MergeCommit+":refs/heads/"+row.Branch)
		}
		_, err := f.db.TransitionImplementStory(t.Context(), in)
		mustProgress(t, err)
	}
	mustProgress(t, wakeImplementParent(t.Context(), f.db, parent, time.Now()))
	runOwedImplementStages(t, f, req)

	trail := implementTrail(t, f)
	order := []string{"sync_done:conflict", "sync_done_resolve:success", "sync_done_resolved:resolved", "sync_done:current", "report_done:published"}
	last := -1
	for _, step := range order {
		i := trailIndex(trail, step)
		if i <= last {
			t.Fatalf("trail = %v, want %v in order", trail, order)
		}
		last = i
	}
	if resolves != 1 || !remoteHolds(t, f, "lf/123-feature", mainTip) {
		t.Fatalf("the resolver ran once (%d) and its merge reached the feature branch", resolves)
	}
	if replies := implementCompletionReplies(forge); len(replies) != 1 || !strings.Contains(replies[0], "now at commit `"+f.repo.Git("rev-parse", "origin/lf/123-feature")+"`") {
		t.Fatalf("one completion reply naming the final synced head: %q", replies)
	}
}

func TestImplementCoordinatorRepairsStaleMergedLedger(t *testing.T) {
	f, forge, req := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Error("a clean stale merge must not launch a worker")
		return nil, nil
	}))
	parent := seedImplementParent(t, f,
		store.ManagedStory{Key: "story-001", IssueNumber: 201, IssueID: 7001},
		store.ManagedStory{Key: "story-002", IssueNumber: 202, IssueID: 7002})
	commits := map[string]string{}
	mergeImplementStories(t, f, parent, commits, "story-001", "story-002")
	old := implementStatesOf(t, f, parent)
	tips := map[string]string{}
	for _, key := range []string{"story-001", "story-002"} {
		tips[key] = advanceRemote(t, f, "lf/"+key, key+".txt", "round two\n")
	}
	// Completed stories disappear from bind's unfinished set, but their stored
	// branch identity must still be reconciled and reported.
	managed, err := f.db.ListManagedStories(t.Context(), parent, 1)
	mustProgress(t, err)
	for _, story := range managed {
		previous := story.State
		story.State = "completed"
		mustProgress(t, f.db.CompareManagedStory(t.Context(), story, previous))
	}
	if out, err := Execute(t.Context(), f.opts, req); err != nil || !out.ResearchGraph {
		t.Fatalf("%+v %v", out, err)
	}
	replies := implementCompletionReplies(forge)
	if len(replies) != 1 {
		t.Fatalf("completion replies: %q", replies)
	}
	head := f.repo.Git("rev-parse", "origin/lf/123-feature")
	if !strings.Contains(replies[0], "now at commit `"+head+"`") {
		t.Fatalf("wrong completion head: %s", replies[0])
	}
	rows := implementStatesOf(t, f, parent)
	for key, tip := range tips {
		row := rows[key]
		if f.repo.Git("merge-base", tip, head) != tip || row.State != store.ImplementMerged || row.MergeCommit == old[key].MergeCommit || row.Revision != old[key].Revision+2 || row.WorkerSession != old[key].WorkerSession {
			t.Fatalf("story work or ledger not reconciled: %+v", row)
		}
	}
	rt := &implementRuntime{opts: f.opts, req: req, parentRef: mustParseRef(t, parent), generation: 1}
	bindings, err := rt.bindings()
	mustProgress(t, err)
	for range 2 {
		res, err := (ImplementMergeActivity{b: bindings}).Execute(t.Context(), f.opts, ActivityRequest{Mode: "implement"})
		if err != nil || res.Outcome != "none" {
			t.Fatalf("unchanged merge: %+v %v", res, err)
		}
	}
	for key, row := range implementStatesOf(t, f, parent) {
		if row != rows[key] {
			t.Fatalf("unchanged refs revised ledger: %+v", row)
		}
	}
	if f.repo.Git("rev-parse", "origin/lf/123-feature") != head {
		t.Fatal("unchanged refs produced another merge")
	}
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
	mustProgress(t, err)
	// A later push after merge draining must suppress a completion replay.
	advanceRemote(t, f, "lf/story-002", "late.txt", "late work\n")
	before := len(forge.all())
	if _, err := rt.reportDone(t.Context(), f.opts, ActivityRequest{InstanceID: instance.ID, Node: "report_done", Attrs: map[string]string{"lf_source": "merge"}}); err == nil {
		t.Fatal("completion accepted a story head absent from the feature")
	}
	if len(forge.all()) != before {
		t.Fatal("drift posted a false completion")
	}
}

func TestImplementCoordinatorFinalVerificationAfterDrain(t *testing.T) {
	for _, scenario := range []string{"story advanced", "story missing", "fetch failed", "base advanced"} {
		t.Run(scenario, func(t *testing.T) {
			f, forge, req := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
				t.Error("coordinator must not launch a worker")
				return nil, nil
			}))
			parent := seedImplementParent(t, f, store.ManagedStory{Key: "story-001", IssueNumber: 201, IssueID: 7001})
			mergeImplementStories(t, f, parent, map[string]string{}, "story-001")
			before := implementStatesOf(t, f, parent)["story-001"]
			changed := make(chan string, 1)
			forge.afterPost = func() {
				instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
				if err != nil {
					return
				}
				attempt, err := f.db.LatestGraphNodeAttempt(t.Context(), instance.ID, "handoff")
				if err != nil || attempt.Status != store.GraphRunning {
					return
				}
				// The handoff runs after merge returned none and before sync_done.
				forge.mu.Lock()
				forge.afterPost = nil
				forge.mu.Unlock()
				tip := ""
				switch scenario {
				case "story advanced":
					tip = advanceRemote(t, f, "lf/story-001", "late.txt", "late work\n")
				case "story missing":
					f.repo.Git("push", "--quiet", "origin", "--delete", "lf/story-001")
				case "fetch failed":
					f.repo.Git("remote", "set-url", "origin", filepath.Join(t.TempDir(), "absent.git"))
				case "base advanced":
					tip = advanceRemote(t, f, gittest.DefaultBranch, "base.txt", "late base fix\n")
				}
				changed <- tip
			}
			_, runErr := Execute(t.Context(), f.opts, req)
			var tip string
			select {
			case tip = <-changed:
			default:
				t.Fatal("fixture did not change refs after merge draining")
			}
			replies := implementCompletionReplies(forge)
			if scenario == "base advanced" {
				head := f.repo.Git("rev-parse", "origin/lf/123-feature")
				if runErr != nil || len(replies) != 1 || !strings.Contains(replies[0], "now at commit `"+head+"`") || head == before.MergeCommit || f.repo.Git("merge-base", tip, head) != tip || f.repo.Git("merge-base", before.MergeCommit, head) != before.MergeCommit {
					t.Fatalf("final sync was not reflected in completion: %q %v", replies, runErr)
				}
			} else if len(replies) != 0 {
				t.Fatalf("uncertain or stale Git evidence posted completion: %q", replies)
			}
			if row := implementStatesOf(t, f, parent)["story-001"]; row != before {
				t.Fatalf("reporting changed ledger: %+v", row)
			}
		})
	}
}

// parkAndAskImplementOwner runs the canonical coordinator until its one story
// is parked and the owner is asked, and returns the open owner wait.
func parkAndAskImplementOwner(t *testing.T, f *fixture, req Request) (string, store.GraphInstance, store.GraphOwnerWait) {
	t.Helper()
	enableWorkerSyncFixture(f)
	parent := seedImplementParent(t, f, store.ManagedStory{Key: "story-001", IssueNumber: 201, IssueID: 7001})
	if out, err := Execute(t.Context(), f.opts, req); err != nil || !out.ResearchGraph {
		t.Fatalf("%+v %v", out, err)
	}
	row := implementStatesOf(t, f, parent)["story-001"]
	_, err := f.db.TransitionImplementStory(t.Context(), store.ImplementTransition{Parent: parent, Generation: 1, Key: "story-001", From: store.ImplementRunning, To: store.ImplementParked, ExpectedRevision: row.Revision, Reason: "The worker stopped with a blocked answer: main is red.", At: implementClock})
	mustProgress(t, err)
	mustProgress(t, wakeImplementParent(t.Context(), f.db, parent, time.Now()))
	runOwedImplementStages(t, f, req)
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
	mustProgress(t, err)
	wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	if err != nil || wait.State != "waiting" {
		t.Fatalf("the owner has one open question: %+v %v", wait, err)
	}
	return parent, instance, wait
}

// replyToImplementOwner claims the owner's reply restarting story-001 and runs
// the coordinator on it.
func replyToImplementOwner(t *testing.T, f *fixture, req Request, parent string, instance store.GraphInstance, wait store.GraphOwnerWait) {
	t.Helper()
	delivery, err := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
	mustProgress(t, err)
	const reply = "story-001: main is fixed now, try again."
	_, err = f.db.ClaimGraphOwnerReply(t.Context(), store.GraphOwnerReply{InstanceID: instance.ID, OwnerID: 77, AuthorID: 77, CommentID: delivery.CommentID + 100, QuestionCommentID: delivery.CommentID, TaskRef: parent, SourceRef: parent, Mode: "implement", Workflow: "implement", DeliveryKey: wait.DeliveryKey, Body: reply, At: time.Now()})
	mustProgress(t, err)
	answer := req
	answer.Trigger.CommentID, answer.Trigger.Body = delivery.CommentID+100, reply
	if out, err := Execute(t.Context(), f.opts, answer); err != nil {
		t.Fatalf("%+v %v", out, err)
	}
}

// afterAsk is the coordinator trail after its owner question.
func afterAsk(t *testing.T, trail []string) []string {
	t.Helper()
	asked := trailIndex(trail, "ask_owner:published")
	if asked < 0 {
		t.Fatalf("the owner was never asked: %v", trail)
	}
	return trail[asked+1:]
}

// #966: a fix that reaches main while a story is parked reaches the feature
// branch before the owner's reply restarts the story.
func TestImplementCoordinatorMergesMainIntoTheFeatureBranchBeforeResumedStoriesStart(t *testing.T) {
	f, _, req := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Error("a clean base sync needs no agent")
		return nil, nil
	}))
	parent, instance, wait := parkAndAskImplementOwner(t, f, req)
	fix := advanceRemote(t, f, gittest.DefaultBranch, "fix.txt", "fixed on main\n")
	if remoteHolds(t, f, "lf/123-feature", fix) {
		t.Fatal("the feature branch must be behind main before the reply")
	}

	replyToImplementOwner(t, f, req, parent, instance, wait)
	resumed := afterAsk(t, implementTrail(t, f))
	synced, started := trailIndex(resumed, "sync_start:synced"), trailIndex(resumed, "ready:started")
	if synced < 0 || started < 0 || synced > started {
		t.Fatalf("main is merged into the feature branch before the resumed story starts: %v", resumed)
	}
	if !remoteHolds(t, f, "lf/123-feature", fix) {
		t.Fatal("the feature branch does not hold main's fix")
	}
	if row := implementStatesOf(t, f, parent)["story-001"]; row.State != store.ImplementRunning {
		t.Fatalf("the story restarted: %+v", row)
	}
}

// #966: with main unchanged, the resumed sync is current and moves nothing.
func TestImplementCoordinatorResumeWithMainUnchangedLeavesTheFeatureBranch(t *testing.T) {
	f, _, req := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Error("a clean base sync needs no agent")
		return nil, nil
	}))
	parent, instance, wait := parkAndAskImplementOwner(t, f, req)
	f.repo.Git("fetch", "--quiet", "origin")
	before := f.repo.Git("rev-parse", "origin/lf/123-feature")

	replyToImplementOwner(t, f, req, parent, instance, wait)
	resumed := afterAsk(t, implementTrail(t, f))
	current, started := trailIndex(resumed, "sync_start:current"), trailIndex(resumed, "ready:started")
	if current < 0 || started < 0 || current > started || trailIndex(resumed, "sync_start:synced") >= 0 {
		t.Fatalf("the resumed sync is current before the story restarts: %v", resumed)
	}
	f.repo.Git("fetch", "--quiet", "origin")
	if after := f.repo.Git("rev-parse", "origin/lf/123-feature"); after != before {
		t.Fatalf("the feature branch moved from %s to %s", before, after)
	}
	if row := implementStatesOf(t, f, parent)["story-001"]; row.State != store.ImplementRunning {
		t.Fatalf("the story restarted: %+v", row)
	}
}

// Resumption keeps the same original request and starts permitted stories while
// unresolved preparation freshness remains a caveat.
func TestImplementCoordinatorResumeDefersConflictAndStartsPermittedStories(t *testing.T) {
	f, _, req := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Fatal("preparation conflict must not launch a resolver")
		return nil, nil
	}))
	parent, instance, wait := parkAndAskImplementOwner(t, f, req)
	mainTip := seedMainFeatureConflict(t, f)
	before := f.repo.Git("rev-parse", "origin/lf/123-feature")
	asked, err := f.db.LatestGraphDispatch(t.Context(), instance.ID)
	mustProgress(t, err)
	mustProgress(t, f.db.FinishGraphDispatch(t.Context(), asked.ID, time.Now()))
	replyToImplementOwner(t, f, req, parent, instance, wait)
	runOwedImplementStages(t, f, req)
	resumed := afterAsk(t, implementTrail(t, f))
	want := []string{"sync_start:deferred", "merge:none", "ready:started"}
	for i, step := range want {
		if i >= len(resumed) || resumed[i] != step {
			t.Fatalf("resumed trail=%v, want prefix %v", resumed, want)
		}
	}
	if remoteHolds(t, f, "lf/123-feature", mainTip) || f.repo.Git("rev-parse", "origin/lf/123-feature") != before {
		t.Fatal("deferred base synchronization changed feature history")
	}
	if row := implementStatesOf(t, f, parent)["story-001"]; row.State != store.ImplementRunning {
		t.Fatalf("story did not restart: %+v", row)
	}
}

// #1123 c8: once Implement has finished with every story merged, a plain owner
// comment sent through the Implement runtime admits a new coordinator that
// reaches the completion step and posts "Implement is finished" again. The
// turn does not refuse it; the daemon guard that keeps such comments out of
// Implement is what prevents the repeated note.
func TestImplementPlainCommentAfterFinishedImplementRepostsCompletion(t *testing.T) {
	f, forge, req := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Error("the coordinator never writes code")
		return nil, nil
	}))
	parent := seedImplementParent(t, f,
		store.ManagedStory{Key: "story-001", IssueNumber: 201, IssueID: 7001},
		store.ManagedStory{Key: "story-002", IssueNumber: 202, IssueID: 7002})
	mergeImplementStories(t, f, parent, map[string]string{}, "story-001", "story-002")
	if out, err := Execute(t.Context(), f.opts, req); err != nil || !out.ResearchGraph {
		t.Fatalf("%+v %v", out, err)
	}
	first, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
	mustProgress(t, err)
	if first.Status == store.GraphRunning || len(implementCompletionReplies(forge)) != 1 {
		t.Fatalf("the first coordinator finishes with one completion reply: %+v %d", first, len(implementCompletionReplies(forge)))
	}

	req.Trigger.CommentID++
	req.Trigger.Body = "what are the blocking findings? tell me exactly"
	req.Command = ParseCommand(req.Trigger.Body)
	if out, err := Execute(t.Context(), f.opts, req); err != nil || !out.ResearchGraph {
		t.Fatalf("the plain comment is admitted into Implement: %+v %v", out, err)
	}
	second, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
	mustProgress(t, err)
	if second.ID <= first.ID || second.CurrentNode != "finished" {
		t.Fatalf("a new coordinator reaches the completion step: first %+v second %+v", first, second)
	}
	if got := implementCompletionReplies(forge); len(got) != 2 {
		t.Fatalf("the completion note is posted again: %d", len(got))
	}
}
