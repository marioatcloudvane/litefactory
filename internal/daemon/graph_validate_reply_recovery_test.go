package daemon_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// stoppedValidateFixture parks a Validate instance at repair_agent on an
// Implement-labelled managed parent past planning, which also has an older
// Implement instance, and cancels the dispatch without allocating a run: the
// state #529 was left in. Plain owner comments there resolve to Implement.
func stoppedValidateFixture(t *testing.T, launched bool) (*fixture, store.GraphInstance, store.GraphDispatch, *[]turn.Request) {
	t.Helper()
	f := newFixture(t)
	var calls []turn.Request
	f.turns.fn = func(ctx context.Context, req turn.Request) (turn.Result, error) {
		calls = append(calls, req)
		out, err := f.ranSuccessfully(ctx, req)
		out.ResearchGraph = store.IsGraphTrigger(req.Trigger.CommentID)
		return out, err
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
	if _, err := f.db.AdmitWorkflowPath(t.Context(), session.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	implement, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "implement", ProfileVersion: 1, Path: "implement/implement.dot", Digest: strings.Repeat("d", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: session.ID, DefinitionID: implement.ID, CurrentNode: "await", StartedAt: f.clock.Now()}); err != nil {
		t.Fatal(err)
	}
	validate, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "validate", ProfileVersion: 1, Path: "validate/validate.dot", Digest: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	var instance store.GraphInstance
	if launched {
		admitted, err := f.db.AdmitWorkflowLaunch(t.Context(), store.WorkflowLaunch{Mode: "validate", SessionID: session.ID, TriggerID: 22, DefinitionID: validate.ID, Start: "repair_agent", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: graphCapacityFiles(t, "validate", "repair_agent")})
		if err != nil {
			t.Fatal(err)
		}
		instance = admitted.Instance
	} else {
		instance, err = f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: session.ID, DefinitionID: validate.ID, CurrentNode: "repair_agent", StartedAt: f.clock.Now()})
		if err != nil {
			t.Fatal(err)
		}
	}
	if instance.CurrentNode != "repair_agent" {
		t.Fatalf("instance at %q", instance.CurrentNode)
	}
	at := f.clock.Now()
	dispatch, err := f.db.ParkGraphInstance(t.Context(), instance.ID, instance.CurrentNode, at, at)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.db.CancelGraphDispatch(t.Context(), dispatch.ID, "startup stopped", at); err != nil {
		t.Fatal(err)
	}
	stopped, err := f.db.GetGraphInstance(t.Context(), instance.ID)
	if err != nil || !stopped.NextDueAt.IsZero() {
		t.Fatalf("not stopped: %+v %v", stopped, err)
	}
	return f, stopped, dispatch, &calls
}

// An owner reply on a Validate stopped at repair_agent resumes that node in
// Validate, although the reply itself resolves to Implement (#558).
func TestOwnerReplyResumesValidateStoppedAtRepairAgent(t *testing.T) {
	f, instance, cancelled, calls := stoppedValidateFixture(t, true)
	assertValidateStopNotice(t, f, instance, true)
	if _, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("fixture must have no published question wait: %v", err)
	}
	const reply = "Continue the saved validation; retain the earlier edits and check the repair."
	f.forge.addComment(testIssue, 42, owner(), reply)
	f.settle()
	input, err := f.db.GetOwnerInstruction(t.Context(), instance.SessionID, 42)
	if err != nil || input.Body != reply || input.ActorID != ownerID {
		t.Fatalf("owner reply guidance was not retained: %+v %v", input, err)
	}
	if len(*calls) != 1 || !store.IsGraphTrigger((*calls)[0].Trigger.CommentID) || (*calls)[0].Mode != agent.ModeValidate {
		t.Fatalf("calls: %+v\n%s", *calls, f.logs)
	}
	resumed, ok := store.GraphTriggerDispatch((*calls)[0].Trigger.CommentID)
	if !ok || resumed == cancelled.ID {
		t.Fatalf("ran dispatch %d, cancelled %d", resumed, cancelled.ID)
	}
	latest, err := f.db.LatestGraphDispatch(t.Context(), instance.ID)
	if err != nil || latest.ID != resumed || latest.Node != "repair_agent" || latest.LaunchID != cancelled.LaunchID || latest.InstanceID != instance.ID {
		t.Fatalf("no fresh repair_agent dispatch: %+v %v", latest, err)
	}
	wm, exists, err := f.db.GetWatermark(t.Context(), instance.SessionID)
	if err != nil || !exists || wm.LastCommentID < 42 {
		t.Fatalf("reply unconsumed: %+v %v", wm, err)
	}
	f.settle()
	again, err := f.db.LatestGraphDispatch(t.Context(), instance.ID)
	if len(*calls) != 1 || err != nil || again.ID != latest.ID {
		t.Fatalf("replay resumed again: calls=%d latest=%+v %v", len(*calls), again, err)
	}
}

