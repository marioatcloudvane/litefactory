package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

func TestGraphBugConcreteHostAnalystContinuationLeavesWorktreeAlone(t *testing.T) {
	calls := 0
	runner := lifecycleMCPHost(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		if req.Graph == nil || req.Delivery == nil || req.Delivery.MainRole != "bug-analyst" {
			t.Fatalf("concrete graph binding missing: %+v", req)
		}
		if !strings.Contains(req.Prompt, "Retained Bug conversation") || !strings.Contains(req.Prompt, "Factory source preparation") || !strings.Contains(req.Prompt, "The worktree is at commit") {
			t.Fatal("retained source/conversation missing")
		}
		if e := req.ValidateBugAnalyst(); e != nil {
			t.Fatal(e)
		}
		raw, e := json.Marshal(map[string]any{"version": 1, "response": "blocked", "result": map[string]any{"outcome": "blocked", "reason": "Controlled owner evidence needed", "comment": "Partial diagnosis retained", "payload": map[string]any{"bug": map[string]any{"response": "blocked", "invocation": fmt.Sprintf("analysis:%d:%d", req.Graph.InstanceID, req.Graph.AttemptID), "base_revision": 0, "evidence": []string{"Controlled owner report"}}}}})
		mustProgress(t, e)
		submitLiveResult(t, req, string(raw))
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: `{"request_id":"receipt-only"}`, FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledResearchFixture(t, runner)
	f.opts.Tiers.ModelByTier[config.Kind("claude")][config.Tier("flagship")] = "fixture-model"
	f.opts.AutoSyncBase = true
	req.Workflow = agent.WorkflowBug
	req.Mode = ""
	req.Command = ParseCommand("Investigate this bug")
	req.Trigger.Body = "Investigate this bug"
	req.ToolProfile = "bug-analyst"
	req.Trigger.AuthorID = 42
	_, e := f.db.AdmitWorkflow(t.Context(), store.TaskWorkflowAdmission{SessionID: f.session.ID, Version: 1, Family: "standalone", Workflow: "bug"})
	mustProgress(t, e)
	runtime, e := NewGraphBugRuntime(t.Context(), f.opts, BugRuntimeConfig{Forge: bugRuntimeJourneyForge{forge.client}, OwnerID: 42, Permissions: func(agent.Kind, string) (BugRolePermissions, error) {
		return BugRolePermissions{Tools: []string{"Read", "Glob", "Grep"}, AllowedTools: []string{"Read", "Glob", "Grep"}}, nil
	}})
	mustProgress(t, e)
	f.opts.Bug = runtime
	out, e := Execute(t.Context(), f.opts, req)
	mustProgress(t, e)
	if calls != 1 || out.Pushed {
		t.Fatalf("calls=%d pushed=%t result=%+v", calls, out.Pushed, out)
	}
	instance, e := f.db.LatestStandaloneBugInstance(t.Context(), f.session.ID)
	mustProgress(t, e)
	wait, e := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	mustProgress(t, e)
	if wait.State != "waiting" || wait.OriginNode != "analyst" {
		t.Fatalf("stop=%+v", wait)
	}
	session, e := f.db.GetSession(t.Context(), f.session.ID)
	mustProgress(t, e)
	mustProgress(t, os.WriteFile(filepath.Join(session.WorktreePath, "retained.txt"), []byte("retained unfinished work\n"), 0600))
	f.repo.WriteFile("main-change.txt", "current main\n")
	f.repo.Commit("advance main before owner continuation")
	f.repo.Git("push", "origin", "main")
	delivery, e := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
	mustProgress(t, e)
	req.Trigger.CommentID = max(req.Trigger.CommentID+1, delivery.CommentID+1)
	req.Trigger.Body = "Continue with the retained work"
	req.Command = ParseCommand(req.Trigger.Body)
	out, e = Execute(t.Context(), f.opts, req)
	mustProgress(t, e)
	if calls != 2 || out.Pushed {
		t.Fatalf("continuation calls=%d pushed=%t", calls, out.Pushed)
	}
	saved, e := os.ReadFile(filepath.Join(session.WorktreePath, "retained.txt"))
	mustProgress(t, e)
	if string(saved) != "retained unfinished work\n" {
		t.Fatal("dirty work lost")
	}
	// The analyst reads the worktree as it is: it neither commits nor merges
	// main, and no source record is stored.
	if _, e = os.Stat(filepath.Join(session.WorktreePath, "main-change.txt")); e == nil {
		t.Fatal("the read-only analyst synchronized with main")
	}
	if _, e = f.db.LatestGraphRecord(t.Context(), instance.ID, "source_checkpoint"); e == nil {
		t.Fatal("a source checkpoint was stored")
	}
}

// Controlled issue identity over the existing offline forge fixture.
type bugRuntimeJourneyForge struct{ *github.Client }

func (f bugRuntimeJourneyForge) GetIssue(context.Context, github.Repo, int) (github.Issue, error) {
	return github.Issue{ID: 10123, Number: testIssue, State: "open", APIURL: f.BaseURL() + "repos/acme/acme-api/issues/123"}, nil
}

func TestGraphBugConcreteHostRefusesLegacyPinWithoutDispatchOrMutation(t *testing.T) {
	calls := 0
	f, forge, req := assembledResearchFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		calls++
		return nil, fmt.Errorf("legacy dispatch forbidden")
	}))
	req.Workflow = agent.WorkflowBug
	req.Mode = ""
	req.Command = ParseCommand("Investigate retained work")
	req.Trigger.AuthorID = 42
	req.Trigger.Body = req.Command.Text
	_, err := f.db.AdmitWorkflow(t.Context(), store.TaskWorkflowAdmission{SessionID: f.session.ID, Version: 1, Family: "standalone", Workflow: "bug"})
	mustProgress(t, err)
	definition, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Workflow: "bug", ProfileVersion: 1, Path: "bug/bug.dot", Digest: strings.Repeat("a", 64), CreatedAt: time.Now()})
	mustProgress(t, err)
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: definition.ID, CurrentNode: "record", StartedAt: time.Now()})
	mustProgress(t, err)
	runtime, err := NewGraphBugRuntime(t.Context(), f.opts, BugRuntimeConfig{Forge: bugRuntimeJourneyForge{forge.client}, OwnerID: 42, Permissions: func(agent.Kind, string) (BugRolePermissions, error) {
		return BugRolePermissions{Tools: []string{"Read"}, AllowedTools: []string{"Read"}}, nil
	}})
	mustProgress(t, err)
	beforeSession, err := f.db.GetSession(t.Context(), f.session.ID)
	mustProgress(t, err)
	mustProgress(t, os.WriteFile(filepath.Join(f.repo.Dir, "retained-unfinished.txt"), []byte("untouched old edits"), 0600))
	f.opts.Bug = runtime
	_, err = Execute(t.Context(), f.opts, req)
	if err == nil || !strings.Contains(err.Error(), "unsupported pinned legacy Bug") {
		t.Fatalf("legacy pin admitted: %v", err)
	}
	if calls != 0 {
		t.Fatal("legacy provider dispatched")
	}
	after, err := f.db.GetGraphInstance(t.Context(), instance.ID)
	mustProgress(t, err)
	if after != instance {
		t.Fatalf("legacy instance mutated: before=%+v after=%+v", instance, after)
	}
	afterSession, err := f.db.GetSession(t.Context(), f.session.ID)
	mustProgress(t, err)
	if afterSession.State != beforeSession.State || afterSession.WorktreePath != beforeSession.WorktreePath || afterSession.Branch != beforeSession.Branch {
		t.Fatal("legacy session changed")
	}
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
	mustProgress(t, err)
	if len(attempts) != 0 {
		t.Fatal("legacy attempt allocated")
	}
	saved, err := os.ReadFile(filepath.Join(f.repo.Dir, "retained-unfinished.txt"))
	mustProgress(t, err)
	if string(saved) != "untouched old edits" {
		t.Fatal("legacy work changed")
	}
}

