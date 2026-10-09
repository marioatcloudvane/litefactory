package daemon_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

func graphClosureFixture(t *testing.T, mode string) (*fixture, *closureForge, store.GraphInstance) {
	t.Helper()
	f, g := closedBacklogFixture(t)
	ctx := t.Context()
	at := f.clock.Now()
	def := store.GraphDefinition{Team: "closure", Mode: mode, ProfileVersion: 1, Path: mode + ".dot", Digest: strings.Repeat("d", 64), CreatedAt: at}
	if mode == "bug" {
		def.Mode, def.Workflow = "", mode
	}
	def, err := f.db.CreateGraphDefinition(ctx, def)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := f.db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: f.session().ID, DefinitionID: def.ID, CurrentNode: "waiting", StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "waiting", Limit: 3, StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.db.FinishGraphNodeAttempt(ctx, attempt.ID, store.GraphSucceeded, "done", at); err != nil {
		t.Fatal(err)
	}
	if err = f.db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "closure-question", InstanceID: instance.ID, SourceAttemptID: attempt.ID, Node: "waiting", Kind: "question", Candidate: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	if err = f.db.ClearSessionNeedsPoll(ctx, instance.SessionID); err != nil {
		t.Fatal(err)
	}
	return f, g, instance
}

func TestGraphIssueClosureDiscoversIdleWaits(t *testing.T) {
	for _, mode := range []string{"research", "plan", "implement", "validate", "spike", "bug"} {
		t.Run(mode, func(t *testing.T) {
			f, g, instance := graphClosureFixture(t, mode)
			if f.session().NeedsPoll || !instance.NextDueAt.IsZero() {
				t.Fatal("fixture is not idle")
			}
			for range 2 {
				f.settle()
			}
			got, err := f.db.GetGraphInstance(t.Context(), instance.ID)
			if err != nil || got.Status != store.GraphCancelled || got.FinishedAt.IsZero() || !got.NextDueAt.IsZero() {
				t.Fatalf("instance=%+v %v", got, err)
			}
			wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
			if err != nil || wait.State != "cancelled" {
				t.Fatalf("wait=%+v %v", wait, err)
			}
			trace, err := f.db.ListGraphTransitions(t.Context(), instance.ID)
			if err != nil || len(trace) != 1 || trace[0].OnOutcome != "issue closed by owner" {
				t.Fatalf("trace=%+v %v", trace, err)
			}
			if len(g.reads) != 1 || len(f.turns.calls()) != 0 {
				t.Fatalf("reads=%v turns=%v", g.reads, f.turns.calls())
			}
		})
	}
}

func TestGraphIssueClosureRequiresConfirmedClosure(t *testing.T) {
	for _, state := range []string{"open", "unknown", "error"} {
		t.Run(state, func(t *testing.T) {
			f, g, instance := graphClosureFixture(t, "validate")
			g.after = func(_ context.Context, _ int, issue github.Issue, _ error) (github.Issue, error) {
				issue.State = state
				if state == "error" {
					return issue, errors.New("lookup failed")
				}
				return issue, nil
			}
			f.settle()
			got, err := f.db.GetGraphInstance(t.Context(), instance.ID)
			if err != nil || !reflect.DeepEqual(instance, got) {
				t.Fatalf("uncertain observation changed graph: %+v %v", got, err)
			}
			g.after = nil
			f.settle()
			got, err = f.db.GetGraphInstance(t.Context(), instance.ID)
			if err != nil || got.Status != store.GraphCancelled {
				t.Fatalf("closure retry: %+v %v", got, err)
			}
		})
	}
}

func TestGraphIssueClosureRespectsSessionLock(t *testing.T) {
	f, _, instance := graphClosureFixture(t, "research")
	if ok, err := f.db.AcquireSessionLock(t.Context(), instance.SessionID, "other-worker"); err != nil || !ok {
		t.Fatalf("lock: %v %v", ok, err)
	}
	f.settle()
	got, err := f.db.GetGraphInstance(t.Context(), instance.ID)
	if err != nil || !reflect.DeepEqual(instance, got) {
		t.Fatalf("locked graph changed: %+v %v", got, err)
	}
	if err = f.db.ReleaseSessionLock(t.Context(), instance.SessionID, "other-worker"); err != nil {
		t.Fatal(err)
	}
	f.clock.advance(time.Minute)
	f.settle()
	got, err = f.db.GetGraphInstance(t.Context(), instance.ID)
	if err != nil || got.Status != store.GraphCancelled {
		t.Fatalf("unlocked graph: %+v %v", got, err)
	}
}

func TestGraphIssueClosureLetsRunningRunFinish(t *testing.T) {
	f, _, instance := graphClosureFixture(t, "spike")
	run := f.allocateRunningRun(f.session(), 5501)
	before, err := f.db.GetRun(t.Context(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.settle()
	got, err := f.db.GetGraphInstance(t.Context(), instance.ID)
	if err != nil || !reflect.DeepEqual(instance, got) {
		t.Fatalf("running graph changed: %+v %v", got, err)
	}
	after, err := f.db.GetRun(t.Context(), run.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("running run changed: %+v %v", after, err)
	}
}
