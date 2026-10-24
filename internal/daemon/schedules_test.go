package daemon_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestDueScheduleDispatchesStoredPayloadAndCountsPendingWork(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Scheduled work")
	f.settle() // create the session and watermark

	session := f.session()
	// The fixture clock advances on every daemon clock read. Keep the due time
	// well beyond the setup ticks, then advance deterministically past only the
	// first of the two rows.
	due := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	first, _, err := f.db.CreateOrGetSchedule(t.Context(), store.CreateScheduleParams{
		SessionID: session.ID, SourceCommentID: 701, OwnerHandle: ownerLogin,
		Payload: "/spike first scheduled payload", DueAt: due,
	})
	if err != nil {
		t.Fatalf("create first schedule: %v", err)
	}
	if _, _, err := f.db.CreateOrGetSchedule(t.Context(), store.CreateScheduleParams{
		SessionID: session.ID, SourceCommentID: 702, OwnerHandle: ownerLogin,
		Payload: "/spike later scheduled payload", DueAt: due.Add(2 * time.Hour),
	}); err != nil {
		t.Fatalf("create later schedule: %v", err)
	}

	before := f.settle()
	if before.Scheduled != 2 {
		t.Fatalf("before due scheduled=%d, want 2; calls=%+v\nlogs:\n%s", before.Scheduled, f.turns.calls(), f.logs)
	}
	if got := len(f.turns.calls()); got != 0 {
		t.Fatalf("ran %d turns before due", got)
	}

	f.clock.advance(25 * time.Hour)
	after := f.settle()
	if after.Scheduled != 1 {
		t.Fatalf("after due scheduled=%d, want 1\nlogs:\n%s", after.Scheduled, f.logs)
	}
	calls := f.turns.calls()
	if len(calls) != 1 {
		t.Fatalf("ran %d scheduled turns, want 1", len(calls))
	}
	if got := calls[0].Command.Text; got != "first scheduled payload" {
		t.Errorf("scheduled instruction = %q, want stored payload only", got)
	}
	if strings.Contains(calls[0].Trigger.Body, "/schedule") {
		t.Errorf("scheduled trigger carried scheduling metadata: %q", calls[0].Trigger.Body)
	}
	stored, err := f.db.GetSchedule(t.Context(), first.ID)
	if err != nil {
		t.Fatalf("read completed schedule: %v", err)
	}
	if stored.Status != store.ScheduleSucceeded || stored.RunID == 0 {
		t.Errorf("completed schedule = %+v, want succeeded with run id", stored)
	}
}

