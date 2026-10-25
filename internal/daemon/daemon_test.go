package daemon_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"github.com/cloudvane/litefactory/internal/testutil"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// TestStartReportsAnAbandonedRunAndDoesNotRerunIt is AC-2.21 and D-B.
//
// A daemon killed mid-run leaves a runs row marked running and a task lock held
// by an instance that no longer exists. The next start fails the run as
// abandoned, posts the ordinary failure comment and advances the watermark past
// the comment that caused it. It never runs it again: (session, triggering
// comment) is unique on the runs row, so a re-run would not merely be wasteful,
// it would violate the daemon's own constraint.
func TestStartReportsAnAbandonedRunAndDoesNotRerunIt(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()

	session := f.session()
	f.forge.addComment(testIssue, 200, owner(), "/spike the run that was killed")

	// What a killed daemon leaves behind.
	orphan := f.allocateRunningRun(session, 200)
	if acquired, err := f.db.AcquireSessionLock(t.Context(), session.ID, "daemon-instance-previous"); err != nil {
		t.Fatalf("take the previous instance's lock: %v", err)
	} else if !acquired {
		t.Fatal("the previous instance's lock was not taken")
	}

	reconciled, err := f.daemon.Start(t.Context())
	if err != nil {
		t.Fatalf("Start: %v\nlogs:\n%s", err, f.logs)
	}

	if reconciled.AbandonedRuns != 1 {
		t.Errorf("abandoned %d runs, want 1", reconciled.AbandonedRuns)
	}
	if reconciled.Reported != 1 {
		t.Errorf("reported %d abandoned runs, want 1", reconciled.Reported)
	}
	if reconciled.ReclaimedLocks != 1 {
		t.Errorf("reclaimed %d locks, want 1", reconciled.ReclaimedLocks)
	}

	posted, _, _ := f.forge.snapshot()
	if len(posted) != 1 {
		t.Fatalf("posted %d comments for an abandoned run, want 1 — silence is never an outcome", len(posted))
	}
	if !strings.HasPrefix(posted[0], turn.MarkerPrefix) {
		t.Errorf("the failure comment carries no marker:\n%s", posted[0])
	}
	if !strings.Contains(posted[0], "abandoned") {
		t.Errorf("the failure comment does not say the run was abandoned:\n%s", posted[0])
	}

	if got := f.watermark(); got != 200 {
		t.Errorf("watermark is %d, want 200: reporting an abandoned run consumes its comment", got)
	}
	reloaded, err := f.db.GetRun(t.Context(), orphan.ID)
	if err != nil {
		t.Fatalf("read the abandoned run: %v", err)
	}
	if reloaded.Status != store.RunReported {
		t.Errorf("the abandoned run is %q, want %q", reloaded.Status, store.RunReported)
	}
	if reloaded.Error != store.ReasonAbandoned {
		t.Errorf("the abandoned run's reason is %q, want %q", reloaded.Error, store.ReasonAbandoned)
	}
	if session := f.session(); session.LockOwner != "" {
		t.Errorf("the task is still locked by %q", session.LockOwner)
	}
	if !strings.Contains(f.logs.String(), "reclaimed a task lock from a previous daemon instance") {
		t.Error("the reclaimed lock was not logged (AC-3.2)")
	}

	// And it is not re-run: the comment is below the watermark, so it is never
	// handed to a turn again.
	f.settle()

	if calls := f.turns.calls(); len(calls) != 0 {
		t.Errorf("ran %d turns after reconciliation, want none — an abandoned run is never retried", len(calls))
	}
	if runs := f.runs(); len(runs) != 1 {
		t.Errorf("allocated %d runs, want 1", len(runs))
	}
}

