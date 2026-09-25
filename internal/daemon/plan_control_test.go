package daemon_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/turn"
)

// A graph Plan control on the parent issue reaches a Plan turn with its exact
// body, even when the issue still carries the label of an earlier mode. Without
// a graph Plan route the same comment is an unknown command, as before.
func TestPlanControlOnParentReachesGraphPlan(t *testing.T) {
	const body = "/approve-plan set 2302ce4dd8c33b0b4c4cf7eff8895e9e03886d1a18ae6791f50ee1a3be4b1a65"
	for _, tc := range []struct {
		name  string
		route string
	}{
		{"graph session runs Plan", "graph"},
		{"legacy session replies unknown", "legacy"},
		{"session without a Plan route replies unknown", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.turns.fn = func(ctx context.Context, req turn.Request) (turn.Result, error) {
				out, err := f.ranSuccessfully(ctx, req)
				out.ResearchGraph = true
				return out, err
			}
			f.plantCursor()
			f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Parent", User: stranger(), Labels: toFakeLabels("Mode: Research")})
			f.settle()
			session := f.sessionOf(testIssue)
			if tc.route != "" {
				if _, err := f.db.AdmitPlanWorkflowPath(t.Context(), session.ID, tc.route); err != nil {
					t.Fatal(err)
				}
			}

			f.forge.addComment(testIssue, 70, owner(), body)
			f.settle()

			calls := f.turns.calls()
			posted, _, _ := f.forge.snapshot()
			if tc.route == "graph" {
				if len(calls) != 1 || calls[0].Mode != agent.ModePlan || calls[0].Trigger.Body != body || calls[0].Command.Kind != turn.KindPlanControl {
					t.Fatalf("the control did not reach a Plan turn: %+v logs=%s", calls, f.logs)
				}
				for _, p := range posted {
					if strings.Contains(p, "do not recognise") {
						t.Fatalf("a graph Plan control drew an unknown-command reply:\n%s", p)
					}
				}
			} else {
				if len(calls) != 0 {
					t.Fatalf("a Plan control ran without a graph Plan route: %+v", calls)
				}
				if len(posted) != 1 || !strings.Contains(posted[0], "do not recognise") || !strings.Contains(posted[0], "/approve-plan") {
					t.Fatalf("want one unknown-command reply, got %q", posted)
				}
			}
			if got := f.watermark(); got != 70 {
				t.Fatalf("watermark is %d, want 70", got)
			}
		})
	}
}

// A graph Plan story issue is observed by its parent, which reads the owner's
// replies there as Plan input. When the story issue is also a task the daemon
// has seen, its own worker records the reply and does nothing else: no
// unknown-command answer, no second run beside the Plan revision.
func TestGraphPlanStoryIssueDefersCommentsToItsParent(t *testing.T) {
	const parentIssue = 900
	for _, body := range []string{"/approve-plan story-001 story plan-35-364", "please tighten the scope"} {
		t.Run(body, func(t *testing.T) {
			f := newFixture(t)
			f.daemon = f.newDaemon(daemon.Options{Forge: refinementForge{Client: f.client, parent: parentIssue}})
			f.plantCursor()
			f.forge.addIssue(fakeIssue{ID: 9000, Number: parentIssue, Title: "Parent", User: stranger(), Labels: toFakeLabels("Mode: Plan")})
			f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Story", User: stranger()})
			f.settle()
			parent := f.sessionOf(parentIssue)
			if _, err := f.db.AdmitPlanWorkflowPath(t.Context(), parent.ID, "graph"); err != nil {
				t.Fatal(err)
			}
			if _, err := f.db.ReservePlanStory(t.Context(), parent.TaskRef, "story-001"); err != nil {
				t.Fatal(err)
			}
			if err := f.db.BindPlanStoryIssue(t.Context(), parent.TaskRef, "story-001", 1230, testIssue); err != nil {
				t.Fatal(err)
			}

			f.forge.addComment(testIssue, 70, owner(), body)
			f.settle()

			for _, call := range f.turns.calls() {
				if call.Issue.Number == testIssue {
					t.Fatalf("the story issue ran its own turn: %+v", call)
				}
			}
			posted, _, _ := f.forge.snapshot()
			for _, p := range posted {
				if strings.Contains(p, "do not recognise") {
					t.Fatalf("the story issue answered its parent's Plan input:\n%s", p)
				}
			}
			if got := f.watermark(); got != 70 {
				t.Fatalf("the story issue's comment was not consumed: watermark %d", got)
			}
		})
	}
}
