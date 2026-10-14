package daemon_test

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/naming"
	"reflect"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// These rows model the pre-fix stop: consumed owner authority, exhausted fixer,
// successful revalidation transition, and no continuation work after reopening.
func TestStrandedBugStartupAndTick(t *testing.T) {
	for _, entry := range []string{"startup", "tick"} {
		t.Run(entry, func(t *testing.T) {
			f, instance, old := stoppedGraphFixture(t, true)
			ctx, at := t.Context(), f.clock.Now()
			check := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			_, ok, err := f.db.ReserveGraphDispatch(ctx, old.ID, at)
			check(err)
			if !ok {
				t.Fatal("reserve refused")
			}
			check(f.db.FinishGraphDispatch(ctx, old.ID, at))
			check(f.db.ParkGraphNode(ctx, store.GraphStop{Key: "consumed", InstanceID: instance.ID, Origin: "conversation", Cause: "exhausted", Reason: "old stop", Candidate: []byte(`{}`), At: at, AllowRunless: true}))
			session := f.sessionOf(testIssue)
			_, err = f.db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: "consumed", SessionID: session.ID, Task: session.TaskRef, Destination: "test", TriggerNamespace: "graph", Purpose: "bug-question", BotUserID: botID, Body: "old question", Marker: "consumed"})
			check(err)
			_, err = f.db.MarkReportDeliverySent(ctx, "consumed")
			check(err)
			_, err = f.db.ConfirmReportDelivery(ctx, "consumed", 101)
			check(err)
			_, err = f.db.CompleteReportDelivery(ctx, "consumed")
			check(err)
			_, err = f.db.ClaimGraphOwnerReply(ctx, store.GraphOwnerReply{InstanceID: instance.ID, OwnerID: ownerID, AuthorID: ownerID, CommentID: 102, QuestionCommentID: 101, TaskRef: session.TaskRef, SourceRef: session.TaskRef, Workflow: "bug", DeliveryKey: "consumed", Body: "continue", At: at})
			check(err)
			a, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "conversation", Limit: 16, StartedAt: at})
			check(err)
			check(f.db.CompleteGraphNodeAttempt(ctx, a.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "approved", FinishedAt: at, InstanceStatus: store.GraphRunning, Transition: &store.GraphTransition{InstanceID: instance.ID, FromNode: "conversation", ToNode: "fixer", OnOutcome: "approved", At: at}}))
			for i := range 3 {
				a, err = f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "fixer", Limit: 3, StartedAt: at})
				check(err)
				if i < 2 {
					check(f.db.FinishGraphNodeAttempt(ctx, a.ID, store.GraphSucceeded, "succeeded", at))
				}
			}
			// Move through finalization using the same atomic completion as runtime.
			check(f.db.CompleteGraphNodeAttempt(ctx, a.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "succeeded", FinishedAt: at, InstanceStatus: store.GraphRunning, Transition: &store.GraphTransition{InstanceID: instance.ID, FromNode: "fixer", ToNode: "finalization", OnOutcome: "succeeded", At: at}}))
			final, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "finalization", Limit: 16, StartedAt: at})
			check(err)
			check(f.db.CompleteGraphNodeAttempt(ctx, final.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "revalidate", FinishedAt: at, InstanceStatus: store.GraphRunning, Outputs: []store.GraphOutput{{Kind: store.GraphOutputJSON, Payload: []byte(`{"record":"delivery_progress","outcome":"revalidate","reason":"main was merged"}`)}}, Transition: &store.GraphTransition{InstanceID: instance.ID, FromNode: "finalization", ToNode: "fixer", OnOutcome: "revalidate", At: at}}))
			before, err := f.db.ListGraphNodeAttempts(ctx, instance.ID)
			check(err)
			oldWait, err := f.db.GetGraphOwnerWait(ctx, instance.ID)
			check(err)
			if oldWait.State != "allocated" {
				t.Fatalf("wait=%+v", oldWait)
			}
			// Open a new connection to the persisted fixture, with all runtime dependencies
			// rebound to it. The original fixture connection stays owned by its cleanup.
			db, err := store.Open(ctx, f.db.Path())
			check(err)
			t.Cleanup(func() { check(db.Close()) })
			f.db = db
			renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{}), Limit: github.CommentSizeLimit})
			check(err)
			reporter, err := turn.NewReporter(turn.ReporterOptions{DB: db, Forge: f.client, Renderer: renderer, Logger: f.logger, BotUserID: botID})
			check(err)
			f.reporter = reporter
			runtime, err := turn.NewGraphBugRuntime(ctx, turn.Options{DB: db, Git: new(git.Binary), CloneGate: unusedRecoveryCloneGate{}, ResearchReporter: reporter, MCP: &turn.MCPRuntime{Now: f.clock.Now, Redactor: redact.New(redact.Options{Logger: f.logger}), BotID: botID}}, turn.BugRuntimeConfig{Forge: f.client, OwnerID: ownerID, Permissions: func(agent.Kind, string) (turn.BugRolePermissions, error) {
				return turn.BugRolePermissions{}, errors.New("exhaustion must not prepare a fixer")
			}})
			check(err)
			f.turns.fn = func(ctx context.Context, req turn.Request) (turn.Result, error) {
				ref, err := naming.ParseRef(session.TaskRef)
				if err != nil {
					return turn.Result{}, err
				}
				return runtime.Execute(ctx, req, turn.Result{Session: session, Ref: ref})
			}
			f.daemon = f.newDaemon(daemon.Options{Bug: runtime})
			if entry == "startup" {
				for range 2 {
					_, err = f.daemon.Start(ctx)
					check(err)
				}
				f.clock.advance(time.Now().Add(time.Hour).Sub(f.clock.peek()))
				due, err := db.ListDueGraphDispatches(ctx, f.clock.Now(), 10)
				check(err)
				if len(due) != 1 {
					t.Fatalf("pending=%+v", due)
				}
			}
			f.settle()
			wait, err := db.GetGraphOwnerWait(ctx, instance.ID)
			check(err)
			if wait.State != "waiting" || wait.DeliveryKey == oldWait.DeliveryKey {
				t.Fatalf("fresh wait=%+v logs=%s", wait, f.logs)
			}
			delivery, err := db.GetReportDelivery(ctx, wait.DeliveryKey)
			check(err)
			if delivery.CommentID == 0 {
				t.Fatal("notice not delivered")
			}
			for range 2 {
				f.settle()
			}
			after, err := db.ListGraphNodeAttempts(ctx, instance.ID)
			check(err)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("recovery changed attempts")
			}
			reopened, err := store.Open(ctx, db.Path())
			check(err)
			defer reopened.Close()
			same, err := reopened.GetGraphOwnerWait(ctx, instance.ID)
			check(err)
			if same.DeliveryKey != wait.DeliveryKey || same.State != "waiting" {
				t.Fatalf("wait changed: %+v", same)
			}
			posted, _, _ := f.forge.snapshot()
			if len(posted) != 1 {
				t.Fatalf("notices=%d", len(posted))
			}
		})
	}
}