// TestAnAbandonedRunIsReconciledEvenWhenItsIssueWasClosed is AC-2.22b.
//
// Closing an issue stops litefactory *starting* anything on it (AC-2.22); it
// does not cancel a report that is already owed. Startup reconciliation works
// off the runs table and never asks the forge which issues are open, so a run
// killed mid-flight is failed, reported and consumed however its issue now
// stands — which is the only defensible answer, because the alternative is a
// run row that stays running for ever and a comment nobody ever hears back
// about (AC-4.1).
//
// This is asserted rather than derived: "the listing is not involved" is
// exactly the kind of claim that stops being true when someone adds a
// convenience filter to Start.
func TestAnAbandonedRunIsReconciledEvenWhenItsIssueWasClosed(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle() // first sight, while the issue is still open

	session := f.session()
	f.forge.addComment(testIssue, 200, owner(), "/spike the run that was killed")
	orphan := f.allocateRunningRun(session, 200)

	// And then the issue is closed, which is what a human does when the daemon
	// has gone quiet mid-run.
	f.forge.closeIssue(testIssue)

	reconciled, err := f.daemon.Start(t.Context())
	if err != nil {
		t.Fatalf("Start: %v\nlogs:\n%s", err, f.logs)
	}
	if reconciled.AbandonedRuns != 1 || reconciled.Reported != 1 {
		t.Fatalf("abandoned %d runs and reported %d, want 1 and 1 — a closed issue does not "+
			"cancel a report that is already owed\nlogs:\n%s",
			reconciled.AbandonedRuns, reconciled.Reported, f.logs)
	}

	posted, _, _ := f.forge.snapshot()
	if len(posted) != 1 {
		t.Fatalf("posted %d comments for an abandoned run on a closed issue, want 1", len(posted))
	}
	if !strings.Contains(posted[0], "abandoned") {
		t.Errorf("the failure comment does not say the run was abandoned:\n%s", posted[0])
	}
	if got := f.watermark(); got != 200 {
		t.Errorf("watermark is %d, want 200: reporting an abandoned run consumes its comment", got)
	}
	reloaded, err := f.db.GetRun(t.Context(), orphan.ID)
	if err != nil {
		t.Fatalf("read the abandoned run: %v", err)
	}
	if reloaded.Status != store.RunReported {
		t.Errorf("the abandoned run is %q, want %q", reloaded.Status, store.RunReported)
	}

	// The tick that follows is where AC-2.22 takes over: the issue is closed
	// now, so nothing more happens on it, and the abandoned run is not re-run.
	f.settle()

	if calls := f.turns.calls(); len(calls) != 0 {
		t.Errorf("ran %d turns after reconciliation, want none", len(calls))
	}
}

// TestStartWithNothingToReconcileIsQuiet keeps the ordinary case honest: a
// clean start posts nothing.
func TestStartWithNothingToReconcileIsQuiet(t *testing.T) {
	f := newFixture(t)

	reconciled, err := f.daemon.Start(t.Context())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if reconciled != (daemon.Reconciled{}) {
		t.Errorf("a clean start reconciled %+v, want nothing", reconciled)
	}
	if posted, _, _ := f.forge.snapshot(); len(posted) != 0 {
		t.Errorf("a clean start posted %d comments, want none", len(posted))
	}
}

// allocateRunningRun leaves behind the wreckage of a daemon that was killed
// mid-run: a runs row still marked running.
func (f *fixture) allocateRunningRun(session store.Session, triggerCommentID int64) store.Run {
	f.t.Helper()

	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		f.t.Fatalf("parse the task ref: %v", err)
	}
	sessionRef, err := naming.SessionRef(ref, store.FirstSegmentOrdinal)
	if err != nil {
		f.t.Fatalf("derive the session ref: %v", err)
	}
	segment, err := f.db.GetOrCreateSegment(f.t.Context(), session.ID, store.FirstSegmentOrdinal, sessionRef, store.AgentClaude)
	if err != nil {
		f.t.Fatalf("create the segment: %v", err)
	}
	run, err := f.db.AllocateRun(f.t.Context(), store.AllocateRunParams{
		SessionID:        session.ID,
		SegmentID:        segment.ID,
		Mode:             store.ModeSpike,
		Agent:            store.AgentClaude,
		TriggerCommentID: triggerCommentID,
		LogPath: func(ordinal int) string {
			path, pathErr := naming.RunLogPath(f.paths.Home(), ref, ordinal)
			if pathErr != nil {
				return ""
			}
			return path
		},
	})
	if err != nil {
		f.t.Fatalf("allocate the run: %v", err)
	}
	return run
}

