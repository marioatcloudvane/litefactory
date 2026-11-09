package turn

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
)

// TestImplementSizingRoutingEndToEnd follows approved local facts through a new
// shipped capture, configured delivery, the retained worker answer and real save.
// No provider or external repository is used: the forge and Git remote are local.
func TestImplementSizingRoutingEndToEnd(t *testing.T) {
	for _, team := range []string{"go-development", "dotnet-web", "python-fastapi", "swift-ios"} {
		for _, scenario := range []struct {
			name, size, reason, roleTier, worker, tier string
			old                                        bool
		}{
			{name: "small", size: "S", reason: "Bounded change", worker: "worker_agent", tier: "balanced"},
			{name: "medium", size: "M", reason: "Several connected surfaces", worker: "worker_agent", tier: "balanced"},
			{name: "large", size: "L", reason: "Broad change", worker: "worker_flagship", tier: "flagship"},
			{name: "missing", worker: "worker_flagship", tier: "flagship"},
			{name: "invalid", size: "XL", reason: "Unknown size", worker: "worker_flagship", tier: "flagship"},
			{name: "changed role default", size: "S", reason: "Bounded change", roleTier: "fast", worker: "worker_agent", tier: "fast"},
			{name: "old unsized approval", old: true, worker: "worker_flagship", tier: "flagship"},
		} {
			t.Run(team+"/"+scenario.name, func(t *testing.T) {
				var launches []agent.Request
				runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
					launches = append(launches, req)
					if req.Graph.Node != scenario.worker || req.Delivery == nil || len(req.Delivery.Evidence.Specialists) != 1 {
						t.Fatalf("wrong worker or missing delivery: %+v", req.Graph)
					}
					evidence := req.Delivery.Evidence.Specialists[0]
					if evidence.Tier != scenario.tier || evidence.Model != "configured-"+scenario.tier || req.Delivery.MainModel != evidence.Model {
						t.Fatalf("configured routing evidence: %+v model=%s", evidence, req.Model)
					}
					mustProgress(t, os.WriteFile(filepath.Join(req.WorktreePath, "sized.txt"), []byte("implemented\n"), 0600))
					submitLiveResult(t, req, implementCompletedJSON)
					return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
				})
				f, _, req := assembledImplementFixture(t, runner)
				useSizingShippedCapture(t, f, team, scenario.roleTier)
				f.opts.Tiers.ModelByTier = map[config.Kind]map[config.Tier]string{"claude": {"balanced": "configured-balanced", "flagship": "configured-flagship", "fast": "configured-fast"}}
				if scenario.old {
					seedImplementStory(t, f)
				} else {
					seedSizedImplementStory(t, f, scenario.size, scenario.reason)
				}
				out, err := Execute(t.Context(), f.opts, req)
				if err != nil || !out.Succeeded() {
					t.Fatalf("execute: %+v %v", out, err)
				}
				runOwedImplementStages(t, f, req)
				row := implementWorkerRow(t, f)
				if len(launches) != 1 || row.State != store.ImplementDone || !out.Pushed {
					t.Fatalf("one selected worker must save before done: launches=%d row=%+v out=%+v", len(launches), row, out)
				}
				instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
				mustProgress(t, err)
				attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
				mustProgress(t, err)
				workers, results, syncs, facts := 0, 0, 0, 0
				resultNode := "worker_result"
				if scenario.worker == "worker_flagship" {
					resultNode = "worker_flagship_result"
				}
				for _, a := range attempts {
					switch a.Node {
					case "worker_agent", "worker_flagship":
						workers++
						if a.Node != scenario.worker {
							t.Fatalf("other worker ran: %+v", a)
						}
						run, err := f.db.GetRun(t.Context(), a.RunID)
						mustProgress(t, err)
						meta := f.readMeta(run.Ordinal)
						if meta.Delivery == nil || meta.Delivery.MainModel != "configured-"+scenario.tier || meta.Delivery.Evidence.Specialists[0].Tier != scenario.tier {
							t.Fatalf("retained metadata lacks selected tier/model: %+v", meta)
						}
					case resultNode:
						if a.Outcome == "completed" {
							results++
						}
					case "sync_before_worker":
						syncs++
					case "sizing_valid", "size_s", "size_m":
						outputs, err := f.db.ListGraphOutputs(t.Context(), a.ID)
						mustProgress(t, err)
						if len(outputs) == 0 {
							t.Fatalf("condition lacks retained source facts: %+v", a)
						}
						var proof struct {
							Facts      map[string]string          `json:"facts"`
							Source     *store.ApprovedStorySizing `json:"source"`
							Parent     string                     `json:"parent"`
							Generation int                        `json:"generation"`
							Story      string                     `json:"story"`
							Digest     string                     `json:"approved_digest"`
							Outcome    string                     `json:"outcome"`
						}
						mustProgress(t, json.Unmarshal(outputs[0].Payload, &proof))
						if proof.Parent != implementTaskParent || proof.Generation != 1 || proof.Story != "story-001" || proof.Digest == "" || proof.Outcome != a.Outcome || proof.Facts["story.size"] != scenario.size {
							t.Fatalf("condition lost approved source binding: %+v", proof)
						}
						if scenario.old {
							if proof.Source != nil {
								t.Fatalf("old approval acquired a newer snapshot: %+v", proof.Source)
							}
						} else if proof.Source == nil || proof.Source.ProjectionRevision != proof.Digest || proof.Source.EntryID != "s1" || proof.Source.SizeReason != scenario.reason {
							t.Fatalf("source provenance missing: %+v", proof.Source)
						}
						facts++
					}
				}
				if workers != 1 || results != 1 || syncs != 1 || facts == 0 {
					t.Fatalf("selected result/sync/fact evidence: workers=%d results=%d syncs=%d facts=%d", workers, results, syncs, facts)
				}
			})
		}
	}
}

