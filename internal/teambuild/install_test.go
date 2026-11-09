package teambuild

import (
	"bytes"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/teampackage"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestOutOfGraphSkillClosureIsInstalledAndStraySkillRefused(t *testing.T) {
	skill := []byte("---\nname: brainstorm\ndescription: Fixture skill.\nresources:\n  - notes.md\n---\nFixture body.\n")
	notes := []byte("Fixture notes.\n")

	t.Run("listed skill installed", func(t *testing.T) {
		dir := flatFixture(t, "go-development")
		skillDir := filepath.Join(dir, "go-development/research/skills/brainstorm")
		put(t, filepath.Join(skillDir, "SKILL.md"), skill)
		put(t, filepath.Join(skillDir, "notes.md"), notes)
		files, release, err := Build(t.Context(), dir, "go-development")
		if err != nil {
			t.Fatal(err)
		}
		for name, want := range map[string][]byte{"research/skills/brainstorm/SKILL.md": skill, "research/skills/brainstorm/notes.md": notes} {
			if !bytes.Equal(files[name], want) {
				t.Fatalf("%s not built faithfully: %q", name, files[name])
			}
			listed := false
			for _, f := range release.Files {
				listed = listed || f.Path == name
			}
			if !listed {
				t.Fatalf("%s missing from the inventory", name)
			}
		}
	})

	t.Run("unlisted stray skill refused", func(t *testing.T) {
		dir := flatFixture(t, "go-development")
		put(t, filepath.Join(dir, "go-development/research/skills/stray/SKILL.md"), []byte("---\nname: stray\ndescription: Fixture stray.\n---\nStray body.\n"))
		_, _, err := Build(t.Context(), dir, "go-development")
		if err == nil || !strings.Contains(err.Error(), "research/skills/stray/SKILL.md: resource is outside the captured role closure") {
			t.Fatalf("stray skill not refused: %v", err)
		}
	})

	t.Run("unlisted stray resource of listed skill refused", func(t *testing.T) {
		dir := flatFixture(t, "go-development")
		skillDir := filepath.Join(dir, "go-development/research/skills/brainstorm")
		put(t, filepath.Join(skillDir, "SKILL.md"), skill)
		put(t, filepath.Join(skillDir, "notes.md"), notes)
		put(t, filepath.Join(skillDir, "undeclared.md"), notes)
		_, _, err := Build(t.Context(), dir, "go-development")
		if err == nil || !strings.Contains(err.Error(), "research/skills/brainstorm/undeclared.md: resource is outside the captured role closure") {
			t.Fatalf("undeclared resource not refused: %v", err)
		}
	})
}

// outOfGraphPath reports an installed file of a listed out-of-graph skill; the
// selected-workflow view exposes only what graph validation read.
func outOfGraphPath(name string) bool {
	for w, skills := range outOfGraphSkills {
		for _, skill := range skills {
			if strings.HasPrefix(name, w+"/skills/"+skill+"/") {
				return true
			}
		}
	}
	return false
}

