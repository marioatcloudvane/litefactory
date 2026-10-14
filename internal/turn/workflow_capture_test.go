package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/store/storetest"
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
		for _, provenance := range []string{"local", "fetched", "builtin", ""} {
			t.Run(string(variant.workflow)+"/"+provenance, func(t *testing.T) {
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
					def, err = fixtureInstalled(def, nil)
					if err != nil {
						return nil, err
					}
					def.Origin.GraphPath = string(w) + "/entry.dot"
					def.Origin.GraphFile = "entry.dot"
					if provenance == "fetched" {
						def.Origin.Kind = "fetched"
						def.Origin.Source = "https://example.com/teams.json"
						def.Origin.ReleaseDigest = strings.Repeat("a", 64)
					}
					return def, nil
				}
				def, err := capturedWorkflow(t.Context(), f.opts, req, variant.workflow, specs)
				if err != nil {
					t.Fatal(err)
				}
				switch provenance {
				case "fetched":
					def.Origin.Kind = "fetched"
					def.Origin.Source = "https://example.com/teams.json"
					def.Origin.ReleaseDigest = strings.Repeat("a", 64)
				case "builtin":
					def.Origin = teamsource.WorkflowOrigin{Kind: "builtin", GraphPath: string(variant.workflow) + "/entry.dot", GraphFile: "entry.dot", Team: def.Profile.Team}
				case "":
					def.Origin = teamsource.WorkflowOrigin{}
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
				pin, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: def.Profile.Team, Mode: string(def.Profile.Mode), Workflow: string(def.Profile.Workflow), ProfileVersion: def.Profile.ProfileVersion, Path: string(variant.workflow) + "/entry.dot", Digest: def.Profile.Digest, TeamSnapshotDigest: def.Digest, CreatedAt: time.Now()})
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
				restored, err := capturedWorkflow(t.Context(), f.opts, req, variant.workflow, specs)
				if err != nil {
					t.Fatal(err)
				}
				if restored.Digest != def.Digest || calls != 1 || !reflect.DeepEqual(restored.Files, def.Files) || !reflect.DeepEqual(restored.Origin, def.Origin) || restored.Profile.Digest != def.Profile.Digest {
					t.Fatalf("retained replay changed bytes: %v", err)
				}
				dispatch := store.GraphDispatch{InstanceID: admitted.Instance.ID, SessionID: f.session.ID, LaunchID: admitted.LaunchID}
				if err := ValidateGraphDispatchRecovery(t.Context(), f.db, dispatch); err != nil {
					t.Fatal(err)
				}
				foreign := dispatch
				foreign.InstanceID++
				if err := ValidateGraphDispatchRecovery(t.Context(), f.db, foreign); err == nil {
					t.Fatal("foreign capture accepted for recovery")
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
	_, err = capturedWorkflow(t.Context(), f.opts, req, agent.WorkflowImplement, specs)
	if !errors.Is(err, expected) {
		t.Fatalf("lost source cause: %v", err)
	}
	if _, err = f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("failed capture admitted execution: %v", err)
	}
}