func TestOwnerStopBugFinalizerAndReplyChannel(t *testing.T) {
	for _, source := range []string{"cli", "comment"} {
		for _, role := range []string{"bug-fixer", "bug-analyst"} {
			t.Run(source+"/"+role, func(t *testing.T) {
				f, p, inst := approvedGraphRepairFixture(t)
				ctx := t.Context()
				segment, err := f.db.GetOrCreateSegment(ctx, f.session.ID, 2, "stop-fixer", store.AgentClaude)
				mustProgress(t, err)
				run, err := f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segment.ID, Workflow: "bug", Agent: store.AgentClaude, TriggerCommentID: 140, LogPath: func(int) string { return "stop.log" }})
				mustProgress(t, err)
				attempt, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: inst.ID, Node: "fixer", RunID: run.ID, Limit: 3, StartedAt: f.opts.MCP.Now()})
				mustProgress(t, err)
				binary, err := git.Resolve(ctx)
				mustProgress(t, err)
				clone, err := binary.Open(ctx, f.repo.Dir)
				mustProgress(t, err)
				tree, err := git.EnsureWorktree(ctx, clone, git.EnsureWorktreeOptions{Dir: filepath.Join(f.storeRepo.WorktreeRoot, "owner-stop"), Branch: "lf/owner-stop", BaseBranch: gittest.DefaultBranch})
				mustProgress(t, err)
				head, err := tree.Worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
				mustProgress(t, err)
				mustProgress(t, os.WriteFile(filepath.Join(tree.Worktree.Path(), "unfinished.txt"), []byte("retain me\n"), 0600))
				var stop store.OwnerStop
				var command store.DoctorCommand
				if source == "cli" {
					stop, err = f.db.RequestOwnerStop(ctx, f.session.TaskRef)
				} else {
					command, err = f.db.AdmitDoctorCommand(ctx, store.DoctorCommand{RepoID: f.storeRepo.ID, TaskRef: f.session.TaskRef, IssueID: 123, IssueNumber: testIssue, CommentID: 200, ActorID: 7, CreatedAt: time.Now().UTC(), Operation: "stop"})
					mustProgress(t, err)
					err = json.Unmarshal([]byte(command.ResultBody), &stop)
				}
				mustProgress(t, err)
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				step, err := parkGraphAction(cancelled, f.opts, GraphNodeRequest{Profile: p, InstanceID: inst.ID, RunID: run.ID, At: f.opts.MCP.Now()}, GraphNodeResult{InstanceID: inst.ID, AttemptID: attempt.ID, Node: "fixer", Activity: ActivityResult{Diagnostic: "cancelled"}}, "failed", false)
				mustProgress(t, err)
				if step.Status != "waiting_owner" {
					t.Fatalf("step=%+v", step)
				}
				wait, err := f.db.GetGraphOwnerWait(ctx, inst.ID)
				mustProgress(t, err)
				_, err = f.opts.ResearchReporter.RecoverGraphStopQuestion(ctx, wait.DeliveryKey)
				mustProgress(t, err)
				if _, err = f.db.GetReportDelivery(ctx, wait.DeliveryKey); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("graph stop report exists: %v", err)
				}
				prep := &preparation{run: run, segment: segment, session: f.session, repo: f.storeRepo, ref: f.ref(), worktree: tree.Worktree, shaBefore: head, author: f.opts.Author, metaPath: filepath.Join(t.TempDir(), "meta.json"), request: agent.Request{Permissions: agent.Permissions{Profile: role}}}
				w := researchWorkflow{opts: f.opts, req: Request{Workflow: agent.WorkflowBug, graphBugPreparation: true}, prep: prep, process: &agent.RunResult{Outcome: agent.OutcomeAbandoned, Reason: "cancelled", FinishedAt: time.Now()}, policy: BugWorkflowPolicy()}
				out, err := w.finish(cancelled, context.Canceled)
				if !errors.Is(err, context.Canceled) || out.Outcome != agent.OutcomeAbandoned || out.Reason != store.ReasonOwnerStop {
					t.Fatalf("out=%+v err=%v", out, err)
				}
				saved, err := f.db.GetRun(ctx, run.ID)
				mustProgress(t, err)
				if saved.Error != store.ReasonOwnerStop || saved.Status != store.RunFailed {
					t.Fatalf("run=%+v", saved)
				}
				if role == "bug-fixer" {
					if !saved.Published || saved.CommitSHA == "" || !out.Committed {
						t.Fatalf("not saved: %+v %+v", saved, out)
					}
				} else if saved.Published || saved.CommitSHA != "" {
					t.Fatal("analyst saved work")
				}
				body, err := os.ReadFile(filepath.Join(tree.Worktree.Path(), "unfinished.txt"))
				mustProgress(t, err)
				if string(body) != "retain me\n" {
					t.Fatal("lost work")
				}
				runtime := BugRuntime{opts: f.opts, cfg: BugRuntimeConfig{OwnerID: 7}, definition: &teamsource.WorkflowDefinition{Profile: p}}
				req := Request{SessionID: f.session.ID, Workflow: agent.WorkflowBug, Trigger: Trigger{CommentID: 400, AuthorID: 7, Body: "Continue the approved repair"}}
				if source == "comment" {
					req.Trigger.CommentID = command.CommentID - 1
				}
				_, err = f.db.RecordComment(ctx, store.Comment{SessionID: f.session.ID, ForgeCommentID: req.Trigger.CommentID, AuthorUserID: 7, Body: req.Trigger.Body, Classification: store.ClassificationInstruction, CreatedAt: stop.At.Add(-time.Second)})
				mustProgress(t, err)
				if _, err = runtime.graphAdmission(ctx, req); !errors.Is(err, store.ErrGraphReplyPredatesQuestion) {
					t.Fatalf("old reply admitted: %v", err)
				}
				req.Trigger.CommentID = 401
				_, err = f.db.RecordComment(ctx, store.Comment{SessionID: f.session.ID, ForgeCommentID: 401, AuthorUserID: 7, Body: req.Trigger.Body, Classification: store.ClassificationInstruction, CreatedAt: stop.At.Add(time.Second)})
				mustProgress(t, err)
				if source == "comment" {
					if _, err = runtime.graphAdmission(ctx, req); err == nil {
						t.Fatal("unpublished doctor reply admitted")
					}
					command, err = f.db.ClaimDoctorCommand(ctx, "", true, "fixture")
					mustProgress(t, err)
					command.ResultBody = "Workflow stopped by the owner. Reply to resume."
					mustProgress(t, f.db.CompleteDoctorCommand(ctx, command.ID, command.ResultBody, 0))
					mustProgress(t, f.db.EnsureDoctorDeliveries(ctx, command, []string{command.ResultBody}, 99))
					debts, err := f.db.PendingDoctorDeliveries(ctx)
					mustProgress(t, err)
					if len(debts) != 1 {
						t.Fatalf("debts=%d", len(debts))
					}
					_, err = f.db.ConfirmReportDelivery(ctx, debts[0].Key, 300)
					mustProgress(t, err)
					mustProgress(t, f.db.CompleteDoctorDelivery(ctx, debts[0].Key))
				}
				req.Trigger.AuthorID = 8
				if _, err = runtime.graphAdmission(ctx, req); err == nil {
					t.Fatal("foreign reply admitted")
				}
				req.Trigger.AuthorID = 7
				_, err = runtime.graphAdmission(ctx, req)
				mustProgress(t, err)
				wait, err = f.db.GetGraphOwnerWait(ctx, inst.ID)
				mustProgress(t, err)
				if wait.State != "claimed" || wait.OriginNode != "fixer" {
					t.Fatalf("wait=%+v", wait)
				}
				handoff, err := GraphBugApprovedHandoff(ctx, f.db, inst.ID)
				mustProgress(t, err)
				if handoff.Candidate.Revision != 1 {
					t.Fatal("approval changed")
				}
				nextRun, err := f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segment.ID, Workflow: "bug", Agent: store.AgentClaude, TriggerCommentID: req.Trigger.CommentID, LogPath: func(int) string { return "resume.log" }})
				mustProgress(t, err)
				next, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: inst.ID, Node: wait.OriginNode, RunID: nextRun.ID, Limit: 3, StartedAt: stop.At.Add(2 * time.Second)})
				mustProgress(t, err)
				resumed, err := f.db.GetGraphOwnerWait(ctx, inst.ID)
				mustProgress(t, err)
				if resumed.State != "allocated" || resumed.ContinuationAttemptID != next.ID || next.Node != "fixer" {
					t.Fatalf("continuation=%+v attempt=%+v", resumed, next)
				}

			})
		}
	}
}

