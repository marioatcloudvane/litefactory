package daemon

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// The owner's triggering comment is handled only together with the failure
// comment: a failed graph Plan run with none published is refused, while every
// other run owes nothing and is left to the existing consumption.
func TestRequirePlanFailureReported(t *testing.T) {
	t.Parallel()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "plan-failure.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{db: db}
	failedPlan := store.Run{ID: 41, Mode: string(agent.ModePlan), Status: store.RunFailed, Error: "agent failed"}
	for _, tt := range []struct {
		name    string
		result  turn.Result
		refused bool
	}{
		{"failed graph Plan run without a failure comment", turn.Result{ResearchGraph: true, Run: failedPlan}, true},
		{"succeeded graph Plan run", turn.Result{ResearchGraph: true, Run: store.Run{ID: 42, Mode: string(agent.ModePlan), Status: store.RunSucceeded}}, false},
		{"failed legacy Plan run", turn.Result{Run: failedPlan}, false},
		{"failed Research graph run", turn.Result{ResearchGraph: true, Run: store.Run{ID: 43, Mode: string(agent.ModeResearch), Status: store.RunFailed}}, false},
		{"no run", turn.Result{ResearchGraph: true}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := d.requirePlanFailureReported(t.Context(), tt.result)
			if tt.refused != (err != nil) {
				t.Fatalf("refused=%v, want %v", err != nil, tt.refused)
			}
			if err != nil && !strings.Contains(err.Error(), "stays unhandled") {
				t.Fatalf("refusal does not say the owner comment stays unhandled: %v", err)
			}
		})
	}
}