func TestWorkflowCaptureFreshRefusalsAcrossSixWorkflows(t *testing.T) {
	for _, workflow := range []agent.Workflow{agent.WorkflowResearch, agent.WorkflowPlan, agent.WorkflowImplement, agent.WorkflowValidate, agent.WorkflowSpike, agent.WorkflowBug} {
		for _, condition := range []string{"absent resolver", "nil definition", "nil profile", "builtin", "historical", "bad provenance", "missing resource", "changed digest", "cancellation"} {
			t.Run(string(workflow)+"/"+condition, func(t *testing.T) {
				f := newFixture(t, nil)
				req := f.request()
				var specs graphexec.Registry
				var err error
				switch workflow {
				case agent.WorkflowResearch:
					specs, err = ResearchSpecs()
				case agent.WorkflowPlan:
					specs, err = PlanSpecs()
				case agent.WorkflowImplement:
					specs, err = ImplementSpecs()
				case agent.WorkflowValidate:
					specs, err = ValidateFlowSpecs()
				case agent.WorkflowSpike:
					specs, err = SpikeSpecs()
				case agent.WorkflowBug:
					specs, err = SimplifiedBugSpecs()
				}
				if err != nil {
					t.Fatal(err)
				}
				if condition == "absent resolver" {
					f.opts.WorkflowSource = nil
				} else {
					f.opts.WorkflowSource = func(ctx context.Context, repo store.Repo, w agent.Workflow, r graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
						if condition == "nil definition" {
							return nil, nil
						}
						if condition == "cancellation" {
							return nil, context.Canceled
						}
						def, err := fixtureWorkflowSource(ctx, repo, w, r)
						if err != nil {
							return nil, err
						}
						switch condition {
						case "nil profile":
							def.Profile = nil
						case "builtin":
							def.Origin.Kind = "builtin"
						case "historical":
							def.Origin = teamsource.WorkflowOrigin{}
						case "bad provenance":
							def.Origin.ConfigDigest = "bad"
						case "missing resource":
							delete(def.Files, def.Origin.GraphFile)
						case "changed digest":
							def.Digest = strings.Repeat("0", 64)
						}
						return def, nil
					}
				}
				def, err := capturedWorkflow(t.Context(), f.opts, req, workflow, specs)
				if err == nil || def != nil || !strings.Contains(err.Error(), f.storeRepo.Path) || !strings.Contains(err.Error(), string(workflow)) {
					t.Fatalf("refusal lost context: %v %v", def, err)
				}
				if condition == "cancellation" && !errors.Is(err, context.Canceled) {
					t.Fatalf("lost cancellation: %v", err)
				}
				if _, err = f.db.LatestWorkflowInstance(t.Context(), f.session.ID, string(workflow)); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("refusal admitted work: %v", err)
				}
			})
		}
	}
}

func TestWorkflowCaptureSavedGraphWithoutEvidenceNeverSelectsFresh(t *testing.T) {
	f := newFixture(t, nil)
	req := f.request()
	specs, err := ImplementSpecs()
	if err != nil {
		t.Fatal(err)
	}
	def, err := fixtureWorkflowSource(t.Context(), f.storeRepo, agent.WorkflowImplement, specs)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: def.Profile.Team, Mode: "implement", ProfileVersion: def.Profile.ProfileVersion, Path: workflowGraphPath(def), Digest: def.Profile.Digest, TeamSnapshotDigest: def.Digest})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: pin.ID, CurrentNode: def.Profile.Start, StartedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	f.opts.WorkflowSource = func(context.Context, store.Repo, agent.Workflow, graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		t.Fatal("saved graph selected a fresh workflow")
		return nil, nil
	}
	if _, err = capturedWorkflow(t.Context(), f.opts, req, agent.WorkflowImplement, specs); !errors.Is(err, store.ErrGraphInvariant) || !strings.Contains(err.Error(), f.storeRepo.Path) {
		t.Fatalf("missing saved evidence accepted: %v", err)
	}
	saved, err := f.db.GetGraphInstance(t.Context(), instance.ID)
	if err != nil || !reflect.DeepEqual(instance, saved) {
		t.Fatalf("saved work changed: %+v %v", saved, err)
	}
}

func TestWorkflowCapturePersistencePrecedesExecutionAndFailureStopsDriver(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			f, _, req := assembledResearchFixture(t, &stubRunner{})
			calls := 0
			f.opts.ResearchDriver = func(ctx context.Context, o Options, r Request, result Result, def *teamsource.ResearchDefinition, admitted store.ResearchAdmission) (Result, error) {
				calls++
				saved, err := o.DB.GetWorkflowCapture(ctx, admitted.LaunchID)
				if err != nil {
					t.Fatal(err)
				}
				var files map[string][]byte
				if err = json.Unmarshal(saved.Files, &files); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(files, def.Files) || !reflect.DeepEqual(saved.Provenance, store.WorkflowProvenance(def.Origin)) {
					t.Fatal("execution preceded exact capture persistence")
				}
				return result, nil
			}
			if failure {
				// SQL fault injection stays in storetest; turn never embeds SQL.
				storetest.FailWorkflowCaptureWrite(t, f.db)
			}
			_, err := Execute(t.Context(), f.opts, req)
			if failure {
				if err == nil || calls != 0 || !strings.Contains(err.Error(), "capture persistence fault") || !strings.Contains(err.Error(), f.storeRepo.Path) || !strings.Contains(err.Error(), "research") {
					t.Fatalf("persistence failure reached execution: calls=%d err=%v", calls, err)
				}
				if _, err = f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "research"); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("failed admission retained partial work: %v", err)
				}
			} else if err != nil || calls != 1 {
				t.Fatalf("captured execution failed: calls=%d err=%v", calls, err)
			}
		})
	}
}