type ownerStopAcceptedActivity struct{}

func (ownerStopAcceptedActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	instance, err := opts.DB.GetGraphInstance(ctx, req.InstanceID)
	if err != nil {
		return ActivityResult{}, err
	}
	session, err := opts.DB.GetSession(ctx, instance.SessionID)
	if err != nil {
		return ActivityResult{}, err
	}
	if _, err = opts.DB.RequestOwnerStop(ctx, session.TaskRef); err != nil {
		return ActivityResult{}, err
	}
	return ActivityResult{Outcome: "succeeded", ProcessResult: &agent.RunResult{Outcome: agent.OutcomeAbandoned, Reason: "cancelled"}}, nil
}

func TestOwnerStopBugCannotAdvanceAcceptedResult(t *testing.T) {
	f, p, inst := approvedGraphRepairFixture(t)
	ctx := t.Context()
	segment, err := f.db.GetOrCreateSegment(ctx, f.session.ID, 2, "stop-fixer", store.AgentClaude)
	mustProgress(t, err)
	run, err := f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segment.ID, Workflow: "bug", Agent: store.AgentClaude, TriggerCommentID: 140, LogPath: func(int) string { return "stop.log" }})
	mustProgress(t, err)
	var activities ActivityRegistry
	mustProgress(t, activities.Register(ResearchAgentAction, ownerStopAcceptedActivity{}))
	out, err := ExecuteNodeAction(ctx, f.opts, GraphNodeRequest{Profile: p, Activities: activities, InstanceID: inst.ID, RunID: run.ID, At: f.opts.MCP.Now()})
	mustProgress(t, err)
	if out.Status != "waiting_owner" || out.NextNode != "fixer" {
		t.Fatalf("stop advanced to delivery: %+v", out)
	}
	wait, err := f.db.GetGraphOwnerWait(ctx, inst.ID)
	mustProgress(t, err)
	if _, err = f.db.GetReportDelivery(ctx, wait.DeliveryKey); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stop created graph publication: %v", err)
	}
}