// blockingExecutor is a turn that will not come back until it is let go. It
// stands in for a claude that is mid-run when the daemon is signalled.
type blockingExecutor struct {
	started chan struct{}
	release chan struct{}
}

func (e *blockingExecutor) Execute(ctx context.Context, _ turn.Request) (turn.Result, error) {
	select {
	case e.started <- struct{}{}:
	default:
	}
	<-e.release
	// Reporting the cancellation is what a real turn does when the daemon is
	// shutting down: nothing is consumed, and the run row is left for the next
	// start to reconcile (D-B).
	if err := ctx.Err(); err != nil {
		return turn.Result{}, err
	}
	return turn.Result{Disposition: turn.DispositionLocked}, nil
}

// TestRunExitsWithinThreeSecondsOfSIGTERM is AC-2.1, asserted against a real
// signal and with a turn that refuses to unwind.
//
// The turn is deliberately wedged: a run that is mid-`claude` can take ten
// seconds or more to die, because the harness gives the process subtree that
// long and its pipes as long again. Waiting for it would blow launchd's patience
// and the criterion. The daemon abandons it instead, and the next start
// reconciles the row it left behind.
func TestRunExitsWithinThreeSecondsOfSIGTERM(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()
	f.forge.addComment(testIssue, 210, owner(), "/spike a run that will not stop")

	executor := &blockingExecutor{started: make(chan struct{}, 1), release: make(chan struct{})}
	defer close(executor.release)

	ticks := make(chan time.Time)
	loop := f.newDaemon(daemon.Options{
		Turn:  executor,
		Ticks: ticks,
		// The daemon's own log goes nowhere: the loop goroutine outlives Run by
		// design here, and a buffer shared with the test would be shared with it
		// too.
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	ctx, stop := signal.NotifyContext(t.Context(), syscall.SIGTERM)
	defer stop()

	done := make(chan error, 1)
	go func() { done <- loop.Run(ctx) }()

	ticks <- time.Now()
	select {
	case <-executor.started:
	case <-time.After(testutil.Wait):
		t.Fatal("the tick never reached the turn")
	}

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v, want nil: being asked to stop is not a failure", err)
		}
	case <-time.After(testutil.Wait):
		t.Fatal("Run did not return before the SIGTERM hang deadline")
	}
}

// TestRunStopsWithoutTicking covers the ordinary case: nothing in flight, the
// signal arrives, the loop returns at once.
func TestRunStopsWithoutTicking(t *testing.T) {
	f := newFixture(t)
	ticks := make(chan time.Time)
	loop := f.newDaemon(daemon.Options{Ticks: ticks})

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- loop.Run(ctx) }()

	// Run reconciles before it loops, so waiting for a tick to be accepted is
	// what proves the loop is up.
	select {
	case ticks <- time.Now():
	case <-time.After(testutil.Wait):
		t.Fatal("the loop never started")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v, want nil", err)
		}
	case <-time.After(testutil.Wait):
		t.Fatal("Run did not return within 3 seconds of cancellation")
	}
}