func useSizingShippedCapture(t *testing.T, f *fixture, team, roleTier string) {
	t.Helper()
	root, err := teamsourcetest.Workflow(t.Context(), team, "implement")
	mustProgress(t, err)
	files := fstest.MapFS{}
	mustProgress(t, fs.WalkDir(root, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		raw, err := fs.ReadFile(root, name)
		if err != nil {
			return err
		}
		if name == "story-worker.md" && roleTier != "" {
			raw = []byte(strings.Replace(string(raw), "tier: balanced", "tier: "+roleTier, 1))
		}
		files[name] = &fstest.MapFile{Data: raw}
		return nil
	}))
	f.opts.WorkflowSource = func(ctx context.Context, repo store.Repo, workflow agent.Workflow, specs graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		if workflow != agent.WorkflowImplement {
			return fixtureWorkflowSource(ctx, repo, workflow, specs)
		}
		return fixtureInstalled(teamsource.CaptureWorkflowIdentity(ctx, files, "implement.dot", team, workflow, specs))
	}
}

func seedSizedImplementStory(t *testing.T, f *fixture, size, reason string) {
	t.Helper()
	ctx := t.Context()
	f.otherSession(900)
	const title = "Health endpoint"
	const body = "As an operator I want a health endpoint."
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(title+"\x00"+body)))
	story := store.ManagedStory{ParentTask: implementTaskParent, Generation: 1, Key: "story-001", IssueNumber: f.session.IssueNumber, IssueID: 4242, BodyHash: digest, State: "planning"}
	mustProgress(t, f.db.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: implementTaskParent, Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}))
	mustProgress(t, f.db.PutManagedStory(ctx, story))
	_, err := f.db.ReservePlanStory(ctx, implementTaskParent, story.Key)
	mustProgress(t, err)
	mustProgress(t, f.db.BindPlanStoryShell(ctx, implementTaskParent, story.Key, story.IssueID, story.IssueNumber, digest))
	mustProgress(t, f.db.BindFeatureContract(ctx, store.FeatureContract{Parent: implementTaskParent, Generation: 1, ContentVersion: 2, SubmissionVersion: 2}))
	mustProgress(t, f.db.PutSubmissionProjection(ctx, store.SubmissionProjection{Parent: implementTaskParent, Generation: 1, Story: story.Key, IssueID: story.IssueID, Revision: digest, Title: title, Body: body, ContentVersion: 2, Content: []byte(`{}`)}))
	state := planbrief.PlanState{SharedRevision: "initial", Stories: []planbrief.PlanStoryState{{Key: story.Key, Draft: planbrief.PlanDraft{Key: story.Key}}}, StorySet: &planbrief.PlanStorySetState{Revision: "set-revision", Promoted: true, PromotedRevision: "set-revision", Keys: map[string]string{"s1": story.Key}, Entries: []planbrief.PlanStorySetEntry{{ID: "s1", Size: size, SizeReason: reason}}}}
	state.StorySet.Revision = planbrief.PlanStorySetRevision(state.StorySet.Entries)
	state.StorySet.PromotedRevision = state.StorySet.Revision
	state.AssembledApproval = state.PlanSetRevision()
	state.PublishedRevision = state.AssembledApproval
	state.SavedRevision = state.AssembledApproval
	revision, err := f.db.SavePlanWorkflowState(ctx, implementTaskParent, 0, state)
	mustProgress(t, err)
	mustProgress(t, f.db.RetainPublishedStorySizing(ctx, implementTaskParent, 1, story.Key, story.IssueID, digest, revision))
	mustProgress(t, f.db.SetManagedFeatureReady(ctx, implementTaskParent, 1, true))
	mustProgress(t, f.db.ApproveManagedStories(ctx, implementTaskParent, 1, []store.ApprovedStoryRevision{{ManagedStory: story, ParentID: 9, Title: title, Body: body, Hash: digest, FetchedAt: implementClock}}))
	mustProgress(t, f.db.DeliverStoryScopeNotice(ctx, implementTaskParent, 1, story.Key, "freeze"))
	mustProgress(t, f.db.ActivateManagedScope(ctx, implementTaskParent, 1))
	mustProgress(t, f.db.EnsureImplementStories(ctx, implementTaskParent, 1, []store.ImplementStorySeed{{Key: story.Key, Position: 0, Branch: "lf/story-001"}}, implementClock))
	row, err := f.db.GetImplementStory(ctx, implementTaskParent, 1, story.Key)
	mustProgress(t, err)
	_, err = f.db.TransitionImplementStory(ctx, store.ImplementTransition{Parent: implementTaskParent, Generation: 1, Key: story.Key, From: store.ImplementWaiting, To: store.ImplementRunning, ExpectedRevision: row.Revision, At: implementClock})
	mustProgress(t, err)
	mustProgress(t, f.db.SetImplementWorkerSession(ctx, implementTaskParent, 1, story.Key, f.session.ID, implementClock))
}