func bugBootstrapFixture(t *testing.T, runner Runner) (*fixture, *BugRuntime, Request) {
	t.Helper()
	f, forge, req := assembledResearchFixture(t, runner)
	f.opts.Tiers.ModelByTier[config.Kind("claude")][config.Tier("flagship")] = "fixture-model"
	req.Workflow, req.Mode, req.ToolProfile = agent.WorkflowBug, "", "bug-analyst"
	req.Trigger.AuthorID = 42
	req.Trigger.Body = "Investigate the bootstrap failure"
	req.Command = ParseCommand(req.Trigger.Body)
	_, err := f.db.AdmitWorkflow(t.Context(), store.TaskWorkflowAdmission{SessionID: f.session.ID, Version: 1, Family: "standalone", Workflow: "bug"})
	mustProgress(t, err)
	runtime, err := NewGraphBugRuntime(t.Context(), f.opts, BugRuntimeConfig{Forge: bugRuntimeJourneyForge{forge.client}, OwnerID: 42, Permissions: func(agent.Kind, string) (BugRolePermissions, error) {
		return BugRolePermissions{Tools: []string{"Read"}, AllowedTools: []string{"Read"}}, nil
	}})
	mustProgress(t, err)
	f.opts.Bug = runtime
	return f, runtime, req
}

