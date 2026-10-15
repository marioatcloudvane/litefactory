package daemon

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// loopLog is a goroutine-safe log sink: the loop logs from its own goroutine.
type loopLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *loopLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *loopLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func loopDaemon(logs *loopLog, tick func(context.Context) (TickResult, error)) *Daemon {
	return &Daemon{
		log:    slog.New(slog.NewJSONHandler(logs, nil)),
		pool:   newPool(1),
		doctor: doctorLane{pool: newPool(1)},
		tick:   tick,
	}
}

// A phase deadline under a live daemon context is an ordinary failed tick:
// it is logged and the loop ticks again (issue #900).
func TestLoopSurvivesTickDeadline(t *testing.T) {
	logs := &loopLog{}
	calls := make(chan int, 4)
	n := 0
	d := loopDaemon(logs, func(context.Context) (TickResult, error) {
		n++
		calls <- n
		if n == 1 {
			return TickResult{}, fmt.Errorf("discover: %w", context.DeadlineExceeded)
		}
		return TickResult{}, nil
	})
	ctx := t.Context()
	if !d.beat(ctx, wakeTimer) {
		t.Fatal("beat ended the loop on a phase deadline with a live context")
	}
	<-calls
	if got := logs.String(); !strings.Contains(got, `"msg":"tick failed`) || !strings.Contains(got, "discover: context deadline exceeded") {
		t.Fatalf("tick failure not logged with its deadline error:\n%s", got)
	}

	// The same through the loop: the deadline tick is followed by another.
	n = 0
	ticks := make(chan time.Time)
	done := make(chan struct{})
	go func() { defer close(done); d.loop(ctx, ticks, true) }()
	select {
	case <-calls:
	case <-time.After(5 * time.Second):
		t.Fatal("immediate tick did not run")
	}
	select {
	case ticks <- time.Now():
	case <-done:
		t.Fatal("loop returned after a phase deadline with a live context")
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not take the next tick")
	}
	select {
	case got := <-calls:
		if got != 2 {
			t.Fatalf("tick call = %d", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second tick did not run")
	}
	close(ticks)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not return after the tick source closed")
	}
	if !strings.Contains(logs.String(), "heartbeat loop ended; waiting for dispatched workers") {
		t.Fatalf("loop end not logged:\n%s", logs.String())
	}
}

// Cancelling the daemon's own context still ends the loop, and the end is
// logged with the cancellation before the worker join.
func TestLoopEndsWhenDaemonContextCancelled(t *testing.T) {
	logs := &loopLog{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d := loopDaemon(logs, func(context.Context) (TickResult, error) {
		cancel()
		return TickResult{}, fmt.Errorf("discover: %w", context.Canceled)
	})
	if d.beat(ctx, wakeTimer) {
		t.Fatal("beat continued after the daemon context was cancelled")
	}
	if strings.Contains(logs.String(), "tick failed") {
		t.Fatalf("shutdown logged as a tick failure:\n%s", logs.String())
	}

	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	// A worker still in flight holds the join open, as in the reported dump.
	release := make(chan struct{})
	d.pool.wg.Add(1)
	go func() { <-release; d.pool.wg.Done() }()
	done := make(chan struct{})
	go func() { defer close(done); d.loop(ctx, make(chan time.Time), true) }()
	deadline := time.After(5 * time.Second)
	for !strings.Contains(logs.String(), "heartbeat loop ended") {
		select {
		case <-done:
			t.Fatal("loop joined before logging its end")
		case <-deadline:
			t.Fatalf("loop end not logged before the worker join:\n%s", logs.String())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if !strings.Contains(logs.String(), `"error":"context canceled"`) {
		t.Fatalf("loop end line lacks the context error:\n%s", logs.String())
	}
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not return after the worker finished")
	}
}