// TestRunTicksFromItsTickSource proves the loop is wired to the ticker at all —
// everything else in this package tests Tick directly.
func TestRunTicksFromItsTickSource(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()
	f.forge.addComment(testIssue, 220, owner(), "/spike from the loop")

	ticks := make(chan time.Time)
	loop := f.newDaemon(daemon.Options{Ticks: ticks})

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- loop.Run(ctx) }()

	ticks <- time.Now()
	// The next send is only accepted once the previous tick has finished, so it
	// is the synchronisation point rather than a sleep.
	ticks <- time.Now()

	// Closing the source is what joins the turns. loop returns when the channel
	// closes, and its deferred pool.wait() cannot return until every worker it
	// dispatched has finished — so by the time Run returns, the turn this test
	// is about has run to completion. Cancelling instead would race the
	// dispatch: a worker not yet scheduled would find its context already done
	// and correctly do nothing.
	close(ticks)
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	cancel()

	if got := f.turns.triggers(); !slices.Equal(got, []int64{220}) {
		t.Errorf("the loop ran turns for %v, want [220]", got)
	}
	if got := f.watermark(); got != 220 {
		t.Errorf("watermark is %d, want 220", got)
	}
}

// TestRunLogsFreeDiskThresholdAtStart: the "daemon started" line names the
// active free-disk threshold in GB, 0 when the guard is off.
func TestRunLogsFreeDiskThresholdAtStart(t *testing.T) {
	for _, tt := range []struct {
		minFree uint64
		want    string
	}{{7 << 30, "min_free_disk_gb=7"}, {0, "min_free_disk_gb=0"}} {
		t.Run(tt.want, func(t *testing.T) {
			f := newFixture(t)
			ticks := make(chan time.Time)
			loop := f.newDaemon(daemon.Options{Ticks: ticks, MinFreeDiskBytes: tt.minFree})
			done := make(chan error, 1)
			go func() { done <- loop.Run(t.Context()) }()
			ticks <- time.Now()
			close(ticks)
			if err := <-done; err != nil {
				t.Fatalf("Run: %v", err)
			}
			var started string
			for _, line := range strings.Split(f.logs.String(), "\n") {
				if strings.Contains(line, "daemon started") {
					started = line
				}
			}
			if !strings.Contains(started, " "+tt.want) {
				t.Fatalf("daemon started line = %q, want %s\nlogs:\n%s", started, tt.want, f.logs)
			}
		})
	}
}

