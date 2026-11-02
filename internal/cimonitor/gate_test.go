package cimonitor_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/cimonitor"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestGateDoctorLongWaitSurvivesRestartAndCannotChangeAlerts(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	file := filepath.Join(t.TempDir(), "ci.db")
	db, err := store.Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	if err = db.UpdateAlerts(ctx, func(s *store.AlertLedger) error {
		s.Subjects["known failure"] = &store.AlertSubject{Rule: "main_red", Active: true, CleanCount: 1}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := db.ReadAlerts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	gate := &cimonitor.Gate{Store: db, Key: "origin/credential", Now: func() time.Time { return now }}
	err = gate.Do(ctx, func(context.Context) (github.ActionsTiming, error) {
		return github.ActionsTiming{RetryAfter: now.Add(2 * time.Hour), PollInterval: 3 * time.Hour, RateLimitReset: now.Add(4 * time.Hour), RateLimited: true}, github.ErrRateLimited
	})
	if !errors.Is(err, github.ErrRateLimited) {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	gate = &cimonitor.Gate{Store: db, Key: "origin/credential", Now: func() time.Time { return now.Add(2 * time.Hour) }}
	called := false
	for range 2 {
		err = gate.Do(ctx, func(context.Context) (github.ActionsTiming, error) { called = true; return github.ActionsTiming{}, nil })
		var deferred *cimonitor.DeferredError
		if !errors.As(err, &deferred) || !deferred.Until.Equal(now.Add(4*time.Hour)) {
			t.Fatalf("deferred=%v err=%v", deferred, err)
		}
	}
	if called {
		t.Fatal("request escaped server wait")
	}
	after, err := db.ReadAlerts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision || !after.Subjects["known failure"].Active || after.Subjects["known failure"].CleanCount != 1 {
		t.Fatalf("diagnostic changed incidents=%+v", after)
	}
	gate.Now = func() time.Time { return now.Add(4 * time.Hour) }
	if err = gate.Do(ctx, func(context.Context) (github.ActionsTiming, error) { called = true; return github.ActionsTiming{}, nil }); err != nil || !called {
		t.Fatalf("expired bound err=%v called=%v", err, called)
	}
}

func TestGateUnavailableStoreFailsClosed(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	called := false
	gate := &cimonitor.Gate{Store: db, Key: "origin"}
	if err = gate.Do(ctx, func(context.Context) (github.ActionsTiming, error) { called = true; return github.ActionsTiming{}, nil }); err == nil || called {
		t.Fatalf("closed store request called=%v err=%v", called, err)
	}
}

func TestGateSerializesCallersAndFallbackIncreases(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	file := filepath.Join(t.TempDir(), "concurrent.db")
	db, err := store.Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	other, err := store.Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := other.Close(); err != nil {
			t.Error(err)
		}
	})
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	first := &cimonitor.Gate{Store: db, Key: "same", Now: func() time.Time { return now }}
	second := &cimonitor.Gate{Store: other, Key: "same", Now: func() time.Time { return now }}
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan error, 1)
	// One request runs until released; the bounded context also ends it if the
	// test fails, so this test owns no unbounded background work.
	go func() {
		finished <- first.Do(ctx, func(ctx context.Context) (github.ActionsTiming, error) {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
			}
			return github.ActionsTiming{}, github.ErrRateLimited
		})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first request did not start")
	}
	err = second.Do(ctx, func(context.Context) (github.ActionsTiming, error) {
		t.Error("concurrent HTTP request admitted")
		return github.ActionsTiming{}, nil
	})
	var deferred *cimonitor.DeferredError
	if !errors.As(err, &deferred) {
		t.Fatalf("concurrent err=%v", err)
	}
	close(release)
	select {
	case err = <-finished:
		if !errors.Is(err, github.ErrRateLimited) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first request did not finish")
	}
	state, err := db.CITransport(ctx, "same")
	if err != nil || !state.NotBefore.Equal(now.Add(time.Minute)) {
		t.Fatalf("fallback=%+v err=%v", state, err)
	}
	now = now.Add(time.Minute)
	err = second.Do(ctx, func(context.Context) (github.ActionsTiming, error) {
		return github.ActionsTiming{}, github.ErrRateLimited
	})
	if !errors.Is(err, github.ErrRateLimited) {
		t.Fatal(err)
	}
	state, err = db.CITransport(ctx, "same")
	if err != nil || !state.NotBefore.Equal(now.Add(2*time.Minute)) || state.Failures != 2 {
		t.Fatalf("fallback growth=%+v err=%v", state, err)
	}
}

func TestGateRetainsTimingAfterJournalFailureBeforeAnyNewRequest(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	file := filepath.Join(t.TempDir(), "failed-finish.db")
	db, err := store.Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	gate := &cimonitor.Gate{Store: db, Key: "origin", Now: func() time.Time { return now }}
	calls := 0
	err = gate.Do(ctx, func(context.Context) (github.ActionsTiming, error) {
		calls++
		if err := db.Close(); err != nil {
			t.Error(err)
		}
		return github.ActionsTiming{RetryAfter: now.Add(4 * time.Hour)}, nil
	})
	if err == nil {
		t.Fatal("journal failure was hidden")
	}
	db, err = store.Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	gate.Store = db
	now = now.Add(5 * time.Minute)
	// An independently restarted process sees the expired orphan lease and must
	// not assume that the unknown server delay has elapsed.
	restarted := &cimonitor.Gate{Store: db, Key: "origin", Now: func() time.Time { return now }}
	err = restarted.Do(ctx, func(context.Context) (github.ActionsTiming, error) { calls++; return github.ActionsTiming{}, nil })
	var uncertain *cimonitor.TransportUncertainError
	if !errors.As(err, &uncertain) {
		t.Fatalf("orphan error=%v", err)
	}
	// The original process retained timing and can recover only its failed write.
	err = gate.Do(ctx, func(context.Context) (github.ActionsTiming, error) { calls++; return github.ActionsTiming{}, nil })
	var deferred *cimonitor.DeferredError
	if !errors.As(err, &deferred) || !deferred.Until.Equal(now.Add(3*time.Hour+55*time.Minute)) {
		t.Fatalf("retained wait=%v err=%v", deferred, err)
	}
	if calls != 1 {
		t.Fatalf("journal retry repeated network request: calls=%d", calls)
	}
	state, err := db.CITransport(ctx, "origin")
	if err != nil || state.Lease != "" || !state.NotBefore.Equal(deferred.Until) {
		t.Fatalf("recovered timing=%+v err=%v", state, err)
	}
}
