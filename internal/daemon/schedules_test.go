package daemon_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/store"
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
