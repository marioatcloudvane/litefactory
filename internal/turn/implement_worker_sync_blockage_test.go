package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestImplementWorkerSyncBlockagesPreserveWorkWithoutLaunching(t *testing.T) {
	for _, delay := range []time.Duration{0, 30 * time.Second} {
		t.Run(delay.String(), func(t *testing.T) {
			for _, scenario := range []string{"conflict", "dirty", "unpublished", "missing remote"} {
				t.Run(scenario, func(t *testing.T) {
					calls := 0
					f, forge, req := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
						calls++
						t.Error("blocked sync launched worker")
						return nil, nil
					}))
					enableWorkerSyncFixture(f)
					f.opts.GraphRetryDelay = delay
					seedImplementStory(t, f)
					_, err := f.db.FreezeSessionSlug(t.Context(), f.session.ID, testSlug, "lf/story-001")
					mustProgress(t, err)
					parent := f.otherSession(900)
					_, err = f.db.FreezeSessionSlug(t.Context(), parent.ID, "feature", "lf/900-feature")
					mustProgress(t, err)
					f.repo.WriteFile("shared.txt", "original\n")
					f.repo.Commit("shared ancestor")
					f.repo.Git("push", "--quiet", "origin", "HEAD:refs/heads/lf/900-feature")
					f.repo.Git("push", "--quiet", "origin", "HEAD:refs/heads/lf/story-001")
					clone, err := f.opts.Git.Open(t.Context(), f.repo.Dir)
					mustProgress(t, err)
					checkout, err := git.EnsureWorktree(t.Context(), clone, git.EnsureWorktreeOptions{Dir: filepath.Join(f.storeRepo.WorktreeRoot, "retained"), Branch: "lf/story-001", BaseBranch: "lf/900-feature", Remote: "origin"})
					mustProgress(t, err)
					dir := checkout.Worktree.Path()
					mustProgress(t, f.db.SetSessionWorktree(t.Context(), f.session.ID, dir))
					switch scenario {
					case "conflict":
						mustProgress(t, os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("story edit\n"), 0600))
						f.repo.GitIn(dir, "add", "shared.txt")
						f.repo.GitIn(dir, "commit", "--quiet", "-m", "story edit")
						f.repo.GitIn(dir, "push", "--quiet", "origin", "HEAD:refs/heads/lf/story-001")
						advanceRemote(t, f, "lf/900-feature", "shared.txt", "base edit\n")
					case "dirty":
						mustProgress(t, os.WriteFile(filepath.Join(dir, "retained.txt"), []byte("uncommitted work\n"), 0600))
					case "unpublished", "missing remote":
						mustProgress(t, os.WriteFile(filepath.Join(dir, "retained.txt"), []byte("unpublished work\n"), 0600))
						f.repo.GitIn(dir, "add", "retained.txt")
						f.repo.GitIn(dir, "commit", "--quiet", "-m", "unpublished")
					}
					if scenario == "missing remote" {
						f.repo.Git("push", "--quiet", "origin", ":refs/heads/lf/story-001")
					}
					before := f.repo.GitIn(dir, "rev-parse", "HEAD")
					status := f.repo.GitIn(dir, "status", "--porcelain")
					remote := f.repo.Git("ls-remote", "origin", "refs/heads/lf/story-001")
					indexPath := f.repo.GitIn(dir, "rev-parse", "--git-path", "index")
					index, err := os.ReadFile(indexPath)
					mustProgress(t, err)
					retainedPath := "retained.txt"
					if scenario == "conflict" {
						retainedPath = "shared.txt"
					}
					content, err := os.ReadFile(filepath.Join(dir, retainedPath))
					mustProgress(t, err)
					_, err = Execute(t.Context(), f.opts, req)
					if err != nil {
						t.Fatalf("sync blockage: %v", err)
					}
					row := implementWorkerRow(t, f)
					if row.State != store.ImplementParked || row.Attempts != 0 || row.LastFailure != "" || calls != 0 {
						t.Fatalf("sync consumed worker budget or remained runnable: calls=%d row=%+v", calls, row)
					}
					afterIndex, err := os.ReadFile(indexPath)
					mustProgress(t, err)
					afterContent, err := os.ReadFile(filepath.Join(dir, retainedPath))
					mustProgress(t, err)
					if !bytes.Equal(index, afterIndex) || !bytes.Equal(content, afterContent) {
						t.Fatal("retained index or file bytes changed")
					}
					if got := f.repo.GitIn(dir, "rev-parse", "HEAD"); got != before {
						t.Fatalf("retained head moved: %s -> %s", before, got)
					}
					if got := f.repo.GitIn(dir, "status", "--porcelain"); got != status {
						t.Fatalf("retained bytes/status changed: %q -> %q", status, got)
					}
					if got := f.repo.Git("ls-remote", "origin", "refs/heads/lf/story-001"); got != remote {
						t.Fatal("blocked sync published story history")
					}
					instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
					mustProgress(t, err)
					attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
					mustProgress(t, err)
					count := 0
					for _, attempt := range attempts {
						if attempt.Node == "sync_before_worker" {
							count++
						}
						if attempt.Node == "worker_agent" {
							t.Fatal("worker attempt reserved")
						}
					}
					expected := 1
					if count != expected {
						t.Fatalf("sync dispatches = %d, want %d", count, expected)
					}
					for _, want := range []string{"Base synchronization", "lf/story-001", "lf/900-feature"} {
						if !strings.Contains(row.Reason, want) {
							t.Fatalf("reason lacks %q: %s", want, row.Reason)
						}
					}
					if scenario == "conflict" && !strings.Contains(row.Reason, "shared.txt") {
						t.Fatal("conflict path absent from reason")
					}
					specific := map[string]string{"dirty": "uncommitted or untracked", "unpublished": "local commits", "missing remote": "unpublished history"}[scenario]
					if !strings.Contains(row.Reason, specific) {
						t.Fatalf("specific reason absent: %s", row.Reason)
					}
					posts := forge.all()
					if len(posts) != 1 || !strings.Contains(html.UnescapeString(posts[0].body), row.Reason) {
						t.Fatalf("factory blockage missing first-answer report: %+v", posts)
					}
				})
			}
		})
	}
}