func TestImplementSizingRepairRestoresApprovedFactsAndCapture(t *testing.T) {
	for _, scenario := range []struct {
		name, size, worker string
		old                bool
	}{
		{name: "role worker", size: "S", worker: "worker_agent"},
		{name: "flagship worker", size: "L", worker: "worker_flagship"},
		{name: "retained absent snapshot", worker: "worker_flagship", old: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
				calls++
				if req.Graph.Node != scenario.worker {
					t.Fatalf("repair changed approved routing: node=%s calls=%d", req.Graph.Node, calls)
				}
				result := implementIncompleteJSON
				if calls == 3 {
					mustProgress(t, os.WriteFile(filepath.Join(req.WorktreePath, "repaired.txt"), []byte("finished\n"), 0600))
					result = implementCompletedJSON
				}
				submitLiveResult(t, req, result)
				return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
			})
			f, _, request := assembledImplementFixture(t, runner)
			useSizingShippedCapture(t, f, "go-development", "")
			if scenario.old {
				f.otherSession(900)
				seedImplementStory(t, f)
			} else {
				seedSizedImplementStory(t, f, scenario.size, "Approved sizing")
			}
			if _, err := Execute(t.Context(), f.opts, request); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("first dispatch calls=%d", calls)
			}
			// A subsequent Plan can add sizing or reverse the old size, but cannot
			// change the approved work's retained snapshot (including its absence).
			state, revision, err := f.db.GetPlanWorkflowState(t.Context(), implementTaskParent)
			mustProgress(t, err)
			changed := "S"
			if scenario.size == "S" {
				changed = "L"
			}
			state.SharedRevision = "later"
			state.StorySet = &planbrief.PlanStorySetState{Revision: "later-plan", PromotedRevision: "later-plan", Promoted: true, Keys: map[string]string{"s1": "story-001"}, Entries: []planbrief.PlanStorySetEntry{{ID: "s1", Size: changed, SizeReason: "Later unrelated Plan"}}}
			_, err = f.db.SavePlanWorkflowState(t.Context(), implementTaskParent, revision, state)
			mustProgress(t, err)
			// The second incomplete answer causes a third decision after the Plan
			// change, so replay also proves fresh evaluation uses approved facts.
			// Each Execute reconstructs the runtime. A resumed capture must be read
			// from its retained bytes even when the current source is unavailable.
			f.opts.WorkflowSource = func(context.Context, store.Repo, agent.Workflow, graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
				return nil, fmt.Errorf("new capture unavailable during retained repair")
			}
			runOwedImplementStages(t, f, request)
			row := implementWorkerRow(t, f)
			if calls != 3 || row.Attempts != 2 || row.State != store.ImplementDone {
				t.Fatalf("repair did not finish on retained route: calls=%d row=%+v", calls, row)
			}
			instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
			mustProgress(t, err)
			attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
			mustProgress(t, err)
			syncs, workers := 0, 0
			for _, a := range attempts {
				if a.Node == "sync_before_worker" {
					syncs++
				}
				if a.Node == scenario.worker {
					workers++
				}
			}
			if syncs != 3 || workers != 3 {
				t.Fatalf("repair bypassed fresh synchronization: syncs=%d workers=%d", syncs, workers)
			}
		})
	}
}

