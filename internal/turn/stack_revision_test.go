package turn

import (
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"testing"
)

func TestFreshStackRevisionRefusesChangedCommittedOverride(t *testing.T) {
	f := newFixture(t, nil)
	original := `{"version":1,"environment":[{"name":"CI","value":"1"}]}`
	revision := commitStackOverride(t, f, original)
	resolved, err := config.ResolveStackProfile([]byte(`{"version":1,"guidance":[{"id":"stack","skill":"implement/skills/stack/SKILL.md"}],"override_fields":["environment"]}`), []byte(original))
	if err != nil {
		t.Fatal(err)
	}
	req := f.request()
	req.researchDefinition = &teamsource.WorkflowDefinition{StackProfile: &resolved, StackFresh: true}
	clone, err := f.opts.Git.Open(t.Context(), f.repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = validateFreshStackRevision(t.Context(), clone, req, revision); err != nil {
		t.Fatal("same admitted override refused", err)
	}
	changed := commitStackOverride(t, f, `{"version":1,"environment":[{"name":"CI","value":"0"}]}`)
	if err = validateFreshStackRevision(t.Context(), clone, req, changed); err == nil {
		t.Fatal("changed configuration launched stale fresh profile")
	}
	req.researchDefinition.StackFresh = false
	if err = validateFreshStackRevision(t.Context(), nil, req, "HEAD"); err != nil {
		t.Fatal("retained attempt reopened repository", err)
	}
}
