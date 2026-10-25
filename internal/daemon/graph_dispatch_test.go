package daemon

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// The daemon cannot call a turn's own validation, so the rules it has to
// satisfy are asserted here instead. A live dispatch failed every tick with
// "free_text has no instruction text" because nothing covered this seam: the
// selection was tested, the workflow was tested, and the request between them
// was not.
func TestGraphStepRequestIsAcceptableToATurn(t *testing.T) {
	t.Parallel()
	dispatch := store.GraphDispatch{ID: 7, Node: "shape"}
	trigger, command := graphStepRequest(dispatch, "owner")

	if !command.Spawns() {
		t.Fatalf("a graph step must spawn a run, got kind %s", command.Kind)
	}
	if strings.TrimSpace(command.Text) == "" {
		t.Fatal("a turn refuses a free-text command with no instruction text")
	}
	if strings.TrimSpace(trigger.Body) == "" {
		t.Fatal("the trigger body is what the turn records as the request")
	}
	if trigger.CommentID != dispatch.TriggerID() {
		t.Fatalf("trigger %d is not the dispatch's own identity %d", trigger.CommentID, dispatch.TriggerID())
	}
	if !store.IsGraphTrigger(trigger.CommentID) {
		t.Fatal("a graph step must run under a graph trigger, or it consumes the comment watermark")
	}
	if !strings.Contains(command.Text, dispatch.Node) {
		t.Errorf("the instruction does not say which stage asked for the run: %q", command.Text)
	}
}

func TestInterruptedGraphDispatchTaskOwnership(t *testing.T) {
	for _, guard := range []string{"worker", "task lock", "unsupported capture"} {
		t.Run(guard, func(t *testing.T) {
			f := newResearchRecoveryFixture(t)
			ctx := t.Context()
			researchRecoveryCheck(t, f.db.FinishGraphNodeAttempt(ctx, f.attempt.ID, store.GraphSucceeded, "success", f.at))
			_, err := f.db.FinishRun(ctx, f.run.ID, store.RunOutcome{Status: store.RunSucceeded})
			researchRecoveryCheck(t, err)
			old, err := f.db.ParkGraphInstance(ctx, f.instance.ID, f.instance.CurrentNode, f.at, f.at)
			researchRecoveryCheck(t, err)
			_, ok, err := f.db.ReserveGraphDispatch(ctx, old.ID, f.at)
			researchRecoveryCheck(t, err)
			if !ok {
				t.Fatal("reserve refused")
			}
			run, err := f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: f.session.ID, SegmentID: f.run.SegmentID, Mode: "research", Agent: store.AgentClaude, TriggerCommentID: old.TriggerID(), LogPath: func(int) string { return "interrupted.log" }})
			researchRecoveryCheck(t, err)
			_, err = f.db.FinishRun(ctx, run.ID, store.RunOutcome{Status: store.RunFailed, Error: store.ReasonAbandoned})
			researchRecoveryCheck(t, err)
			d := &Daemon{db: f.db, instanceID: "recovery", log: slog.New(slog.NewTextHandler(io.Discard, nil))}
			if guard != "unsupported capture" {
				researchRecoveryCheck(t, f.db.SetGraphInstanceStatus(ctx, f.instance.ID, store.GraphSucceeded, time.Time{}, f.at))
			}
			if guard == "worker" && !d.inflight.enter(f.session.ID) {
				t.Fatal("worker ownership refused")
			}
			if guard == "task lock" {
				ok, err := f.db.AcquireSessionLock(ctx, f.session.ID, "live-worker")
				researchRecoveryCheck(t, err)
				if !ok {
					t.Fatal("lock refused")
				}
			}
			researchRecoveryCheck(t, d.reconcileGraphDispatches(ctx, f.at))
			got, err := f.db.LatestGraphDispatch(ctx, f.instance.ID)
			researchRecoveryCheck(t, err)
			if got.ID != old.ID || got.Status != "running" {
				t.Fatalf("protected dispatch changed: %+v", got)
			}
			if guard == "unsupported capture" {
				return
			}
			if guard == "worker" {
				d.inflight.leave(f.session.ID)
			} else {
				researchRecoveryCheck(t, f.db.ReleaseSessionLock(ctx, f.session.ID, "live-worker"))
			}
			researchRecoveryCheck(t, d.reconcileGraphDispatches(ctx, f.at))
			got, err = f.db.LatestGraphDispatch(ctx, f.instance.ID)
			researchRecoveryCheck(t, err)
			if got.ID != old.ID || got.Status != "finished" {
				t.Fatalf("idle dispatch not settled: %+v", got)
			}
		})
	}
}

