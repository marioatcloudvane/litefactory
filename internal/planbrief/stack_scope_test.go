package planbrief_test

import (
	"github.com/cloudvane/litefactory/internal/planbrief"
	"reflect"
	"testing"
)

func TestReleaseScopeAdmitsNeutralOwnerAndSelectedNonGoGuidance(t *testing.T) {
	roles := []string{"implementation-engineer", "unit-test-engineer", "integration-test-engineer"}
	guidance := []string{"synthetic-stack", "agent-setup"}
	scope := planbrief.ReleaseScope(roles, guidance)
	roles[0] = "foreign"
	guidance[0] = "foreign"
	d := draftFixture("acceptance")
	d.Tasks[0].OwnerRole = "implementation-engineer"
	d.Guidance = []string{"synthetic-stack"}
	if err := d.Validate("acceptance", scope); err != nil {
		t.Fatal("selected release contracts rejected", err)
	}
	for _, change := range []string{"foreign Go owner", "other owner", "unselected Go", "foreign guidance", "missing proof"} {
		t.Run(change, func(t *testing.T) {
			next := draftFixture("acceptance")
			next.Tasks[0].OwnerRole = "implementation-engineer"
			next.Guidance = []string{"synthetic-stack"}
			switch change {
			case "foreign Go owner":
				next.Tasks[0].OwnerRole = "go-frontend-engineer"
			case "other owner":
				next.Tasks[0].OwnerRole = "other-engineer"
			case "unselected Go":
				next.Guidance = []string{"go"}
			case "foreign guidance":
				next.Guidance = []string{"foreign"}
			case "missing proof":
				next.Criteria[0].ProofMethod = ""
			}
			if err := next.Validate("acceptance", scope); err == nil {
				t.Fatal("out-of-scope contract accepted")
			}
		})
	}
}
func TestReleaseScopeAdmitsGoPrefixedOwnerOfItsNeutralRole(t *testing.T) {
	scope := planbrief.ReleaseScope([]string{"implementation-engineer", "unit-test-engineer"}, []string{"agent-setup"})
	d := draftFixture("acceptance")
	d.Tasks[0].OwnerRole = "go-implementation-engineer"
	if err := d.Validate("acceptance", scope); err != nil {
		t.Fatal("stored go- owner of a release role rejected:", err)
	}
	plan, err := planbrief.CompletePlanDrafts("summary", []planbrief.PlanDraft{d}, scope)
	if err != nil {
		t.Fatal("typed handoff rejected the stored go- owner:", err)
	}
	if got := plan.Stories[0].Tasks[0].OwnerRole; got != "go-implementation-engineer" {
		t.Fatalf("stored owner rewritten to %q", got)
	}
	for _, owner := range []string{"go-integration-test-engineer", "go-", "go-go-implementation-engineer", "rust-implementation-engineer"} {
		next := draftFixture("acceptance")
		next.Tasks[0].OwnerRole = owner
		if err := next.Validate("acceptance", scope); err == nil {
			t.Fatalf("owner %q outside the release accepted", owner)
		}
	}
}
func TestHistoricalScopeRefusesNeutralOwner(t *testing.T) {
	d := draftFixture("acceptance")
	d.Tasks[0].OwnerRole = "implementation-engineer"
	d.Guidance = []string{"go", "agent-setup"}
	if err := d.Validate("acceptance", planbrief.TaskOwnerScope()); err == nil {
		t.Fatal("historical scope accepted a neutral owner")
	}
}
func TestReleaseScopeRetainsExplicitLegacyContract(t *testing.T) {
	scope := planbrief.TaskOwnerScope()
	before := append([]string{}, scope.Roles...)
	d := draftFixture("acceptance")
	d.Tasks[0].OwnerRole = "go-implementation-engineer"
	d.Guidance = []string{"go", "agent-setup"}
	if err := d.Validate("acceptance", scope); err != nil {
		t.Fatal(err)
	}
	scope.Roles[0] = "modified"
	if !reflect.DeepEqual(before, planbrief.TaskOwnerScope().Roles) {
		t.Fatal("caller changed legacy role identities")
	}
}
func TestEmptyReleaseGuidanceDoesNotEnableLegacyGoFallback(t *testing.T) {
	d := draftFixture("technical")
	d.Guidance = []string{"go"}
	if err := d.Validate("technical", planbrief.ReleaseScope([]string{"implementation-engineer"}, []string{})); err == nil {
		t.Fatal("empty selected guidance silently fell back to Go")
	}
}
