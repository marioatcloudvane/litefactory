package daemon_test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

// Backlog reads run on the tick goroutine; mode confirmation reads run on
// workers. Tests inspect reads and change the hook only after Quiesce, and move
// all issues/comments outside discovery's listing window.
type closureForge struct {
	labelFixtureForge
	mu    sync.Mutex
	reads []int
	after func(context.Context, int, github.Issue, error) (github.Issue, error)
}

func (g *closureForge) GetIssue(ctx context.Context, repo github.Repo, number int) (github.Issue, error) {
	// Quota observation is an independent real HTTP read, not a backlog
	// candidate and must not invoke candidate-specific cancellation/errors.
	if number == 1 {
		return g.labelFixtureForge.GetIssue(ctx, repo, number)
	}
	g.mu.Lock()
	g.reads = append(g.reads, number)
	g.mu.Unlock()
	issue, err := g.labelFixtureForge.GetIssue(ctx, repo, number)
	if g.after != nil {
		return g.after(ctx, number, issue, err)
	}
	return issue, err
}

func closedBacklogFixture(t *testing.T) (*fixture, *closureForge) {
	t.Helper()
	f := newFixture(t)
	f.plantCursor()
	f.issue("Retire this task")
	f.settle()
	f.clock.advance(pastListing)
	f.settle() // advance the stored cursor beyond the initial listing
	f.forge.closeIssue(testIssue)
	f.needsPoll(testIssue)
	g := &closureForge{labelFixtureForge: f.client}
	f.daemon = f.newDaemon(daemon.Options{MaxConcurrentRuns: 1, Forge: g})
	return f, g
}

func TestClosedBacklogRetirementSurvivesRestart(t *testing.T) {
	for _, state := range []store.SessionState{store.SessionIdle, store.SessionFailed} {
		t.Run(string(state), func(t *testing.T) {
			f, g := closedBacklogFixture(t)
			if err := f.db.SetSessionState(t.Context(), f.session().ID, state); err != nil {
				t.Fatal(err)
			}
			if _, err := f.db.AdvanceWatermark(t.Context(), f.session().ID, 91); err != nil {
				t.Fatal(err)
			}
			before := f.session()
			result := f.settle()
			if result.Dispatched != 0 || result.Deferred != 0 || len(f.turns.calls()) != 0 {
				t.Fatalf("closed retirement started work: %+v", result)
			}
			after := f.session()
			if after.NeedsPoll {
				t.Fatal("closed session retained backlog")
			}
			// Only the marker and its bookkeeping timestamp may change.
			after.NeedsPoll, after.UpdatedAt = before.NeedsPoll, before.UpdatedAt
			if !reflect.DeepEqual(before, after) || f.watermark() != 91 {
				t.Fatalf("retirement changed session or watermark: before=%+v after=%+v watermark=%d", before, after, f.watermark())
			}
			if !slices.Equal(g.reads, []int{testIssue}) {
				t.Fatalf("backlog GETs = %v", g.reads)
			}
			for range 3 {
				f.settle()
			}
			if len(g.reads) != 1 || f.session().NeedsPoll {
				t.Fatal("retired backlog returned on subsequent ticks")
			}

			if err := f.db.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := store.Open(t.Context(), f.paths.DBPath())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := reopened.Close(); err != nil {
					t.Error(err)
				}
			})
			if err := reopened.Migrate(t.Context()); err != nil {
				t.Fatal(err)
			}
			f.db = reopened
			f.daemon = f.newDaemon(daemon.Options{Forge: g, InstanceID: "closure-restarted"})
			// No reports or comments are owed here; Start and Tick must use durable
			// discovery state, with no worker needing the fixture's old filter.
			if _, err := f.daemon.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			f.settle()
			if len(g.reads) != 1 || f.session().NeedsPoll {
				t.Fatal("restart restored retired backlog")
			}
		})
	}
}

