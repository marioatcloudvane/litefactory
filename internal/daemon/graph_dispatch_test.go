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
