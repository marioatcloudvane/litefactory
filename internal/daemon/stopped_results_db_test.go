package daemon_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// The executor boundary supplies an observed stopped result; real Tick,
// SQLite persistence and HTTP report/source accounting run unchanged.
func TestStoppedResultSQLiteOutageRecoversWithoutRelaunch(t *testing.T) {
	f := newFixture(t)
	f.daemon = f.newDaemon(daemon.Options{MaxConcurrentRuns: 1})
	f.plantCursor()
	f.issue("Persist stopped result")
	f.settle()
	session := f.session()
	f.forge.addComment(testIssue, 40, owner(), "/spike preserve the stopped result")
	var repairs atomic.Int64
	var run store.Run
	discoveryDone := make(chan struct{})
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		run = f.allocateRunningRun(session, req.Trigger.CommentID)
		ref, err := naming.ParseRef(session.TaskRef)
		if err != nil {
			return turn.Result{}, err
		}
		result := turn.Result{Disposition: turn.DispositionRan, SessionID: session.ID, Session: session, Ref: ref, Run: run, TriggerCommentID: 40, Outcome: agent.OutcomeFailed, LogPath: run.LogPath}
		<-discoveryDone
		if err := f.db.Close(); err != nil {
			return turn.Result{}, err
		}
		result.Repair = func(ctx context.Context) (turn.Result, error) {
			repairs.Add(1)
			finished, err := f.db.FinishRun(ctx, run.ID, store.RunOutcome{Status: store.RunFailed, SemanticOutcome: "failed", Error: "observed process failure", FinishedAt: run.StartedAt.Add(time.Second)})
			if err != nil {
				return turn.Result{}, err
			}
			repaired := result
			repaired.Repair = nil
			repaired.Run = finished
			repaired.Harness = fakeRunResult(finished, agent.OutcomeFailed)
			return repaired, nil
		}
		return result, errors.New("terminal persistence unavailable")
	})
	f.tick()
	close(discoveryDone)
	if err := f.daemon.Quiesce(t.Context()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		f.clock.advance(testInterval * time.Duration(1<<i))
		if _, err := f.tickErr(); err == nil {
			t.Fatal("closed database did not surface discovery outage")
		}
		if err := f.daemon.Quiesce(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if repairs.Load() != 2 {
		t.Fatalf("repair debt was hidden behind failed DB discovery: %d attempts", repairs.Load())
	}
	if len(f.turns.calls()) != 1 {
		t.Fatalf("outage relaunched executor: %d calls", len(f.turns.calls()))
	}
	posted, _, _ := f.forge.snapshot()
	if len(posted) != 0 {
		t.Fatalf("unpersisted result posted: %v", posted)
	}
	// All workers have joined. Restore the injected store handle in place so
	// every existing component retains its original dependency and repair debt.
	reopened, err := store.Open(t.Context(), f.paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	*f.db = *reopened
	if f.watermark() >= 40 {
		t.Fatal("outage consumed source")
	}
	saved, err := f.db.GetRun(t.Context(), run.ID)
	if err != nil || saved.Status != store.RunRunning {
		t.Fatalf("outage changed durable result: %+v %v", saved, err)
	}
	f.clock.advance(4 * testInterval)
	f.settle()
	f.settle()
	saved, err = f.db.GetRun(t.Context(), run.ID)
	if err != nil || saved.Status != store.RunReported {
		t.Fatalf("repair did not finish and report: %+v %v", saved, err)
	}
	posted, _, _ = f.forge.snapshot()
	if len(posted) != 1 || f.watermark() != 40 || len(f.turns.calls()) != 1 {
		t.Fatalf("repair effects: posts=%d watermark=%d executor=%d", len(posted), f.watermark(), len(f.turns.calls()))
	}
}

func TestStoppedResultStartupPreservesPrelaunchAllocation(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Never launched")
	f.settle()
	session := f.session()
	segment, err := f.db.GetOrCreateSegment(t.Context(), session.ID, store.FirstSegmentOrdinal, "prelaunch-conversation", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: store.ModeSpike, Agent: store.AgentClaude, TriggerCommentID: 40, Prelaunch: true, LogPath: func(int) string { return "/never-launched.jsonl" }})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := f.db.AcquireSessionLock(t.Context(), session.ID, "dead-instance"); err != nil || !ok {
		t.Fatalf("lock: %v %v", ok, err)
	}
	reconciled, err := f.daemon.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	saved, err := f.db.GetRun(t.Context(), run.ID)
	if err != nil || saved.Status != store.RunPrelaunch || saved.Ordinal != run.Ordinal {
		t.Fatalf("startup converted prelaunch into invocation: %+v %v", saved, err)
	}
	if reconciled.AbandonedRuns != 0 || reconciled.Reported != 0 || reconciled.ReclaimedLocks != 1 {
		t.Fatalf("reconciliation=%+v", reconciled)
	}
	posted, _, _ := f.forge.snapshot()
	if len(posted) != 0 || f.watermark() >= 40 || len(f.turns.calls()) != 0 {
		t.Fatal("startup reported or consumed never-launched preparation")
	}
	if session := f.session(); session.LockOwner != "" {
		t.Fatal("old owner lock survived exclusive startup")
	}
}

func TestStoppedResultScheduledPrelaunchRemainsRetryable(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Retry scheduled preparation")
	f.settle()
	session := f.session()
	due := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	scheduled, _, err := f.db.CreateOrGetSchedule(t.Context(), store.CreateScheduleParams{SessionID: session.ID, SourceCommentID: 701, OwnerHandle: ownerLogin, Payload: "/spike prepare again", DueAt: due})
	if err != nil {
		t.Fatal(err)
	}
	segment, err := f.db.GetOrCreateSegment(t.Context(), session.ID, store.FirstSegmentOrdinal, "prelaunch-schedule", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	var runID int64
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		existing, found, err := f.db.GetRunByTrigger(ctx, session.ID, req.Trigger.CommentID)
		if err != nil {
			return turn.Result{}, err
		}
		if !found {
			existing, err = f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: store.ModeSpike, Agent: store.AgentClaude, TriggerCommentID: req.Trigger.CommentID, Prelaunch: true, LogPath: func(int) string { return "/prelaunch-schedule.jsonl" }})
			if err != nil {
				return turn.Result{}, err
			}
		}
		runID = existing.ID
		return turn.Result{}, errors.New("temporary preparation failure")
	})
	f.clock.advance(25 * time.Hour)
	for i := 0; i < 2; i++ {
		f.settle()
		saved, err := f.db.GetSchedule(t.Context(), scheduled.ID)
		if err != nil || saved.Status != store.SchedulePending {
			t.Fatalf("prelaunch schedule stranded: %+v %v", saved, err)
		}
		rows := f.runs()
		if len(rows) != 1 || rows[0].ID != runID || rows[0].Status != store.RunPrelaunch || rows[0].Ordinal != 1 {
			t.Fatalf("retry changed allocation: %+v", rows)
		}
	}
	if len(f.turns.calls()) != 2 {
		t.Fatalf("prelaunch schedule did not retry: %d attempts", len(f.turns.calls()))
	}
	posted, _, _ := f.forge.snapshot()
	if len(posted) != 0 {
		t.Fatalf("nonterminal schedule reported: %v", posted)
	}
}

