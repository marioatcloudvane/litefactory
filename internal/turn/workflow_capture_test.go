package turn

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

func TestWorkflowCaptureAllSixSourcesAndRetainedReplay(t *testing.T) {
	variants := []struct {
		workflow agent.Workflow
		specs    func() (graphexec.Registry, error)
		capture  func(context.Context, graphexec.Registry) (*teamsource.WorkflowDefinition, error)
	}{
		{agent.WorkflowResearch, ResearchSpecs, teamsource.CaptureResearch},
		{agent.WorkflowPlan, PlanSpecs, teamsource.CapturePlan},
		{agent.WorkflowImplement, ImplementSpecs, teamsource.CaptureImplement},
		{agent.WorkflowValidate, ValidateFlowSpecs, teamsource.CaptureValidate},
		{agent.WorkflowSpike, SpikeSpecs, teamsource.CaptureSpike},
		{agent.WorkflowBug, SimplifiedBugSpecs, teamsource.CaptureBugParent},
	}
	for _, variant := range variants {
		t.Run(string(variant.workflow), func(t *testing.T) {
			f := newFixture(t, nil)
			req := f.request()
			req.Mode = agent.Mode(variant.workflow)
			if variant.workflow == agent.WorkflowBug {
				req.Mode = ""
				req.Workflow = agent.WorkflowBug
			}
			specs, err := variant.specs()
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			f.opts.WorkflowSource = func(ctx context.Context, repo store.Repo, w agent.Workflow, r graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
				calls++
				if repo.ID != f.storeRepo.ID || w != variant.workflow {
					t.Fatal("task worktree lost registered clone identity")
				}
				def, err := variant.capture(ctx, r)
				if err != nil {
					return nil, err
				}
				// Package slug deliberately differs from its declared builtin team.
				files := def.Files
				files["entry.dot"] = files[string(w)+".dot"]
				delete(files, string(w)+".dot")
				def, err = teamsource.CaptureWorkflowIdentity(ctx, teamsource.RetainedFiles(files), "entry.dot", def.Profile.Team, w, r)
				if err != nil {
					return nil, err
				}
				def.Origin = teamsource.WorkflowOrigin{Kind: "builtin", GraphPath: string(w) + "/entry.dot", GraphFile: "entry.dot", Team: def.Profile.Team}
				return def, nil
			}
			forbiddenBuiltin := func(context.Context, graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
				t.Fatal("linked admission fell back to builtin")
				return nil, nil
			}
			def, err := capturedWorkflow(t.Context(), f.opts, req, variant.workflow, specs, forbiddenBuiltin)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.db.AdmitWorkflowPath(t.Context(), f.session.ID, "graph"); err != nil {
				t.Fatal(err)
			}
			if variant.workflow == agent.WorkflowBug {
				if _, err = f.db.AdmitWorkflow(t.Context(), store.TaskWorkflowAdmission{SessionID: f.session.ID, Version: 1, Family: "standalone", Workflow: "bug", SourceTrigger: req.Trigger.CommentID}); err != nil {
					t.Fatal(err)
				}
			}
			if variant.workflow == agent.WorkflowPlan {
				if _, err = f.db.AdmitPlanPath(t.Context(), f.session.ID); err != nil {
					t.Fatal(err)
				}
			}
			pin, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: def.Profile.Team, Mode: string(def.Profile.Mode), Workflow: string(def.Profile.Workflow), ProfileVersion: def.Profile.ProfileVersion, Path: workflowGraphPath(def), Digest: def.Profile.Digest, TeamSnapshotDigest: def.Digest, CreatedAt: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(def.Files)
			if err != nil {
				t.Fatal(err)
			}
			topology, err := graphexec.SemanticDigest(def.Profile)
			if err != nil {
				t.Fatal(err)
			}
			admitted, err := f.db.AdmitWorkflowLaunch(t.Context(), store.WorkflowLaunch{SessionID: f.session.ID, TriggerID: req.Trigger.CommentID, DefinitionID: pin.ID, Mode: string(def.Profile.Mode), Workflow: string(def.Profile.Workflow), Start: def.Profile.Start, ContentDigest: def.Digest, TopologyDigest: topology, Files: raw, Provenance: store.WorkflowProvenance(def.Origin)})
			if err != nil {
				t.Fatal(err)
			}
			if name, err := retainedGraphFile(t.Context(), f.db, admitted.Instance.ID); err != nil || name != "entry.dot" {
				t.Fatalf("lost mapped graph path: %s %v", name, err)
			}
			f.opts.WorkflowSource = func(context.Context, store.Repo, agent.Workflow, graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
				t.Fatal("continuation reopened current source after removal")
				return nil, nil
			}
			restored, err := capturedWorkflow(t.Context(), f.opts, req, variant.workflow, specs, forbiddenBuiltin)
			if err != nil || restored.Digest != def.Digest || calls != 1 {
				t.Fatalf("retained replay changed bytes: %v", err)
			}
			saved, err := f.db.GetWorkflowCapture(t.Context(), admitted.LaunchID)
			if err != nil {
				t.Fatal(err)
			}
			saved.Files = []byte(`{"missing.dot":"eA=="}`)
			if _, err = restoreWorkflow(t.Context(), f.db, saved, variant.workflow, specs); err == nil {
				t.Fatal("missing retained resources accepted")
			}
		})
	}
}

func TestWorkflowCaptureFreshFailureDoesNotFallBack(t *testing.T) {
	f := newFixture(t, nil)
	req := f.request()
	specs, err := ImplementSpecs()
	if err != nil {
		t.Fatal(err)
	}
	expected := errors.New("selected package workflow disappeared")
	f.opts.WorkflowSource = func(context.Context, store.Repo, agent.Workflow, graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		return nil, expected
	}
	_, err = capturedWorkflow(t.Context(), f.opts, req, agent.WorkflowImplement, specs, func(context.Context, graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		t.Fatal("failure fell back")
		return nil, nil
	})
	if !errors.Is(err, expected) {
		t.Fatalf("lost source cause: %v", err)
	}
	if _, err = f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("failed capture admitted execution: %v", err)
	}
}
