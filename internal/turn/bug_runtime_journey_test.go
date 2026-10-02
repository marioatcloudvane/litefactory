package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

type bugRuntimeJourneyForge struct{ *github.Client }

func (f bugRuntimeJourneyForge) GetIssue(context.Context, github.Repo, int) (github.Issue, error) {
	return github.Issue{ID: 10123, Number: testIssue, State: "open", APIURL: f.BaseURL() + "repos/acme/acme-api/issues/123"}, nil
}

// This composed fixture uses a real git repository, SQLite and the actual private
// result server. The provider is controlled; it is not host sandbox proof.
func TestBugRuntimeJourneyFirstAnalystAndRetainedStop(t *testing.T) {
	var session int64
	calls := 0
	runner := lifecycleMCPHost(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		if req.Workflow != agent.WorkflowBug || req.Mode != "" || req.Graph == nil || req.Delivery == nil || req.Delivery.MainRole != "bug-analyst" {
			t.Fatalf("wrong concrete Bug invocation: %+v", req)
		}
		if e := req.ValidateBugAnalyst(); e != nil {
			t.Fatal(e)
		}
		assertBugInspectionCatalog(t, req)
		raw, _ := json.Marshal(map[string]any{"version": 1, "response": "blocked", "result": map[string]any{"outcome": "blocked", "reason": "Controlled fixture lacks reproduction evidence", "comment": "Investigation retained; no repair is authorized.", "payload": map[string]any{"bug": map[string]any{"response": "blocked", "invocation": fmt.Sprintf("analysis:%d:%d", session, req.Graph.AttemptID), "base_revision": 0, "evidence": []string{"controlled owner report"}}}}})
		submitLiveResult(t, req, string(raw))
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: string(raw), FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledResearchFixture(t, runner)
	session = f.session.ID
	f.opts.Tiers.ModelByTier[config.Kind("claude")][config.Tier("flagship")] = "fixture-model"
	req.Workflow = agent.WorkflowBug
	req.Mode = ""
	req.Command = ParseCommand("Investigate this bug")
	req.Trigger.Body = "Investigate this bug"
	req.ToolProfile = "bug-analyst"
	_, e := f.db.AdmitWorkflow(t.Context(), store.TaskWorkflowAdmission{SessionID: session, Version: 1, Family: "standalone", Workflow: "bug"})
	if e != nil {
		t.Fatal(e)
	}
	withoutGate := f.opts
	withoutGate.CloneGate = nil
	if _, err := NewBugRuntime(t.Context(), withoutGate, BugRuntimeConfig{Forge: bugRuntimeJourneyForge{forge.client}, OwnerID: 42, Permissions: func(agent.Kind, string) (BugRolePermissions, error) {
		return BugRolePermissions{}, nil
	}}); err == nil || !strings.Contains(err.Error(), "clone gate") {
		t.Fatalf("missing clone gate admitted: %v", err)
	}
	runtime, e := NewBugRuntime(t.Context(), f.opts, BugRuntimeConfig{Forge: bugRuntimeJourneyForge{forge.client}, OwnerID: 42, Permissions: func(_ agent.Kind, role string) (BugRolePermissions, error) {
		return BugRolePermissions{Tools: []string{"Read", "Glob", "Grep"}, AllowedTools: []string{"Read", "Glob", "Grep"}}, nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	f.opts.Bug = runtime
	out, e := Execute(t.Context(), f.opts, req)
	if e != nil || calls != 1 {
		t.Fatalf("first Bug dispatch calls=%d result=%+v err=%v logs=%s", calls, out, e, f.logs.String())
	}
	life, e := f.db.GetBugLifecycle(t.Context(), session)
	if e != nil {
		t.Fatal(e)
	}
	i, e := f.db.GetGraphInstance(t.Context(), life.ParentInstance)
	if e != nil {
		t.Fatal(e)
	}
	if i.CurrentNode != "record_analysis" || i.Status != store.GraphRunning {
		t.Fatalf("missing retained record checkpoint: %+v", i)
	}
	state, _, approval, e := f.db.CurrentBugAnalysis(t.Context(), session)
	if e != nil || state.AnalysisUsed != 1 || approval != nil {
		t.Fatalf("budget/approval %+v %+v %v", state, approval, e)
	}
}