func TestGraphBugBootstrapAndUntouchedRecovery(t *testing.T) {
	for _, scenario := range []string{"body", "comment", "untouched", "consume failure", "graph trigger"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			runner := lifecycleMCPHost(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
				calls++
				mustProgress(t, req.ValidateBugAnalyst())
				if !strings.Contains(req.Prompt, "Investigate the bootstrap failure") {
					t.Fatal("bootstrap input missing from analyst prompt")
				}
				raw, err := json.Marshal(map[string]any{"version": 1, "response": "blocked", "result": map[string]any{"outcome": "blocked", "reason": "Controlled evidence needed", "comment": "Diagnosis retained", "payload": map[string]any{"bug": map[string]any{"response": "blocked", "invocation": fmt.Sprintf("analysis:%d:%d", req.Graph.InstanceID, req.Graph.AttemptID), "base_revision": 0, "evidence": []string{"Controlled report"}}}}})
				mustProgress(t, err)
				submitLiveResult(t, req, string(raw))
				return &agent.RunResult{Outcome: agent.OutcomeSucceeded, FinishedAt: time.Now()}, nil
			})
			f, runtime, req := bugBootstrapFixture(t, runner)
			ctx := t.Context()
			_, _, err := f.db.BootstrapWatermark(ctx, f.session.ID, 7)
			mustProgress(t, err)
			var original store.WorkflowAdmission
			switch scenario {
			case "body":
				req.Trigger.CommentID = 0
				req.Issue.Body, req.Trigger.Body = req.Trigger.Body, ""
			case "untouched", "graph trigger":
				body := req
				body.Trigger.CommentID = 0
				original, err = runtime.graphAdmission(ctx, body)
				mustProgress(t, err)
				if scenario == "graph trigger" {
					dispatch, err := f.db.ParkGraphInstance(ctx, original.Instance.ID, original.Instance.CurrentNode, f.opts.MCP.Now(), f.opts.MCP.Now())
					mustProgress(t, err)
					_, reserved, err := f.db.ReserveGraphDispatch(ctx, dispatch.ID, f.opts.MCP.Now())
					mustProgress(t, err)
					if !reserved {
						t.Fatal("dispatch not reserved")
					}
					req.Trigger.CommentID = dispatch.TriggerID()
				}
			case "consume failure":
				// A separate closed reporter database injects failure after admission,
				// without changing the graph store or production Reporter validation.
				broken, err := store.Open(ctx, filepath.Join(t.TempDir(), "closed.db"))
				mustProgress(t, err)
				mustProgress(t, broken.Close())
				reporter := *runtime.opts.ResearchReporter
				reporter.db = broken
				runtime.opts.ResearchReporter = &reporter
				_, err = Execute(ctx, f.opts, req)
				if err == nil || !strings.Contains(err.Error(), "consume comment") || calls != 0 {
					t.Fatalf("consume failure: calls=%d err=%v", calls, err)
				}
				original.Instance, err = f.db.LatestStandaloneBugInstance(ctx, f.session.ID)
				mustProgress(t, err)
				runtime.opts.ResearchReporter = f.opts.ResearchReporter
				req.Trigger.CommentID++
			}
			_, err = Execute(ctx, f.opts, req)
			mustProgress(t, err)
			instance, err := f.db.LatestStandaloneBugInstance(ctx, f.session.ID)
			mustProgress(t, err)
			if original.Instance.ID != 0 && instance.ID != original.Instance.ID {
				t.Fatal("recovery replaced the retained instance")
			}
			wait, err := f.db.GetGraphOwnerWait(ctx, instance.ID)
			mustProgress(t, err)
			if calls != 1 || wait.OriginNode != "analyst" {
				t.Fatalf("calls=%d wait=%+v", calls, wait)
			}
			if scenario == "untouched" || scenario == "consume failure" {
				captured, err := f.db.HasWorkflowLaunch(ctx, f.session.ID, req.Trigger.CommentID)
				mustProgress(t, err)
				if !captured {
					t.Fatal("fresh owner input has no retained launch")
				}
			}
			wm, _, err := f.db.GetWatermark(ctx, f.session.ID)
			mustProgress(t, err)
			want := req.Trigger.CommentID
			if want == 0 || store.IsGraphTrigger(want) {
				want = 7
			}
			if wm.LastCommentID != want {
				t.Fatalf("watermark=%d want=%d", wm.LastCommentID, want)
			}
			attemptsBefore, err := f.db.ListGraphNodeAttempts(ctx, instance.ID)
			mustProgress(t, err)
			runsBefore, err := f.db.ListRuns(ctx, f.session.ID)
			mustProgress(t, err)
			// A replay may be refused by existing request/dispatch validation; it must
			// never rerun analysis or replace the retained launch.
			_, err = Execute(ctx, f.opts, req)
			if err != nil {
				t.Logf("replay refused without re-execution: %v", err)
			}
			if calls != 1 {
				t.Fatalf("replay invoked analyst %d times", calls)
			}
			attemptsAfter, err := f.db.ListGraphNodeAttempts(ctx, instance.ID)
			mustProgress(t, err)
			runsAfter, err := f.db.ListRuns(ctx, f.session.ID)
			mustProgress(t, err)
			if len(attemptsAfter) != len(attemptsBefore) || len(runsAfter) != len(runsBefore) {
				t.Fatal("replay allocated duplicate work")
			}
		})
	}
}

