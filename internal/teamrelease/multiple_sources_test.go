package teamrelease

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/teampackage"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/turn"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func secondSource(t *testing.T, root string) string {
	t.Helper()
	const second = "internal/temporary-team"
	for name, raw := range snapshot(t, filepath.Join(root, "internal/teamsource")) {
		raw = bytes.ReplaceAll(raw, []byte("go-development"), []byte("temporary-team"))
		put(t, filepath.Join(root, second, filepath.FromSlash(name)), raw)
	}
	return second
}
func releaseCatalogue(t *testing.T, root string) teampackage.Catalogue {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "teams/teams.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c teampackage.Catalogue
	if err = json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}
func TestMultipleSourcesPreservePublishedReleaseAndExactSelection(t *testing.T) {
	root := fixture(t)
	generate(t, root)
	first := releaseCatalogue(t, root).Releases[0]
	before := snapshot(t, filepath.Join(root, "teams", first.Download))
	second := secondSource(t, root)
	sources := []string{"internal/teamsource", second}
	if err := GenerateSources(t.Context(), root, false, sources); err != nil {
		t.Fatal(err)
	}
	c := releaseCatalogue(t, root)
	if len(c.Releases) != 2 {
		t.Fatalf("releases=%d", len(c.Releases))
	}
	if !reflect.DeepEqual(before, snapshot(t, filepath.Join(root, "teams", first.Download))) {
		t.Fatal("adding team rewrote historical release bytes")
	}
	for _, r := range c.Releases {
		raw, err := os.ReadFile(filepath.Join(root, "teams", r.Download, config.StackProfilePath))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		found := false
		for _, f := range r.Files {
			if f.Path == config.StackProfilePath {
				found = true
				if f.SHA256 != hex.EncodeToString(sum[:]) {
					t.Fatal("profile checksum mismatch")
				}
			}
		}
		if !found {
			t.Fatal("profile missing from immutable inventory")
		}
		m, err := teampackage.New(teampackage.Options{Paths: config.NewPaths(canonicalTemp(t)), Validate: turn.ValidateTeamWorkflows})
		if err != nil {
			t.Fatal(err)
		}
		got, _, err := m.Install(t.Context(), filepath.Join(root, "teams/teams.json"), r.Slug, r.Version)
		if err != nil {
			t.Fatal(err)
		}
		if got.Identity.Slug != r.Slug || got.Identity.Version != r.Version || got.Identity.Digest != r.Checksum {
			t.Fatal("exact release identity lost")
		}
	}
	all := snapshot(t, root)
	if err := GenerateSources(t.Context(), root, false, []string{second, "internal/teamsource"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(all, snapshot(t, root)) {
		t.Fatal("candidate ordering changed output")
	}
	if err := GenerateSources(t.Context(), root, true, sources); err != nil {
		t.Fatal(err)
	}
	// A single-source invocation must retain releases omitted from this generation.
	generate(t, root)
	if !reflect.DeepEqual(c, releaseCatalogue(t, root)) {
		t.Fatal("omitted team removed from combined catalogue")
	}
}
func TestInvalidSecondSourceLeavesWholePublicationUnchanged(t *testing.T) {
	root := fixture(t)
	generate(t, root)
	second := secondSource(t, root)
	before := snapshot(t, filepath.Join(root, "teams"))
	put(t, filepath.Join(root, second, config.StackProfilePath), []byte(`{"version":99}`))
	mutated := snapshot(t, filepath.Join(root, "internal"))
	if err := GenerateSources(t.Context(), root, false, []string{"internal/teamsource", second}); err == nil {
		t.Fatal("invalid candidate accepted")
	}
	if !reflect.DeepEqual(before, snapshot(t, filepath.Join(root, "teams"))) || !reflect.DeepEqual(mutated, snapshot(t, filepath.Join(root, "internal"))) {
		t.Fatal("invalid batch changed publication or materialized source")
	}
}

// Exercise canonical source closure without generating or modifying published releases.
func TestCanonicalPlanTaskOwnersSurviveRetainedReplay(t *testing.T) {
	for _, root := range []string{"internal/teamsource", "internal/teamsource/python-fastapi", "internal/teamsource/swift-ios", "teams/source/dotnet-web"} {
		t.Run(root, func(t *testing.T) {
			source := os.DirFS(filepath.Join("../..", root))
			raw, err := os.ReadFile(filepath.Join("../..", root, config.StackProfilePath))
			if err != nil {
				t.Fatal(err)
			}
			profile, err := config.ResolveStackProfile(raw, nil)
			if err != nil {
				t.Fatal(err)
			}
			guidance, err := teamsource.CaptureStackGuidance(t.Context(), source, profile.Effective)
			if err != nil {
				t.Fatal(err)
			}
			team := filepath.Base(root)
			if team == "teamsource" {
				team = "go-development"
			}
			definition := capture(t, source, "plan", team)
			if err := teamsource.AttachStackProfile(t.Context(), definition, profile, strings.Repeat("a", 40), guidance); err != nil {
				t.Fatal(err)
			}
			if err := canonicalPlanOwnerCheck(definition); err != nil {
				t.Fatal(err)
			}
			replay, err := teamsource.CaptureWorkflowIdentity(t.Context(), teamsource.RetainedFiles(definition.Files), "plan.dot", team, agent.WorkflowPlan, registry(t, "plan"))
			if err != nil {
				t.Fatal(err)
			}
			if err := canonicalPlanOwnerCheck(replay); err != nil {
				t.Fatal(err)
			}
			if replay.Digest != definition.Digest {
				t.Fatal("retained replay changed capture")
			}
			// Model the missing declaration bug, leaving the attached stack profile intact.
			for _, role := range []string{"implementation-engineer", "integration-test-engineer", "unit-test-engineer"} {
				delete(replay.Files, "skills/plan-acceptance-shaping/"+role+".md")
			}
			if err := canonicalPlanOwnerCheck(replay); err == nil {
				t.Fatal("missing declarations passed source/release coverage")
			}
			if _, err := turn.PlanContentScope(replay); err == nil || err.Error() != "plan scope: release declares no task owners" {
				t.Fatalf("missing declarations: %v", err)
			}
		})
	}
}

func canonicalPlanOwnerCheck(d *teamsource.WorkflowDefinition) error {
	want := []string{"implementation-engineer", "integration-test-engineer", "unit-test-engineer"}
	owners, err := teamsource.TaskOwnerRoles(d)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(owners, want) {
		return fmt.Errorf("Plan task owners = %v; want %v", owners, want)
	}
	scope, err := turn.PlanContentScope(d)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(scope.Roles, want) {
		return fmt.Errorf("Plan scope roles = %v; want %v", scope.Roles, want)
	}
	return nil
}
