package daemon

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

type terminalBoundaryHandler struct{ observe func(slog.Record) }

func (h terminalBoundaryHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h terminalBoundaryHandler) Handle(_ context.Context, r slog.Record) error {
	h.observe(r)
	return nil
}
func (h terminalBoundaryHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h terminalBoundaryHandler) WithGroup(string) slog.Handler      { return h }

// AC-3: the terminal log sees body cleanup and released ownership, but still
// belongs to the pool lifetime joined by Quiesce. No Tick runs in the logger.
func TestWorkerLifecycleAC3CleanupBoundary(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		name := "normal"
		if cancelled {
			name = "shutdown cancellation"
		}
		t.Run(name, func(t *testing.T) {
			d := &Daemon{pool: newPool(1), now: func() time.Time { panic("worker read scheduling clock") }}
			if !d.inflight.enter(42) || !d.pool.try() {
				t.Fatal("admission failed")
			}
			var cleaned atomic.Bool
			observed := make(chan slog.Record, 1)
			release := make(chan struct{})
			defer close(release)
			handler := terminalBoundaryHandler{observe: func(r slog.Record) {
				if !cleaned.Load() {
					t.Error("terminal log before body cleanup")
				}
				if d.inflight.size() != 0 {
					t.Error("terminal log before ownership release")
				}
				if d.pool.try() {
					t.Error("terminal log promises a free slot")
					d.pool.done()
				}
				observed <- r.Clone()
				<-release
			}}
			task := &task{session: store.Session{ID: 42}, workerLog: slog.New(handler)}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			go d.runWorker(ctx, task, func() {
				defer cleaned.Store(true)
				if cancelled {
					cancel()
				}
			})
			var record slog.Record
			select {
			case record = <-observed:
			case <-time.After(5 * time.Second):
				t.Fatal("no terminal record")
			}
			if record.Message != "worker finished" || record.Level != slog.LevelDebug {
				t.Fatalf("terminal record=%v", record)
			}
			attrs := map[string]any{}
			record.Attrs(func(a slog.Attr) bool { attrs[a.Key] = a.Value.Any(); return true })
			want := "completed"
			if cancelled {
				want = "cancelled"
			}
			if attrs["status"] != want || attrs["execution_requests"] != int64(0) {
				t.Fatalf("terminal attributes=%v", attrs)
			}
			join, stop := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer stop()
			if err := d.Quiesce(join); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Quiesce did not join held terminal logger: %v", err)
			}
			release <- struct{}{}
			final, finish := context.WithTimeout(t.Context(), 5*time.Second)
			defer finish()
			if err := d.Quiesce(final); err != nil {
				t.Fatal(err)
			}
			// Actual subsequent-Tick reuse is covered by the external fixture.
			// Do not Add to this WaitGroup while a timed-out join's waiter may
			// still be returning from Wait.

		})
	}
}

// ExerciseWorkerRequests is a test-only bridge to construct one private worker
// over the external HTTP/SQLite fixture. Default drain yields after a spawning
// comment, so multiple requests here prove the accounting boundary without
// claiming that normal polling batches spawning comments today.
func ExerciseWorkerRequests(t *testing.T, d *Daemon, repo store.Repo, session store.Session, issue github.Issue, triggers []turn.Trigger) {
	t.Helper()
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	log := d.log.With(slog.String("repo", repo.Owner+"/"+repo.Name), slog.String("task", session.TaskRef), slog.Int64("session_id", session.ID))
	workerTask := &task{repo: repo, session: session, issue: issue, forgeRepo: github.Repo{Owner: repo.Owner, Name: repo.Name}, ref: ref, log: log, workerLog: log, tickAt: time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)}
	if !d.inflight.enter(session.ID) || !d.pool.try() {
		t.Fatal("could not admit constructed worker")
	}
	d.runWorker(t.Context(), workerTask, func() {
		for _, trigger := range triggers {
			copyTask := *workerTask
			_ = d.run(t.Context(), &copyTask, trigger, turn.ParseCommand(trigger.Body), log)
		}
	})
}