func TestStoppedResultContinuationPrelaunchRemainsRetryable(t *testing.T) {
	f := newFixture(t)
	continuation := seedAutomaticAdmission(t, f, time.Hour)
	f.settle() // Settle the interrupted source report before testing preparation.
	before, _, _ := f.forge.snapshot()
	var allocatedID int64
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		if req.Continuation == nil || req.Continuation.ID != continuation.ID {
			return turn.Result{}, errors.New("wrong continuation admitted")
		}
		existing, found, err := f.db.GetRunByTrigger(ctx, continuation.SessionID, req.Trigger.CommentID)
		if err != nil {
			return turn.Result{}, err
		}
		if !found {
			existing, err = f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: continuation.SessionID, SegmentID: continuation.SegmentID, Mode: store.ModeSpike, Agent: store.AgentClaude, TriggerCommentID: req.Trigger.CommentID, Prelaunch: true, LogPath: func(int) string { return "/prelaunch-continuation.jsonl" }})
			if err != nil {
				return turn.Result{}, err
			}
		}
		allocatedID = existing.ID
		return turn.Result{}, errors.New("retryable continuation preparation")
	})
	f.clock.advance(2 * time.Hour)
	for i := 0; i < 2; i++ {
		f.settle()
		current, err := f.db.GetContinuation(t.Context(), continuation.ID)
		if err != nil || current.Status != "pending" || current.AllocatedRunID != 0 {
			t.Fatalf("prelaunch continuation stranded: %+v %v", current, err)
		}
		allocated, err := f.db.GetRun(t.Context(), allocatedID)
		if err != nil || allocated.Status != store.RunPrelaunch || allocated.Ordinal != 2 {
			t.Fatalf("prelaunch continuation allocation: %+v %v", allocated, err)
		}
	}
	if len(f.turns.calls()) != 2 || len(f.runs()) != 2 {
		t.Fatalf("continuation retry changed identity: calls=%d rows=%+v", len(f.turns.calls()), f.runs())
	}
	after, _, _ := f.forge.snapshot()
	if len(after) != len(before) || f.watermark() != 42 {
		t.Fatalf("prelaunch recovery posted or consumed: before=%d after=%d watermark=%d", len(before), len(after), f.watermark())
	}
}
