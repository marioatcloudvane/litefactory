package daemon_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// A Validate instance's owed step runs in Validate even though its issue still
// carries the Implement label and its managed feature is implementing; it must
// never be taken by the Implement runtime.
func TestValidateGraphStepRunsInValidateWhateverTheLabelSays(t *testing.T) {
	f := newFixture(t)
	var modes []agent.Mode
	f.turns.fn = func(ctx context.Context, req turn.Request) (turn.Result, error) {
		out, err := f.ranSuccessfully(ctx, req)
		if err != nil {
			return out, err
		}
		if _, ok := store.GraphTriggerDispatch(req.Trigger.CommentID); ok {
			modes = append(modes, req.Mode)
		}
		out.ResearchGraph = true
		return out, nil
	}
	f.plantCursor()
	f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Parent", User: stranger(), Labels: toFakeLabels("Mode: Implement")})
	f.settle()
	session := f.sessionOf(testIssue)
	ref := session.TaskRef
	if err := f.db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: ref, Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.CompareManagedFeature(t.Context(), store.ManagedFeature{ParentTask: ref, Generation: 1, State: "implementing"}, "planning"); err != nil {
		t.Fatal(err)
	}
	pin, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "validate", ProfileVersion: 1, Path: "validate/validate.dot", Digest: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	due := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: session.ID, DefinitionID: pin.ID, CurrentNode: "repair_agent", StartedAt: due, NextDueAt: due})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.ParkGraphInstance(t.Context(), instance.ID, "repair_agent", due, due); err != nil {
		t.Fatal(err)
	}

	f.settle()

	if len(modes) != 1 || modes[0] != agent.ModeValidate {
		t.Fatalf("graph step modes = %v, want one validate step\nlogs:\n%s", modes, f.logs)
	}
}

func TestValidateGraphStepWaitsUntilDue(t *testing.T) {
	f := newFixture(t)
	var modes []agent.Mode
	f.turns.fn = func(ctx context.Context, req turn.Request) (turn.Result, error) {
		out, err := f.ranSuccessfully(ctx, req)
		if err != nil {
			return out, err
		}
		if _, ok := store.GraphTriggerDispatch(req.Trigger.CommentID); ok {
			modes = append(modes, req.Mode)
		}
		out.ResearchGraph = true
		return out, nil
	}
	f.plantCursor()
	f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Parent", User: stranger(), Labels: toFakeLabels("Mode: Implement")})
	f.settle()
	session := f.sessionOf(testIssue)
	ref := session.TaskRef
	if err := f.db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: ref, Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.CompareManagedFeature(t.Context(), store.ManagedFeature{ParentTask: ref, Generation: 1, State: "implementing"}, "planning"); err != nil {
		t.Fatal(err)
	}
	pin, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "validate", ProfileVersion: 1, Path: "validate/validate.dot", Digest: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	due := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: session.ID, DefinitionID: pin.ID, CurrentNode: "repair_agent", StartedAt: due, NextDueAt: due})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.ParkGraphInstance(t.Context(), instance.ID, "repair_agent", due, due); err != nil {
		t.Fatal(err)
	}

	f.settle()
	if len(modes) != 0 {
		t.Fatalf("Validate dispatched before its due time: %v", modes)
	}
}

// validateStrayDispatchFixture parks a Validate instance at its factory-only
// suite stage on a managed parent past planning, with the stage's dispatch
// due at dueIn from now. turnFn handles every turn.
func validateStrayDispatchFixture(t *testing.T, dueIn time.Duration, turnFn func(*fixture, context.Context, turn.Request) (turn.Result, error)) (*fixture, store.GraphInstance, store.GraphDispatch) {
	t.Helper()
	f := newFixture(t)
	f.turns.fn = func(ctx context.Context, req turn.Request) (turn.Result, error) { return turnFn(f, ctx, req) }
	f.plantCursor()
	f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Parent", User: stranger(), Labels: toFakeLabels("Mode: Implement")})
	f.settle()
	session := f.sessionOf(testIssue)
	ctx := t.Context()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(f.db.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: session.TaskRef, Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}))
	check(f.db.CompareManagedFeature(ctx, store.ManagedFeature{ParentTask: session.TaskRef, Generation: 1, State: "implementing"}, "planning"))
	_, err := f.db.AdmitWorkflowPath(ctx, session.ID, "graph")
	check(err)
	def, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "builtin", Mode: "validate", ProfileVersion: 1, Path: "validate/validate.dot", Digest: strings.Repeat("a", 64)})
	check(err)
	at := f.clock.peek()
	instance, err := f.db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: session.ID, DefinitionID: def.ID, CurrentNode: "suite", StartedAt: at})
	check(err)
	dispatch, err := f.db.ParkGraphInstance(ctx, instance.ID, "suite", at.Add(dueIn), at)
	check(err)
	return f, instance, dispatch
}