func TestGraphBugDeliveryConcreteConflictAndOwnerWait(t *testing.T) {
	for _, variant := range []string{"resolved", "refused", "invalid", "head drift", "union", "union no advisor", "budget", "mixed"} {
		t.Run(variant, func(t *testing.T) {
			f, h, req, forge, _ := graphFinalizationFixture(t)
			f.opts.MCP.Paths = f.paths
			f.opts.ConflictContentMaxBytes = 65536
			additions := variant == "union" || variant == "union no advisor" || variant == "budget" || variant == "mixed"
			seed := "package fixture\nimport \"testing\"\n"
			taskAddition := "// " + strings.Repeat("task ", 1800) + "\nfunc TestTask(t *testing.T) {\n t.Log(\"task\")\n}\n"
			baseAddition := "// " + strings.Repeat("base ", 1800) + "\nfunc TestBase(t *testing.T) {\n t.Log(\"base\")\n}\n"
			if additions {
				commitInSyncDir(t, f.repo, f.repo.Dir, "go.mod", "module fixture\ngo 1.26\n")
				commitInSyncDir(t, f.repo, f.repo.Dir, "append_test.go", seed)
				f.repo.Git("push", "origin", gittest.DefaultBranch)
			}
			if !additions || variant == "mixed" {
				commitInSyncDir(t, f.repo, f.repo.Dir, "README.md", "# fixture\nold change\n")
				f.repo.Git("push", "origin", gittest.DefaultBranch)
			}
			ctx := t.Context()
			branch := "lf/786-recovery"
			_, err := f.db.FreezeSessionSlug(ctx, f.session.ID, "recovery", branch)
			mustProgress(t, err)
			path := filepath.Join(f.storeRepo.WorktreeRoot, "recovery")
			mustProgress(t, f.db.SetSessionWorktree(ctx, f.session.ID, path))
			clone, err := f.opts.Git.Open(ctx, f.repo.Dir)
			mustProgress(t, err)
			wt, err := git.EnsureWorktree(ctx, clone, git.EnsureWorktreeOptions{Dir: path, Branch: branch, BaseBranch: gittest.DefaultBranch})
			mustProgress(t, err)
			if additions {
				commitInSyncDir(t, f.repo, path, "append_test.go", seed+taskAddition)
				commitInSyncDir(t, f.repo, f.repo.Dir, "append_test.go", seed+baseAddition)
				f.repo.Git("push", "origin", gittest.DefaultBranch)
			}
			if !additions || variant == "mixed" {
				conflictBaseAndBranch(t, f.repo, path)
			}
			if variant == "budget" {
				f.opts.ConflictContentMaxBytes = 16384
			}
			// Saving must retain unfinished work before scratch synchronization.
			mustProgress(t, os.WriteFile(filepath.Join(path, "retained.txt"), []byte("unfinished repair\n"), 0600))
			locked, err := f.db.AcquireSessionLock(ctx, f.session.ID, f.opts.InstanceID)
			mustProgress(t, err)
			if !locked {
				t.Fatal("missing writer exclusion")
			}
			gate := newTestGate()
			f.opts.CloneGate = gate
			advisor := &stubConflictRunner{stubRunner: newStubRunner(nil), propose: func(ctx context.Context, request agent.ConflictResolutionRequest) (agent.ConflictResolution, error) {
				gate.mu.Lock()
				held := gate.held[f.repo.Dir]
				gate.mu.Unlock()
				if held != 0 {
					t.Fatal("advisor called while holding clone gate")
				}
				if !strings.Contains(request.Guidance, "/approve-bug analysis 1") {
					t.Fatal("retained guidance missing")
				}
				if variant == "refused" || variant == "mixed" {
					return agent.ConflictResolution{}, errors.New("cannot choose behavior")
				}
				if variant == "head drift" {
					commitInSyncDir(t, f.repo, path, "rival.txt", "intervening work\n")
					f.repo.GitIn(path, "push", "--quiet", "origin", branch)
				}
				suffix := ""
				if variant == "invalid" {
					suffix = "-invalid"
				}
				return resolvingAdvisor("resolved change\n", suffix).ProposeConflictResolution(ctx, request)
			}}
			f.opts.Runners = runnerSet(advisor)
			if variant == "union no advisor" {
				f.opts.Runners = runnerSet(newStubRunner(nil))
			}
			runtime := &BugRuntime{opts: f.opts}
			h.Resolve = func(_ context.Context, _ Options, _ ActivityRequest, eligible GraphBugRepairEligibility) (GraphDeliveryBinding, error) {
				b := forge.binding
				b.Branch, b.Target, b.Eligibility = branch, gittest.DefaultBranch, eligible
				return b, nil
			}
			h.Save, h.PublishSaved, h.SyncBase = runtime.saveGraphDelivery, runtime.pushGraphDelivery, runtime.syncGraphDelivery
			out, err := h.Execute(ctx, f.opts, req)
			mustProgress(t, err)
			wantCalls := 1
			if variant == "union" || variant == "union no advisor" || variant == "budget" {
				wantCalls = 0
			}
			if forge.merges != 0 || forge.closes != 0 || advisor.proposeCallCount() != wantCalls {
				t.Fatalf("merges=%d closes=%d advisor=%d out=%+v", forge.merges, forge.closes, advisor.proposeCallCount(), out)
			}
			if f.repo.GitIn(f.repo.Origin, "show", branch+":retained.txt") != "unfinished repair" {
				t.Fatal("retained work lost")
			}
			if variant == "union" || variant == "union no advisor" {
				if out.Outcome != "revalidate" {
					t.Fatalf("union did not require fresh checks: %+v", out)
				}
				merged := f.repo.GitIn(f.repo.Origin, "show", branch+":append_test.go")
				if merged != strings.TrimSuffix(seed+taskAddition+baseAddition, "\n") {
					t.Fatal("union bytes differ")
				}
				fixtureDir := t.TempDir()
				mustProgress(t, os.WriteFile(filepath.Join(fixtureDir, "go.mod"), []byte("module fixture\ngo 1.26\n"), 0600))
				mustProgress(t, os.WriteFile(filepath.Join(fixtureDir, "append_test.go"), []byte(merged), 0600))
				cmd := exec.CommandContext(ctx, "go", "test", "./...")
				cmd.Dir = fixtureDir
				if output, e := cmd.CombinedOutput(); e != nil {
					t.Fatalf("merged fixture tests: %v\n%s", e, output)
				}
				return
			}
			if variant == "budget" {
				if out.Outcome != "blocked" {
					t.Fatalf("budget accepted: %+v", out)
				}
				for _, want := range []string{"settings.conflict_content_max_bytes", "16384", "requires", "at least"} {
					if !strings.Contains(out.Diagnostic, want) {
						t.Fatalf("missing %q: %+v", want, out)
					}
				}
				return
			}
			if variant == "resolved" || variant == "head drift" {
				if out.Outcome != "revalidate" {
					t.Fatalf("out=%+v", out)
				}
				if variant == "resolved" {
					remote := f.repo.GitIn(f.repo.Origin, "rev-parse", branch)
					base := f.repo.Git("rev-parse", "origin/"+gittest.DefaultBranch)
					f.repo.Git("merge-base", "--is-ancestor", base, remote)
				} else if f.repo.GitIn(f.repo.Origin, "show", branch+":rival.txt") != "intervening work" {
					t.Fatal("lost concurrent head")
				}
				return
			}
			if out.Outcome != "blocked" || !strings.Contains(out.Diagnostic, "README.md") {
				t.Fatalf("conflict not actionable: %+v", out)
			}
			local, err := wt.Worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
			mustProgress(t, err)
			if remote := f.repo.GitIn(f.repo.Origin, "rev-parse", branch); remote != local {
				t.Fatal("refusal changed branch")
			}
			// The standard graph stop route retains one question and recovers it
			// without another publication; the reply resumes this same node.
			specs, err := SimplifiedBugSpecs()
			mustProgress(t, err)
			definition, err := teamsource.CaptureBugParent(ctx, specs)
			mustProgress(t, err)
			questionForge := newFakeForge(t)
			f.opts.ResearchReporter = newReporter(t, f, questionForge, nil)
			_, err = parkGraphAction(ctx, f.opts, GraphNodeRequest{Profile: definition.Profile, InstanceID: req.InstanceID, At: f.opts.MCP.Now()}, GraphNodeResult{InstanceID: req.InstanceID, AttemptID: req.NodeAttemptID, Node: "finalization", NextNode: "finalization", Activity: out}, "blocked", true)
			mustProgress(t, err)
			wait, err := f.db.GetGraphOwnerWait(ctx, req.InstanceID)
			mustProgress(t, err)
			for range 2 {
				_, err = f.opts.ResearchReporter.RecoverGraphStopQuestion(ctx, wait.DeliveryKey)
				mustProgress(t, err)
			}
			if wait.OriginNode != "finalization" || questionForge.attemptCount() != 1 {
				t.Fatalf("wait=%+v posts=%d", wait, questionForge.attemptCount())
			}
			delivery, err := f.db.GetReportDelivery(ctx, wait.DeliveryKey)
			mustProgress(t, err)
			_, err = AdmitGraphBugOwnerReply(ctx, f.db, store.GraphOwnerReply{InstanceID: req.InstanceID, OwnerID: 7, AuthorID: 7, CommentID: delivery.CommentID + 1000, QuestionCommentID: delivery.CommentID, TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, Workflow: "bug", DeliveryKey: wait.DeliveryKey, Body: "Keep both additions within the approved remedy", At: f.opts.MCP.Now().Add(time.Minute)})
			mustProgress(t, err)
			guidance, err := f.db.GraphDiagnosisGuidance(ctx, req.InstanceID)
			mustProgress(t, err)
			if len(guidance) < 2 || guidance[len(guidance)-1].ReplyBody != "Keep both additions within the approved remedy" {
				t.Fatalf("reply not retained: %+v", guidance)
			}
		})
	}
}