func TestImplementWorkerSyncParkingLostResponseReplaysWithoutDuplicate(t *testing.T) {
	f, forge, _ := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Fatal("no worker expected")
		return nil, nil
	}))
	enableWorkerSyncFixture(f)
	seedImplementStory(t, f)
	row := implementWorkerRow(t, f)
	rt := &implementRuntime{opts: f.opts, worker: &row, definition: mustCaptureImplement(t)}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	forge.afterPost = cancel
	reason := "Base synchronization stopped before the first worker answer: conflict in shared.txt"
	_, err := rt.parkSyncStory(ctx, reason)
	if err == nil {
		t.Fatal("lost report response hidden")
	}
	parked := implementWorkerRow(t, f)
	if parked.State != store.ImplementParked || parked.Attempts != 0 {
		t.Fatalf("uncertain delivery leaves story runnable: %+v", parked)
	}
	forge.recover()
	for range 2 {
		_, err = rt.parkSyncStory(t.Context(), reason)
		mustProgress(t, err)
	}
	if forge.attemptCount() != 1 {
		t.Fatal("replay duplicated sync report")
	}
	if got := implementWorkerRow(t, f); got != parked {
		t.Fatalf("replay changed ledger: %+v", got)
	}
}

func TestImplementWorkerSyncCumulativeExhaustionAfterSuccesses(t *testing.T) {
	f, _, req := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Fatal("no launch expected")
		return nil, nil
	}))
	enableWorkerSyncFixture(f)
	seedImplementStory(t, f)
	row := implementWorkerRow(t, f)
	definition := mustCaptureImplement(t)
	at := time.Now()
	pin, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: definition.Profile.Team, Mode: "implement", ProfileVersion: 1, Path: "test/worker-sync.dot", Digest: definition.Profile.Digest, TeamSnapshotDigest: definition.Digest, CreatedAt: at})
	mustProgress(t, err)
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: pin.ID, Status: store.GraphRunning, CurrentNode: "sync_before_worker", StartedAt: at})
	mustProgress(t, err)
	for i := 0; i < 8; i++ {
		eventAt := at.Add(time.Duration(i) * time.Second)
		outcome := "retry"
		if i == 0 || i == 7 {
			outcome = "current"
		}
		attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "sync_before_worker", StartedAt: eventAt})
		mustProgress(t, err)
		raw, err := json.Marshal(syncBaseOutput{Working: row.Branch, Base: "main", Result: outcome, Reason: "last retained handoff"})
		mustProgress(t, err)
		_, err = f.db.RecordGraphOutput(t.Context(), store.GraphOutput{NodeAttemptID: attempt.ID, Kind: store.GraphOutputJSON, Payload: raw, CreatedAt: eventAt})
		mustProgress(t, err)
		mustProgress(t, f.db.CompleteGraphNodeAttempt(t.Context(), attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: outcome, FinishedAt: eventAt, InstanceStatus: store.GraphRunning, Transition: &store.GraphTransition{InstanceID: instance.ID, FromNode: "sync_before_worker", ToNode: "sync_before_worker", OnOutcome: outcome, At: eventAt}}))
	}
	rt := &implementRuntime{opts: f.opts, req: req, worker: &row, definition: definition}
	reason, err := rt.workerSyncBlockage(t.Context(), instance.ID, true)
	mustProgress(t, err)
	if !strings.Contains(reason, "cumulative limit of 8") || !strings.Contains(reason, "last retained handoff") {
		t.Fatalf("successful dispatches were not counted cumulatively: %q", reason)
	}
	_, err = rt.parkSyncStory(t.Context(), reason)
	mustProgress(t, err)
	parked := implementWorkerRow(t, f)
	if parked.State != store.ImplementParked || parked.Attempts != 0 || parked.LastFailure != "" {
		t.Fatalf("sync exhaustion spent worker budget: %+v", parked)
	}
}

