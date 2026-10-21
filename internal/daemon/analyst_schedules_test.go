package daemon_test

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/analystschedule"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/testutil"
	"github.com/cloudvane/litefactory/internal/turn"
)

func newScheduledAnalystDaemon(t *testing.T, f *fixture, svc *analystschedule.Service, executor daemon.Executor, capacity int, free func(string) (uint64, error)) *daemon.Daemon {
	t.Helper()
	opts := daemon.Options{DefaultAgent: agent.Kind(store.AgentClaude), ToolProfiles: config.DefaultToolProfiles().Permissions, Agents: turn.RunnerSet{agent.Kind(store.AgentClaude): nil}, Classifiers: map[agent.Kind]agent.Classifier{agent.Kind(store.AgentClaude): fakeModeClassifier{}}, DB: f.db, Forge: f.client, Filter: f.filter, Turn: executor, Reporter: f.reporter, InstanceID: "analyst-schedule-test", Interval: testInterval, Logger: f.logger, Now: f.clock.Now, MaxConcurrentRuns: capacity, AnalystSchedules: svc}
	if free != nil {
		opts.MinFreeDiskBytes = 1
		opts.DiskPath = f.paths.Home()
		opts.FreeDiskBytes = free
	}
	d, err := daemon.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func enableScheduledAnalyst(t *testing.T, f *fixture, svc *analystschedule.Service, key string) store.AnalystSchedule {
	t.Helper()
	a, err := svc.Create(t.Context(), key, "github:71", analystschedule.Config{Name: key, Kind: "logs", RepositoryID: f.repo.ID, RunLimit: 1}, f.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	a, err = svc.Enable(t.Context(), a.ID, a.Version, "github:71", f.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func requireScheduledOccurrenceCount(t *testing.T, f *fixture, id string, want int) {
	t.Helper()
	rows, err := f.db.ListAnalystOccurrences(t.Context(), id, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != want {
		t.Fatalf("schedule %s occurrences=%d, want %d: %+v", id, len(rows), want, rows)
	}
}

func TestAnalystScheduleYieldsSharedCapacityToInteractiveWork(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Interactive work has priority")
	f.settle()
	f.forge.addComment(testIssue, 210, owner(), "/spike foreground work")
	svc := &analystschedule.Service{DB: f.db, Authorize: func(context.Context, store.Repo, string) error { return nil }}
	a := enableScheduledAnalyst(t, f, svc, "foreground-priority")
	executor := &blockingExecutor{started: make(chan struct{}, 1), release: make(chan struct{})}
	var release sync.Once
	t.Cleanup(func() {
		release.Do(func() { close(executor.release) })
		joinCtx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), testutil.Wait)
		defer cancel()
		if err := f.daemon.Quiesce(joinCtx); err != nil {
			t.Errorf("join interactive worker: %v", err)
		}
	})
	f.daemon = newScheduledAnalystDaemon(t, f, svc, executor, 1, nil)
	result := f.tick()
	select {
	case <-executor.started:
	case <-time.After(testutil.Wait):
		t.Fatal("interactive worker did not start")
	}
	if result.Dispatched != 1 {
		t.Fatalf("dispatched=%d, want only interactive work", result.Dispatched)
	}
	requireScheduledOccurrenceCount(t, f, a.ID, 0)
	// The next tick must leave the schedule unreserved while the same worker owns
	// the shared slot, rather than creating a scheduler-specific backlog.
	f.tick()
	requireScheduledOccurrenceCount(t, f, a.ID, 0)
	release.Do(func() { close(executor.release) })
	if err := f.daemon.Quiesce(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestAnalystScheduleStartsOnePerTickAndKeepsRepositoryExclusive(t *testing.T) {
	f := newFixture(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var released sync.Once
	t.Cleanup(func() {
		released.Do(func() { close(release) })
		joinCtx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), testutil.Wait)
		defer cancel()
		if err := f.daemon.Quiesce(joinCtx); err != nil {
			t.Errorf("join scheduled worker: %v", err)
		}
	})
	svc := &analystschedule.Service{DB: f.db, Authorize: func(ctx context.Context, _ store.Repo, _ string) error {
		once.Do(func() { close(started) })
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	for i := 0; i < 3; i++ {
		enableScheduledAnalyst(t, f, svc, fmt.Sprintf("exclusive-%d", i))
	}
	f.daemon = newScheduledAnalystDaemon(t, f, svc, f.turns, 3, nil)
	result := f.tick()
	if result.Dispatched != 1 {
		t.Fatalf("dispatched %d schedules in one tick", result.Dispatched)
	}
	select {
	case <-started:
	case <-time.After(testutil.Wait):
		t.Fatal("schedule worker did not begin")
	}
	requireScheduledOccurrenceCount(t, f, "", 1)
	result = f.tick()
	if result.Dispatched != 0 {
		t.Fatalf("dispatched %d while the repository already has active scheduled work", result.Dispatched)
	}
	requireScheduledOccurrenceCount(t, f, "", 1)
	released.Do(func() { close(release) })
	if err := f.daemon.Quiesce(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestAnalystScheduleWaitsForDiskAndForgeHolds(t *testing.T) {
	t.Run("disk", func(t *testing.T) {
		f := newFixture(t)
		svc := &analystschedule.Service{DB: f.db, Authorize: func(context.Context, store.Repo, string) error { return nil }}
		a := enableScheduledAnalyst(t, f, svc, "disk-hold")
		var free uint64
		f.daemon = newScheduledAnalystDaemon(t, f, svc, f.turns, 1, func(string) (uint64, error) { return free, nil })
		f.settle()
		requireScheduledOccurrenceCount(t, f, a.ID, 0)
		free = 2
		f.settle()
		requireScheduledOccurrenceCount(t, f, a.ID, 1)
	})
	t.Run("forge backoff", func(t *testing.T) {
		f := newFixture(t)
		svc := &analystschedule.Service{DB: f.db, Authorize: func(context.Context, store.Repo, string) error { return nil }}
		a := enableScheduledAnalyst(t, f, svc, "forge-hold")
		f.daemon = newScheduledAnalystDaemon(t, f, svc, f.turns, 1, nil)
		const delay = time.Minute
		f.forge.quotaReset = f.clock.Now().Add(delay).Unix()
		f.forge.onListIssues = func(w http.ResponseWriter, call int) bool {
			if call != 1 {
				return false
			}
			writeRateLimit(w, delay)
			return true
		}
		if _, err := f.tickErr(); err == nil {
			t.Fatal("rate limit did not fail discovery")
		}
		result := f.tick()
		if !result.Skipped {
			t.Fatal("standing forge hold did not skip dispatch")
		}
		requireScheduledOccurrenceCount(t, f, a.ID, 0)
		f.clock.advance(delay + time.Second)
		f.settle()
		requireScheduledOccurrenceCount(t, f, a.ID, 1)
	})
}
