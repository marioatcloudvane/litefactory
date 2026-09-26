package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/buganalysis"
	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/bugrepair"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

type bugFixerJourneyForge struct {
	bugRuntimeJourneyForge
	target string
}

func (f bugFixerJourneyForge) GetDeliveryHead(context.Context, github.Repo, string) (string, error) {
	return f.target, nil
}

func TestBugRuntimeFixerAuthoredRepairAndImmutableHandoff(t *testing.T) {
	var f *fixture
	calls := map[string]int{}
	runner := lifecycleMCPHost(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
		role := req.Delivery.MainRole
		calls[role]++
		var result agentresult.Result
		switch role {
		case "bug-analyst":
			analysis := buganalysis.BugAnalysis{Expected: "One result", Actual: "Duplicate results", Reproduction: buganalysis.BugReproduction{Steps: []string{"Call Count"}, Environment: "controlled fixture", Observation: "owner_reported"}, Cause: "The function returns two instead of one", Evidence: []string{"repair.go"}, Remedy: "Return one and add regression coverage", Exclusions: []string{"Do not change other functions"}, Boundaries: &buganalysis.BugRepairBoundaries{AllowedPaths: []string{"repair.go", "repair_test.go"}}, Criteria: []buganalysis.BugCriterion{{ID: "C1", Requirement: "Count returns one", Measurement: "Assert Count()==1"}}}
			result = agentresult.Result{Outcome: "completed", Comment: "Review the exact remedy.", Payload: agentresult.Payload{Bug: &agentresult.Bug{Response: "done", Invocation: fmt.Sprintf("analysis:%d:%d", f.session.ID, req.Graph.AttemptID), Analysis: &analysis}}}
		case "bug-fixer":
			repair, e := f.db.LatestBugRepair(ctx, f.session.ID)
			if e != nil {
				t.Fatal(e)
			}
			for path, content := range map[string]string{"repair.go": "package fixture\nfunc Count() int { return 1 }\n", "repair_test.go": "package fixture\nimport \"testing\"\nfunc TestCount(t *testing.T){if Count()!=1{t.Fatal(\"expected one\")}}\n"} {
				if e = os.WriteFile(filepath.Join(req.WorktreePath, path), []byte(content), 0600); e != nil {
					t.Fatal(e)
				}
			}
			result = agentresult.Result{Outcome: "completed", Comment: "Authored the scoped repair and regression; configured checks remain pending.", Payload: agentresult.Payload{Bug: &agentresult.Bug{Response: "done", Invocation: repair.Request, BaseRevision: repair.AnalysisRevision, Repair: &bugrepair.Evidence{Reservation: repair.Request, AnalysisDigest: repair.AnalysisDigest, SourceCommit: repair.SourceCommit, SourceTree: repair.SourceTree, Summary: "Return one; regression asserts exact count", Coverage: []bugrepair.Coverage{{Criterion: "C1", Changes: []string{"repair.go"}, Tests: []string{"repair_test.go"}}}}}}}
		default:
			t.Fatalf("unexpected role %s", role)
		}
		raw, e := json.Marshal(agentresult.ResearchNode{Version: 1, Response: "done", Result: result})
		if e != nil {
			t.Fatal(e)
		}
		submitLiveResult(t, req, string(raw))
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: string(raw), FinishedAt: time.Now()}, nil
	})
	var req Request
	var forge *fakeForge
	f, forge, req = assembledResearchFixture(t, runner)
	f.repo.WriteFile("repair.go", "package fixture\nfunc Count() int { return 2 }\n")
	f.repo.WriteFile(config.BugRecipePath, "version: 1\nphases:\n  regression:\n    commands:\n      - argv: [echo, fixture]\n  focused:\n    commands:\n      - argv: [echo, fixture]\n  full:\n    commands:\n      - argv: [echo, fixture]\n")
	f.repo.Commit("Controlled bug fixture")
	f.repo.Git("push", "origin", "main")
	f.opts.Tiers.ModelByTier[config.Kind("claude")][config.Tier("flagship")] = "fixture-model"
	req.Workflow = agent.WorkflowBug
	req.Mode = ""
	req.Command = ParseCommand("Investigate this bug")
	req.Trigger.Body = "Investigate this bug"
	req.ToolProfile = "bug-analyst"
	if _, e := f.db.AdmitWorkflow(t.Context(), store.TaskWorkflowAdmission{SessionID: f.session.ID, Version: 1, Family: "standalone", Workflow: "bug"}); e != nil {
		t.Fatal(e)
	}
	runtime, e := NewBugRuntime(t.Context(), f.opts, BugRuntimeConfig{Forge: bugFixerJourneyForge{bugRuntimeJourneyForge{forge.client}, f.repo.Head()}, OwnerID: 42, Isolation: bugchecks.Isolation{Binary: filepath.Join(t.TempDir(), "unavailable-isolator"), PATH: "/usr/bin:/bin", Revision: "fixture-unavailable"}, Permissions: func(_ agent.Kind, role string) (BugRolePermissions, error) {
		tools := []string{"Read", "Glob", "Grep"}
		if role == "bug-fixer" {
			tools = append(tools, "Edit", "Write")
		}
		return BugRolePermissions{Tools: tools, AllowedTools: tools}, nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	f.opts.Bug = runtime
	for range 3 {
		if _, e = Execute(t.Context(), f.opts, req); e != nil {
			t.Fatal(e)
		}
		life, e := f.db.GetBugLifecycle(t.Context(), f.session.ID)
		if e != nil {
			t.Fatal(e)
		}
		if life.Disposition == "analysis-review" {
			break
		}
	}
	life, e := f.db.GetBugLifecycle(t.Context(), f.session.ID)
	if e != nil {
		t.Fatal(e)
	}
	wait, e := f.db.GetGraphOwnerWait(t.Context(), life.ParentInstance)
	if e != nil {
		t.Fatal(e)
	}
	published, e := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
	if e != nil {
		t.Fatal(e)
	}
	req.Trigger = Trigger{CommentID: published.CommentID + 1, AuthorID: 42, Body: "/approve-bug analysis 1"}
	req.Command = Command{Kind: KindFreeText, Text: req.Trigger.Body}
	if _, e = runtime.continueOwner(t.Context(), req, life); e != nil {
		t.Fatal(e)
	}
	if _, e = Execute(t.Context(), f.opts, req); e != nil {
		t.Fatalf("approved fixer dispatch: %v logs=%s", e, f.logs.String())
	}
	if calls["bug-fixer"] != 1 || calls["bug-analyst"] != 1 {
		t.Fatalf("unexpected substantive calls %+v", calls)
	}
	life, e = f.db.GetBugLifecycle(t.Context(), f.session.ID)
	if e != nil {
		t.Fatal(e)
	}
	instance, e := f.db.GetGraphInstance(t.Context(), life.ParentInstance)
	if e != nil {
		t.Fatal(e)
	}
	if instance.CurrentNode != "record_repair" {
		t.Fatalf("fixer did not park before handoff: %+v", instance)
	}
	// Execute the concrete handoff callback at the saved checkpoint. Test child
	// execution follows below; this handoff proves authored source,
	// exact approval, actual git capture and immutable save, not passing checks.
	policy := runtime.parentPolicy(&researchWorkflow{req: req})
	handed, e := policy.RepairRecord(t.Context(), f.opts, ActivityRequest{InstanceID: instance.ID, Node: "record_repair"})
	if e != nil || handed.Outcome != "done" {
		t.Fatalf("immutable handoff %+v %v", handed, e)
	}
	handedAgain, e := policy.RepairRecord(t.Context(), f.opts, ActivityRequest{InstanceID: instance.ID, Node: "record_repair"})
	if e != nil || handedAgain.Outcome != "done" || calls["bug-fixer"] != 1 {
		t.Fatalf("handoff recovery repeated work %+v %v calls=%v", handedAgain, e, calls)
	}
	// Continue through the actual separately persisted test graph. Deliberately
	// unavailable isolation must yield a retained block, never invented check pass.
	if _, e = Execute(t.Context(), f.opts, req); e != nil {
		t.Fatalf("child admission: %v", e)
	}
	child, e := f.db.LatestBugTestCall(t.Context(), f.session.ID)
	if e != nil {
		t.Fatal(e)
	}
	if child.ChildInstance == instance.ID || child.Outcome != "pending" {
		t.Fatalf("not a separate pending child %+v", child)
	}
	due, e := f.db.ListDueGraphDispatches(t.Context(), time.Now().Add(time.Minute), 32)
	if e != nil {
		t.Fatal(e)
	}
	advanced := false
	for _, d := range due {
		if d.InstanceID == child.ChildInstance {
			reserved, ok, e := f.db.ReserveGraphDispatch(t.Context(), d.ID, time.Now())
			if e != nil || !ok {
				t.Fatalf("reserve child %v %v", ok, e)
			}
			if e = runtime.Child.Advance(t.Context(), reserved, time.Now()); e != nil {
				t.Fatal(e)
			}
			advanced = true
			break
		}
	}
	if !advanced {
		t.Fatal("no child dispatch retained")
	}
	child, e = f.db.GetBugTestCall(t.Context(), child.ID)
	if e != nil || child.Outcome != "blocked" {
		t.Fatalf("unavailable isolation child outcome %+v %v", child, e)
	}
	if _, e = Execute(t.Context(), f.opts, req); e != nil {
		t.Fatalf("consume blocked child %v", e)
	}
	life, e = f.db.GetBugLifecycle(t.Context(), f.session.ID)
	if e != nil || life.Disposition != "blocked" || calls["bug-fixer"] != 1 {
		t.Fatalf("blocked child advanced repair %+v %v calls=%v", life, e, calls)
	}

}
