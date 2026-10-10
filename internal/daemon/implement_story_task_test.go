package daemon

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

var implementDaemonClock = time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)

const implementDaemonParent = "github:acme/api#1"

type implementDaemonFixture struct {
	d      *Daemon
	repo   store.Repo
	parent store.Session
	byRepo map[int64]*repoQueue
}

// newImplementDaemonFixture builds a parent with four managed stories, the first
// three running in the ledger and the fourth still waiting.
func newImplementDaemonFixture(t *testing.T) *implementDaemonFixture {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(t.TempDir(), "w")})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := db.GetOrCreateSession(t.Context(), repo, 1)
	if err != nil || parent.TaskRef != implementDaemonParent {
		t.Fatalf("%+v %v", parent, err)
	}
	if err = db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: implementDaemonParent, Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}); err != nil {
		t.Fatal(err)
	}
	if err = db.CompareManagedFeature(t.Context(), store.ManagedFeature{ParentTask: implementDaemonParent, Generation: 1, State: "implementing"}, "planning"); err != nil {
		t.Fatal(err)
	}
	var seeds []store.ImplementStorySeed
	for i, key := range []string{"story-001", "story-002", "story-003", "story-004"} {
		if err = db.PutManagedStory(t.Context(), store.ManagedStory{ParentTask: implementDaemonParent, Generation: 1, Key: key, IssueNumber: 11 + i, IssueID: int64(900 + i), State: "planning"}); err != nil {
			t.Fatal(err)
		}
		seeds = append(seeds, store.ImplementStorySeed{Key: key, Position: i, Branch: "lf/" + key})
	}
	if err = db.EnsureImplementStories(t.Context(), implementDaemonParent, 1, seeds, implementDaemonClock); err != nil {
		t.Fatal(err)
	}
	f := &implementDaemonFixture{d: &Daemon{db: db}, repo: repo, parent: parent}
	for _, key := range seeds[:3] {
		f.move(key.Key, store.ImplementWaiting, store.ImplementRunning)
	}
	f.byRepo = map[int64]*repoQueue{repo.ID: {repo: repo, forgeRepo: github.Repo{Owner: "acme", Name: "api"}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	return f
}

func (f *implementDaemonFixture) row(t *testing.T, key string) store.ImplementStory {
	t.Helper()
	row, err := f.d.db.GetImplementStory(t.Context(), implementDaemonParent, 1, key)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func (f *implementDaemonFixture) move(key, from, to string) {
	row, err := f.d.db.GetImplementStory(context.Background(), implementDaemonParent, 1, key)
	if err != nil {
		panic(err)
	}
	if _, err := f.d.db.TransitionImplementStory(context.Background(), store.ImplementTransition{Parent: implementDaemonParent, Generation: 1, Key: key, From: from, To: to, ExpectedRevision: row.Revision, At: implementDaemonClock}); err != nil {
		panic(err)
	}
}

func TestImplementWorkIDFollowsTheStoryNotItsRepairs(t *testing.T) {
	f := newImplementDaemonFixture(t)
	row := f.row(t, "story-001")
	id := implementWorkID(row, 0)
	if id != implementWorkID(row, 0) {
		t.Fatal("the same work is the same identity")
	}
	if _, err := f.d.db.RecordImplementFailure(t.Context(), implementDaemonParent, 1, "story-001", "broke", implementDaemonClock.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if implementWorkID(f.row(t, "story-001"), 0) != id {
		t.Fatal("a failed attempt is repaired by the graph, not offered as new work")
	}
	if implementWorkID(row, 7) == id {
		t.Fatal("a worker instance that ended is followed by new work")
	}
	other := f.row(t, "story-002")
	if implementWorkID(other, 0) == id {
		t.Fatal("stories have their own work")
	}
}

func TestGraphImplementStoriesOffersEveryRunningStoryAsItsOwnTask(t *testing.T) {
	f := newImplementDaemonFixture(t)
	seen := map[int64]*candidate{}
	if err := f.d.graphImplementStories(t.Context(), f.byRepo, seen, implementDaemonClock); err != nil {
		t.Fatal(err)
	}
	q := f.byRepo[f.repo.ID]
	if len(q.candidates) != 3 || len(seen) != 3 {
		t.Fatalf("the three running stories are offered, the waiting one is not: %d candidates", len(q.candidates))
	}
	var numbers []int
	for _, c := range q.candidates {
		numbers = append(numbers, c.session.IssueNumber)
		if !slices.Contains(c.sources, "implement_story") {
			t.Errorf("sources = %v", c.sources)
		}
	}
	slices.Sort(numbers)
	if !slices.Equal(numbers, []int{11, 12, 13}) {
		t.Fatalf("each story is its own task on its own issue: %v", numbers)
	}
	for _, key := range []string{"story-001", "story-002", "story-003"} {
		row := f.row(t, key)
		session, err := f.d.db.GetOrCreateSession(t.Context(), f.repo, 11+int(key[len(key)-1]-'1'))
		if err != nil || row.WorkerSession != session.ID {
			t.Fatalf("%s: worker session %d, want %d (%v)", key, row.WorkerSession, session.ID, err)
		}
	}
	if row := f.row(t, "story-004"); row.WorkerSession != 0 {
		t.Fatalf("a waiting story has no worker: %+v", row)
	}
	// Offered again, the same story is one candidate, not two.
	if err := f.d.graphImplementStories(t.Context(), f.byRepo, seen, implementDaemonClock); err != nil {
		t.Fatal(err)
	}
	if len(q.candidates) != 3 {
		t.Fatalf("a story already offered this tick is not offered twice: %d", len(q.candidates))
	}
}

func TestGraphImplementStoriesSkipsAttemptedWorkAndLiveWorkers(t *testing.T) {
	f := newImplementDaemonFixture(t)
	if err := f.d.graphImplementStories(t.Context(), f.byRepo, map[int64]*candidate{}, implementDaemonClock); err != nil {
		t.Fatal(err)
	}
	first := f.row(t, "story-001")
	stored, _, err := f.d.db.CreateOrGetSchedule(t.Context(), store.CreateScheduleParams{SessionID: first.WorkerSession, SourceCommentID: implementWorkID(first, 0), OwnerHandle: "owner", Payload: implementWorkPayload, DueAt: implementDaemonClock})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := f.d.db.ReserveSchedule(t.Context(), stored.ID, implementDaemonClock); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := f.d.db.SetSessionState(t.Context(), f.row(t, "story-002").WorkerSession, store.SessionRunning); err != nil {
		t.Fatal(err)
	}
	f.byRepo[f.repo.ID].candidates = nil
	if err := f.d.graphImplementStories(t.Context(), f.byRepo, map[int64]*candidate{}, implementDaemonClock); err != nil {
		t.Fatal(err)
	}
	if got := f.byRepo[f.repo.ID].candidates; len(got) != 1 || got[0].session.IssueNumber != 13 {
		t.Fatalf("story-001 was started and story-002 has a live worker; only story-003 is offered: %d", len(got))
	}
}

func TestImplementWorkableStopsRetryingAFaultThatDoesNotGoAway(t *testing.T) {
	f := newImplementDaemonFixture(t)
	if err := f.d.graphImplementStories(t.Context(), f.byRepo, map[int64]*candidate{}, implementDaemonClock); err != nil {
		t.Fatal(err)
	}
	row := f.row(t, "story-001")
	if id, ok, err := f.d.implementWorkable(t.Context(), row); err != nil || !ok || id != 0 {
		t.Fatalf("no instance yet: %d %v %v", id, ok, err)
	}
	db := f.d.db
	pin, err := db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "implement", ProfileVersion: 1, Path: "implement/implement.dot", Digest: testDigest("a"), TeamSnapshotDigest: testDigest("b")})
	if err != nil {
		t.Fatal(err)
	}
	var last store.GraphInstance
	for i := range maxImplementInstanceFailures {
		last, err = db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: row.WorkerSession, DefinitionID: pin.ID, CurrentNode: "budget", StartedAt: row.StateAt.Add(time.Duration(i+1) * time.Second), NextDueAt: implementDaemonClock})
		if err != nil {
			t.Fatal(err)
		}
		if id, ok, err := f.d.implementWorkable(t.Context(), row); err != nil || ok || id != 0 {
			t.Fatalf("a running worker instance rests for its own dispatch: %d %v %v", id, ok, err)
		}
		if err = db.SetGraphInstanceStatus(t.Context(), last.ID, store.GraphFailed, time.Time{}, implementDaemonClock); err != nil {
			t.Fatal(err)
		}
		id, ok, err := f.d.implementWorkable(t.Context(), row)
		if err != nil {
			t.Fatal(err)
		}
		if want := i+1 < maxImplementInstanceFailures; ok != want || (ok && id != last.ID) {
			t.Fatalf("after %d failed instance(s): ok=%v id=%d", i+1, ok, id)
		}
	}
	parked := f.row(t, row.Key)
	if parked.State != store.ImplementParked || parked.ParkOrigin != store.ImplementOriginWorker || parked.Reason == "" {
		t.Fatalf("failure limit must park visibly: %+v", parked)
	}
	resumedAt := last.StartedAt.Add(time.Minute)
	for _, to := range []string{store.ImplementWaiting, store.ImplementRunning} {
		parked, err = db.TransitionImplementStory(t.Context(), store.ImplementTransition{
			Parent: parked.Parent, Generation: parked.Generation, Key: parked.Key,
			From: parked.State, To: to, ExpectedRevision: parked.Revision, At: resumedAt,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SetImplementWorkerSession(t.Context(), row.Parent, row.Generation, row.Key, row.WorkerSession, resumedAt); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := f.d.implementWorkable(t.Context(), f.row(t, row.Key)); err != nil || !ok {
		t.Fatalf("owner resume must reset the infrastructure window: %v %v", ok, err)
	}
}

func TestImplementCoordinatorDispatchIsRunlessOnlyForTheParent(t *testing.T) {
	f := newImplementDaemonFixture(t)
	if err := f.d.graphImplementStories(t.Context(), f.byRepo, map[int64]*candidate{}, implementDaemonClock); err != nil {
		t.Fatal(err)
	}
	db := f.d.db
	pin, err := db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "implement", ProfileVersion: 1, Path: "implement/implement.dot", Digest: testDigest("a"), TeamSnapshotDigest: testDigest("b")})
	if err != nil {
		t.Fatal(err)
	}
	worker := f.row(t, "story-001").WorkerSession
	for session, want := range map[int64]bool{f.parent.ID: true, worker: false} {
		instance, err := db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: session, DefinitionID: pin.ID, CurrentNode: "await", StartedAt: implementDaemonClock, NextDueAt: implementDaemonClock})
		if err != nil {
			t.Fatal(err)
		}
		dispatch, err := db.ParkGraphInstance(t.Context(), instance.ID, "await", implementDaemonClock, implementDaemonClock)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := f.d.implementCoordinatorDispatch(t.Context(), dispatch); err != nil || got != want {
			t.Fatalf("session %d: runless=%v want %v (%v)", session, got, want, err)
		}
	}
}

