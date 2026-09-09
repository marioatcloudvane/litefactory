package daemon

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestResolveModeLabel(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name           string
		labels         []string
		want           agent.Mode
		found, wantErr bool
	}{
		{"absent", []string{"bug"}, "", false, false},
		{"research", []string{"bug", "Mode: Research"}, agent.ModeResearch, true, false},
		{"plan", []string{"bug", "Mode: Plan"}, agent.ModePlan, true, false},
		{"implement", []string{"bug", "Mode: Implement"}, agent.ModeImplement, true, false},
		{"validate", []string{"bug", "Mode: Validate"}, agent.ModeValidate, true, false},
		{"spike", []string{"bug", "Mode: Spike"}, agent.ModeSpike, true, false},
		{"miscased", []string{"mode: Plan"}, "", false, true},
		{"unknown", []string{"Mode: Deploy"}, "", false, true},
		{"ambiguous", []string{"Mode: Plan", "Mode: Spike"}, "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, found, err := resolveModeLabel(tc.labels)
			if got != tc.want || found != tc.found || (err != nil) != tc.wantErr {
				t.Fatalf("got %q, %t, %v", got, found, err)
			}
		})
	}
}
