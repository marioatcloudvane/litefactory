package store

import (
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestStandardsRunCompetingBindingsAndSelectionVersions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.db")
	db, e := Open(t.Context(), path)
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	if e = db.Migrate(t.Context()); e != nil {
		t.Fatal(e)
	}
	other, e := Open(t.Context(), path)
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if err := other.Close(); err != nil {
			t.Error(err)
		}
	}()
	var wg sync.WaitGroup
	winners := make(chan StandardsRunBinding, 2)
	failures := make(chan error, 2)
	for i, handle := range []*DB{db, other} {
		wg.Add(1)
		go func(i int, handle *DB) {
			defer wg.Done()
			b, e := handle.SaveStandardsRunBinding(t.Context(), StandardsRunBinding{RootRunID: "root", RevisionID: int64(i + 1), Outcome: "unavailable"})
			if e != nil {
				failures <- e
			} else {
				winners <- b
			}
		}(i, handle)
	}
	wg.Wait()
	close(failures)
	close(winners)
	var first int64
	for e := range failures {
		if !errors.Is(e, ErrStandardsConflict) {
			t.Fatal(e)
		}
	}
	for b := range winners {
		if first == 0 {
			first = b.RevisionID
		}
		if b.RevisionID != first {
			t.Fatal("immutable bind replaced")
		}
	}
	var version int64
	for i, handle := range []*DB{db, other} {
		s, e := handle.SaveStandardsRunSelection(t.Context(), StandardsRunSelection{RootRunID: "root", ConsumerKey: "worker", Digest: string(rune('a' + i)), CategoryIDs: []string{string(rune('a' + i))}}, 0)
		if i == 0 {
			if e != nil {
				t.Fatal(e)
			}
			version = s.Version
		} else if !errors.Is(e, ErrStandardsConflict) {
			t.Fatalf("stale selection version %d: %v", version, e)
		}
	}
}