func testDigest(seed string) string {
	out := make([]byte, 64)
	for i := range out {
		out[i] = "0123456789abcdef"[(int(seed[0])+i)%16]
	}
	return string(out)
}

// A deploy that stops the daemon during a worker launch leaves the worker
// instance failed with its attempt still running. After a restart the attempt
// is closed and the story is offered again as new work, which admits a
// successor instance instead of being refused forever.
func TestImplementStoryRelaunchesAfterARestartInterruptedItsLaunch(t *testing.T) {
	f := newImplementDaemonFixture(t)
	if err := f.d.graphImplementStories(t.Context(), f.byRepo, map[int64]*candidate{}, implementDaemonClock); err != nil {
		t.Fatal(err)
	}
	db := f.d.db
	row := f.row(t, "story-001")
	pin, err := db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "implement", ProfileVersion: 1, Path: "implement/implement.dot", Digest: testDigest("a"), TeamSnapshotDigest: testDigest("b")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AdmitResearchPath(t.Context(), row.WorkerSession, "graph"); err != nil {
		t.Fatal(err)
	}
	launch := store.WorkflowLaunch{Mode: "implement", SessionID: row.WorkerSession, TriggerID: implementWorkID(row, 0), DefinitionID: pin.ID, Start: "budget", ContentDigest: testDigest("c"), TopologyDigest: testDigest("d"), Files: []byte(`{"role":"eA=="}`)}
	first, err := db.AdmitWorkflowLaunch(t.Context(), launch)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: first.Instance.ID, Node: "budget", Limit: 8, StartedAt: implementDaemonClock})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SetGraphInstanceStatus(t.Context(), first.Instance.ID, store.GraphFailed, time.Time{}, implementDaemonClock); err != nil {
		t.Fatal(err)
	}

	f.d.instanceID = "replacement"
	f.d.now = func() time.Time { return implementDaemonClock.Add(time.Hour) }
	f.d.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err = f.d.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got, err := db.GetGraphNodeAttempt(t.Context(), attempt.ID); err != nil || got.Status != store.GraphCancelled {
		t.Fatalf("startup left the orphaned attempt open: %+v %v", got, err)
	}

	f.byRepo[f.repo.ID].candidates = nil
	if err = f.d.graphImplementStories(t.Context(), f.byRepo, map[int64]*candidate{}, implementDaemonClock); err != nil {
		t.Fatal(err)
	}
	offered := false
	for _, c := range f.byRepo[f.repo.ID].candidates {
		offered = offered || c.session.ID == row.WorkerSession
	}
	if !offered {
		t.Fatal("the interrupted story is not offered again")
	}
	instanceID, ok, err := f.d.implementWorkable(t.Context(), f.row(t, "story-001"))
	if err != nil || !ok || instanceID != first.Instance.ID {
		t.Fatalf("retry does not follow the failed instance: %d %v %v", instanceID, ok, err)
	}
	retry := implementWorkID(f.row(t, "story-001"), instanceID)
	if retry == launch.TriggerID {
		t.Fatal("the retry reuses the spent work identity")
	}
	if attempted, err := db.ScheduleAttempted(t.Context(), row.WorkerSession, retry); err != nil || attempted {
		t.Fatalf("the retry identity is already spent: %v %v", attempted, err)
	}
	launch.TriggerID = retry
	successor, err := db.AdmitWorkflowLaunch(t.Context(), launch)
	if err != nil || successor.PredecessorID != first.Instance.ID || successor.Instance.ID == first.Instance.ID {
		t.Fatalf("the relaunch was refused: %+v %v", successor, err)
	}
}