func TestInstallationCaptureAndNativeDelivery(t *testing.T) {
	const team = "go-development"
	source := repoSource()
	version := sourceVersion(t, team)
	m, installed := install(t, source, team)
	if !installed.Integrity || len(installed.Workflows) != 7 {
		t.Fatal("incomplete installed release")
	}
	clone, err := m.Clone(t.Context(), identity(source), team, version, "custom", "2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if clone.Origin == nil || clone.Origin.Checksum != installed.Identity.Digest {
		t.Fatal("clone lost origin")
	}
	resolved, err := teamsourcetest.Files(t.Context(), team)
	if err != nil {
		t.Fatal(err)
	}
	err = m.WithCatalogue(t.Context(), func(a *teampackage.CatalogueAccess) error {
		return a.WithSelected(t.Context(), identity(source), team, version, func(got teampackage.Installed, sourceFS fs.FS) error {
			if !reflect.DeepEqual(got.Identity, installed.Identity) {
				t.Fatal("selection lost installed identity")
			}
			for _, w := range Workflows {
				d := capture(t, sourceFS, w, team)
				rules := map[string][]byte{}
				for name, raw := range resolved {
					if rest, ok := strings.CutPrefix(name, w+"/skills/litefactory-rules/"); ok {
						rules[rest] = raw
					}
				}
				for id, node := range d.Nodes {
					for name := range node.Files {
						if strings.Contains(name, "team-guidance") {
							t.Fatalf("%s/%s still carries %s", w, id, name)
						}
					}
					for name, raw := range rules {
						if !bytes.Equal(raw, node.Files["skills/litefactory-rules/"+name]) {
							t.Fatalf("%s/%s misses litefactory rule %s", w, id, name)
						}
					}
					for _, kind := range []string{"claude", "codex"} {
						nativeFiles := snapshot(t, bundle(t, d, node, kind).Root)
						for name, raw := range rules {
							if !bytes.Equal(raw, nativeFiles["plugin/skills/litefactory-rules/"+name]) {
								t.Fatalf("%s/%s/%s omits litefactory rule %s", w, id, kind, name)
							}
						}
					}
				}
			}
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Changed bytes for an installed identity replace it in place; equal bytes
// leave it unchanged; tampered built files never reach the installed folder.
func TestReinstallReplacesInPlaceAndRefusesTamperedFiles(t *testing.T) {
	const team = "go-development"
	dir := copySource(t)
	m, first := install(t, dir, team)
	before := snapshot(t, first.Path)
	files, release, err := Build(t.Context(), dir, team)
	if err != nil {
		t.Fatal(err)
	}
	if _, result, err := m.InstallBuilt(t.Context(), identity(dir), files, release, teampackage.BuildInfo{}); err != nil || result != teampackage.InstallUnchanged {
		t.Fatalf("same bytes: %v, %v", result, err)
	}
	tampered := map[string][]byte{}
	for name, raw := range files {
		tampered[name] = raw
	}
	tampered["stack-profile.json"] = []byte(`{"version":99}`)
	if _, _, err := m.InstallBuilt(t.Context(), identity(dir), tampered, release, teampackage.BuildInfo{}); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("tampered bytes accepted: %v", err)
	}
	if !reflect.DeepEqual(before, snapshot(t, first.Path)) {
		t.Fatal("refused install changed the installed folder")
	}
	p := filepath.Join(dir, "base-team", "spike", "spike.md")
	put(t, p, append(snapshot(t, filepath.Dir(p))["spike.md"], "\nA same-version source edit.\n"...))
	files, release, err = Build(t.Context(), dir, team)
	if err != nil {
		t.Fatal(err)
	}
	replaced, result, err := m.InstallBuilt(t.Context(), identity(dir), files, release, teampackage.BuildInfo{SourceHead: strings.Repeat("a", 40), SourceDirty: true})
	if err != nil || result != teampackage.InstallReplaced || replaced.Identity.Digest == first.Identity.Digest {
		t.Fatalf("changed bytes: %v, %v", result, err)
	}
	got, err := m.Inspect(t.Context(), identity(dir), team, release.Version)
	if err != nil || got.Identity.Digest != release.Checksum || got.Build == nil || !got.Build.SourceDirty {
		t.Fatalf("inspect after replace: %+v, %v", got, err)
	}
	if !bytes.Contains(snapshot(t, got.Path)["spike/spike.md"], []byte("same-version source edit")) {
		t.Fatal("installed folder lacks the source change")
	}
}

// Exercise canonical source closure through retained replay.
func TestCanonicalPlanTaskOwnersSurviveRetainedReplay(t *testing.T) {
	for _, team := range []string{"go-development", "python-fastapi", "swift-ios", "dotnet-web"} {
		t.Run(team, func(t *testing.T) {
			resolved, err := teamsourcetest.Files(t.Context(), team)
			if err != nil {
				t.Fatal(err)
			}
			source := teamsource.RetainedFiles(resolved)
			profile, err := config.ResolveStackProfile(resolved[config.StackProfilePath], nil)
			if err != nil {
				t.Fatal(err)
			}
			guidance, err := teamsource.CaptureStackGuidance(t.Context(), source, profile.Effective)
			if err != nil {
				t.Fatal(err)
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
				t.Fatal("missing declarations passed source coverage")
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