// A due schedule whose issue is closed is settled once without a run, so it is
// not offered, and its issue not read, again on every tick.
func TestDueScheduleOnClosedIssueSettlesOnce(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Scheduled work")
	f.settle()
	session := f.session()
	scheduled, _, err := f.db.CreateOrGetSchedule(t.Context(), store.CreateScheduleParams{
		SessionID: session.ID, SourceCommentID: 701, OwnerHandle: ownerLogin,
		Payload: "/spike scheduled payload", DueAt: f.clock.peek(),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.forge.closeIssue(testIssue)
	f.clock.advance(time.Minute)
	f.settle()
	stored, err := f.db.GetSchedule(t.Context(), scheduled.ID)
	if err != nil || stored.Status != store.ScheduleFailed || stored.RunID != 0 {
		t.Fatalf("schedule on a closed issue = %+v %v, want settled without a run\nlogs:\n%s", stored, err, f.logs)
	}
	reads := f.forge.issueReadsOf(testIssue)
	for range 3 {
		f.clock.advance(time.Minute)
		f.settle()
	}
	if got := f.forge.issueReadsOf(testIssue); got != reads {
		t.Fatalf("a settled schedule read its closed issue %d more times", got-reads)
	}
	if calls := f.turns.calls(); len(calls) != 0 {
		t.Fatalf("ran %d turns for a closed issue", len(calls))
	}
	if n := strings.Count(f.logs.String(), "scheduled work settled without a run because its issue is closed"); n != 1 {
		t.Fatalf("settled %d times, want once", n)
	}
}

// A real prepare error must pass through scheduled accounting and discovery;
// the fixture's registered path deliberately is not a Git clone.
func TestImplementPrepareFailureRetriesAndParks(t *testing.T) {
	f := newFixture(t)
	originalHandler := f.forge.server.Config.Handler
	f.forge.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/parent") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		originalHandler.ServeHTTP(w, r)
	})
	f.plantCursor()
	f.issueWithLabels("Worker", "Mode: Implement")
	f.settle()
	ctx := t.Context()
	session := f.session()
	parentSession, err := f.db.GetOrCreateSession(ctx, f.repo, 900)
	if err != nil {
		t.Fatal(err)
	}
	parent := parentSession.TaskRef
	f.forge.addIssue(fakeIssue{Number: 900, Title: "Parent", User: stranger()})
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(f.db.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: parent, Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}))
	check(f.db.PutManagedStory(ctx, store.ManagedStory{ParentTask: parent, Generation: 1, Key: "story-001", IssueNumber: testIssue, IssueID: 4242, State: "planning"}))
	check(f.db.EnsureImplementStories(ctx, parent, 1, []store.ImplementStorySeed{{Key: "story-001", Branch: "lf/story-001"}}, f.clock.peek()))
	row, err := f.db.GetImplementStory(ctx, parent, 1, "story-001")
	check(err)
	_, err = f.db.TransitionImplementStory(ctx, store.ImplementTransition{Parent: parent, Generation: 1, Key: row.Key, From: store.ImplementWaiting, To: store.ImplementRunning, ExpectedRevision: row.Revision, At: f.clock.peek()})
	check(err)
	check(f.db.SetImplementWorkerSession(ctx, parent, 1, row.Key, session.ID, f.clock.peek()))
	binary, err := git.Resolve(ctx)
	check(err)
	authority, err := turn.NewAttemptAuthority(ctx, f.db)
	check(err)
	t.Cleanup(func() { check(authority.Close(context.WithoutCancel(ctx))) })
	runner := &prepareFailureRunner{}
	opts := turn.Options{DB: f.db, Git: binary, Paths: f.paths, InstanceID: "prepare-failure", Logger: f.logger,
		CloneGate: unusedRecoveryCloneGate{}, Runners: turn.RunnerSet{agent.Kind(store.AgentClaude): runner}, ResearchReporter: f.reporter,
		MCP: &turn.MCPRuntime{Authority: authority, Paths: f.paths, Executable: "/factory/litefactory", Now: f.clock.Now}}
	// This fixture isolates legacy preparation: supply the pinned released definition
	// so admission succeeds before the deliberately broken clone is opened.
	opts.WorkflowSource = func(ctx context.Context, repo store.Repo, workflow agent.Workflow, specs graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		if repo.ID != session.RepoID || workflow != agent.WorkflowImplement {
			t.Fatal("preparation fixture selected another repository or workflow")
		}
		definition, err := teamsource.CaptureWorkflow(ctx, os.DirFS("../../teams/go-development/v1.0.2/implement"), "implement.dot", "go-development", agent.ModeImplement, specs)
		if err != nil {
			return nil, err
		}
		config := []byte("implement: implement/implement.dot\n")
		digest := sha256.Sum256(config)
		definition.Origin = teamsource.WorkflowOrigin{Kind: "local", Source: "local", Slug: "fixture", Version: "1.0.0", GraphPath: "implement/implement.dot", GraphFile: "implement.dot", Team: definition.Profile.Team, TeamConfig: config, ConfigDigest: hex.EncodeToString(digest[:])}
		return definition, nil
	}
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		req.Mode = agent.ModeImplement
		return turn.Execute(ctx, opts, req)
	})
	var lastID int64
	for i := 0; i < 3; i++ {
		f.settle()
		instance, err := f.db.LatestWorkflowInstance(ctx, session.ID, "implement")
		if err != nil {
			t.Fatalf("%v calls=%+v logs=%s", err, f.turns.calls(), f.logs)
		}
		if instance.ID == lastID || instance.Status != store.GraphFailed {
			t.Fatalf("attempt %d: %+v\n%s", i, instance, f.logs)
		}
		lastID = instance.ID
		calls := f.turns.calls()
		if len(calls) != i+1 {
			t.Fatalf("attempt %d: %d calls", i, len(calls))
		}
		trigger := calls[len(calls)-1].Trigger.CommentID
		_, exists, err := f.db.GetRunByTrigger(ctx, session.ID, trigger)
		check(err)
		if exists {
			t.Fatal("prepare must fail before allocation")
		}
		// The fixture has created exactly one schedule per failed instance.
		scheduleID := int64(i + 1)
		if !store.IsScheduleTrigger(trigger) {
			t.Fatalf("not scheduled: %d", trigger)
		}
		scheduled, err := f.db.GetSchedule(ctx, scheduleID)
		check(err)
		if scheduled.TriggerID() != trigger || scheduled.Status != store.ScheduleFailed || scheduled.RunID != 0 {
			t.Fatalf("%+v", scheduled)
		}
	}
	f.settle()
	row, err = f.db.GetImplementStory(ctx, parent, 1, row.Key)
	check(err)
	if row.State != store.ImplementParked || row.ParkOrigin != store.ImplementOriginWorker || row.Attempts != 0 || !strings.Contains(row.Reason, "could not be opened") {
		t.Fatalf("%+v", row)
	}
	f.settle()
	after, err := f.db.GetImplementStory(ctx, parent, 1, row.Key)
	check(err)
	if runner.calls != 0 || len(f.turns.calls()) != 3 || after.Revision != row.Revision {
		t.Fatal("parked work repeated")
	}
}