// failImplementSchedule records the story work under id as a launch that failed
// before any worker instance started.
func failImplementSchedule(t *testing.T, f *implementDaemonFixture, session, id int64) {
	t.Helper()
	stored, _, err := f.d.db.CreateOrGetSchedule(t.Context(), store.CreateScheduleParams{SessionID: session, SourceCommentID: id, OwnerHandle: "owner", Payload: implementWorkPayload, DueAt: implementDaemonClock})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := f.d.db.ReserveSchedule(t.Context(), stored.ID, implementDaemonClock); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, err := f.d.db.FinishSchedule(t.Context(), stored.ID, store.ScheduleFailed, 0, implementDaemonClock); err != nil {
		t.Fatal(err)
	}
}

func offeredImplementStory(t *testing.T, f *implementDaemonFixture, session int64) bool {
	t.Helper()
	f.byRepo[f.repo.ID].candidates = nil
	if err := f.d.graphImplementStories(t.Context(), f.byRepo, map[int64]*candidate{}, implementDaemonClock); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.byRepo[f.repo.ID].candidates {
		if c.session.ID == session {
			return true
		}
	}
	return false
}

func implementWorkerTask(t *testing.T, f *implementDaemonFixture, session int64) *task {
	t.Helper()
	s, err := f.d.db.GetSession(t.Context(), session)
	if err != nil {
		t.Fatal(err)
	}
	return &task{session: s, tickAt: implementDaemonClock}
}