// restValidateAtOwnerAsk does what the in-process factory stages do: the
// suite stage passes, the instance moves to waiting and the owner question is
// published and confirmed, which opens the owner wait.
func restValidateAtOwnerAsk(ctx context.Context, f *fixture, instance store.GraphInstance) error {
	at := f.clock.peek()
	attempt, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "suite", Limit: 3, StartedAt: at})
	if err != nil {
		return err
	}
	if err = f.db.CompleteGraphNodeAttempt(ctx, attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "published", FinishedAt: at,
		Transition: &store.GraphTransition{InstanceID: instance.ID, FromNode: "suite", ToNode: "waiting", OnOutcome: "published", At: at}}); err != nil {
		return err
	}
	if err = f.db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "validate-question", InstanceID: instance.ID, SourceAttemptID: attempt.ID, Node: "suite", Kind: "question", Candidate: []byte("{}")}); err != nil {
		return err
	}
	session, err := f.db.GetSession(ctx, instance.SessionID)
	if err != nil {
		return err
	}
	if _, err = f.db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: "validate-question", SessionID: session.ID, TriggerID: 20, Task: session.TaskRef, Destination: "validate", TriggerNamespace: "issue-comment", Purpose: "validate-question", BotUserID: 99, Body: "Validate report", Marker: "validate-question"}); err != nil {
		return err
	}
	if _, err = f.db.MarkReportDeliverySent(ctx, "validate-question"); err != nil {
		return err
	}
	if _, err = f.db.ConfirmReportDelivery(ctx, "validate-question", 30); err != nil {
		return err
	}
	_, err = f.db.CompleteReportDelivery(ctx, "validate-question")
	return err
}

func assertNoFalseStop(t *testing.T, f *fixture) {
	t.Helper()
	posts, _, _ := f.forge.snapshot()
	for _, body := range posts {
		if strings.Contains(body, "Work stopped") {
			t.Fatalf("a stop notice was posted: %q", body)
		}
	}
	for _, text := range []string{"invalid authenticated graph reply", "queue stops here", "could not start"} {
		if strings.Contains(f.logs.String(), text) {
			t.Fatalf("logs contain %q:\n%s", text, f.logs)
		}
	}
}

// #979 C1: a Validate stage that ran in the worker and rested at the owner ask
// without a run closes its dispatch instead of requeueing it.
func TestValidateDispatchThatRanWithoutARunIsFinished(t *testing.T) {
	var steps int
	var instance store.GraphInstance
	f, instance, dispatch := validateStrayDispatchFixture(t, 0, func(f *fixture, ctx context.Context, req turn.Request) (turn.Result, error) {
		if _, ok := store.GraphTriggerDispatch(req.Trigger.CommentID); !ok {
			return f.ranSuccessfully(ctx, req)
		}
		steps++
		if err := restValidateAtOwnerAsk(ctx, f, instance); err != nil {
			return turn.Result{}, err
		}
		session, err := f.db.GetSession(ctx, req.SessionID)
		if err != nil {
			return turn.Result{}, err
		}
		return turn.Result{Disposition: turn.DispositionRan, SessionID: session.ID, Session: session, TriggerCommentID: req.Trigger.CommentID, ResearchGraph: true}, nil
	})

	f.settle()
	f.clock.advance(time.Hour)
	f.settle()

	if steps != 1 {
		t.Fatalf("the stage ran %d times, want 1\n%s", steps, f.logs)
	}
	latest, err := f.db.LatestGraphDispatch(t.Context(), instance.ID)
	if err != nil || latest.ID != dispatch.ID || latest.Status != "finished" || latest.Attempts != 1 {
		t.Fatalf("dispatch not finished once: %+v %v\n%s", latest, err, f.logs)
	}
	wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	if err != nil || wait.State != "waiting" {
		t.Fatalf("owner wait: %+v %v", wait, err)
	}
	assertNoFalseStop(t, f)
}

// #979 C2, C3: a dispatch that fires after the instance moved on to the owner
// ask is dropped quietly, and the owner's next Validate command is still
// claimed against the open owner wait.
func TestStaleValidateDispatchIsDroppedQuietly(t *testing.T) {
	var calls []turn.Request
	f, instance, dispatch := validateStrayDispatchFixture(t, time.Hour, func(f *fixture, ctx context.Context, req turn.Request) (turn.Result, error) {
		calls = append(calls, req)
		return f.ranSuccessfully(ctx, req)
	})
	if err := restValidateAtOwnerAsk(t.Context(), f, instance); err != nil {
		t.Fatal(err)
	}
	moved, err := f.db.GetGraphInstance(t.Context(), instance.ID)
	if err != nil || moved.CurrentNode != "waiting" {
		t.Fatalf("instance: %+v %v", moved, err)
	}

	f.clock.advance(2 * time.Hour)
	f.settle()

	if len(calls) != 0 {
		t.Fatalf("the stale dispatch ran a turn: %+v\n%s", calls, f.logs)
	}
	latest, err := f.db.LatestGraphDispatch(t.Context(), instance.ID)
	if err != nil || latest.ID != dispatch.ID || latest.Status != "finished" {
		t.Fatalf("stale dispatch not closed: %+v %v\n%s", latest, err, f.logs)
	}
	if !strings.Contains(f.logs.String(), "dropped a stale graph dispatch") {
		t.Fatalf("no stale-drop log:\n%s", f.logs)
	}
	assertNoFalseStop(t, f)

	const ship = 900_000
	f.forge.addComment(testIssue, ship, owner(), turn.ValidateShipCommand+" r1 -- All good")
	f.settle()
	if len(calls) != 1 || calls[0].Trigger.CommentID != ship || calls[0].Mode != agent.ModeValidate {
		t.Fatalf("the ship command did not reach Validate: %+v\n%s", calls, f.logs)
	}
	wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	if err != nil || wait.State != "claimed" || wait.ReplyID != ship {
		t.Fatalf("the ship command was not claimed: %+v %v", wait, err)
	}
	assertNoFalseStop(t, f)
}
