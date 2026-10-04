package daemon_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/turn"
)

func stoppedGraphFixture(t *testing.T, bug ...bool) (*fixture, store.GraphInstance, store.GraphDispatch) {
	t.Helper()
	f := newFixture(t)
	isBug := len(bug) > 0 && bug[0]
	if isBug {
		runtime, err := turn.NewGraphBugRuntime(t.Context(), turn.Options{DB: f.db, Git: new(git.Binary), CloneGate: unusedRecoveryCloneGate{}, ResearchReporter: f.reporter, MCP: &turn.MCPRuntime{Redactor: redact.New(redact.Options{Logger: f.logger}), BotID: botID}}, turn.BugRuntimeConfig{Forge: f.client, OwnerID: ownerID, Permissions: func(agent.Kind, string) (turn.BugRolePermissions, error) {
			return turn.BugRolePermissions{}, errors.New("test must not allocate a role")
		}})
		if err != nil {
			t.Fatal(err)
		}
		f.daemon = f.newDaemon(daemon.Options{Bug: runtime})
	}
	f.plantCursor()
	label := "Mode: Research"
	if isBug {
		label = "bug"
	}
	f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Stopped graph", User: stranger(), Labels: toFakeLabels(label)})
	f.settle()
	s := f.sessionOf(testIssue)
	if _, err := f.db.AdmitWorkflowPath(t.Context(), s.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	definition := store.GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research/research.dot", Digest: strings.Repeat("a", 64)}
	if isBug {
		specs, err := turn.SimplifiedBugSpecs()
		if err != nil {
			t.Fatal(err)
		}
		capture, err := teamsource.CaptureBugParent(t.Context(), specs)
		if err != nil {
			t.Fatal(err)
		}
		definition = store.GraphDefinition{Team: "builtin", Workflow: "bug", ProfileVersion: 2, Path: "bug/bug.dot", Digest: capture.Profile.Digest}
		if _, err = f.db.AdmitWorkflow(t.Context(), store.TaskWorkflowAdmission{SessionID: s.ID, Version: 1, Family: "standalone", Workflow: "bug", SourceTrigger: 22}); err != nil {
			t.Fatal(err)
		}
	}
	pin, err := f.db.CreateGraphDefinition(t.Context(), definition)
	if err != nil {
		t.Fatal(err)
	}
	launch := store.WorkflowLaunch{Mode: "research", SessionID: s.ID, TriggerID: 22, DefinitionID: pin.ID, Start: "research", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: []byte(`{"research.dot":"eA=="}`)}
	if isBug {
		launch.Mode = ""
		launch.Workflow = "bug"
		launch.Start = "conversation"
	}
	admitted, err := f.db.AdmitWorkflowLaunch(t.Context(), launch)
	if err != nil {
		t.Fatal(err)
	}
	at := f.clock.Now()
	d, err := f.db.ParkGraphInstance(t.Context(), admitted.Instance.ID, admitted.Instance.CurrentNode, at, at)
	if err != nil {
		t.Fatal(err)
	}
	return f, admitted.Instance, d
}

func TestOwnerReplyResumesCancelledSavedGraph(t *testing.T) {
	f, instance, dispatch := stoppedGraphFixture(t)
	if err := f.db.CancelGraphDispatch(t.Context(), dispatch.ID, "startup stopped", f.clock.Now()); err != nil {
		t.Fatal(err)
	}
	f.turns.fn = func(ctx context.Context, req turn.Request) (turn.Result, error) {
		if !store.IsGraphTrigger(req.Trigger.CommentID) {
			t.Error("recovery also executed an owner-comment run")
		}
		got, err := f.db.GetGraphInstance(ctx, instance.ID)
		if err != nil || got.DefinitionID != instance.DefinitionID || got.CurrentNode != instance.CurrentNode {
			t.Errorf("saved work changed: %+v %v", got, err)
		}
		return f.ranSuccessfully(ctx, req)
	}
	f.forge.addComment(testIssue, 42, owner(), "Please resume")
	f.settle()
	calls := f.turns.calls()
	if len(calls) != 1 || !store.IsGraphTrigger(calls[0].Trigger.CommentID) {
		t.Fatalf("calls: %+v\n%s", calls, f.logs)
	}
	wm, exists, err := f.db.GetWatermark(t.Context(), instance.SessionID)
	if err != nil || !exists || wm.LastCommentID < 42 {
		t.Fatalf("reply unconsumed: %+v %v", wm, err)
	}
	f.settle()
	if len(f.turns.calls()) != 1 {
		t.Fatal("replay executed again")
	}
}

func TestDispatchExhaustionPublishesOneStopNotice(t *testing.T) {
	for _, bug := range []bool{false, true} {
		name := "ordinary"
		if bug {
			name = "bug"
		}
		t.Run(name, func(t *testing.T) { testDispatchExhaustion(t, bug) })
	}
}

func testDispatchExhaustion(t *testing.T, bug bool) {
	f, instance, dispatch := stoppedGraphFixture(t, bug)
	f.turns.fn = func(context.Context, turn.Request) (turn.Result, error) {
		return turn.Result{}, errors.New("startup refused")
	}
	for n := 0; n < store.MaxGraphDispatchAttempts; n++ {
		f.clock.advance(time.Hour)
		f.settle()
	}
	latest, err := f.db.LatestGraphDispatch(t.Context(), instance.ID)
	if err != nil || latest.ID != dispatch.ID || latest.Status != "cancelled" {
		t.Fatalf("not stopped: %+v %v\n%s", latest, err, f.logs)
	}
	count := 0
	posts, _, _ := f.forge.snapshot()
	for _, body := range posts {
		if strings.Contains(body, "Work stopped at "+instance.CurrentNode+":") {
			count++
			if !strings.Contains(body, "startup refused") || !strings.Contains(body, "Reply on this open issue to resume") {
				t.Fatalf("notice: %s", body)
			}
		}
	}
	if count != 1 {
		t.Fatalf("stop notices=%d posts=%v\n%s", count, posts, f.logs)
	}
	f.clock.advance(time.Hour)
	f.settle()
	posts2, _, _ := f.forge.snapshot()
	if len(posts2) != len(posts) {
		t.Fatal("stopped tick posted again")
	}
}

func TestCancelledGraphNeedsReplyAfterReopening(t *testing.T) {
	f, instance, dispatch := stoppedGraphFixture(t)
	f.forge.closeIssue(testIssue)
	f.settle()
	latest, err := f.db.LatestGraphDispatch(t.Context(), instance.ID)
	if err != nil || latest.ID != dispatch.ID || latest.Status != "cancelled" {
		t.Fatalf("closure: %+v %v", latest, err)
	}
	if len(f.turns.calls()) != 0 {
		t.Fatal("closed work executed")
	}
	posts, _, _ := f.forge.snapshot()
	for _, p := range posts {
		if strings.Contains(p, "Work stopped at") {
			t.Fatal("closure announced recovery")
		}
	}
	f.forge.mu.Lock()
	rs := f.forge.state(testOwnerName, testRepoName)
	for i := range rs.issues {
		if rs.issues[i].Number == testIssue {
			rs.issues[i].State = "open"
			rs.issues[i].UpdatedAt = f.clock.Now()
		}
	}
	f.forge.mu.Unlock()
	f.settle()
	if len(f.turns.calls()) != 0 {
		t.Fatal("reopen alone executed work")
	}
	f.forge.addComment(testIssue, 42, owner(), "Resume the saved work")
	f.settle()
	calls := f.turns.calls()
	if len(calls) != 1 || !store.IsGraphTrigger(calls[0].Trigger.CommentID) {
		t.Fatalf("reply failed recovery: %+v\n%s", calls, f.logs)
	}
}

// No source preparation is permitted in these startup-failure fixtures.
type unusedRecoveryCloneGate struct{}

func (unusedRecoveryCloneGate) Acquire(context.Context, string) (func(), error) {
	return nil, errors.New("unexpected source preparation")
}