func TestImplementSizingRetainedSingleWorkerCaptureKeepsOriginalRoute(t *testing.T) {
	calls := 0
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		if req.Graph.Node != "worker_agent" || req.Delivery.Evidence.Specialists[0].Tier != "balanced" {
			t.Fatalf("old capture changed route: node=%+v delivery=%+v", req.Graph, req.Delivery)
		}
		mustProgress(t, os.WriteFile(filepath.Join(req.WorktreePath, "legacy.txt"), []byte("done\n"), 0600))
		submitLiveResult(t, req, implementCompletedJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, _, req := assembledImplementFixture(t, runner)
	seedSizedImplementStory(t, f, "L", "Large approved story in an older capture")
	out, err := Execute(t.Context(), f.opts, req)
	if err != nil || !out.Succeeded() || calls != 1 || implementWorkerRow(t, f).State != store.ImplementDone {
		t.Fatalf("legacy single-worker capture must finish unchanged: out=%+v calls=%d err=%v", out, calls, err)
	}
}

func TestImplementSizingSelectedWorkerLifecycle(t *testing.T) {
	for _, size := range []string{"S", "L"} {
		for _, scenario := range []string{"incomplete budget", "failed budget", "blocked", "save failure"} {
			t.Run(size+"/"+scenario, func(t *testing.T) {
				worker := "worker_agent"
				if size == "L" {
					worker = "worker_flagship"
				}
				calls := 0
				var f *fixture
				runner := lifecycleMCPHost(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
					calls++
					if req.Graph.Node != worker {
						t.Fatalf("wrong selected worker: %s", req.Graph.Node)
					}
					capture, err := f.db.WorkflowCaptureForAttempt(ctx, req.Graph.AttemptID)
					mustProgress(t, err)
					cost, err := GraphDispatchSlotCost(ctx, f.db, store.GraphDispatch{InstanceID: req.Graph.InstanceID, LaunchID: capture.ID, Node: req.Graph.Node})
					mustProgress(t, err)
					if cost != 3 {
						t.Fatalf("selected worker capacity: %d", cost)
					}
					if scenario == "failed budget" {
						return &agent.RunResult{Outcome: agent.OutcomeFailed, Reason: "sized worker crashed", FinishedAt: time.Now()}, nil
					}
					result := implementIncompleteJSON
					if scenario == "blocked" {
						result = implementBlockedJSON
					}
					if scenario == "save failure" {
						result = implementCompletedJSON
						mustProgress(t, os.WriteFile(filepath.Join(req.WorktreePath, "save.txt"), []byte(fmt.Sprintf("attempt %d\n", calls)), 0600))
						if calls == 1 {
							hook := f.repo.GitIn(req.WorktreePath, "rev-parse", "--git-path", "hooks/pre-push")
							if !filepath.IsAbs(hook) {
								hook = filepath.Join(req.WorktreePath, hook)
							}
							mustProgress(t, os.MkdirAll(filepath.Dir(hook), 0700))
							mustProgress(t, os.WriteFile(hook, []byte("#!/bin/sh\nrm -- \"$0\"\nexit 1\n"), 0700))
						}
					}
					submitLiveResult(t, req, result)
					return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
				})
				var req Request
				f, _, req = assembledImplementFixture(t, runner)
				useSizingShippedCapture(t, f, "go-development", "")
				seedSizedImplementStory(t, f, size, "Lifecycle sizing")
				_, executeErr := Execute(t.Context(), f.opts, req)
				if scenario == "save failure" {
					if executeErr == nil || !strings.Contains(executeErr.Error(), "push") {
						t.Fatalf("expected visible save error: %v", executeErr)
					}
				} else if executeErr != nil {
					t.Fatal(executeErr)
				}
				runOwedImplementStages(t, f, req)
				row := implementWorkerRow(t, f)
				wantCalls, wantAttempts, wantState := store.MaxImplementAttempts, store.MaxImplementAttempts, store.ImplementParked
				if scenario == "blocked" {
					wantCalls, wantAttempts = 1, 0
				}
				if scenario == "save failure" {
					wantCalls, wantAttempts, wantState = 2, 1, store.ImplementDone
				}
				if calls != wantCalls || row.Attempts != wantAttempts || row.State != wantState {
					t.Fatalf("shared lifecycle budget: calls=%d row=%+v", calls, row)
				}
				instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
				mustProgress(t, err)
				attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
				mustProgress(t, err)
				syncs := 0
				for _, a := range attempts {
					if a.Node == "sync_before_worker" {
						syncs++
					}
					if (a.Node == "worker_agent" || a.Node == "worker_flagship") && a.Node != worker {
						t.Fatalf("mixed worker results: %+v", a)
					}
				}
				if syncs != calls {
					t.Fatalf("each worker launch must synchronize: syncs=%d calls=%d", syncs, calls)
				}
			})
		}
	}
}

