package daemon_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/turn"
)

// A graph Plan control on the parent issue reaches a Plan turn with its exact
// body, even when the issue still carries the label of an earlier mode. Without
// a graph Plan route the same comment is an unknown command, as before.
func TestPlanControlOnParentReachesGraphPlan(t *testing.T) {
	const body = "`/approve-plan set ab853aa4bf79081a5330b2499672434e34f380bd18013b75a5df1392c2bef64f`"
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
