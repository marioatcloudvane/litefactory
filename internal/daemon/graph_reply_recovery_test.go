package daemon_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
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
	var bugCapture *teamsource.WorkflowDefinition
	definition := store.GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research/research.dot", Digest: strings.Repeat("a", 64)}
	if isBug {
		specs, err := turn.SimplifiedBugSpecs()
		if err != nil {
			t.Fatal(err)
		}
		// The recovery fixtures capture the current Bug source; their synthetic
		// histories write the graph rows directly instead of running agents.
		capture, err := teamsourcetest.CaptureBug(t.Context(), specs)
		if err != nil {
			t.Fatal(err)
		}
		bugCapture = capture
		definition = store.GraphDefinition{Team: capture.Profile.Team, Workflow: "bug", ProfileVersion: 2, Path: "bug/bug.dot", Digest: capture.Profile.Digest}
		if _, err = f.db.AdmitWorkflow(t.Context(), store.TaskWorkflowAdmission{SessionID: s.ID, Version: 1, Family: "standalone", Workflow: "bug", SourceTrigger: 22}); err != nil {
			t.Fatal(err)
		}
	}
	pin, err := f.db.CreateGraphDefinition(t.Context(), definition)
	if err != nil {
		t.Fatal(err)
	}
	launch := store.WorkflowLaunch{Mode: "research", SessionID: s.ID, TriggerID: 22, DefinitionID: pin.ID, Start: "research", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: graphCapacityFiles(t, "research", "research")}
	if isBug {
		launch.Mode = ""
		launch.Workflow = "bug"
		launch.Start = "conversation"
		launch.ContentDigest = bugCapture.Digest
		launch.TopologyDigest, err = graphexec.SemanticDigest(bugCapture.Profile)
		if err != nil {
			t.Fatal(err)
		}
		launch.Files, err = json.Marshal(bugCapture.Files)
		if err != nil {
			t.Fatal(err)
		}
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
		if strings.Contains(body, "Work at `"+instance.CurrentNode+"`") {
			count++
			if !strings.Contains(body, "startup refused") || !strings.Contains(body, "Reply on this open issue with guidance to resume") {
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
		if strings.Contains(p, "Work at `") {
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

// A small coordinator loop uses the real node executor and store to cross the
// runless seam: the dispatch names await, but the exhausted node is merge.
type limitCoordinatorActivity struct{}

func (limitCoordinatorActivity) Execute(context.Context, turn.Options, turn.ActivityRequest) (turn.ActivityResult, error) {
	return turn.ActivityResult{Outcome: "next"}, nil
}

func limitCoordinatorFixture(t *testing.T) (*fixture, turn.GraphNodeRequest, store.GraphDispatch) {
	t.Helper()
	f := newFixture(t)
	f.plantCursor()
	f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Coordinator limit", User: stranger(), Labels: toFakeLabels("Mode: Implement")})
	f.settle()
	session := f.sessionOf(testIssue)
	if _, err := f.db.AdmitWorkflowPath(t.Context(), session.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	rawProfile := []byte(`digraph {team=test;mode=implement;profile_version=1;start=merge;
 merge [lf_action=lf_implement_work,lf_outcomes="next",lf_limit=1];
 await [lf_action=lf_implement_work,lf_outcomes="next",lf_limit=2];
 merge -> await [on=next]; await -> merge [on=next];}`)
	profile, err := graphexec.ParseProfile(rawProfile)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: profile.Team, Mode: string(profile.Mode), ProfileVersion: profile.ProfileVersion, Path: "test/implement.dot", Digest: profile.Digest})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := capturedGraphInstance(t, f.db, store.GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: profile.Start, StartedAt: f.clock.Now()}, rawProfile)
	if err != nil {
		t.Fatal(err)
	}
	req := turn.GraphNodeRequest{Profile: profile, InstanceID: instance.ID, At: f.clock.Now()}
	if err := req.Activities.Register("lf_implement_work", limitCoordinatorActivity{}); err != nil {
		t.Fatal(err)
	}
	if _, err := turn.ExecuteNodeAction(t.Context(), turn.Options{DB: f.db}, req); err != nil {
		t.Fatal(err)
	}
	dispatch, err := f.db.ParkGraphInstance(t.Context(), instance.ID, "await", f.clock.Now(), f.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	return f, req, dispatch
}

func TestRunlessNodeLimitStopsAndReports(t *testing.T) {
	for _, posting := range []string{"success", "uncertain"} {
		t.Run(posting, func(t *testing.T) {
			f, req, dispatch := limitCoordinatorFixture(t)
			f.turns.fn = func(ctx context.Context, _ turn.Request) (turn.Result, error) {
				req.At = f.clock.Now()
				if _, err := turn.ExecuteNodeAction(ctx, turn.Options{DB: f.db}, req); err != nil {
					return turn.Result{}, err
				}
				result, err := turn.ExecuteNodeAction(ctx, turn.Options{DB: f.db}, req)
				if err != nil {
					return turn.Result{}, err
				}
				if result.Status != store.GraphFailed || result.AttemptID != 0 {
					t.Errorf("expected real exhaustion: %+v", result)
				}
				return turn.Result{}, errors.New(result.Activity.Diagnostic)
			}
			if posting == "uncertain" {
				f.forge.onPostComment = func(w http.ResponseWriter, _ int) bool {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "post response unavailable"})
					return true
				}
			}
			f.settle()
			instance, err := f.db.GetGraphInstance(t.Context(), req.InstanceID)
			if err != nil || instance.Status != store.GraphFailed || instance.FinishedAt.IsZero() || !instance.NextDueAt.IsZero() {
				t.Fatalf("terminal instance: %+v %v", instance, err)
			}
			stopped, err := f.db.LatestGraphDispatch(t.Context(), instance.ID)
			if err != nil || stopped.ID != dispatch.ID || stopped.Node != "await" || stopped.Status != "cancelled" || stopped.AllocatedRunID != 0 || stopped.Attempts != 1 || !strings.Contains(stopped.CancellationReason, `node "merge" dispatch limit 1 exhausted`) {
				t.Fatalf("cancelled dispatch: %+v %v\n%s", stopped, err, f.logs)
			}
			if strings.Contains(f.logs.String(), "requeue graph step") || strings.Contains(f.logs.String(), "CHECK constraint") {
				t.Fatalf("terminal work requeued: %s", f.logs)
			}
			if posting == "uncertain" {
				pending, found, err := f.db.FindPendingReportDelivery(t.Context(), instance.SessionID, dispatch.TriggerID())
				if err != nil || !found || !pending.PossiblySent {
					t.Fatalf("missing delivery debt: %+v %v %v", pending, found, err)
				}
				f.forge.onPostComment = nil
				f.clock.advance(time.Hour)
				f.settle()
				posts, _, _ := f.forge.snapshot()
				if len(posts) != 0 {
					t.Fatalf("uncertain notice resent: %v", posts)
				}
				f.forge.addComment(testIssue, 900040, bot(), pending.Body)
				if _, found, err := f.reporter.ReportPending(t.Context(), instance.SessionID, dispatch.TriggerID()); err != nil || !found {
					t.Fatalf("recover retained notice: found=%v error=%v", found, err)
				}
				f.clock.advance(time.Hour)
				f.settle()
				saved, err := f.db.GetReportDelivery(t.Context(), pending.Key)
				if err != nil || !saved.Completed || saved.CommentID != 900040 {
					t.Fatalf("delivery not recovered: %+v %v\n%s", saved, err, f.logs)
				}
			} else {
				posts, _, _ := f.forge.snapshot()
				if len(posts) != 1 || !strings.Contains(posts[0], `node "merge" dispatch limit 1 exhausted`) || !strings.Contains(posts[0], "litefactory logs --all --grep") || strings.Contains(posts[0], "Reply on this open issue") {
					t.Fatalf("stop notice: %v", posts)
				}
			}
			posts, _, _ := f.forge.snapshot()
			f.clock.advance(time.Hour)
			f.settle()
			after, _, _ := f.forge.snapshot()
			attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
			if err != nil || len(attempts) != 2 || len(f.turns.calls()) != 1 || len(after) != len(posts) {
				t.Fatalf("terminal work repeated: attempts=%+v calls=%d posts=%v error=%v", attempts, len(f.turns.calls()), after, err)
			}
			latest, err := f.db.LatestGraphDispatch(t.Context(), instance.ID)
			if err != nil || latest != stopped {
				t.Fatalf("terminal dispatch changed: %+v %v", latest, err)
			}
		})
	}
}

func TestRunlessCoordinatorStartupRetryAndSuccess(t *testing.T) {
	f, req, dispatch := limitCoordinatorFixture(t)
	f.turns.fn = func(context.Context, turn.Request) (turn.Result, error) {
		return turn.Result{}, errors.New("transient startup refusal")
	}
	f.settle()
	pending, err := f.db.LatestGraphDispatch(t.Context(), req.InstanceID)
	instance, instanceErr := f.db.GetGraphInstance(t.Context(), req.InstanceID)
	if err != nil || instanceErr != nil || pending.Status != "pending" || pending.Attempts != 1 || instance.Status != store.GraphRunning || instance.NextDueAt.IsZero() {
		t.Fatalf("not retried: %+v %+v %v %v", pending, instance, err, instanceErr)
	}
	f.turns.fn = func(ctx context.Context, _ turn.Request) (turn.Result, error) {
		req.At = f.clock.Now()
		_, err := turn.ExecuteNodeAction(ctx, turn.Options{DB: f.db}, req)
		return turn.Result{}, err
	}
	f.clock.advance(time.Hour)
	f.settle()
	// Discovery can hold the task during the due-dispatch phase.
	f.settle()
	done, err := f.db.LatestGraphDispatch(t.Context(), req.InstanceID)
	if err != nil || done.ID != dispatch.ID || done.Status != "finished" || done.AllocatedRunID != 0 || len(f.turns.calls()) != 2 {
		t.Fatalf("runless success: %+v %v\n%s", done, err, f.logs)
	}
	posts, _, _ := f.forge.snapshot()
	if len(posts) != 0 {
		t.Fatalf("successful work reported stopped: %v", posts)
	}
}