func TestImplementWorkerSyncTransientRetryDelayAndBound(t *testing.T) {
	for _, delay := range []time.Duration{0, time.Minute} {
		t.Run(delay.String(), func(t *testing.T) {
			f, forge, req := assembledImplementFixture(t, nil)
			enableWorkerSyncFixture(f)
			seedImplementStory(t, f)
			now := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
			f.opts.MCP.Now = func() time.Time { return now }
			f.opts.GraphRetryDelay = delay
			row := implementWorkerRow(t, f)
			definition := mustCaptureImplement(t)
			profile := definition.Profile
			pin, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: profile.Team, Mode: "implement", ProfileVersion: 1, Path: "test/worker-sync.dot", Digest: profile.Digest, TeamSnapshotDigest: definition.Digest})
			mustProgress(t, err)
			topology, err := graphexec.SemanticDigest(profile)
			mustProgress(t, err)
			files, err := json.Marshal(definition.Files)
			mustProgress(t, err)
			_, err = f.db.AdmitWorkflowPath(t.Context(), f.session.ID, "graph")
			mustProgress(t, err)
			admission, err := f.db.AdmitWorkflowLaunch(t.Context(), store.WorkflowLaunch{Mode: "implement", SessionID: f.session.ID, TriggerID: req.Trigger.CommentID, DefinitionID: pin.ID, Start: "sync_before_worker", ContentDigest: definition.Digest, TopologyDigest: topology, Files: files, Provenance: store.WorkflowProvenance(definition.Origin)})
			mustProgress(t, err)
			rt := &implementRuntime{opts: f.opts, req: req, worker: &row, definition: definition}
			policy, err := rt.policy()
			mustProgress(t, err)
			register := policy.RegisterEffects
			policy.RegisterEffects = func(specs *graphexec.Registry, activities *ActivityRegistry, p *graphexec.Profile, scope func(context.Context, store.GraphNodeAttempt) (agentresult.Context, error), worktree func() (string, string, error), env WorkflowEnv) error {
				if err := register(specs, activities, p, scope, worktree, env); err != nil {
					return err
				}
				// Exercise the real sync activity's transient lock refusal.
				activities.handlers[graphexec.SyncBaseAction] = SyncBaseActivity{SyncBaseBinding{Bind: func(context.Context, Options, ActivityRequest) (SyncBaseTarget, error) {
					return SyncBaseTarget{Working: row.Branch, Base: "main", Lock: func(context.Context) (func() error, bool, error) { return nil, false, nil }}, nil
				}}}
				return nil
			}
			for count := 1; count <= 9; count++ {
				_, err = ExecuteWorkflow(t.Context(), f.opts, req, Result{}, definition, admission, policy)
				if delay == 0 {
					if err == nil || !strings.Contains(err.Error(), "graph retry requires a positive host delay") {
						t.Fatalf("zero delay: %v", err)
					}
					return
				}
				if count == 9 {
					if err == nil {
						t.Fatal("ninth dispatch exceeded bound")
					}
					reason, readErr := rt.workerSyncBlockage(t.Context(), admission.Instance.ID, true)
					mustProgress(t, readErr)
					if !strings.Contains(reason, "cumulative limit of 8") {
						t.Fatalf("exhaustion: %s", reason)
					}
					_, err = rt.parkSyncStory(t.Context(), reason)
					mustProgress(t, err)
					break
				}
				mustProgress(t, err)
				instance, err := f.db.GetGraphInstance(t.Context(), admission.Instance.ID)
				mustProgress(t, err)
				attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
				mustProgress(t, err)
				if len(attempts) != count || instance.CurrentNode != "sync_before_worker" || !instance.NextDueAt.Equal(now.Add(delay)) {
					t.Fatalf("retry did not defer one dispatch: %+v attempts=%d", instance, len(attempts))
				}
				early, err := f.db.ListDueGraphDispatches(t.Context(), now, 100)
				mustProgress(t, err)
				if len(early) != 0 {
					t.Fatalf("early dispatch: %+v", early)
				}
				dispatch, err := f.db.LatestGraphDispatch(t.Context(), instance.ID)
				mustProgress(t, err)
				now = instance.NextDueAt
				reserved, ok, err := f.db.ReserveGraphDispatch(t.Context(), dispatch.ID, now)
				mustProgress(t, err)
				if !ok {
					t.Fatal("due dispatch unavailable")
				}
				req.Trigger.CommentID = reserved.TriggerID()
			}
			parked := implementWorkerRow(t, f)
			if parked.State != store.ImplementParked || parked.Attempts != 0 || parked.LastFailure != "" || len(forge.all()) != 1 {
				t.Fatalf("exhaustion did not park without worker budget: %+v", parked)
			}
		})
	}
}
