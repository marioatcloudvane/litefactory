package teamsource

import (
	"bytes"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

func TestImplementSlotPolicyCaptureAndRetainedReplay(t *testing.T) {
	t.Parallel()
	var registry graphexec.Registry
	specs := append(graphexec.ImplementSpecs(), graphexec.ActionSpec{Name: "lf_agent", Mode: agent.ModeImplement, Outcomes: []string{"success", "failed"}, Attributes: graphexec.AgentAttributes()})
	for _, spec := range specs {
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
	registry.Policy = graphexec.ValidateImplementGraph
	fresh, err := captureImplementGo(t.Context(), t, registry)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range fresh.Profile.Nodes {
		want := 1
		if node.Terminal {
			want = 0
		} else if node.Name == "worker_agent" {
			want = 3
		}
		cost, err := graphexec.SlotCost(fresh.Profile, node.Name)
		if err != nil || cost != want {
			t.Fatalf("%s cost=%d want=%d err=%v", node.Name, cost, want, err)
		}
	}
	// Retained bytes from the former policy remain independent of current source.
	historical := RetainedFiles{}
	for name, data := range fresh.Files {
		historical[name] = bytes.Clone(data)
	}
	historical["implement.dot"] = bytes.Replace(historical["implement.dot"], []byte(`lf_slot_budget="3", `), nil, 1)
	old, err := CaptureWorkflow(t.Context(), historical, "implement.dot", "go-development", agent.ModeImplement, registry)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := CaptureWorkflow(t.Context(), RetainedFiles(old.Files), "implement.dot", "go-development", agent.ModeImplement, registry)
	if err != nil {
		t.Fatal(err)
	}
	cost, err := graphexec.SlotCost(replay.Profile, "worker_agent")
	if err != nil || cost != 1 || replay.Digest != old.Digest || old.Digest == fresh.Digest {
		t.Fatalf("retained cost=%d err=%v digest preserved=%t", cost, err, replay.Digest == old.Digest)
	}
}