func TestClosedBacklogReleasesAdmissionForOpenWork(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	for _, n := range []int{testIssue, 124, 125, 126} {
		f.forge.addIssue(fakeIssue{Number: n, Title: "Task", User: stranger()})
	}
	f.bootstrapIssues(f.repo, testIssue, 124, 125, 126)
	f.clock.advance(pastListing)
	f.settle()
	for _, n := range []int{testIssue, 124, 125} {
		f.forge.closeIssue(n)
		f.needsPoll(n)
	}
	f.forge.addComment(126, 301, owner(), "/spike open work")
	g := &closureForge{labelFixtureForge: f.client}
	f.daemon = f.newDaemon(daemon.Options{MaxConcurrentRuns: 1, Forge: g})
	result := f.settle()
	if result.Dispatched != 1 || result.Deferred != 0 {
		t.Fatalf("closed skips blocked open work: %+v\n%s", result, f.logs)
	}
	if !slices.Equal(f.turns.triggers(), []int64{301}) {
		t.Fatalf("executed %v", f.turns.triggers())
	}
	for _, n := range []int{testIssue, 124, 125} {
		if f.sessionOf(n).NeedsPoll {
			t.Errorf("issue %d still flagged", n)
		}
	}
	// Closed backlog candidates are read once each. Subsequent reads belong
	// to the open issue's durable mode admission and confirmation.
	if len(g.reads) < 3 || !slices.Equal(g.reads[:3], []int{testIssue, 124, 125}) {
		t.Fatalf("backlog reads = %v", g.reads)
	}
	for _, number := range g.reads[3:] {
		if number != 126 {
			t.Fatalf("closed issue reread during admission: %v", g.reads)
		}
	}
}

func TestUncertainBacklogObservationRetainsRetry(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state string
		err   error
	}{
		{name: "unknown", state: "unknown"},
		{name: "missing", err: &github.APIError{StatusCode: http.StatusNotFound, Message: "missing"}},
		{name: "inaccessible", err: &github.APIError{StatusCode: http.StatusForbidden, Message: "inaccessible"}},
		{name: "transport", err: errors.New("connection lost")},
		{name: "rate limited", err: &github.RateLimitError{APIError: github.APIError{StatusCode: http.StatusTooManyRequests}, RetryAfter: limitDelay}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, g := closedBacklogFixture(t)
			g.after = func(_ context.Context, _ int, issue github.Issue, _ error) (github.Issue, error) {
				issue.State = tc.state
				return issue, tc.err
			}
			result, err := f.tickErr()
			if err := f.daemon.Quiesce(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err != nil {
				t.Fatalf("tick failed outside candidate error handling: %v", err)
			}
			if tc.err != nil && !strings.Contains(f.logs.String(), "materialise") && !strings.Contains(f.logs.String(), "backing off") {
				t.Fatalf("lookup error was not handled: %s", f.logs)
			}
			if !f.session().NeedsPoll || result.Dispatched != 0 || len(f.turns.calls()) != 0 {
				t.Fatalf("uncertain lookup lost retry or ran work: %+v", result)
			}
			if tc.name == "rate limited" {
				skipped := f.settle()
				if !skipped.Skipped {
					t.Fatal("rate limit did not hold next tick")
				}
				f.clock.advance(limitDelay + time.Second)
			}
			// The same task must be admitted again after the failed/unknown read.
			g.after = nil
			f.settle()
			if f.session().NeedsPoll || len(g.reads) != 2 {
				t.Fatalf("retry did not retire closure: reads=%v", g.reads)
			}
			f.forge.addIssue(fakeIssue{Number: 124, Title: "Useful work", User: stranger()})
			if got := f.settle(); got.Dispatched != 1 {
				t.Fatalf("admission leaked: %+v", got)
			}
		})
	}
}

