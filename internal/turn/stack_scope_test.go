package turn

import (
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"reflect"
	"testing"
)

func TestPlanContentScopeUsesCapturedReleaseOwnerAndGuidance(t *testing.T) {
	profile, err := config.ResolveStackProfile([]byte(`{"version":1,"guidance":[{"id":"synthetic-stack","skill":"plan/skills/stack/SKILL.md"}]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	d := &teamsource.WorkflowDefinition{StackProfile: &profile, Files: map[string][]byte{"skills/owner/implementation-engineer.md": []byte("---\nname: implementation-engineer\ndescription: Build story\ntask_owner: true\n---\nAdvice.")}}
	scope, err := PlanContentScope(d)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(scope.Roles, []string{"implementation-engineer"}) || !reflect.DeepEqual(scope.Guidance, []string{"synthetic-stack"}) {
		t.Fatal("scope did not derive from retained release", scope)
	}
	delete(d.Files, "skills/owner/implementation-engineer.md")
	if _, err = PlanContentScope(d); err == nil {
		t.Fatal("modern release without declared owners used Go defaults")
	}
}
func TestPlanContentScopePreservesHistoricalOwnerIdentities(t *testing.T) {
	scope, err := PlanContentScope(&teamsource.WorkflowDefinition{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(scope, planbrief.TaskOwnerScope()) {
		t.Fatal("historical capture identities changed")
	}
	if _, err = PlanContentScope(nil); err == nil {
		t.Fatal("absent admitted capture accepted")
	}
}