func TestStrandedGraphRecoveryPreservesMissingCapture(t *testing.T) {
	f := newResearchRecoveryFixture(t)
	ctx := t.Context()
	researchRecoveryCheck(t, f.db.FinishGraphNodeAttempt(ctx, f.attempt.ID, store.GraphSucceeded, "success", f.at))
	_, err := f.db.FinishRun(ctx, f.run.ID, store.RunOutcome{Status: store.RunSucceeded})
	researchRecoveryCheck(t, err)
	researchRecoveryCheck(t, f.db.SetGraphInstanceStatus(ctx, f.instance.ID, store.GraphRunning, time.Time{}, time.Time{}))
	var logs strings.Builder
	d := &Daemon{db: f.db, instanceID: "stranded", log: slog.New(slog.NewTextHandler(&logs, nil))}
	for range 2 {
		researchRecoveryCheck(t, d.reconcileGraphDispatches(ctx, f.at))
	}
	due, err := f.db.ListDueGraphDispatches(ctx, f.at, 10)
	researchRecoveryCheck(t, err)
	if len(due) != 0 {
		t.Fatalf("invalid capture dispatched: %+v", due)
	}
	if !strings.Contains(logs.String(), "stranded graph capture cannot resume; retained work preserved") {
		t.Fatalf("missing refusal diagnostic: %s", logs.String())
	}
	instance, err := f.db.GetGraphInstance(ctx, f.instance.ID)
	researchRecoveryCheck(t, err)
	if instance.Status != store.GraphRunning || !instance.NextDueAt.IsZero() {
		t.Fatalf("retained work changed: %+v", instance)
	}
}

