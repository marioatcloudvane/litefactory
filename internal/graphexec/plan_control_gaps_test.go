package graphexec_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

// Story-003 T-022 gap tests for the declared-structure checker (AC-022, AC-024,
// AC-025, AC-043). They reuse the in-memory fixture of plan_control_test.go.

// The bound is derived from the largest set, not a literal: a full set plus one
// owner-requested revision pass.
func TestPlanCreationBound_IsDerivedFromTheSetLimit(t *testing.T) {
	t.Parallel()
	if want := planbrief.MaxPlanStorySetEntries + 1; graphexec.PlanCreationBound != want {
		t.Fatalf("creation bound %d, want the set limit plus one revision pass (%d)", graphexec.PlanCreationBound, want)
	}
	if graphexec.PlanNodeLimitMax < graphexec.PlanCreationBound {
		t.Fatalf("the ceiling %d is below the creation bound %d", graphexec.PlanNodeLimitMax, graphexec.PlanCreationBound)
	}
}

// A cycle node one below the bound cannot visit every story of a full set, and
// the exact bound and the ceiling are accepted; one past the ceiling is refused.
func TestPlanControl_CycleBoundBoundaries(t *testing.T) {
	t.Parallel()
	pick := func(limit int) string {
		return fmt.Sprintf(`story_pick [lf_action="lf_loop",lf_outcomes="true,false",lf_field="response",lf_condition="create",lf_limit="%d"]`, limit)
	}
	tests := []struct {
		name    string
		limit   int
		wantErr bool
	}{
		{"one below the bound/refused", graphexec.PlanCreationBound - 1, true},
		{"the twelve stories alone/refused", planbrief.MaxPlanStorySetEntries - 1, true},
		{"exact bound/accepted", graphexec.PlanCreationBound, false},
		{"ceiling/accepted", graphexec.PlanNodeLimitMax, false},
		{"one past the ceiling/refused", graphexec.PlanNodeLimitMax + 1, true},
		{"zero/refused", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base := planPolicyGraph()
			old := pick(graphexec.PlanCreationBound)
			if !strings.Contains(base, old) {
				t.Fatalf("fixture lacks %q", old)
			}
			err := checkPlan(t, strings.Replace(base, old, pick(tt.limit), 1))
			if (err != nil) != tt.wantErr {
				t.Fatalf("limit %d: err=%v, wantErr=%v", tt.limit, err, tt.wantErr)
			}
		})
	}
}

// Names are the definition's to choose: renaming nodes consistently, adding a
// second terminal still validates, because the checker follows edges and attributes, not names.
func TestPlanControl_StructurallySoundRenamesAndAdditionsValidate(t *testing.T) {
	t.Parallel()
	rename := func(oldName, newName string) func(string) string {
		return func(g string) string { return strings.ReplaceAll(g, oldName, newName) }
	}
	tests := []struct {
		name   string
		mutate func(string) string
	}{
		{"cycle selector renamed", rename("story_pick", "next_story")},
		{"set stage renamed", rename("set_", "proposal_")},
		{"set review renamed", rename("set_review", "proposal_notice")},
		{"promotion terminal renamed", rename("promoted", "approved_end")},
		{"wait terminal renamed", rename("waiting", "owner_wait")},
		{"extra terminal and edge", func(g string) string {
			g = strings.Replace(g, `waiting [lf_terminal="true"];`, `waiting [lf_terminal="true"];parked [lf_terminal="true"];`, 1)
			return strings.Replace(g, `select_acceptance -> stopped`, `select_acceptance -> parked`, 1)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base := planPolicyGraph()
			got := tt.mutate(base)
			if got == base {
				t.Fatal("mutation changed nothing")
			}
			if err := checkPlan(t, got); err != nil {
				t.Fatalf("a structurally sound graph was refused: %v", err)
			}
		})
	}
}

// Broken structure is refused however the nodes are named, and every refusal
// carries a fault that names what is wrong.
func TestPlanControl_StructuralBreaksAreRefusedWithAReason(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, old, next, wantFault string
	}{
		{"loop continues at a non-agent", `set_loop -> set_agent [lf_on="true"]`, `set_loop -> set_question [lf_on="true"]`, "must continue at an agent"},
		{"exhausted loop continues", `story_loop -> stopped [lf_on="false"]`, `story_loop -> story_agent [lf_on="false"]`, "leave at a terminal"},
		{"review of another agent", `story_review [lf_action="lf_ask",lf_outcomes="published",lf_kind="review",lf_source="story_agent"`, `story_review [lf_action="lf_ask",lf_outcomes="published",lf_kind="review",lf_source="set_agent"`, "reviewed as story_agent"},
		{"question decision reads no agent", `lf_condition="ask",lf_source="set_agent"`, `lf_condition="ask",lf_source="select_set"`, "must name an agent node"},
		{"undeclared cycle without a loop", `story_review -> story_pick [lf_on="published"]`, `story_review -> story_agent [lf_on="published"]`, "bypasses a bounded loop"},
		{"promotion reachable from a wait", `set_review -> waiting [lf_on="published"]`, `set_review -> promoted [lf_on="published"]`, "promotion terminal"},
		{"failure comment does not end", `failed -> stopped [lf_on="published"]`, `failed -> story_loop [lf_on="published"]`, "failure comment must end"},
		{"agent conflict outcome skips the failure comment", `technical_draft -> failed [lf_on="conflict"]`, `technical_draft -> waiting [lf_on="conflict"]`, "must reach failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base := planPolicyGraph()
			if !strings.Contains(base, tt.old) {
				t.Fatalf("fixture lacks %q", tt.old)
			}
			err := checkPlan(t, strings.Replace(base, tt.old, tt.next, 1))
			if err == nil {
				t.Fatal("unsafe structure accepted")
			}
			if !strings.Contains(err.Error(), tt.wantFault) {
				t.Fatalf("refusal %q does not name %q", err, tt.wantFault)
			}
		})
	}
}

// Independent structural faults are all reported, so one failure does not hide
// another (STYLE-9).
func TestPlanControl_IndependentFaultsAreAllReported(t *testing.T) {
	t.Parallel()
	g := planPolicyGraph()
	g = strings.Replace(g, `set_draft -> failed [lf_on="uncertain"]`, `set_draft -> promoted [lf_on="uncertain"]`, 1)
	g = strings.Replace(g, `technical_ask -> waiting`, `technical_ask -> story_loop`, 1)
	err := checkPlan(t, g)
	if err == nil {
		t.Fatal("broken graph accepted")
	}
	for _, want := range []string{"set_draft", "technical_ask"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("faults %q do not mention %s", err, want)
		}
	}
}