func TestStandardsRootGraphRecoveryAndNewOwner(t *testing.T) {
	db, launch := captureFixture(t)
	ctx := t.Context()
	admitted, err := db.AdmitWorkflowLaunch(ctx, launch)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := db.GetOrCreateSegment(ctx, launch.SessionID, 1, "standards-root", AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	allocate := func(trigger int64) Run {
		t.Helper()
		r, e := db.AllocateRun(ctx, AllocateRunParams{SessionID: launch.SessionID, SegmentID: segment.ID, Mode: "research", Agent: AgentClaude, TriggerCommentID: trigger, Prelaunch: true, StartedAt: graphTestTime(), LogPath: func(int) string { return "root.log" }})
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	original := allocate(launch.TriggerID)
	check := func(run, expected int64) {
		t.Helper()
		got, e := db.ResolveStandardsRoot(ctx, run)
		if e != nil || got != expected {
			t.Fatalf("root %d=%d want %d: %v", run, got, expected, e)
		}
	}
	check(original.ID, original.ID)
	dispatch, err := db.ParkGraphInstance(ctx, admitted.Instance.ID, admitted.Instance.CurrentNode, graphTestTime(), graphTestTime())
	if err != nil {
		t.Fatal(err)
	}
	child := allocate(dispatch.TriggerID())
	check(child.ID, original.ID)
	fallback := allocate(55)
	if err = db.LinkStandardsRun(ctx, fallback.ID, original.ID); err != nil {
		t.Fatal(err)
	}
	check(fallback.ID, original.ID)
	attempt, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: admitted.Instance.ID, LaunchID: admitted.LaunchID, Node: admitted.Instance.CurrentNode, Limit: 8, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	reviewer := allocate(56)
	if err = db.LinkStandardsGraphRun(ctx, reviewer.ID, attempt.ID); err != nil {
		t.Fatal(err)
	}
	check(reviewer.ID, original.ID)
	recovery := allocate(57)
	stamp := FormatTime(graphTestTime())
	if _, err = db.sql.ExecContext(ctx, `INSERT INTO automatic_continuations(source_run_id,root_run_id,session_id,segment_id,attempt,due_at,reset_bound,precision,context_json,created_at,allocated_run_id) VALUES(?,?,?,?,1,?,?, 'exact','{}',?,?)`, child.ID, original.ID, launch.SessionID, segment.ID, stamp, stamp, stamp, recovery.ID); err != nil {
		t.Fatal(err)
	}
	check(recovery.ID, original.ID)
	if err = db.CompleteGraphNodeAttempt(ctx, attempt.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "success", FinishedAt: graphTestTime(), InstanceStatus: GraphRunning}); err != nil {
		t.Fatal(err)
	}
	next := launch
	next.TriggerID = 58
	if _, err = db.AdmitWorkflowLaunch(ctx, next); err != nil {
		t.Fatal(err)
	}
	newOwner := allocate(next.TriggerID)
	check(newOwner.ID, newOwner.ID)
}

func TestStandardsRunMutationsRecheckAttemptLease(t *testing.T) {
	db, lease := mcpLeaseFixture(t)
	ctx := t.Context()
	if _, err := db.SaveStandardsRunBinding(ctx, StandardsRunBinding{RootRunID: "lease-root", Outcome: "unavailable"}); err != nil {
		t.Fatal(err)
	}
	stamped := WithMCPLease(ctx, lease)
	choice := StandardsRunSelection{RootRunID: "lease-root", ConsumerKey: "worker", Digest: "selection", AttemptID: "forged"}
	if _, err := db.SaveStandardsRunSelection(stamped, choice, 0); !errors.Is(err, ErrResultAuthority) {
		t.Fatalf("forged attempt: %v", err)
	}
	if err := db.RevokeMCPLease(ctx, lease, "cancelled"); err != nil {
		t.Fatal(err)
	}
	choice.AttemptID = lease.Binding.Attempt
	if _, err := db.SaveStandardsRunSelection(stamped, choice, 0); !errors.Is(err, ErrResultAuthority) {
		t.Fatalf("revoked selection write: %v", err)
	}
	if err := db.AppendStandardsRunObservation(stamped, StandardsRunObservation{RootRunID: "lease-root", ConsumerKey: "worker", AttemptID: lease.Binding.Attempt, Kind: "offered"}); !errors.Is(err, ErrResultAuthority) {
		t.Fatalf("revoked observation write: %v", err)
	}
}

func TestStandardsRootPrelaunchDispatchWithoutOwnerRun(t *testing.T) {
	db, launch := captureFixture(t)
	ctx := t.Context()
	admitted, err := db.AdmitWorkflowLaunch(ctx, launch)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := db.GetOrCreateSegment(ctx, launch.SessionID, 1, "prelaunch", AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err := db.ParkGraphInstance(ctx, admitted.Instance.ID, admitted.Instance.CurrentNode, graphTestTime(), graphTestTime())
	if err != nil {
		t.Fatal(err)
	}
	allocate := func(trigger int64) Run {
		t.Helper()
		run, e := db.AllocateRun(ctx, AllocateRunParams{SessionID: launch.SessionID, SegmentID: segment.ID, Mode: "research", Agent: AgentClaude, TriggerCommentID: trigger, Prelaunch: true, LogPath: func(int) string { return "prelaunch.log" }})
		if e != nil {
			t.Fatal(e)
		}
		return run
	}
	first := allocate(dispatch.TriggerID())
	// Another reserved dispatch is not needed to resolve identity: the first run's
	// private trigger already proves its launch before StartRun attaches ownership.
	reviewerAttempt, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: admitted.Instance.ID, LaunchID: admitted.LaunchID, Node: admitted.Instance.CurrentNode, Limit: 8, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	second := allocate(77)
	if err = db.LinkStandardsGraphRun(ctx, second.ID, reviewerAttempt.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{first.ID, second.ID} {
		root, e := db.ResolveStandardsRoot(ctx, id)
		if e != nil || root != first.ID {
			t.Fatalf("prelaunch root %d=%d want %d: %v", id, root, first.ID, e)
		}
	}
}

func TestStandardsRootAutomaticPrelaunchUsesReservedContinuation(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	session := testSession(t, db, 123)
	original := testRun(t, db, session.ID, 42)
	outcome, intent := recoveryIntent(original)
	_, continuation, err := db.FinishInterruptedRun(ctx, original.ID, outcome, intent, "retained-resume")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, e := db.ReserveContinuation(ctx, continuation.ID, continuation.DueAt); e != nil || !ok {
		t.Fatalf("reserve: %v %v", ok, e)
	}
	resumed, err := db.AllocateRun(ctx, AllocateRunParams{SessionID: original.SessionID, SegmentID: original.SegmentID, Mode: original.Mode, Agent: original.Agent, TriggerCommentID: continuation.TriggerID(), Prelaunch: true, LogPath: func(int) string { return "automatic-prelaunch.log" }})
	if err != nil {
		t.Fatal(err)
	}
	retained, err := db.GetContinuation(ctx, continuation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retained.AllocatedRunID != 0 || retained.Status != "reserved" {
		t.Fatalf("fixture crossed launch boundary: %+v", retained)
	}
	root, err := db.ResolveStandardsRoot(ctx, resumed.ID)
	if err != nil || root != original.ID {
		t.Fatalf("prelaunch recovery root %d want %d: %v", root, original.ID, err)
	}
	if _, err = db.StartRun(ctx, resumed.ID); err != nil {
		t.Fatal(err)
	}
	root, err = db.ResolveStandardsRoot(ctx, resumed.ID)
	if err != nil || root != original.ID {
		t.Fatalf("started recovery root %d want %d: %v", root, original.ID, err)
	}
}

func TestStandardsRunArchiveRetainsInspectableBinding(t *testing.T) {
	db, launch := captureFixture(t)
	ctx := t.Context()
	admitted, err := db.AdmitWorkflowLaunch(ctx, launch)
	if err != nil {
		t.Fatal(err)
	}
	original := testRun(t, db, launch.SessionID, launch.TriggerID)
	root := strconv.FormatInt(original.ID, 10)
	attempt, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: admitted.Instance.ID, LaunchID: admitted.LaunchID, RunID: original.ID, Node: admitted.Instance.CurrentNode, Limit: 8, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	reviewer := testRun(t, db, launch.SessionID, 99)
	if err = db.LinkStandardsGraphRun(ctx, reviewer.ID, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if err = db.LinkStandardsRun(ctx, original.ID, original.ID); err != nil {
		t.Fatal(err)
	}
	if err = db.LinkStandardsRun(ctx, reviewer.ID, original.ID); err != nil {
		t.Fatal(err)
	}
	binding, err := db.SaveStandardsRunBinding(ctx, StandardsRunBinding{RootRunID: root, Digest: "retained-digest", Source: "local-published-book", Outcome: "verified", SchemaVersion: 1, SnapshotBytes: []byte(`{"categories":[],"standards":[]}`)})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := db.SaveStandardsRunSelection(ctx, StandardsRunSelection{RootRunID: root, ConsumerKey: "worker", Digest: "selection", CategoryIDs: []string{}, StandardIDs: []string{}, AttemptID: "one", Reason: "explicit empty"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AppendStandardsRunObservation(ctx, StandardsRunObservation{RootRunID: root, ConsumerKey: "worker", AttemptID: "one", Kind: "acknowledgment", SelectionVersion: selection.Version}); err != nil {
		t.Fatal(err)
	}
	finished := graphTestTime().Add(time.Second)
	if err = db.CompleteGraphNodeAttempt(ctx, attempt.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "success", FinishedAt: finished, InstanceStatus: GraphSucceeded}); err != nil {
		t.Fatal(err)
	}
	for _, run := range []Run{original, reviewer} {
		if _, err = db.FinishRun(ctx, run.ID, RunOutcome{Status: RunSucceeded, FinishedAt: finished}); err != nil {
			t.Fatal(err)
		}
		if _, err = db.CompleteRunReport(ctx, run.ID, 100+run.ID); err != nil {
			t.Fatal(err)
		}
	}
	session, err := db.GetSession(ctx, launch.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	family := ArchiveFamily{SessionID: session.ID, RepoID: session.RepoID, TaskRef: session.TaskRef, IssueClosed: true}
	deleted, err := db.DeleteArchivedFamily(ctx, family, archiveTestTrailer(t, db, family), finished.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if deleted["standards_graph_run_aliases"] != 1 || deleted["research_launch_definitions"] != 1 {
		t.Fatalf("graph retention metadata not archived: %v", deleted)
	}
	kept, err := db.GetStandardsRunBinding(ctx, root)
	if err != nil || kept.Digest != binding.Digest || string(kept.SnapshotBytes) != string(binding.SnapshotBytes) {
		t.Fatalf("binding lost: %+v %v", kept, err)
	}
	choices, err := db.ListStandardsRunSelections(ctx, root, "worker")
	if err != nil || len(choices) != 1 {
		t.Fatalf("selection lost: %+v %v", choices, err)
	}
	observations, err := db.ListStandardsRunObservations(ctx, root, "worker")
	if err != nil || len(observations) != 1 {
		t.Fatalf("observations lost: %+v %v", observations, err)
	}
	for _, run := range []Run{original, reviewer} {
		if _, err = db.GetRun(ctx, run.ID); err != nil {
			t.Fatal(err)
		}
		resolved, e := db.ResolveStandardsRoot(ctx, run.ID)
		if e != nil || resolved != original.ID {
			t.Fatalf("archived root %d=%d want %d: %v", run.ID, resolved, original.ID, e)
		}
	}
	if _, err = db.sql.ExecContext(ctx, `DELETE FROM standards_run_bindings WHERE root_run_id=?`, root); err == nil {
		t.Fatal("inspectable binding allowed deletion")
	}
	forgotten, err := db.ForgetArchivedFamily(ctx, family, finished.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"standards_run_bindings", "standards_run_selections", "standards_run_observations", "standards_run_aliases"} {
		if forgotten[table] == 0 {
			t.Fatalf("forget left standards table %s: %v", table, forgotten)
		}
	}
	if _, err = db.GetStandardsRunBinding(ctx, root); !errors.Is(err, ErrNotFound) {
		t.Fatalf("forgotten binding remained: %v", err)
	}
	// SQLite can reuse the highest deleted run ID. An explicit reused ID must not
	// recover guidance from the forgotten invocation.
	newSession := testSession(t, db, 124)
	newRun := testRun(t, db, newSession.ID, 1000)
	if newRun.ID != original.ID {
		t.Fatalf("fixture did not reuse run ID: %d want %d", newRun.ID, original.ID)
	}
	if _, err = db.GetStandardsRunBinding(ctx, strconv.FormatInt(newRun.ID, 10)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("new invocation inherited forgotten guidance: %v", err)
	}

}

func TestStandardsMechanicalGraphAnchorsAndWorkerHandoff(t *testing.T) {
	db, launch := captureFixture(t)
	ctx := t.Context()
	source, err := db.AdmitWorkflowLaunch(ctx, launch)
	if err != nil {
		t.Fatal(err)
	}
	root, err := db.RecordStandardsGraphInvocation(ctx, source.LaunchID, 0)
	if err != nil || root >= 0 {
		t.Fatalf("mechanical anchor: %d %v", root, err)
	}
	retry, err := db.RecordStandardsGraphInvocation(ctx, source.LaunchID, 0)
	if err != nil || retry != root {
		t.Fatalf("source rebound: %d %v", retry, err)
	}
	if _, err = db.RecordStandardsGraphInvocation(ctx, source.LaunchID, root-1); !errors.Is(err, ErrStandardsConflict) {
		t.Fatalf("changed source root: %v", err)
	}
	sourceSession, err := db.GetSession(ctx, launch.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SaveStandardsStoryInvocation(ctx, sourceSession.TaskRef, 1, "story-one", graphTestTime(), root); err != nil {
		t.Fatal(err)
	}
	inherited, err := db.GetStandardsStoryInvocation(ctx, sourceSession.TaskRef, 1, "story-one", graphTestTime())
	if err != nil || inherited != root {
		t.Fatalf("work handoff: %d %v", inherited, err)
	}
	if _, err = db.GetStandardsStoryInvocation(ctx, sourceSession.TaskRef, 1, "story-one", graphTestTime().Add(time.Second)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("work timestamp crossed invocation: %v", err)
	}
	child := testSession(t, db, 125)
	if _, err = db.AdmitWorkflowPath(ctx, child.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	childLaunch := launch
	childLaunch.SessionID = child.ID
	childLaunch.TriggerID = 200
	admittedChild, err := db.AdmitWorkflowLaunch(ctx, childLaunch)
	if err != nil {
		t.Fatal(err)
	}
	if got, e := db.RecordStandardsGraphInvocation(ctx, admittedChild.LaunchID, inherited); e != nil || got != root {
		t.Fatalf("child root: %d %v", got, e)
	}
	worker := testRun(t, db, child.ID, childLaunch.TriggerID)
	resolved, err := db.ResolveStandardsRoot(ctx, worker.ID)
	if err != nil || resolved != root {
		t.Fatalf("cross-session root: %d %v", resolved, err)
	}
	if err = db.LinkStandardsRun(ctx, worker.ID, root); err != nil {
		t.Fatal(err)
	}
	newOwner := childLaunch
	newOwner.TriggerID = 201
	next, err := db.AdmitWorkflowLaunch(ctx, newOwner)
	if err != nil {
		t.Fatal(err)
	}
	nextRoot, err := db.RecordStandardsGraphInvocation(ctx, next.LaunchID, 0)
	if err != nil || nextRoot == root || nextRoot >= 0 {
		t.Fatalf("new owner reused root: %d %v", nextRoot, err)
	}
}

func TestStandardsGraphAnchorSurvivesArchivedLaunchIDReuse(t *testing.T) {
	db, launch := captureFixture(t)
	ctx := t.Context()
	source, err := db.AdmitWorkflowLaunch(ctx, launch)
	if err != nil {
		t.Fatal(err)
	}
	root, err := db.RecordStandardsGraphInvocation(ctx, source.LaunchID, 0)
	if err != nil {
		t.Fatal(err)
	}
	rootKey := strconv.FormatInt(root, 10)
	if _, err = db.SaveStandardsRunBinding(ctx, StandardsRunBinding{RootRunID: rootKey, Outcome: "unavailable", Reason: "retained source"}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(ctx, `DELETE FROM standards_run_bindings WHERE root_run_id=?`, rootKey); err == nil {
		t.Fatal("mechanical source binding allowed deletion")
	}
	attempt, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: source.Instance.ID, LaunchID: source.LaunchID, Node: source.Instance.CurrentNode, Limit: 8, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteGraphNodeAttempt(ctx, attempt.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "success", FinishedAt: graphTestTime(), InstanceStatus: GraphSucceeded}); err != nil {
		t.Fatal(err)
	}
	session, err := db.GetSession(ctx, launch.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	family := ArchiveFamily{SessionID: session.ID, RepoID: session.RepoID, TaskRef: session.TaskRef, IssueClosed: true}
	if _, err = db.DeleteArchivedFamily(ctx, family, archiveTestTrailer(t, db, family), graphTestTime().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	nextLaunch := launch
	nextLaunch.TriggerID++
	if _, err = db.AdmitWorkflowPath(ctx, session.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	next, err := db.AdmitWorkflowLaunch(ctx, nextLaunch)
	if err != nil {
		t.Fatal(err)
	}
	if next.LaunchID != source.LaunchID {
		t.Fatalf("fixture did not reuse launch ID: %d want %d", next.LaunchID, source.LaunchID)
	}
	nextRoot, err := db.RecordStandardsGraphInvocation(ctx, next.LaunchID, 0)
	if err != nil || nextRoot == root {
		t.Fatalf("reused launch inherited root: %d %v", nextRoot, err)
	}
	retained, err := db.GetStandardsRunBinding(ctx, rootKey)
	if err != nil || retained.Reason != "retained source" {
		t.Fatalf("archived source binding lost: %+v %v", retained, err)
	}
}

func TestStandardsSharedMechanicalRootSurvivesIndependentWorkerForget(t *testing.T) {
	db, launch := captureFixture(t)
	ctx := t.Context()
	source, err := db.AdmitWorkflowLaunch(ctx, launch)
	if err != nil {
		t.Fatal(err)
	}
	root, err := db.RecordStandardsGraphInvocation(ctx, source.LaunchID, 0)
	if err != nil {
		t.Fatal(err)
	}
	rootKey := strconv.FormatInt(root, 10)
	if _, err = db.SaveStandardsRunBinding(ctx, StandardsRunBinding{RootRunID: rootKey, Outcome: "unavailable", Reason: "shared parent snapshot"}); err != nil {
		t.Fatal(err)
	}
	sessions := []Session{}
	runs := []Run{}
	children := []WorkflowAdmission{}
	for i := range 2 {
		session := testSession(t, db, 125+i)
		if _, err = db.AdmitWorkflowPath(ctx, session.ID, "graph"); err != nil {
			t.Fatal(err)
		}
		childLaunch := launch
		childLaunch.SessionID = session.ID
		childLaunch.TriggerID = 200 + int64(i)
		admitted, err := db.AdmitWorkflowLaunch(ctx, childLaunch)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.RecordStandardsGraphInvocation(ctx, admitted.LaunchID, root); err != nil {
			t.Fatal(err)
		}
		run := testRun(t, db, session.ID, childLaunch.TriggerID)
		if err = db.LinkStandardsRun(ctx, run.ID, root); err != nil {
			t.Fatal(err)
		}
		sessions = append(sessions, session)
		runs = append(runs, run)
		children = append(children, admitted)
	}
	first := runs[0]
	attempt, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: children[0].Instance.ID, LaunchID: children[0].LaunchID, RunID: first.ID, Node: children[0].Instance.CurrentNode, Limit: 8, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	finished := first.StartedAt.Add(time.Second)
	if err = db.CompleteGraphNodeAttempt(ctx, attempt.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "success", FinishedAt: finished, InstanceStatus: GraphSucceeded}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.FinishRun(ctx, first.ID, RunOutcome{Status: RunSucceeded, FinishedAt: finished}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.CompleteRunReport(ctx, first.ID, 777); err != nil {
		t.Fatal(err)
	}
	family := ArchiveFamily{SessionID: sessions[0].ID, RepoID: sessions[0].RepoID, TaskRef: sessions[0].TaskRef, IssueClosed: true}
	if _, err = db.DeleteArchivedFamily(ctx, family, archiveTestTrailer(t, db, family), finished.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// Recovery can still resolve its source's immutable alias after the source
	// graph metadata is gone; no current graph or book lookup is needed.
	recovery := testRun(t, db, sessions[1].ID, 999)
	stamp := FormatTime(finished)
	if _, err = db.sql.ExecContext(ctx, `INSERT INTO automatic_continuations(source_run_id,root_run_id,session_id,segment_id,attempt,due_at,reset_bound,precision,context_json,created_at,allocated_run_id) VALUES(?,?,?,?,1,?,?,'exact','{}',?,?)`, recovery.ID, first.ID, sessions[1].ID, recovery.SegmentID, stamp, stamp, stamp, recovery.ID); err != nil {
		t.Fatal(err)
	}
	resolved, err := db.ResolveStandardsRoot(ctx, recovery.ID)
	if err != nil || resolved != root {
		t.Fatalf("archived recovery source lost negative root: %d %v", resolved, err)
	}
	if _, err = db.sql.ExecContext(ctx, `DELETE FROM automatic_continuations WHERE allocated_run_id=?`, recovery.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ForgetArchivedFamily(ctx, family, finished.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	retained, err := db.GetStandardsRunBinding(ctx, rootKey)
	if err != nil || retained.Reason != "shared parent snapshot" {
		t.Fatalf("child forget removed shared guidance: %+v %v", retained, err)
	}
	resolved, err = db.ResolveStandardsRoot(ctx, runs[1].ID)
	if err != nil || resolved != root {
		t.Fatalf("sibling mapping lost: %d %v", resolved, err)
	}
	if _, err = db.sql.ExecContext(ctx, `DELETE FROM standards_run_bindings WHERE root_run_id=?`, rootKey); err == nil {
		t.Fatal("remaining source/sibling did not protect shared root")
	}
}

func TestStandardsScheduledInvocationFreezesExactSource(t *testing.T) {
	db, launch := captureFixture(t)
	ctx := t.Context()
	source, err := db.AdmitWorkflowLaunch(ctx, launch)
	if err != nil {
		t.Fatal(err)
	}
	root, err := db.RecordStandardsGraphInvocation(ctx, source.LaunchID, 0)
	if err != nil {
		t.Fatal(err)
	}
	schedule, _, err := db.CreateOrGetSchedule(ctx, CreateScheduleParams{SessionID: launch.SessionID, SourceCommentID: 700, OwnerHandle: "owner", Payload: "factory work", DueAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	inherited, err := db.RecordStandardsScheduledInvocation(ctx, launch.SessionID, schedule.TriggerID(), root)
	if err != nil || inherited != root {
		t.Fatalf("schedule source: %d %v", inherited, err)
	}
	if _, err = db.RecordStandardsScheduledInvocation(ctx, launch.SessionID, schedule.TriggerID(), root-1); !errors.Is(err, ErrStandardsConflict) {
		t.Fatalf("schedule source changed: %v", err)
	}
	stored, err := db.GetStandardsScheduledInvocation(ctx, launch.SessionID, schedule.TriggerID())
	if err != nil || stored != root {
		t.Fatalf("retained schedule source: %d %v", stored, err)
	}
	workerLaunch := launch
	workerLaunch.TriggerID = schedule.TriggerID()
	worker, err := db.AdmitWorkflowLaunch(ctx, workerLaunch)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := db.RecordStandardsGraphInvocation(ctx, worker.LaunchID, 0)
	if err != nil || resolved != root {
		t.Fatalf("graph recaptured schedule source: %d %v", resolved, err)
	}
	missing, _, err := db.CreateOrGetSchedule(ctx, CreateScheduleParams{SessionID: launch.SessionID, SourceCommentID: 701, OwnerHandle: "owner", Payload: "historical factory work", DueAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	unavailable, err := db.RecordStandardsScheduledInvocation(ctx, launch.SessionID, missing.TriggerID(), 0)
	if err != nil || unavailable >= 0 || unavailable == root {
		t.Fatalf("missing source anchor: %d %v", unavailable, err)
	}
	binding, err := db.GetStandardsRunBinding(ctx, strconv.FormatInt(unavailable, 10))
	if err != nil || binding.Outcome != "unavailable" || binding.RevisionID != 0 || len(binding.SnapshotBytes) != 0 || binding.Reason == "" {
		t.Fatalf("missing source was not retained unavailable: %+v %v", binding, err)
	}
}
