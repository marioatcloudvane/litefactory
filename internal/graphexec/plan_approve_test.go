package graphexec_test

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
)

func TestPlanApprove_OnlyInPlan(t *testing.T) {
	spec := graphexec.PlanApproveSpec()
	if spec.Name != graphexec.PlanApproveAction {
		t.Fatalf("spec name %q", spec.Name)
	}
}
