package daemon_test

import (
	"context"
	"github.com/cloudvane/litefactory/internal/testutil"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// Discovery is explicitly empty; per-issue reads, child relationship checks,
// comments and reports still use the existing local HTTP/real SQLite fixture.
type emptyDiscoveryForge struct{ refinementForge }

func (emptyDiscoveryForge) ListIssues(context.Context, github.Repo, time.Time) ([]github.Issue, error) {
	return nil, nil
}
func (emptyDiscoveryForge) ListCommentedIssues(context.Context, github.Repo, time.Time) ([]int, error) {
	return nil, nil
}

func TestQuietPollingAC5SummaryAliases(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("held worker summary")
	f.settle()
	logs := &syncBuffer{}
	f.daemon = f.newDaemon(daemon.Options{Logger: slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))})
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
	f.forge.addComment(testIssue, 80, owner(), "/spike held")
	result, err := f.daemon.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(testutil.Wait):
		t.Fatal("worker did not start")
	}
	if result.Dispatched != 1 || result.InFlight != 1 || result.Issues != 1 {
		t.Fatalf("snapshot=%+v", result)
	}
	records := dispatchRecords(t, logs, "tick complete")
	if len(records) != 1 {
		t.Fatalf("summaries=%v", records)
	}
	r := records[0]
	if r["level"] != "INFO" {
		t.Fatalf("summary level=%v", r)
	}
	for _, pair := range [][2]string{{"workers_dispatched", "dispatched"}, {"workers_in_flight", "in_flight"}, {"forge_issues", "issues"}} {
		if r[pair[0]] != float64(1) || r[pair[0]] != r[pair[1]] {
			t.Fatalf("alias %s differs: %v", pair[0], r)
		}
	}
}

func TestQuietPollingAC6TwoManagedParents(t *testing.T) {
	f := managedRefinementFixture(t)
	const secondParent = 124
	f.forge.addIssue(fakeIssue{ID: 1240, Number: secondParent, Title: "Second parent", User: stranger()})
	f.settle()
	second := f.sessionOf(secondParent)
	if err := f.db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: second.TaskRef, Generation: 1, State: "planning"}); err != nil {
		t.Fatal(err)
	}
	parents := []store.Session{f.sessionOf(testIssue), second}
	logs := &syncBuffer{}
	f.daemon = f.newDaemon(daemon.Options{Logger: slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})), Forge: emptyDiscoveryForge{refinementForge{Client: f.client, parent: testIssue}}})
	for tick := 0; tick < 3; tick++ {
		result := f.settle()
		if result.Issues != 0 || result.Dispatched != 2 {
			t.Fatalf("tick %d: %+v logs=%s", tick, result, logs)
		}
		dispatch := dispatchRecords(t, logs, "worker dispatched")
		finished := dispatchRecords(t, logs, "worker finished")
		if len(dispatch) != (tick+1)*2 || len(finished) != (tick+1)*2 {
			t.Fatalf("lifetime count: dispatched=%v finished=%v", dispatch, finished)
		}
		seen := map[any]bool{}
		for _, r := range dispatch[tick*2:] {
			seen[r["task"]] = true
			if !reflect.DeepEqual(r["dispatch_sources"], []any{"managed_registry"}) {
				t.Fatalf("unexpected enrollment=%v", r)
			}
		}
		for _, parent := range parents {
			if !seen[parent.TaskRef] {
				t.Fatalf("parent %s not attributed: %v", parent.TaskRef, dispatch)
			}
			session, err := f.db.GetSession(t.Context(), parent.ID)
			if err != nil {
				t.Fatal(err)
			}
			if session.NeedsPoll || session.LockOwner != "" {
				t.Fatalf("unclean parent after quiescence: %+v", session)
			}
			runs, err := f.db.ListRuns(t.Context(), parent.ID)
			if err != nil || len(runs) != 0 {
				t.Fatalf("quiet parent prepared runs=%v err=%v", runs, err)
			}
		}
		for _, r := range finished[tick*2:] {
			if r["execution_requests"] != float64(0) || r["status"] != "completed" {
				t.Fatalf("quiet outcome=%v", r)
			}
		}
		if len(f.turns.calls()) != 0 || len(dispatchRecords(t, logs, "run prepared")) != 0 {
			t.Fatal("quiet polling requested execution")
		}
	}
}

func TestQuietPollingAC7ChildInputWithoutParentChange(t *testing.T) {
	for _, promoted := range []bool{false, true} {
		name := "closed child refinement"
		if promoted {
			name = "post promotion refusal"
		}
		t.Run(name, func(t *testing.T) {
			f := managedRefinementFixture(t)
			parent := f.session()
			repo := github.Repo{Owner: testOwnerName, Name: testRepoName}
			before, err := f.client.GetIssue(t.Context(), repo, testIssue)
			if err != nil {
				t.Fatal(err)
			}
			if promoted {
				if err := f.db.CompareManagedFeature(t.Context(), store.ManagedFeature{ParentTask: parent.TaskRef, Generation: 1, State: "implementing"}, "planning"); err != nil {
					t.Fatal(err)
				}
			}
			f.daemon = f.newDaemon(daemon.Options{Forge: emptyDiscoveryForge{refinementForge{Client: f.client, parent: testIssue}}})
			f.forge.addComment(200, 80, owner(), "new child acceptance input")
			after, err := f.client.GetIssue(t.Context(), repo, testIssue)
			if err != nil {
				t.Fatal(err)
			}
			if !after.UpdatedAt.Equal(before.UpdatedAt) {
				t.Fatal("child fixture changed parent timestamp")
			}
			result := f.settle()
			if result.Issues != 0 || result.Dispatched != 1 {
				t.Fatalf("registry admission=%+v", result)
			}
			calls := f.turns.calls()
			if promoted {
				if len(calls) != 0 {
					t.Fatal("promoted child input executed")
				}
				posts, _, _ := f.forge.snapshot()
				found := false
				for _, p := range posts {
					if strings.Contains(p, "locked after promotion") {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing promotion refusal: %v", posts)
				}
			} else if len(calls) != 1 || calls[0].Issue.Number != testIssue || calls[0].Trigger.CommentID != 80 || calls[0].Mode != agent.ModePlan {
				t.Fatalf("child input not routed to parent: %+v", calls)
			}
			if before.UpdatedAt.IsZero() {
				t.Fatal("fixture lacks parent timestamp")
			}
			if f.watermark() != 0 {
				t.Fatal("child input advanced parent watermark")
			}
		})
	}
}