// No process is expected for the deliberately broken clone.
type prepareFailureRunner struct{ calls int }

func (r *prepareFailureRunner) Run(context.Context, agent.Request) (*agent.RunResult, error) {
	r.calls++
	return nil, errors.New("unexpected agent launch")
}

func TestValidateImplementScheduleHandsOffUntilWorkerParks(t *testing.T) {
	f := newFixture(t)
	// Fresh-dispatch chaining also checks the store clock; keep future polls
	// in the future for both clocks.
	f.clock.advance(time.Since(f.clock.peek()) + time.Minute)
	ctx := t.Context()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	repo := gittest.New(t)
	repo.Git("clone", "--quiet", repo.Origin, f.repo.Path)
	f.repo.RemoteURL = repo.Origin
	var err error
	f.repo, err = f.db.UpsertRepo(ctx, f.repo)
	check(err)
	originalHandler := f.forge.server.Config.Handler
	f.forge.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/parent") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		originalHandler.ServeHTTP(w, r)
	})
	f.plantCursor()
	f.issueWithLabels("Reopened parent", "Mode: Implement")
	f.settle()
	session := f.session()
	parent := session.TaskRef
	check(f.db.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: parent, Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}))
	var revisions []store.ApprovedStoryRevision
	var seeds []store.ImplementStorySeed
	for i, key := range []string{"story-001", "story-002", "story-003"} {
		story := store.ManagedStory{ParentTask: parent, Generation: 1, Key: key, IssueNumber: 201 + i, IssueID: int64(7001 + i), State: "planning"}
		if i > 0 {
			story.Dependencies = []string{"story-001"}
		}
		check(f.db.PutManagedStory(ctx, story))
		revisions = append(revisions, store.ApprovedStoryRevision{ManagedStory: story, ParentID: 1230, Title: key, Body: key, Hash: strings.Repeat("cd", 32), FetchedAt: f.clock.peek()})
		seeds = append(seeds, store.ImplementStorySeed{Key: key, Position: i, Branch: "lf/" + key})
	}
	check(f.db.SetManagedFeatureReady(ctx, parent, 1, true))
	check(f.db.ApproveManagedStories(ctx, parent, 1, revisions))
	for _, r := range revisions {
		check(f.db.DeliverStoryScopeNotice(ctx, parent, 1, r.Key, "freeze"))
	}
	check(f.db.ActivateManagedScope(ctx, parent, 1))
	_, err = f.db.FreezeSessionSlug(ctx, session.ID, "feature", "lf/feature")
	check(err)
	repo.Git("push", "--quiet", "origin", "HEAD:refs/heads/lf/feature")
	check(f.db.EnsureImplementStories(ctx, parent, 1, seeds, f.clock.peek()))
	// Validate reopens previously merged rows in the same generation.
	for _, seed := range seeds {
		for _, states := range [][2]string{{store.ImplementWaiting, store.ImplementRunning}, {store.ImplementRunning, store.ImplementDone}, {store.ImplementDone, store.ImplementMerged}, {store.ImplementMerged, store.ImplementWaiting}} {
			row, e := f.db.GetImplementStory(ctx, parent, 1, seed.Key)
			check(e)
			transition := store.ImplementTransition{Parent: parent, Generation: 1, Key: seed.Key, From: states[0], To: states[1], ExpectedRevision: row.Revision, At: f.clock.peek()}
			if states[1] == store.ImplementMerged {
				transition.MergeCommit = repo.Git("rev-parse", "HEAD")
			}
			if states[0] == store.ImplementMerged {
				transition.Guidance = "Repair blocking Validate findings"
			}
			_, e = f.db.TransitionImplementStory(ctx, transition)
			check(e)
		}
	}
	binary, err := git.Resolve(ctx)
	check(err)
	authority, err := turn.NewAttemptAuthority(ctx, f.db)
	check(err)
	t.Cleanup(func() { check(authority.Close(context.WithoutCancel(ctx))) })
	opts := turn.Options{DB: f.db, Git: binary, Paths: f.paths, InstanceID: "coordinator-regression", Logger: f.logger, CloneGate: make(lifecycleCloneGate, 1), Runners: turn.RunnerSet{agent.Kind(store.AgentClaude): noAgentRunner{t}}, Author: git.Author{Name: "Test", Email: "test@example.invalid"}, ResearchReporter: f.reporter, SubIssueCreator: f.client, MCP: &turn.MCPRuntime{Authority: authority, Paths: f.paths, Executable: "/factory/litefactory", Now: f.clock.Now, Redactor: redact.New(redact.Options{}), Comments: f.client, BotID: botID}}
	opts.WorkflowSource = func(ctx context.Context, _ store.Repo, _ agent.Workflow, specs graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		definition, e := teamsource.CaptureImplement(ctx, specs)
		if e != nil {
			return nil, e
		}
		config := []byte("implement: implement/implement.dot\n")
		digest := sha256.Sum256(config)
		definition.Origin = teamsource.WorkflowOrigin{Kind: "local", Source: "local", Slug: "fixture", Version: "1.0.0", GraphPath: "implement/implement.dot", GraphFile: "implement.dot", Team: definition.Profile.Team, TeamConfig: config, ConfigDigest: hex.EncodeToString(digest[:])}
		return definition, nil
	}
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) { return turn.Execute(ctx, opts, req) })
	scheduled, _, err := f.db.CreateOrGetSchedule(ctx, store.CreateScheduleParams{SessionID: session.ID, SourceCommentID: 701, OwnerHandle: ownerLogin, Payload: turn.ValidateImplementPayload, DueAt: f.clock.peek()})
	check(err)
	f.settle()
	saved, err := f.db.GetSchedule(ctx, scheduled.ID)
	check(err)
	if saved.Status != store.ScheduleSucceeded || saved.RunID != 0 {
		t.Fatalf("schedule=%+v\n%s", saved, f.logs)
	}
	instance, err := f.db.LatestWorkflowInstance(ctx, session.ID, "implement")
	check(err)
	before, err := f.db.ListGraphNodeAttempts(ctx, instance.ID)
	check(err)
	pending, err := f.db.LatestGraphDispatch(ctx, instance.ID)
	check(err)
	if pending.Status != "pending" || pending.Node != "await" {
		t.Fatalf("dispatch=%+v", pending)
	}
	for i := 0; i < 65; i++ {
		f.settle()
	}
	after, err := f.db.ListGraphNodeAttempts(ctx, instance.ID)
	check(err)
	if len(after) != len(before) || !f.clock.peek().Before(pending.DueAt) {
		t.Fatalf("attempts %d -> %d, now=%s due=%s", len(before), len(after), f.clock.peek(), pending.DueAt)
	}
	row, err := f.db.GetImplementStory(ctx, parent, 1, "story-001")
	check(err)
	if row.State != store.ImplementRunning {
		t.Fatalf("worker=%+v", row)
	}
	_, err = f.db.TransitionImplementStory(ctx, store.ImplementTransition{Parent: parent, Generation: 1, Key: row.Key, From: store.ImplementRunning, To: store.ImplementParked, ExpectedRevision: row.Revision, Reason: "Sandbox launch blocked", At: f.clock.peek()})
	check(err)
	// The parent turn now succeeds while its stories cannot be swept (#1010),
	// so the woken step runs chained after it, which checks the store clock.
	_, err = f.db.WakeGraphInstance(ctx, instance.ID, "await", time.Now())
	check(err)
	f.settle()
	wait, err := f.db.GetGraphOwnerWait(ctx, instance.ID)
	check(err)
	if wait.State != "waiting" {
		t.Fatalf("wait=%+v", wait)
	}

	finalAttempts, err := f.db.ListGraphNodeAttempts(ctx, instance.ID)
	check(err)
	outcomes := map[string]string{}
	for _, attempt := range finalAttempts {
		outcomes[attempt.Node] = attempt.Outcome
	}
	for node, want := range map[string]string{"ready": "drained", "handoff": "posted", "any_parked": "true", "ask_owner": "published"} {
		if outcomes[node] != want {
			t.Fatalf("%s=%s want=%s", node, outcomes[node], want)
		}
	}
	for i := 0; i < 3; i++ {
		f.settle()
	}
	posts, _, _ := f.forge.snapshot()
	questions := 0
	for _, post := range posts {
		if strings.Contains(post, "Reply on this issue to restart the stopped stories.") {
			questions++
			if !strings.Contains(post, "Sandbox launch blocked") {
				t.Fatal(post)
			}
		}
	}
	if questions != 1 {
		t.Fatalf("questions=%d posts=%v", questions, posts)
	}
	for _, key := range []string{"story-002", "story-003"} {
		row, e := f.db.GetImplementStory(ctx, parent, 1, key)
		check(e)
		if row.State != store.ImplementWaiting {
			t.Fatalf("dependent=%+v", row)
		}
	}
}

