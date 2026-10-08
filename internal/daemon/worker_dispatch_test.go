package daemon_test

import (
	"context"
	"encoding/json"
	"github.com/cloudvane/litefactory/internal/testutil"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

func dispatchRecords(t *testing.T, logs *syncBuffer, message string) []map[string]any {
	t.Helper()
	var result []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode log: %v: %s", err, line)
		}
		if record["msg"] == message {
			for _, key := range []string{"repo", "task", "session_id", "dispatch_sources", "schedule_id", "continuation_id"} {
				if strings.Count(line, `"`+key+`":`) > 1 {
					t.Fatalf("duplicate attribute %s: %s", key, line)
				}
			}
			result = append(result, record)
		}
	}
	return result
}

// AC-1: each real launch path identifies its handoff before executor entry;
// the second tick while the executor is held cannot emit another handoff.
func TestWorkerDispatchAC1LaunchPaths(t *testing.T) {
	for _, path := range []string{"ordinary", "scheduled", "continuation"} {
		t.Run(path, func(t *testing.T) {
			f := newFixture(t)
			logs := &syncBuffer{}
			f.logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			var durableID int64
			var durableKey, source string
			if path == "continuation" {
				c := seedAutomaticAdmission(t, f, time.Hour)
				durableID, durableKey, source = c.ID, "continuation_id", "automatic_continuation"
				f.clock.advance(2 * time.Hour)
			} else {
				f.plantCursor()
				f.issue("observable worker")
				f.settle()
				f.daemon = f.newDaemon(daemon.Options{MaxConcurrentRuns: 1})
				if path == "scheduled" {
					s, _, err := f.db.CreateOrGetSchedule(t.Context(), store.CreateScheduleParams{SessionID: f.session().ID, SourceCommentID: 701, OwnerHandle: ownerLogin, Payload: "/spike scheduled", DueAt: f.clock.Now().Add(-time.Hour)})
					if err != nil {
						t.Fatal(err)
					}
					durableID, durableKey, source = s.ID, "schedule_id", "due_schedule"
				} else {
					source = "forge_discovery"
					f.forge.addComment(testIssue, 40, owner(), "/spike observe")
				}
			}
			started := make(chan bool, 1)
			release := make(chan struct{})
			defer close(release)
			f.turns.fn = func(ctx context.Context, req turn.Request) (turn.Result, error) {
				started <- strings.Contains(logs.String(), `"msg":"worker dispatched"`)
				select {
				case <-release:
				case <-ctx.Done():
					return turn.Result{}, ctx.Err()
				}
				return f.ranSuccessfully(ctx, req)
			}
			// Cancellation guarantees cleanup even if an assertion fails while held.
			ctx, cancel := context.WithCancel(t.Context())
			defer func() {
				cancel()
				join, stop := context.WithTimeout(context.Background(), testutil.Wait)
				defer stop()
				_ = f.daemon.Quiesce(join)
			}()
			result, err := f.daemon.Tick(ctx)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case seen := <-started:
				if !seen {
					t.Fatal("executor entered before dispatch diagnostic")
				}
			case <-time.After(testutil.Wait):
				t.Fatal("worker did not reach executor")
			}
			if result.Dispatched != 1 {
				t.Fatalf("dispatched=%d", result.Dispatched)
			}
			records := dispatchRecords(t, logs, "worker dispatched")
			if len(records) != 1 {
				t.Fatalf("handoffs=%v", records)
			}
			r := records[0]
			if r["level"] != "DEBUG" || r["repo"] != testOwnerName+"/"+testRepoName || r["task"] != f.session().TaskRef || r["session_id"] != float64(f.session().ID) {
				t.Fatalf("identity=%v", r)
			}
			sources, ok := r["dispatch_sources"].([]any)
			if !ok || len(sources) == 0 || sources[0] != source {
				t.Fatalf("sources=%v", r)
			}
			if durableKey != "" && r[durableKey] != float64(durableID) {
				t.Fatalf("durable identity=%v", r)
			}
			if path == "ordinary" {
				f.forge.addIssue(fakeIssue{ID: 2000, Number: 200, Title: "waiting for pool", User: stranger()})
			}
			next, err := f.daemon.Tick(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if path == "ordinary" && next.Deferred < 1 {
				t.Fatalf("full pool deferrals=%d want at least 1", next.Deferred)
			}
			if got := dispatchRecords(t, logs, "worker dispatched"); len(got) != 1 {
				t.Fatalf("busy admission emitted handoff: %v", got)
			}
		})
	}
}