func TestFailedClosureClearRetainsReadableRetry(t *testing.T) {
	f, g := closedBacklogFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	g.after = func(_ context.Context, _ int, issue github.Issue, err error) (github.Issue, error) {
		// The authoritative HTTP read succeeded; cancellation makes the subsequent
		// context-first persistence operation fail without corrupting/deleting rows.
		cancel()
		return issue, err
	}
	result, err := f.daemon.Tick(ctx)
	if !errors.Is(err, context.Canceled) || !strings.Contains(f.logs.String(), "could not materialise") || !strings.Contains(f.logs.String(), "clear") {
		t.Fatalf("clear error not propagated: result=%+v err=%v", result, err)
	}
	if err := f.daemon.Quiesce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if result.Dispatched != 0 || !f.session().NeedsPoll {
		t.Fatal("failed clear lost persisted retry")
	}
	g.after = nil
	f.settle()
	if f.session().NeedsPoll || len(g.reads) != 2 {
		t.Fatal("same session could not be admitted after clear failure")
	}
	f.forge.addIssue(fakeIssue{Number: 124, Title: "Useful work", User: stranger()})
	if got := f.settle(); got.Dispatched != 1 {
		t.Fatalf("clear failure leaked admission: %+v", got)
	}
}

func TestRetiredBacklogReopensWithoutReplayingConsumedComments(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Reusable task")
	f.settle()
	f.forge.addComment(testIssue, 401, owner(), "/spike consumed")
	f.settle()
	f.settle()
	original := f.session()
	f.forge.addComment(testIssue, 402, owner(), "/spike pending")
	f.forge.closeIssue(testIssue)
	// Move both discovery windows past every comment before flagging backlog.
	f.clock.advance(pastListing)
	f.settle()
	f.needsPoll(testIssue)
	g := &closureForge{labelFixtureForge: f.client}
	f.daemon = f.newDaemon(daemon.Options{Forge: g})
	f.settle()
	if f.session().NeedsPoll || f.watermark() != 401 {
		t.Fatal("retirement changed pending comment position")
	}
	f.forge.mu.Lock()
	rs := f.forge.state(testOwnerName, testRepoName)
	for i := range rs.issues {
		if rs.issues[i].Number == testIssue {
			rs.issues[i].State = "open"
			rs.issues[i].UpdatedAt = f.clock.Now()
		}
	}
	f.forge.mu.Unlock()
	f.settle()
	if f.session().ID != original.ID || f.watermark() != 402 || !slices.Equal(f.turns.triggers(), []int64{401, 402}) {
		t.Fatalf("reopening lost/replayed work: session=%+v watermark=%d triggers=%v", f.session(), f.watermark(), f.turns.triggers())
	}
	// The second read confirms the retained mode for the newly admitted trigger.
	if !slices.Equal(g.reads, []int{testIssue, testIssue}) {
		t.Fatalf("closure retirement and fresh mode confirmation reads: %v", g.reads)
	}
}

func TestClosureRetirementPreservesOwedAbandonedReport(t *testing.T) {
	f, g := closedBacklogFixture(t)
	orphan := f.allocateRunningRun(f.session(), 501)
	before, err := f.db.GetRun(t.Context(), orphan.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.settle()
	after, err := f.db.GetRun(t.Context(), orphan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || f.session().NeedsPoll || len(g.reads) != 1 {
		t.Fatal("closure retirement altered owed run or failed to retire")
	}
	reconciled, err := f.daemon.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.AbandonedRuns != 1 || reconciled.Reported != 1 || f.watermark() != 501 {
		t.Fatalf("owed report lost: %+v watermark=%d", reconciled, f.watermark())
	}
	posted, _, _ := f.forge.snapshot()
	if len(posted) != 1 || !strings.Contains(posted[0], "abandoned") {
		t.Fatalf("owed report = %v", posted)
	}
	after, err = f.db.GetRun(t.Context(), orphan.ID)
	if err != nil || after.Status != store.RunReported {
		t.Fatalf("run was not reported: %+v %v", after, err)
	}
}
