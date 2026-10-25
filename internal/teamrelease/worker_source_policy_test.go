package teamrelease

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
)

func TestPendingWorkerBudgetNormalization(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		change func(*graphexec.Profile, *graphexec.Profile)
		want   bool
	}{
		{name: "canonical worker budget", want: true},
		{name: "other budget", change: func(live, _ *graphexec.Profile) { live.Nodes[0].Attrs[graphexec.BudgetAttribute] = "121" }},
		{name: "existing released budget", change: func(_, released *graphexec.Profile) { released.Nodes[0].Attrs[graphexec.BudgetAttribute] = "60" }},
		{name: "other worker attribute", change: func(live, _ *graphexec.Profile) { live.Nodes[0].Attrs["lf_model"] = "different" }},
		{name: "other agent budget", change: func(live, _ *graphexec.Profile) { live.Nodes[1].Attrs[graphexec.BudgetAttribute] = "120" }},
		{name: "coordinator route", change: func(live, _ *graphexec.Profile) { live.Edges[0].To = "worker_agent" }},
		{name: "worker route", change: func(live, _ *graphexec.Profile) { live.Edges[1].To = "coordinator" }},
		{name: "noncanonical worker", change: func(live, released *graphexec.Profile) {
			live.Nodes[0].Attrs["lf_agent_markdown"] = "other"
			released.Nodes[0].Attrs["lf_agent_markdown"] = "other"
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			makeProfile := func() *graphexec.Profile {
				return &graphexec.Profile{
					Nodes: []graphexec.Node{
						{Name: "worker_agent", Action: "lf_agent", Attrs: map[string]string{"lf_agent_markdown": "story-worker"}},
						{Name: "coordinator", Action: "lf_agent", Attrs: map[string]string{"lf_agent_markdown": "coordinator"}},
					},
					Edges: []graphexec.Edge{{From: "select", To: "coordinator", On: "coordinator"}, {From: "worker_agent", To: "result", On: "success"}},
				}
			}
			live, released := makeProfile(), makeProfile()
			live.Nodes[0].Attrs[graphexec.BudgetAttribute] = "120"
			if tt.change != nil {
				tt.change(live, released)
			}
			if got := pendingWorkerSourceMatchesRelease(live, released); got != tt.want {
				t.Fatalf("pending worker policy matches release = %t, want %t", got, tt.want)
			}
			if live.Nodes[0].Attrs[graphexec.BudgetAttribute] == "" {
				t.Fatal("normalization mutated the live profile")
			}
		})
	}
}