// A relaunch whose schedule failed before admitting a successor instance (a
// refused launch, or one orphaned by a restart) moves the story to fresh work
// instead of leaving it Implementing with nothing running.
func TestImplementStoryRelaunchesAfterItsRetryLaunchFailed(t *testing.T) {
	for _, withInstance := range []bool{true, false} {
		f := newImplementDaemonFixture(t)
		if err := f.d.graphImplementStories(t.Context(), f.byRepo, map[int64]*candidate{}, implementDaemonClock); err != nil {
			t.Fatal(err)
		}
		row := f.row(t, "story-001")
		var instanceID int64
		if withInstance {
			pin, err := f.d.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "implement", ProfileVersion: 1, Path: "implement/implement.dot", Digest: testDigest("a"), TeamSnapshotDigest: testDigest("b")})
			if err != nil {
				t.Fatal(err)
			}
			instance, err := f.d.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: row.WorkerSession, DefinitionID: pin.ID, CurrentNode: "budget", StartedAt: row.StateAt.Add(time.Second), NextDueAt: implementDaemonClock})
			if err != nil {
				t.Fatal(err)
			}
			if err = f.d.db.SetGraphInstanceStatus(t.Context(), instance.ID, store.GraphFailed, time.Time{}, implementDaemonClock); err != nil {
				t.Fatal(err)
			}
			instanceID = instance.ID
		}
		spent := implementWorkID(row, instanceID)
		failImplementSchedule(t, f, row.WorkerSession, spent)
		if !offeredImplementStory(t, f, row.WorkerSession) {
			t.Fatalf("instance=%v: the story whose launch failed is not offered again", withInstance)
		}
		id, ok, err := f.d.implementWorkFor(t.Context(), implementWorkerTask(t, f, row.WorkerSession))
		if err != nil || !ok || id == spent {
			t.Fatalf("instance=%v: relaunch identity %d ok=%v (%v), spent %d", withInstance, id, ok, err, spent)
		}
		if attempted, err := f.d.db.ScheduleAttempted(t.Context(), row.WorkerSession, id); err != nil || attempted {
			t.Fatalf("instance=%v: the relaunch identity is already spent: %v %v", withInstance, attempted, err)
		}
	}
}