// AC-2: enrollment by all three discovery sources remains one attributed
// candidate with a stable source union across repeated ticks.
func TestWorkerDispatchAC2CombinedSources(t *testing.T) {
	f := managedRefinementFixture(t)
	logs := &syncBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	f.daemon = f.newDaemon(daemon.Options{Logger: logger, Forge: refinementForge{Client: f.client, parent: testIssue}})
	for i := 0; i < 2; i++ {
		f.forge.addComment(testIssue, int64(80+i), stranger(), "non-owner update")
		if err := f.db.SetSessionNeedsPoll(t.Context(), f.session().ID); err != nil {
			t.Fatal(err)
		}
		result := f.settle()
		if result.Dispatched != 1 {
			t.Fatalf("tick %d handoffs=%d", i, result.Dispatched)
		}
		records := dispatchRecords(t, logs, "worker dispatched")
		if len(records) != i+1 {
			t.Fatalf("duplicated candidate: %v", records)
		}
		want := []any{"forge_discovery", "managed_registry", "backlog"}
		if !reflect.DeepEqual(records[i]["dispatch_sources"], want) {
			t.Fatalf("sources=%v want %v", records[i]["dispatch_sources"], want)
		}
	}
}

func TestWorkerDispatchAC1ContinuationReservationRefusal(t *testing.T) {
	f := newFixture(t)
	logs := &syncBuffer{}
	f.logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	c := seedAutomaticAdmission(t, f, time.Hour)
	f.clock.advance(2 * time.Hour)
	f.forge.onListComments = func(_ http.ResponseWriter, _ int) bool {
		if err := f.db.CancelContinuation(t.Context(), c.ID, "concurrent cancellation", f.clock.Now()); err != nil {
			t.Error(err)
		}
		return false
	}
	f.settle()
	for _, record := range dispatchRecords(t, logs, "worker dispatched") {
		if _, exists := record["continuation_id"]; exists {
			t.Fatalf("reservation refusal logged dispatch: %v", record)
		}
	}
	got, err := f.db.GetContinuation(t.Context(), c.ID)
	if err != nil || got.Status != "cancelled" || len(f.turns.calls()) != 0 {
		t.Fatalf("refused reservation=%+v err=%v calls=%d", got, err, len(f.turns.calls()))
	}
}

func TestWorkerDispatchAC2ContinuationDuplicateIdentity(t *testing.T) {
	f := newFixture(t)
	logs := &syncBuffer{}
	f.logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	c := seedAutomaticAdmission(t, f, time.Hour)
	_, _, err := f.db.CreateOrGetSchedule(t.Context(), store.CreateScheduleParams{SessionID: f.session().ID, SourceCommentID: 701, OwnerHandle: ownerLogin, Payload: "/spike held schedule", DueAt: f.clock.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer func() {
		cancel()
		join, stop := context.WithTimeout(context.Background(), testutil.Wait)
		defer stop()
		_ = f.daemon.Quiesce(join)
	}()
	f.turns.fn = func(ctx context.Context, _ turn.Request) (turn.Result, error) {
		started <- struct{}{}
		<-ctx.Done()
		return turn.Result{}, ctx.Err()
	}
	if _, err = f.daemon.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(testutil.Wait):
		t.Fatal("schedule not started")
	}
	f.clock.advance(2 * time.Hour)
	if _, err = f.daemon.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	records := dispatchRecords(t, logs, "automatic recovery waits for this task's in-flight turn")
	if len(records) != 1 {
		t.Fatalf("duplicate refusal=%v logs=%s", records, logs)
	}
	r := records[0]
	if r["level"] != "DEBUG" || r["continuation_id"] != float64(c.ID) || r["session_id"] != float64(c.SessionID) || r["task"] != f.session().TaskRef || r["repo"] != testOwnerName+"/"+testRepoName {
		t.Fatalf("duplicate identity=%v", r)
	}
	if got := dispatchRecords(t, logs, "worker dispatched"); len(got) != 1 {
		t.Fatalf("duplicate logged successful handoff=%v", got)
	}
}

func TestWorkerDispatchAC1ClosedSchedule(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("closed scheduled work")
	f.settle()
	_, _, err := f.db.CreateOrGetSchedule(t.Context(), store.CreateScheduleParams{SessionID: f.session().ID, SourceCommentID: 701, OwnerHandle: ownerLogin, Payload: "/spike must not start", DueAt: f.clock.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	f.forge.closeIssue(testIssue)
	logs := &syncBuffer{}
	f.daemon = f.newDaemon(daemon.Options{Logger: slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))})
	result := f.settle()
	if result.Dispatched != 0 || len(f.turns.calls()) != 0 || len(dispatchRecords(t, logs, "worker dispatched")) != 0 {
		t.Fatalf("closed task dispatched: %+v logs=%s", result, logs)
	}
}
