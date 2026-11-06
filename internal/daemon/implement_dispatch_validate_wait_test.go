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

// #1457: a parked Implement coordinator dispatch on a managed parent runs in
// Implement while the parent's Validate report waits for the owner. It must
// not be read as an owner reply to that wait; a plain owner comment still is.
func TestImplementDispatchRunsBesideAnOpenValidateWait(t *testing.T) {
	var resolver []turn.Request
	var owners []turn.Request
	var validateInstance store.GraphInstance
	f, validateInstance, _ := validateStrayDispatchFixture(t, time.Hour, func(f *fixture, ctx context.Context, req turn.Request) (turn.Result, error) {
		if _, ok := store.GraphTriggerDispatch(req.Trigger.CommentID); ok {
			resolver = append(resolver, req)
		} else if req.Trigger.CommentID == 900_001 {
			owners = append(owners, req)
		}
		out, err := f.ranSuccessfully(ctx, req)
		if err != nil {
			return out, err
		}
		out.ResearchGraph = true
		return out, nil
	})
	ctx := t.Context()
	if err := restValidateAtOwnerAsk(ctx, f, validateInstance); err != nil {
		t.Fatal(err)
	}
	commit := strings.Repeat("a", 40)
	if _, _, err := f.db.BeginValidateReporter(ctx, store.ValidateReporterResult{InstanceID: validateInstance.ID, Commit: commit, Mode: store.ReporterModeReport, Attempt: "a1", CreatedAt: f.clock.peek()}); err != nil {
		t.Fatal(err)
	}
	if open, err := turn.ValidateDiscussionOpen(ctx, f.db, validateInstance.SessionID); err != nil || !open {
		t.Fatalf("validate discussion open = %v %v, want true", open, err)
	}
	// The stale suite dispatch of the Validate fixture is dropped quietly; it
	// must not be confused with the Implement dispatch below.
	f.clock.advance(2 * time.Hour)
	f.settle()

	def, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "test", Mode: "implement", ProfileVersion: 1, Path: "test/implement.dot", Digest: strings.Repeat("e", 64)})
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := f.db.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Mode: "implement", SessionID: validateInstance.SessionID, TriggerID: 23, DefinitionID: def.ID, Start: "sync_done_resolve", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: graphCapacityFiles(t, "implement", "sync_done_resolve", "sync_done")})
	if err != nil {
		t.Fatal(err)
	}
	implement := admitted.Instance
	at := f.clock.peek()
	dispatch, err := f.db.ParkGraphInstance(ctx, implement.ID, "sync_done_resolve", at, at)
	if err != nil {
		t.Fatal(err)
	}

	f.settle()

	if len(resolver) != 1 || resolver[0].Trigger.CommentID != dispatch.TriggerID() || resolver[0].Mode != agent.ModeImplement {
		t.Fatalf("resolver turns = %+v, want one implement turn for dispatch %d\nlogs:\n%s", resolver, dispatch.ID, f.logs)
	}
	assertValidateWaitOpen(t, f, validateInstance)
	assertNoFalseStop(t, f)
	latest, err := f.db.LatestGraphDispatch(ctx, implement.ID)
	if err != nil || latest.ID != dispatch.ID || latest.Status != "finished" || latest.Attempts != 1 {
		t.Fatalf("dispatch not finished once: %+v %v\n%s", latest, err, f.logs)
	}

	// Replay: another heartbeat neither reruns the resolver nor claims the wait.
	f.clock.advance(time.Hour)
	f.settle()

	if len(resolver) != 1 {
		t.Fatalf("resolver ran %d times, want 1\n%s", len(resolver), f.logs)
	}
	if latest, err = f.db.LatestGraphDispatch(ctx, implement.ID); err != nil || latest.ID != dispatch.ID || latest.Status != "finished" || latest.Attempts != 1 {
		t.Fatalf("dispatch after replay: %+v %v\n%s", latest, err, f.logs)
	}
	assertValidateWaitOpen(t, f, validateInstance)
	assertNoFalseStop(t, f)

	// A plain owner comment still continues the waiting Validate report.
	const comment = 900_001
	f.forge.addComment(testIssue, comment, owner(), "Why did the merge stop?")
	f.settle()

	if len(owners) != 1 || owners[0].Mode != agent.ModeValidate {
		t.Fatalf("owner comment turns = %+v, want one validate turn\n%s", owners, f.logs)
	}
	wait, err := f.db.GetGraphOwnerWait(ctx, validateInstance.ID)
	if err != nil || wait.State != "claimed" || wait.ReplyID != comment {
		t.Fatalf("owner comment was not claimed by the Validate wait: %+v %v\n%s", wait, err, f.logs)
	}
}

func assertValidateWaitOpen(t *testing.T, f *fixture, instance store.GraphInstance) {
	t.Helper()
	wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	if err != nil || wait.State != "waiting" || wait.ReplyID != 0 {
		t.Fatalf("validate owner wait: %+v %v\n%s", wait, err, f.logs)
	}
}