// Failed instances and refused launches share the infrastructure failure
// limit; reaching it parks the story with a reason for the owner to resume.
func TestImplementStoryParksAfterRefusedLaunchesReachTheLimit(t *testing.T) {
	f := newImplementDaemonFixture(t)
	if err := f.d.graphImplementStories(t.Context(), f.byRepo, map[int64]*candidate{}, implementDaemonClock); err != nil {
		t.Fatal(err)
	}
	row := f.row(t, "story-001")
	for retry := range maxImplementInstanceFailures {
		failImplementSchedule(t, f, row.WorkerSession, implementRetryWorkID(row, 0, retry))
	}
	if offeredImplementStory(t, f, row.WorkerSession) {
		t.Fatal("a story at the failure limit is still offered")
	}
	parked := f.row(t, row.Key)
	if parked.State != store.ImplementParked || parked.ParkOrigin != store.ImplementOriginWorker || parked.Reason == "" {
		t.Fatalf("failure limit must park visibly: %+v", parked)
	}
}

// A launch in flight under the current identity is not stepped past.
func TestImplementStoryIsNotRelaunchedWhileItsLaunchIsInFlight(t *testing.T) {
	f := newImplementDaemonFixture(t)
	if err := f.d.graphImplementStories(t.Context(), f.byRepo, map[int64]*candidate{}, implementDaemonClock); err != nil {
		t.Fatal(err)
	}
	row := f.row(t, "story-001")
	stored, _, err := f.d.db.CreateOrGetSchedule(t.Context(), store.CreateScheduleParams{SessionID: row.WorkerSession, SourceCommentID: implementWorkID(row, 0), OwnerHandle: "owner", Payload: implementWorkPayload, DueAt: implementDaemonClock})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := f.d.db.ReserveSchedule(t.Context(), stored.ID, implementDaemonClock); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if offeredImplementStory(t, f, row.WorkerSession) {
		t.Fatal("a story whose launch is in flight is offered again")
	}
	id, ok, err := f.d.implementWorkFor(t.Context(), implementWorkerTask(t, f, row.WorkerSession))
	if err != nil || !ok || id != implementWorkID(row, 0) {
		t.Fatalf("in-flight identity %d ok=%v (%v)", id, ok, err)
	}
}