// TestIntervalOutsideTheClampIsAStartupError is AC-2.7 and D-C. An out-of-range
// interval refuses to start; it is never silently corrected, because a daemon
// that polls every 30 seconds when the file says 5 is a daemon whose
// configuration is a lie.
func TestIntervalOutsideTheClampIsAStartupError(t *testing.T) {
	f := newFixture(t)

	for _, interval := range []time.Duration{time.Second, 9 * time.Second, 61 * time.Second, time.Hour} {
		_, err := daemon.New(daemon.Options{
			DB:         f.db,
			Forge:      f.client,
			Filter:     f.filter,
			Turn:       f.turns,
			Reporter:   f.reporter,
			InstanceID: "daemon-instance-test",
			Interval:   interval,
			Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
		if !errors.Is(err, daemon.ErrInvalidOptions) {
			t.Errorf("interval %s produced %v, want an invalid-options error", interval, err)
		}
	}

	inRange, err := daemon.New(daemon.Options{
		DefaultAgent: agent.Kind(store.AgentClaude),
		ToolProfiles: config.DefaultToolProfiles().Permissions,
		Agents:       turn.RunnerSet{agent.Kind(store.AgentClaude): nil},
		Classifiers:  map[agent.Kind]agent.Classifier{agent.Kind(store.AgentClaude): fakeModeClassifier{}},
		DB:           f.db,
		Forge:        f.client,
		Filter:       f.filter,
		Turn:         f.turns,
		Reporter:     f.reporter,
		InstanceID:   "daemon-instance-test",
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("a daemon with no interval configured: %v", err)
	}
	if got := inRange.Interval(); got != daemon.DefaultInterval {
		t.Errorf("default interval is %s, want %s", got, daemon.DefaultInterval)
	}
}

// TestNewRequiresEveryDependency keeps the wiring failure at startup, where
// somebody is watching, rather than at the first tick thirty seconds later.
func TestNewRequiresEveryDependency(t *testing.T) {
	f := newFixture(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	complete := daemon.Options{
		DB:         f.db,
		Forge:      f.client,
		Filter:     f.filter,
		Turn:       f.turns,
		Reporter:   f.reporter,
		InstanceID: "daemon-instance-test",
		Logger:     logger,
	}

	tests := map[string]func(*daemon.Options){
		"no database":   func(o *daemon.Options) { o.DB = nil },
		"no forge":      func(o *daemon.Options) { o.Forge = nil },
		"no classifier": func(o *daemon.Options) { o.Filter = nil },
		"no executor":   func(o *daemon.Options) { o.Turn = nil },
		"no reporter":   func(o *daemon.Options) { o.Reporter = nil },
		"no instance":   func(o *daemon.Options) { o.InstanceID = "" },
		"no logger":     func(o *daemon.Options) { o.Logger = nil },
	}
	for name, break_ := range tests {
		t.Run(name, func(t *testing.T) {
			opts := complete
			break_(&opts)
			if _, err := daemon.New(opts); !errors.Is(err, daemon.ErrInvalidOptions) {
				t.Errorf("New returned %v, want an invalid-options error", err)
			}
		})
	}
}

// TestNewInstanceIDIsFreshPerCall matters for AC-3.2: "the lock owner is not
// the current instance" is how a lock left by a dead daemon is recognised, so
// two starts must never mint the same id.
func TestNewInstanceIDIsFreshPerCall(t *testing.T) {
	first, err := daemon.NewInstanceID()
	if err != nil {
		t.Fatalf("NewInstanceID: %v", err)
	}
	second, err := daemon.NewInstanceID()
	if err != nil {
		t.Fatalf("NewInstanceID: %v", err)
	}
	if first == second {
		t.Errorf("two instance ids are identical: %q", first)
	}
	if first == "" {
		t.Error("the instance id is empty")
	}
}

func TestInterruptedGraphDispatchStartupAndTick(t *testing.T) {
	for _, restart := range []bool{true, false} {
		t.Run(map[bool]string{true: "restart", false: "periodic"}[restart], func(t *testing.T) {
			f := newFixture(t)
			f.plantCursor()
			f.issueWithLabels("Interrupted graph stage", "Mode: Spike")
			f.settle()
			ctx := t.Context()
			session := f.session()
			specs, err := turn.SpikeSpecs()
			if err != nil {
				t.Fatal(err)
			}
			def, err := teamsourcetest.CaptureSpike(ctx, specs)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.db.AdmitWorkflowPath(ctx, session.ID, "graph"); err != nil {
				t.Fatal(err)
			}
			pin, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: def.Profile.Team, Mode: "spike", ProfileVersion: def.Profile.ProfileVersion, Path: "spike.dot", Digest: def.Profile.Digest, TeamSnapshotDigest: def.Digest})
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(def.Files)
			if err != nil {
				t.Fatal(err)
			}
			topology, err := graphexec.SemanticDigest(def.Profile)
			if err != nil {
				t.Fatal(err)
			}
			admitted, err := f.db.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{SessionID: session.ID, TriggerID: 22, DefinitionID: pin.ID, Mode: "spike", Start: def.Profile.Start, ContentDigest: def.Digest, TopologyDigest: topology, Files: raw})
			if err != nil {
				t.Fatal(err)
			}
			at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			old, err := f.db.ParkGraphInstance(ctx, admitted.Instance.ID, admitted.Instance.CurrentNode, at, at)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok, err := f.db.ReserveGraphDispatch(ctx, old.ID, at); err != nil || !ok {
				t.Fatalf("reserve %v %v", ok, err)
			}
			orphan := f.allocateRunningRun(session, old.TriggerID())
			if err = f.db.ClearSessionNeedsPoll(ctx, session.ID); err != nil {
				t.Fatal(err)
			}
			if restart {
				if ok, err := f.db.AcquireSessionLock(ctx, session.ID, "old-daemon"); err != nil || !ok {
					t.Fatalf("lock %v %v", ok, err)
				}
				reconciled, err := f.daemon.Start(ctx)
				if err != nil || reconciled.AbandonedRuns != 1 {
					t.Fatalf("start %+v %v", reconciled, err)
				}
				next, err := f.db.LatestGraphDispatch(ctx, old.InstanceID)
				if err != nil || next.ID == old.ID || next.Status != "pending" || next.LaunchID != old.LaunchID {
					t.Fatalf("successor %+v %v", next, err)
				}
			} else {
				if _, err = f.db.FinishRun(ctx, orphan.ID, store.RunOutcome{Status: store.RunFailed, Error: store.ReasonAbandoned}); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			f.turns.fn = func(ctx context.Context, req turn.Request) (turn.Result, error) {
				calls++
				if !store.IsGraphTrigger(req.Trigger.CommentID) || req.Trigger.CommentID == old.TriggerID() {
					t.Errorf("replayed old trigger %d", req.Trigger.CommentID)
				}
				out, err := f.ranSuccessfully(ctx, req)
				if err == nil {
					err = f.db.SetGraphInstanceStatus(ctx, old.InstanceID, store.GraphSucceeded, time.Time{}, time.Now().UTC())
				}
				out.ResearchGraph = true
				return out, err
			}
			f.settle()
			f.settle()
			if calls != 1 {
				t.Fatalf("ran %d times; logs %s", calls, f.logs)
			}
			instance, err := f.db.GetGraphInstance(ctx, old.InstanceID)
			if err != nil || instance.Status != store.GraphSucceeded {
				t.Fatalf("instance %+v %v", instance, err)
			}
			history, err := f.db.GetRun(ctx, orphan.ID)
			if err != nil || history.Error != store.ReasonAbandoned {
				t.Fatalf("history %+v %v", history, err)
			}
			if open, err := f.db.ListInterruptedGraphDispatches(ctx); err != nil || len(open) != 0 {
				t.Fatalf("interrupted %+v %v", open, err)
			}
			if due, err := f.db.ListDueGraphDispatches(ctx, time.Now().UTC(), 10); err != nil || len(due) != 0 {
				t.Fatalf("pending %+v %v", due, err)
			}
		})
	}
}

func TestStartReportsExpiredNonGraphWorkerTiming(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Retain the expired worker")
	f.settle()
	session := f.session()
	f.forge.addComment(testIssue, 200, owner(), "/spike the run that expired before restart")
	orphan := f.allocateRunningRun(session, 200)
	deadline := f.clock.peek().Add(-time.Second)
	started := deadline.Add(-91 * time.Second)
	if _, err := f.db.EnsureAttemptTiming(t.Context(), store.AttemptTiming{RunID: orphan.ID, StartedAt: started, Deadline: deadline, Allowance: 91 * time.Second, Source: "legacy"}); err != nil {
		t.Fatal(err)
	}
	reconciled, err := f.daemon.Start(t.Context())
	if err != nil || reconciled.Reported != 1 {
		t.Fatalf("restart=%+v error=%v\n%s", reconciled, err, f.logs)
	}
	saved, err := f.db.GetRun(t.Context(), orphan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.SemanticOutcome != "timed_out" || saved.Error == store.ReasonAbandoned {
		t.Fatalf("expiry reported as shutdown: %+v", saved)
	}
	posts, _, _ := f.forge.snapshot()
	if len(posts) != 1 || !strings.Contains(posts[0], "timed out") || !strings.Contains(posts[0], "91 seconds") || !strings.Contains(posts[0], deadline.UTC().Format(time.RFC3339Nano)) {
		t.Fatalf("missing recovered timing: %v", posts)
	}
	f.settle()
	if calls := f.turns.calls(); len(calls) != 0 {
		t.Fatalf("recovered worker re-executed: %+v", calls)
	}
}
