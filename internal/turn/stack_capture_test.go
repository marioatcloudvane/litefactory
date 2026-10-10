package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAdmittedStackCaptureRetainsResumeFallbackAndDetectsTamper(t *testing.T) {
	f := newFixture(t, nil)
	req := f.request()
	specs, err := ImplementSpecs()
	if err != nil {
		t.Fatal(err)
	}
	d, err := fixtureWorkflowSource(t.Context(), f.storeRepo, agent.WorkflowImplement, specs)
	if err != nil {
		t.Fatal(err)
	}
	base := []byte(`{"version":1,"guidance":[{"id":"synthetic-stack","skill":"implement/skills/stack/SKILL.md"}],"override_fields":["environment"]}`)
	override := []byte(`{"version":1,"environment":[{"name":"CI","value":"1"}]}`)
	resolved, err := config.ResolveStackProfile(base, override)
	if err != nil {
		t.Fatal(err)
	}
	guidance := map[string][]byte{"implement/skills/stack/SKILL.md": []byte("---\nname: stack\ndescription: Retained stack advice\nresources: [guide.md]\n---\nAdvice."), "implement/skills/stack/guide.md": []byte("original stack advice")}
	if err = teamsource.AttachStackProfile(t.Context(), d, resolved, strings.Repeat("a", 40), guidance); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.AdmitWorkflowPath(t.Context(), f.session.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	pin, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: d.Profile.Team, Mode: string(d.Profile.Mode), Workflow: string(d.Profile.Workflow), ProfileVersion: d.Profile.ProfileVersion, Path: d.Origin.GraphPath, Digest: d.Profile.Digest, TeamSnapshotDigest: d.Digest, CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(d.Files)
	if err != nil {
		t.Fatal(err)
	}
	topology, err := graphexec.SemanticDigest(d.Profile)
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := f.db.AdmitWorkflowLaunch(t.Context(), store.WorkflowLaunch{SessionID: f.session.ID, TriggerID: req.Trigger.CommentID, DefinitionID: pin.ID, Mode: string(d.Profile.Mode), Workflow: string(d.Profile.Workflow), Start: d.Profile.Start, ContentDigest: d.Digest, TopologyDigest: topology, Files: raw, Provenance: store.WorkflowProvenance(d.Origin)})
	if err != nil {
		t.Fatal(err)
	}
	f.opts.WorkflowSource = func(context.Context, store.Repo, agent.Workflow, graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		t.Fatal("resume/fallback consulted changed release or disk")
		return nil, nil
	}
	base[0] = '!'
	override[0] = '!'
	guidance["implement/skills/stack/guide.md"][0] = '!'
	for _, consumer := range []string{"resume", "provider fallback"} {
		t.Run(consumer, func(t *testing.T) {
			got, err := capturedWorkflow(t.Context(), f.opts, req, agent.WorkflowImplement, specs)
			if err != nil {
				t.Fatal(err)
			}
			if got.Digest != d.Digest || got.StackRevision != d.StackRevision || !reflect.DeepEqual(got.StackProfile, d.StackProfile) || !reflect.DeepEqual(got.Files, d.Files) {
				t.Fatal("captured profile/override/guidance drifted")
			}
		})
	}
	saved, err := f.db.GetWorkflowCapture(t.Context(), admitted.LaunchID)
	if err != nil {
		t.Fatal(err)
	}
	var files map[string][]byte
	if err = json.Unmarshal(saved.Files, &files); err != nil {
		t.Fatal(err)
	}
	files[".stack-capture/guidance/implement/skills/stack/guide.md"] = []byte("changed retained advice")
	saved.Files, err = json.Marshal(files)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restoreWorkflow(t.Context(), f.db, saved, agent.WorkflowImplement, specs); err == nil {
		t.Fatal("changed retained guidance passed admission digest")
	}
}

func configureFreshStackSource(t *testing.T, f *fixture, base []byte) {
	t.Helper()
	resolved, err := config.ResolveStackProfile(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.opts.WorkflowSource = func(ctx context.Context, repo store.Repo, w agent.Workflow, r graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		d, err := fixtureWorkflowSource(ctx, repo, w, r)
		if err != nil {
			return nil, err
		}
		d.StackProfile = &resolved
		d.StackPackageFiles = map[string][]byte{"implement/skills/stack/SKILL.md": []byte("---\nname: stack\ndescription: Stack advice\n---\nAdvice.")}
		return d, nil
	}
}
func commitStackOverride(t *testing.T, f *fixture, raw string) string {
	t.Helper()
	f.repo.WriteFile(config.StackOverridePath, raw)
	commit := f.repo.Commit("stack override")
	f.repo.Git("push", "origin", f.storeRepo.BaseBranch)
	f.repo.Git("update-ref", "refs/remotes/origin/"+f.storeRepo.BaseBranch, commit)
	return commit
}
func TestFreshStackAdmissionReadsCommittedOverrideAndNewRevision(t *testing.T) {
	f := newFixture(t, nil)
	req := f.request()
	specs, err := ImplementSpecs()
	if err != nil {
		t.Fatal(err)
	}
	configureFreshStackSource(t, f, []byte(`{"version":1,"guidance":[{"id":"stack","skill":"implement/skills/stack/SKILL.md"}],"environment":[{"name":"CI","value":"0"}],"override_fields":["environment"]}`))
	firstRevision := commitStackOverride(t, f, `{"version":1,"environment":[{"name":"CI","value":"1"}]}`)
	// A dirty file is deliberately malformed and must not participate in admission.
	f.repo.WriteFile(config.StackOverridePath, "dirty private-canary")
	first, err := capturedWorkflow(t.Context(), f.opts, req, agent.WorkflowImplement, specs)
	if err != nil {
		t.Fatal(err)
	}
	if first.StackRevision != firstRevision || first.StackProfile.Effective.Environment[0].Value != "1" {
		t.Fatal("fresh admission used dirty or wrong revision")
	}
	secondRevision := commitStackOverride(t, f, `{"version":1,"environment":[{"name":"CI","value":"0"}]}`)
	second, err := capturedWorkflow(t.Context(), f.opts, req, agent.WorkflowImplement, specs)
	if err != nil {
		t.Fatal(err)
	}
	if second.StackRevision != secondRevision || second.StackProfile.Effective.Environment[0].Value != "0" || first.Digest == second.Digest {
		t.Fatal("fresh admission failed to pick new configuration")
	}
}
func TestInvalidStackAdmissionStartsNoProvider(t *testing.T) {
	for _, kind := range []string{"malformed", "unknown", "forbidden", "host executable"} {
		t.Run(kind, func(t *testing.T) {
			runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
				return nil, fmt.Errorf("recording provider should not have started")
			})
			f := newFixture(t, runner)
			base := `{"version":1,"guidance":[{"id":"stack","skill":"implement/skills/stack/SKILL.md"}],"override_fields":["environment"]}`
			raw := `{"version":1}`
			switch kind {
			case "malformed":
				raw = `{"version":1,"environment":[`
			case "unknown":
				raw = `{"version":1,"credential":"private-canary"}`
			case "forbidden":
				raw = `{"version":1,"setup":[]}`
			case "host executable":
				base = `{"version":1,"guidance":[{"id":"stack","skill":"implement/skills/stack/SKILL.md"}],"requirements":{"executables":["unavailable-private-canary-host-tool"]}}`
			}
			configureFreshStackSource(t, f, []byte(base))
			if _, err := f.db.AdmitWorkflowPath(t.Context(), f.session.ID, "graph"); err != nil {
				t.Fatal(err)
			}
			commitStackOverride(t, f, raw)
			_, err := Execute(t.Context(), f.opts, f.request())
			if err == nil || runner.callCount() != 0 {
				t.Fatal("invalid profile admitted", err, runner.callCount())
			}
			if !strings.Contains(err.Error(), "stack.") && !strings.Contains(err.Error(), "stack:") {
				t.Fatal("refusal did not identify stack field", err)
			}
			if strings.Contains(err.Error(), "private-canary") {
				t.Fatal("diagnostic disclosed authored value", err)
			}
		})
	}
}