func TestImplementSizingOwnerRestartKeepsApprovedSizing(t *testing.T) {
	for _, size := range []string{"S", "L"} {
		t.Run(size, func(t *testing.T) {
			calls := 0
			worker := "worker_agent"
			if size == "L" {
				worker = "worker_flagship"
			}
			runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
				calls++
				if req.Graph.Node != worker {
					t.Fatalf("owner restart changed routing: %+v", req.Graph)
				}
				result := implementBlockedJSON
				if calls == 2 {
					mustProgress(t, os.WriteFile(filepath.Join(req.WorktreePath, "resumed.txt"), []byte("done\n"), 0600))
					result = implementCompletedJSON
				}
				submitLiveResult(t, req, result)
				return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
			})
			f, _, request := assembledImplementFixture(t, runner)
			useSizingShippedCapture(t, f, "go-development", "")
			seedSizedImplementStory(t, f, size, "Original approved sizing")
			if _, err := Execute(t.Context(), f.opts, request); err != nil {
				t.Fatal(err)
			}
			row := implementWorkerRow(t, f)
			if row.State != store.ImplementParked {
				t.Fatalf("first answer did not park: %+v", row)
			}
			state, revision, err := f.db.GetPlanWorkflowState(t.Context(), implementTaskParent)
			mustProgress(t, err)
			changed := "S"
			if size == "S" {
				changed = "L"
			}
			state.StorySet.Entries[0].Size = changed
			state.StorySet.Entries[0].SizeReason = "Later Plan sizing"
			state.StorySet.Revision = planbrief.PlanStorySetRevision(state.StorySet.Entries)
			state.StorySet.PromotedRevision = state.StorySet.Revision
			_, err = f.db.SavePlanWorkflowState(t.Context(), implementTaskParent, revision, state)
			mustProgress(t, err)
			// The authenticated coordinator's owner-reply path performs these existing
			// ledger transitions; this test isolates their effect on the worker replay.
			row, err = f.db.TransitionImplementStory(t.Context(), store.ImplementTransition{Parent: implementTaskParent, Generation: 1, Key: "story-001", From: store.ImplementParked, To: store.ImplementWaiting, ExpectedRevision: row.Revision, Guidance: "Owner supplied the missing dependency.", At: implementClock})
			mustProgress(t, err)
			_, err = f.db.TransitionImplementStory(t.Context(), store.ImplementTransition{Parent: implementTaskParent, Generation: 1, Key: "story-001", From: store.ImplementWaiting, To: store.ImplementRunning, ExpectedRevision: row.Revision, At: implementClock})
			mustProgress(t, err)
			mustProgress(t, f.db.SetImplementWorkerSession(t.Context(), implementTaskParent, 1, "story-001", f.session.ID, implementClock))
			resumed := request
			resumed.Trigger.CommentID += 1000
			resumed.Trigger.Body = "Start the resumed story worker."
			if _, err = Execute(t.Context(), f.opts, resumed); err != nil {
				t.Fatal(err)
			}
			row = implementWorkerRow(t, f)
			if calls != 2 || row.State != store.ImplementDone {
				t.Fatalf("resumed worker did not finish: calls=%d row=%+v", calls, row)
			}
		})
	}
}

