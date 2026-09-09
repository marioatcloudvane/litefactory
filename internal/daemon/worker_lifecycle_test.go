package daemon_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// AC-3/AC-4: worker disposition and executor requests are independent of
// prepared runs; bootstrap, control-only and repeated comments exercise that
// distinction through actual Tick and Quiesce lifetimes.
func TestWorkerLifecycleAC3AC4Ordinary(t *testing.T) {
	for _, tc := range []struct {
		name     string
		commands []string
		executor string
		status   string
		count    int
	}{
		{name: "bootstrap", status: "completed"},
		{name: "quiet", status: "completed"},
		{name: "control only", commands: []string{"/schedule list"}, status: "completed"},
		{name: "queued comments", commands: []string{"/spike first", "/spike second"}, status: "deferred", count: 1},
		{name: "failed request", commands: []string{"/spike fail"}, executor: "failed", status: "failed", count: 1},
		{name: "lock refused", commands: []string{"/spike lock"}, executor: "locked", status: "deferred", count: 1},
		{name: "cancelled result", commands: []string{"/spike cancel"}, executor: "cancelled", status: "cancelled", count: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.plantCursor()
			f.issue("worker lifecycle")
			if tc.name != "bootstrap" {
				f.settle()
			}
			logs := &syncBuffer{}
			f.daemon = f.newDaemon(daemon.Options{Logger: slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})), MaxConcurrentRuns: 1})
			for i, command := range tc.commands {
				f.forge.addComment(testIssue, int64(80+i), owner(), command)
			}
			if tc.name == "quiet" {
				if err := f.db.SetSessionNeedsPoll(t.Context(), f.session().ID); err != nil {
					t.Fatal(err)
				}
			}
			switch tc.executor {
			case "failed":
				f.turns.fn = func(context.Context, turn.Request) (turn.Result, error) {
					return turn.Result{}, errors.New("synthetic executor failure")
				}
			case "cancelled":
				f.turns.fn = func(context.Context, turn.Request) (turn.Result, error) { return turn.Result{}, context.Canceled }
			case "locked":
				f.turns.fn = func(context.Context, turn.Request) (turn.Result, error) {
					return turn.Result{Disposition: turn.DispositionLocked}, nil
				}
			}
			result := f.settle()
			if result.Dispatched != 1 {
				t.Fatalf("handoffs=%d logs=%s", result.Dispatched, logs)
			}
			finished := dispatchRecords(t, logs, "worker finished")
			dispatch := dispatchRecords(t, logs, "worker dispatched")
			if len(finished) != 1 || len(dispatch) != 1 {
				t.Fatalf("worker lifecycle dispatch=%v finished=%v", dispatch, finished)
			}
			r := finished[0]
			if r["level"] != "DEBUG" || r["status"] != tc.status || r["execution_requests"] != float64(tc.count) {
				t.Fatalf("terminal=%v logs=%s fixture=%s", r, logs, f.logs)
			}
			if n := len(f.turns.calls()); n != tc.count {
				t.Fatalf("executor calls=%d want %d", n, tc.count)
			}
			for _, key := range []string{"repo", "task", "session_id", "dispatch_sources", "tick_at"} {
				if !reflect.DeepEqual(r[key], dispatch[0][key]) {
					t.Fatalf("mismatched %s: %v / %v", key, r, dispatch[0])
				}
			}
			duration, ok := r["duration"].(float64)
			if !ok || duration < 0 {
				t.Fatalf("invalid duration=%v", r)
			}
			if tc.executor == "failed" && !strings.Contains(logs.String(), "synthetic executor failure") {
				t.Fatal("existing error diagnostic missing")
			}
			if tc.name == "queued comments" {
				if next := f.settle(); next.Dispatched != 1 {
					t.Fatalf("second comment not admitted: %+v", next)
				}
				all := dispatchRecords(t, logs, "worker finished")
				if len(all) != 2 || all[1]["execution_requests"] != float64(1) || len(f.turns.calls()) != 2 {
					t.Fatalf("request count leaked across workers: %v", all)
				}
				if n := len(dispatchRecords(t, logs, "turn complete")); n != 2 {
					t.Fatalf("per-trigger completions=%d", n)
				}
			}

			if tc.name == "control only" {
				posts, _, _ := f.forge.snapshot()
				if len(posts) == 0 {
					t.Fatal("control work produced no reply")
				}
			}
			// A real subsequent Tick demonstrates reusable admission after Quiesce.
			if tc.executor == "" {
				if err := f.db.SetSessionNeedsPoll(t.Context(), f.session().ID); err != nil {
					t.Fatal(err)
				}
				if next := f.settle(); next.Dispatched != 1 {
					t.Fatalf("pool not reusable: %+v", next)
				}
			}
		})
	}
}

