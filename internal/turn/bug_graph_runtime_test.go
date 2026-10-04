package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
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