func TestImplementSizingDeferredSyncKeepsSelectedWorker(t *testing.T) {
	for _, size := range []string{"S", "L"} {
		t.Run(size, func(t *testing.T) {
			calls := 0
			worker := "worker_agent"
			if size == "L" {
				worker = "worker_flagship"
			}
			var f *fixture
			runner := lifecycleMCPHost(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
				calls++
				if req.Graph.Node != worker {
					t.Fatalf("deferred sync changed worker: %+v", req.Graph)
				}
				gate, err := f.db.LatestGraphNodeAttempt(ctx, req.Graph.InstanceID, "sync_before_worker")
				mustProgress(t, err)
				if gate.Outcome != "deferred" {
					t.Fatalf("fixture must exercise deferred conflict: %+v", gate)
				}
				submitLiveResult(t, req, implementBlockedJSON)
				return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
			})
			var req Request
			f, _, req = assembledImplementFixture(t, runner)
			useSizingShippedCapture(t, f, "go-development", "")
			seedSizedImplementStory(t, f, size, "Deferred sync sizing")
			seedSizingSyncConflict(t, f)
			if _, err := Execute(t.Context(), f.opts, req); err != nil {
				t.Fatal(err)
			}
			row := implementWorkerRow(t, f)
			if calls != 1 || row.State != store.ImplementParked || row.Attempts != 0 {
				t.Fatalf("deferred sync must preserve honest blocked outcome: calls=%d row=%+v", calls, row)
			}
		})
	}
}

func seedSizingSyncConflict(t *testing.T, f *fixture) {
	t.Helper()
	ctx := t.Context()
	parent := f.otherSession(900)
	_, err := f.db.FreezeSessionSlug(ctx, parent.ID, "feature", "lf/900-feature")
	mustProgress(t, err)
	_, err = f.db.FreezeSessionSlug(ctx, f.session.ID, testSlug, "lf/story-001")
	mustProgress(t, err)
	f.repo.WriteFile("shared.txt", "ancestor\n")
	f.repo.Commit("shared ancestor")
	f.repo.Git("push", "--quiet", "origin", "HEAD:refs/heads/lf/900-feature")
	f.repo.Git("push", "--quiet", "origin", "HEAD:refs/heads/lf/story-001")
	advanceRemote(t, f, "lf/story-001", "shared.txt", "story edit\n")
	advanceRemote(t, f, "lf/900-feature", "shared.txt", "base edit\n")
}
