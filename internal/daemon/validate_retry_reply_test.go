package daemon_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// validateOwnerWaitFixture parks a Validate instance at a published, confirmed
// owner question on a managed parent past planning, so an owner comment goes
// through the real tick reply path. The turn records the owner wait it sees.
func validateOwnerWaitFixture(t *testing.T) (*fixture, store.GraphInstance, *[]turn.Request, *[]store.GraphOwnerWait) {
	t.Helper()
	f := newFixture(t)
	var calls []turn.Request
	var seen []store.GraphOwnerWait
	var instanceID int64
	f.turns.fn = func(ctx context.Context, req turn.Request) (turn.Result, error) {
		calls = append(calls, req)
		wait, err := f.db.GetGraphOwnerWait(ctx, instanceID)
		if err != nil {
			t.Errorf("read the owner wait at execution: %v", err)
		}
		seen = append(seen, wait)
		return f.ranSuccessfully(ctx, req)
	}
	f.plantCursor()
	f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Parent", User: stranger(), Labels: toFakeLabels("Mode: Implement")})
	f.settle()
	session := f.sessionOf(testIssue)
	ctx := t.Context()
	ref := session.TaskRef
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(f.db.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: ref, Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}))
	check(f.db.CompareManagedFeature(ctx, store.ManagedFeature{ParentTask: ref, Generation: 1, State: "implementing"}, "planning"))
	_, err := f.db.AdmitWorkflowPath(ctx, session.ID, "graph")
	check(err)
	def, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "builtin", Mode: "validate", ProfileVersion: 1, Path: "validate/validate.dot", Digest: strings.Repeat("a", 64)})
	check(err)
	at := f.clock.Now()
	instance, err := f.db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: session.ID, DefinitionID: def.ID, CurrentNode: "owner", StartedAt: at})
	check(err)
	instanceID = instance.ID
	attempt, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "owner", Limit: 3, StartedAt: at})
	check(err)
	check(f.db.FinishGraphNodeAttempt(ctx, attempt.ID, store.GraphSucceeded, "done", at))
	check(f.db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "validate-question", InstanceID: instance.ID, SourceAttemptID: attempt.ID, Node: "owner", Kind: "question", Candidate: []byte("{}")}))
	_, err = f.db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: "validate-question", SessionID: session.ID, TriggerID: 20, Task: ref, Destination: "validate", TriggerNamespace: "issue-comment", Purpose: "validate-question", BotUserID: 99, Body: "Validate report", Marker: "validate-question"})
	check(err)
	_, err = f.db.MarkReportDeliverySent(ctx, "validate-question")
	check(err)
	_, err = f.db.ConfirmReportDelivery(ctx, "validate-question", 30)
	check(err)
	_, err = f.db.CompleteReportDelivery(ctx, "validate-question")
	check(err)
	wait, err := f.db.GetGraphOwnerWait(ctx, instance.ID)
	if err != nil || wait.State != "waiting" {
		t.Fatalf("fixture wait: %+v %v", wait, err)
	}
	return f, instance, &calls, &seen
}

// #577: the owner's /validate start reaches Validate admission with the owner
// wait still open, so admission can end a run whose reviews did not happen. A
// refused start leaves the wait open; a following Validate control command is
// still claimed as the owner's reply.
func TestValidateStartIsNotClaimedAsTheOwnerReply(t *testing.T) {
	f, instance, calls, seen := validateOwnerWaitFixture(t)
	f.forge.addComment(testIssue, 42, owner(), "/validate check the finished work")
	f.settle()
	if len(*calls) != 1 || (*calls)[0].Mode != agent.ModeValidate || (*calls)[0].Command.Name != "/validate" {
		t.Fatalf("the start runs as Validate: %+v\n%s", *calls, f.logs)
	}
	if w := (*seen)[0]; w.State != "waiting" || w.ReplyID != 0 {
		t.Fatalf("admission sees the open owner wait, not a claim: %+v", w)
	}
	wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	if err != nil || wait.State != "waiting" || wait.ReplyID != 0 {
		t.Fatalf("the start leaves the wait open: %+v %v", wait, err)
	}

	f.forge.addComment(testIssue, 43, owner(), turn.ValidateCheckCommand+" r1 story-001:AC-001 pass")
	f.settle()
	if len(*calls) != 2 || (*calls)[1].Mode != agent.ModeValidate {
		t.Fatalf("the control command runs as Validate: %+v\n%s", *calls, f.logs)
	}
	wait, err = f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	if err != nil || wait.State != "claimed" || wait.ReplyID != 43 {
		t.Fatalf("the control command is claimed as the owner reply: %+v %v", wait, err)
	}
}