func TestWorkerLifecycleAC3AC4DurablePaths(t *testing.T) {
	for _, path := range []string{"scheduled", "continuation"} {
		for _, outcome := range []string{"completed", "deferred", "failed"} {
			t.Run(path+"/"+outcome, func(t *testing.T) {
				f := newFixture(t)
				logs := &syncBuffer{}
				f.logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
				var key string
				if path == "continuation" {
					seedAutomaticAdmission(t, f, time.Hour)
					f.clock.advance(2 * time.Hour)
					key = "continuation_id"
				} else {
					f.plantCursor()
					f.issue("scheduled lifecycle")
					f.settle()
					f.daemon = f.newDaemon(daemon.Options{})
					_, _, err := f.db.CreateOrGetSchedule(t.Context(), store.CreateScheduleParams{SessionID: f.session().ID, SourceCommentID: 701, OwnerHandle: ownerLogin, Payload: "/spike scheduled", DueAt: f.clock.Now().Add(-time.Hour)})
					if err != nil {
						t.Fatal(err)
					}
					key = "schedule_id"
				}
				if outcome == "deferred" {
					f.turns.fn = func(context.Context, turn.Request) (turn.Result, error) {
						return turn.Result{Disposition: turn.DispositionLocked}, nil
					}
				}
				if outcome == "failed" {
					f.turns.fn = func(context.Context, turn.Request) (turn.Result, error) {
						return turn.Result{}, errors.New("synthetic failure")
					}
				}
				f.settle()
				records := dispatchRecords(t, logs, "worker finished")
				if len(records) != 1 {
					t.Fatalf("terminal records=%v", records)
				}
				r := records[0]
				if r[key] == nil || r["status"] != outcome || r["execution_requests"] != float64(1) {
					t.Fatalf("terminal=%v logs=%s fixture=%s", r, logs, f.logs)
				}
			})
		}
	}
}

func TestWorkerLifecycleAC4MultipleRequestAccounting(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("multiple request boundary")
	f.settle()
	logs := &syncBuffer{}
	f.daemon = f.newDaemon(daemon.Options{Logger: slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))})
	triggers := []turn.Trigger{{CommentID: 90, Body: "/spike first", Author: ownerLogin}, {CommentID: 91, Body: "/spike second fails", Author: ownerLogin}}
	for _, trigger := range triggers {
		f.forge.addComment(testIssue, trigger.CommentID, owner(), trigger.Body)
	}
	f.turns.fn = func(ctx context.Context, req turn.Request) (turn.Result, error) {
		if req.Trigger.CommentID == 91 {
			return turn.Result{}, errors.New("second invocation failed")
		}
		return f.ranSuccessfully(ctx, req)
	}
	issue, err := f.client.GetIssue(t.Context(), github.Repo{Owner: testOwnerName, Name: testRepoName}, testIssue)
	if err != nil {
		t.Fatal(err)
	}
	daemon.ExerciseWorkerRequests(t, f.daemon, f.repo, f.session(), issue, triggers)
	records := dispatchRecords(t, logs, "worker finished")
	if len(records) != 1 || records[0]["execution_requests"] != float64(2) || records[0]["status"] != "failed" || len(f.turns.calls()) != 2 {
		t.Fatalf("multiple request accounting=%v calls=%d", records, len(f.turns.calls()))
	}
	if len(dispatchRecords(t, logs, "turn complete")) != 1 {
		t.Fatal("per-trigger completion did not remain distinct from worker completion")
	}
}

func TestWorkerLifecycleAC3PreExecutionFailures(t *testing.T) {
	for _, path := range []string{"bootstrap read failure", "continuation final observation"} {
		t.Run(path, func(t *testing.T) {
			f := newFixture(t)
			logs := &syncBuffer{}
			f.logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			want := "failed"
			if path == "bootstrap read failure" {
				f.plantCursor()
				f.issue("bootstrap failure")
				f.daemon = f.newDaemon(daemon.Options{})
			} else {
				seedAutomaticAdmission(t, f, time.Hour)
				f.clock.advance(2 * time.Hour)
				want = "deferred"
			}
			var observations atomic.Int32
			f.forge.onListComments = func(w http.ResponseWriter, _ int) bool {
				call := observations.Add(1)
				if path == "bootstrap read failure" || call == 2 {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "synthetic observation failure"})
					return true
				}
				return false
			}
			f.settle()
			finished := dispatchRecords(t, logs, "worker finished")
			if len(finished) != 1 || finished[0]["status"] != want || finished[0]["execution_requests"] != float64(0) || len(f.turns.calls()) != 0 {
				t.Fatalf("pre-execution terminal=%v logs=%s", finished, logs)
			}
			if !strings.Contains(logs.String(), `"level":"ERROR"`) {
				t.Fatal("existing error diagnostic missing")
			}
		})
	}
}
