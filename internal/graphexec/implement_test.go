package graphexec

import (
	"slices"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestImplementSpecsDeclareTheirOutcomesAndRegister(t *testing.T) {
	want := map[string][]string{
		ImplementReadyAction:   {"started", "waiting", "drained"},
		ImplementMergeAction:   {"merged", "parked", "none", "retry"},
		ImplementHandoffAction: {"posted"},
	}
	var registry Registry
	for _, spec := range []ActionSpec{ImplementReadySpec(), ImplementMergeSpec(), ImplementHandoffSpec()} {
		if spec.Mode != agent.ModeImplement || !slices.Equal(spec.Outcomes, want[spec.Name]) {
			t.Errorf("%s = %+v", spec.Name, spec)
		}
		if err := registry.Register(spec); err != nil {
			t.Errorf("%s: %v", spec.Name, err)
		}
	}
	if len(want) != 3 {
		t.Fatal("unreachable")
	}
}

func TestImplementSpecsAreAdmittedOnlyForImplement(t *testing.T) {
	spec := ImplementMergeSpec()
	spec.Mode = agent.ModePlan
	var registry Registry
	if err := registry.Register(spec); err == nil {
		t.Fatal("an lf_implement_ action must not register under another mode")
	}
}
