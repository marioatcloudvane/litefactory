package daemon_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// A graph stage that parks its instance leaves a dispatch due at once. The
// worker that ran it runs the next one straight away, so a Plan creation cycle
// of several stories finishes in one tick instead of costing a tick per story.
func TestOwedGraphStagesRunBackToBackInOneTick(t *testing.T) {
	f := newFixture(t)
	var instanceID int64
	stages := 0
	f.turns.fn = func(ctx context.Context, req turn.Request) (turn.Result, error) {
		out, err := f.ranSuccessfully(ctx, req)
		if err != nil {
			return out, err
		}
		if spent, ok := store.GraphTriggerDispatch(req.Trigger.CommentID); ok {
			stages++
			// The first two stages each owe another; the third finishes.
			if stages < 3 {
				now := time.Now().UTC()
				if _, err := f.db.ParkGraphInstanceAfter(ctx, instanceID, "story_agent", now, now, spent); err != nil {
					return out, err
				}
			}
		}
		out.ResearchGraph = true
		return out, nil
	}
	f.plantCursor()
	f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Parent Plan", User: stranger(), Labels: toFakeLabels("Mode: Plan")})
	f.settle()
	session := f.sessionOf(testIssue)

	if _, err := f.db.AdmitPlanWorkflowPath(t.Context(), session.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	pin, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "plan", ProfileVersion: 1, Path: "plan/plan.dot", Digest: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := f.db.AdmitWorkflowLaunch(t.Context(), store.WorkflowLaunch{Mode: "plan", SessionID: session.ID, TriggerID: 22, DefinitionID: pin.ID, Start: "story_agent", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: graphCapacityFiles(t, "plan", "story_agent")})
	if err != nil {
		t.Fatal(err)
	}
	instanceID = admitted.Instance.ID
	due := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err = f.db.ParkGraphInstance(t.Context(), instanceID, "story_agent", due, due); err != nil {
		t.Fatal(err)
	}

	f.settle()

	if stages != 3 {
		t.Fatalf("ran %d owed stages in one tick, want all 3\nlogs:\n%s", stages, f.logs)
	}
	if _, ok, err := f.db.NextFreshGraphDispatch(t.Context(), session.ID); err != nil || ok {
		t.Fatalf("an owed stage is still pending: %v %v", ok, err)
	}
	if got := len(f.turns.calls()); got != 3 {
		t.Fatalf("ran %d turns, want 3", got)
	}
}