func TestValidateImplementScheduleRequiresSuccessfulGraphReturn(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result turn.Result
		err    error
		want   store.ScheduleStatus
	}{
		{name: "locked", result: turn.Result{Disposition: turn.DispositionLocked, ResearchGraph: true}, want: store.SchedulePending},
		{name: "not executed", result: turn.Result{}, want: store.SchedulePending},
		{name: "admission error", err: errors.New("admission refused"), want: store.ScheduleFailed},
		{name: "activity error", result: turn.Result{ResearchGraph: true}, err: errors.New("activity failed"), want: store.ScheduleFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.plantCursor()
			f.issueWithLabels("Coordinator", "Mode: Implement")
			f.settle()
			session := f.session()
			scheduled, _, err := f.db.CreateOrGetSchedule(t.Context(), store.CreateScheduleParams{SessionID: session.ID, SourceCommentID: 701, OwnerHandle: ownerLogin, Payload: turn.ValidateImplementPayload, DueAt: f.clock.peek()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.db.AdmitWorkflowPath(t.Context(), session.ID, "graph"); err != nil {
				t.Fatal(err)
			}
			// Even a retained launch cannot prove its activities completed.
			definition, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "core", Mode: "implement", ProfileVersion: 1, Path: "implement/implement.dot", Digest: strings.Repeat("a", 64)})
			if err != nil {
				t.Fatal(err)
			}
			_, err = f.db.AdmitWorkflowLaunch(t.Context(), store.WorkflowLaunch{Mode: "implement", SessionID: session.ID, TriggerID: scheduled.TriggerID(), DefinitionID: definition.ID, Start: "merge", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: []byte(`{"implement.dot":"eA=="}`)})
			if err != nil {
				t.Fatal(err)
			}
			f.turns.set(func(context.Context, turn.Request) (turn.Result, error) { return tc.result, tc.err })
			f.settle()
			saved, err := f.db.GetSchedule(t.Context(), scheduled.ID)
			if err != nil {
				t.Fatal(err)
			}
			if saved.Status != tc.want || saved.RunID != 0 {
				t.Fatalf("schedule=%+v want=%s\n%s", saved, tc.want, f.logs)
			}
		})
	}
}