// A stopped instance a reply cannot resume answers the reply with the reason
// and consumes it, instead of running it as an ordinary comment.
func TestOwnerReplyOnUnresumableStoppedWorkIsAnswered(t *testing.T) {
	f, instance, cancelled, calls := stoppedValidateFixture(t, false)
	assertValidateStopNotice(t, f, instance, false)
	f.forge.addComment(testIssue, 42, owner(), "Please resume")
	f.settle()
	if len(*calls) != 0 {
		t.Fatalf("calls: %+v\n%s", *calls, f.logs)
	}
	latest, err := f.db.LatestGraphDispatch(t.Context(), instance.ID)
	if err != nil || latest.ID != cancelled.ID {
		t.Fatalf("dispatch changed: %+v %v", latest, err)
	}
	posts, _, _ := f.forge.snapshot()
	found := 0
	for _, body := range posts {
		if strings.Contains(body, "could not resume the stopped work at `repair_agent` because it has no saved launch to resume from") {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("posts: %v\n%s", posts, f.logs)
	}
	wm, exists, err := f.db.GetWatermark(t.Context(), instance.SessionID)
	if err != nil || !exists || wm.LastCommentID < 42 {
		t.Fatalf("reply unconsumed: %+v %v", wm, err)
	}
	f.settle()
	if posts2, _, _ := f.forge.snapshot(); len(posts2) != len(posts) || len(*calls) != 0 {
		t.Fatal("replay answered again")
	}
}

// The same notice owner used for exhausted dispatches describes an explicitly
// stopped latest Validate instance before its owner sends a continuation reply.
func assertValidateStopNotice(t *testing.T, f *fixture, instance store.GraphInstance, launched bool) {
	t.Helper()
	dispatch, err := f.db.LatestGraphDispatch(t.Context(), instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.reporter.ReportGraphDispatchStop(t.Context(), dispatch); err != nil {
		t.Fatal(err)
	}
	posts, _, _ := f.forge.snapshot()
	if len(posts) != 1 {
		t.Fatalf("stop notices: %v", posts)
	}
	for _, want := range []string{"repair_agent", "startup stopped"} {
		if !strings.Contains(posts[0], want) {
			t.Fatalf("stop notice lacks %q: %s", want, posts[0])
		}
	}
	if launched {
		if !strings.Contains(posts[0], "Reply on this open issue") || strings.Contains(posts[0], "/doctor") {
			t.Fatalf("saved step notice must select the ordinary reply: %s", posts[0])
		}
	} else if !strings.Contains(posts[0], "no saved launch") || !strings.Contains(posts[0], "/doctor inspect") || strings.Contains(posts[0], "Reply on this open issue") {
		t.Fatalf("missing launch notice must explain refusal and select inspection: %s", posts[0])
	}
	if _, err = f.reporter.ReportGraphDispatchStop(t.Context(), dispatch); err != nil {
		t.Fatal(err)
	}
	if again, _, _ := f.forge.snapshot(); len(again) != len(posts) {
		t.Fatalf("unchanged stop notice was duplicated: %v", again)
	}
}