// A prelaunch run whose own dispatch is no longer live is failed by the
// recovery pass under the session's claim and lock. After a finished dispatch
// the instance is owed again as stranded; after a cancelled one it keeps its
// stop. Live dispatches, claimed or locked sessions and Bug sessions keep their
// prelaunch runs.
func TestReconcileSettlesOrphanedGraphPrelaunchRuns(t *testing.T) {
	for _, tc := range []struct {
		name, mode, dispatch, guard string
		settled, stranded           bool
	}{
		{name: "validate finished", mode: "validate", dispatch: "finished", settled: true, stranded: true},
		{name: "research finished", mode: "research", dispatch: "finished", settled: true, stranded: true},
		{name: "cancelled", mode: "validate", dispatch: "cancelled", settled: true},
		{name: "live", mode: "validate", dispatch: "reserved"},
		{name: "worker", mode: "research", dispatch: "finished", guard: "worker"},
		{name: "task lock", mode: "research", dispatch: "finished", guard: "task lock"},
		{name: "bug", mode: "research", dispatch: "finished", guard: "bug"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newResearchRecoveryFixture(t)
			ctx := t.Context()
			researchRecoveryCheck(t, f.db.FinishGraphNodeAttempt(ctx, f.attempt.ID, store.GraphSucceeded, "success", f.at))
			_, err := f.db.FinishRun(ctx, f.run.ID, store.RunOutcome{Status: store.RunSucceeded})
			researchRecoveryCheck(t, err)
			if tc.mode != "research" {
				definition, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "builtin", Mode: tc.mode, ProfileVersion: 1, Path: tc.mode + ".dot", Digest: strings.Repeat("b", 64), CreatedAt: f.at})
				researchRecoveryCheck(t, err)
				f.instance, err = f.db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: f.session.ID, DefinitionID: definition.ID, CurrentNode: "repair_agent", StartedAt: f.at, NextDueAt: f.at})
				researchRecoveryCheck(t, err)
			}
			if tc.guard == "bug" {
				// A Bug task is admitted before it has any run.
				repo, err := f.db.GetRepo(ctx, f.session.RepoID)
				researchRecoveryCheck(t, err)
				f.session, err = f.db.GetOrCreateSession(ctx, repo, 343)
				researchRecoveryCheck(t, err)
				a, err := f.db.AdmitWorkflow(ctx, store.TaskWorkflowAdmission{SessionID: f.session.ID, Version: 1, Family: "standalone", Workflow: "bug"})
				researchRecoveryCheck(t, err)
				if a.Workflow != "bug" {
					t.Fatalf("admission=%+v", a)
				}
				segment, err := f.db.GetOrCreateSegment(ctx, f.session.ID, 1, "bug", store.AgentClaude)
				researchRecoveryCheck(t, err)
				f.run.SegmentID = segment.ID
				definition, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "builtin", Workflow: "bug", ProfileVersion: 2, Path: "bug.dot", Digest: strings.Repeat("c", 64), CreatedAt: f.at})
				researchRecoveryCheck(t, err)
				f.instance, err = f.db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: f.session.ID, DefinitionID: definition.ID, CurrentNode: "fixer", StartedAt: f.at, NextDueAt: f.at})
				researchRecoveryCheck(t, err)
			}
			parked, err := f.db.ParkGraphInstance(ctx, f.instance.ID, f.instance.CurrentNode, f.at, f.at)
			researchRecoveryCheck(t, err)
			if _, ok, err := f.db.ReserveGraphDispatch(ctx, parked.ID, f.at); err != nil || !ok {
				t.Fatalf("reserve: %v %v", ok, err)
			}
			mode := tc.mode
			if tc.guard == "bug" {
				mode = "bug"
			}
			orphan, err := f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: f.session.ID, SegmentID: f.run.SegmentID, Mode: mode, Agent: store.AgentClaude, TriggerCommentID: parked.TriggerID(), Prelaunch: true, LogPath: func(int) string { return "orphan.log" }})
			researchRecoveryCheck(t, err)
			switch tc.dispatch {
			case "finished":
				researchRecoveryCheck(t, f.db.FinishGraphDispatch(ctx, parked.ID, f.at))
			case "cancelled":
				researchRecoveryCheck(t, f.db.CancelGraphDispatch(ctx, parked.ID, "graph stage could not start in 3 attempts", f.at))
			}
			d := &Daemon{db: f.db, instanceID: "orphans", log: slog.New(slog.NewTextHandler(io.Discard, nil))}
			switch tc.guard {
			case "worker":
				if !d.inflight.enter(f.session.ID) {
					t.Fatal("worker ownership refused")
				}
				defer d.inflight.leave(f.session.ID)
			case "task lock":
				ok, err := f.db.AcquireSessionLock(ctx, f.session.ID, "live-worker")
				researchRecoveryCheck(t, err)
				if !ok {
					t.Fatal("lock refused")
				}
			}
			researchRecoveryCheck(t, d.reconcileGraphDispatches(ctx, f.at))
			got, err := f.db.GetRun(ctx, orphan.ID)
			researchRecoveryCheck(t, err)
			if want := map[bool]store.RunStatus{true: store.RunFailed, false: store.RunPrelaunch}[tc.settled]; got.Status != want {
				t.Fatalf("orphaned run status=%s want %s", got.Status, want)
			}
			stranded, err := f.db.ListStrandedGraphDispatches(ctx)
			researchRecoveryCheck(t, err)
			found := false
			for _, candidate := range stranded {
				found = found || (candidate.InstanceID == f.instance.ID && candidate.Node == f.instance.CurrentNode)
			}
			if found != tc.stranded {
				t.Fatalf("stranded=%v want %v", found, tc.stranded)
			}
			latest, err := f.db.LatestGraphDispatch(ctx, f.instance.ID)
			researchRecoveryCheck(t, err)
			if latest.ID != parked.ID {
				t.Fatalf("a new dispatch was created: %+v", latest)
			}
		})
	}
}
