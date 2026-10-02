package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestBugRuntimeOwnerApprovalAndReplay(t *testing.T) {
	t.Run("approval", func(t *testing.T) { bugRuntimeOwnerReply(t, false, false, false) })
	t.Run("answers", func(t *testing.T) { bugRuntimeOwnerReply(t, true, false, false) })
	t.Run("review-change-crash", func(t *testing.T) { bugRuntimeOwnerReply(t, false, true, false) })
	// Definition rows are shared by graph-file digest, and only the graph is
	// pinned: a task continues after a deploy that changed Bug role files,
	// whether it started before (#401) or after (#408) that deploy.
	t.Run("approval-after-role-only-change", func(t *testing.T) { bugRuntimeOwnerReply(t, false, false, true) })
}
func bugRuntimeOwnerReply(t *testing.T, questions, change, sharedPin bool) {
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
		raw, _ := json.Marshal(map[string]any{"version": 1, "response": "done", "result": map[string]any{"outcome": "completed", "comment": "Review the recorded analysis.", "payload": map[string]any{"bug": map[string]any{"response": "done", "analysis": map[string]any{"expected": "one result", "actual": "duplicate results", "reproduction": map[string]any{"steps": []string{"search once"}, "environment": "fixture", "observation": "owner_reported"}, "cause": "duplicate append", "evidence": []string{"fixture report"}, "remedy": "remove duplicate append", "exclusions": []string{"ranking"}, "criteria": []map[string]any{{"id": "C1", "requirement": "one result", "measurement": "assert result length equals one"}}}, "invocation": fmt.Sprintf("analysis:%d:%d", session, req.Graph.AttemptID), "base_revision": 0, "evidence": []string{"controlled owner report"}}}}})
		if questions {
			var value map[string]any
			if err := json.Unmarshal(raw, &value); err != nil {
				t.Fatal(err)
			}
			value["response"] = "ask"
			result, ok := value["result"].(map[string]any)
			if !ok {
				t.Fatal("fixture result is not an object")
			}
			result["outcome"] = "incomplete"
			result["reason"] = "Need fixture environment"
			result["questions"] = []map[string]any{{"id": "Q1", "audience": "technical", "text": "Which environment?"}}
			outer, ok := result["payload"].(map[string]any)
			if !ok {
				t.Fatal("fixture payload is not an object")
			}
			payload, ok := outer["bug"].(map[string]any)
			if !ok {
				t.Fatal("fixture bug is not an object")
			}
			payload["response"] = "ask"
			analysis, ok := payload["analysis"].(map[string]any)
			if !ok {
				t.Fatal("fixture analysis is not an object")
			}
			analysis["questions"] = []map[string]any{{"id": "Q1", "text": "Which environment?", "material": true}}
			raw, _ = json.Marshal(value)
		}
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
	runtime, e := NewBugRuntime(t.Context(), f.opts, BugRuntimeConfig{Forge: bugRuntimeJourneyForge{forge.client}, OwnerID: 42, Permissions: func(_ agent.Kind, role string) (BugRolePermissions, error) {
		return BugRolePermissions{Tools: []string{"Read", "Glob", "Grep"}, AllowedTools: []string{"Read", "Glob", "Grep"}}, nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	f.opts.Bug = runtime
	if sharedPin {
		for _, d := range []struct {
			path   string
			digest string
		}{{"bug/bug.dot", runtime.definition.Profile.Digest}, {"bug_tests.dot", runtime.childDefinition.Profile.Digest}} {
			if _, e = f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Workflow: "bug", Team: "builtin", ProfileVersion: 1, Path: d.path, Digest: d.digest, TeamSnapshotDigest: strings.Repeat("e", 64)}); e != nil {
				t.Fatal(e)
			}
		}
	}

	disposition := "analysis-review"
	if questions {
		disposition = "question"
	}
	for n := 0; n < 3; n++ {
		if _, e = Execute(t.Context(), f.opts, req); e != nil {
			t.Fatal(e)
		}
		life, e := f.db.GetBugLifecycle(t.Context(), session)
		if e != nil {
			t.Fatal(e)
		}
		if life.Disposition == disposition {
			break
		}
	}
	life, e := f.db.GetBugLifecycle(t.Context(), session)
	if e != nil {
		t.Fatal(e)
	}
	if life.Disposition != disposition {
		t.Fatalf("review missing: %+v", life)
	}
	wait, e := f.db.GetGraphOwnerWait(t.Context(), life.ParentInstance)
	if e != nil {
		t.Fatal(e)
	}
	published, e := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
	if e != nil {
		t.Fatal(e)
	}
	body := "The fixture uses Go on Linux."
	if !questions {
		_, review, _, e := f.db.CurrentBugAnalysisReview(t.Context(), session)
		if e != nil {
			t.Fatal(e)
		}
		body = fmt.Sprintf("/approve-bug analysis %d", review.AnalysisRevision)
	}
	// A comment written before the review was posted (a retry queued behind
	// the run it started) can never answer it: the daemon refuses and consumes
	// it rather than holding the queue behind it (#401).
	early := req
	early.Trigger = Trigger{CommentID: published.CommentID - 1, AuthorID: 42, Body: "try again"}
	if _, e = Execute(t.Context(), f.opts, early); !errors.Is(e, store.ErrGraphReplyPredatesQuestion) {
		t.Fatalf("comment older than the open review was not refused as predating it: %v", e)
	}
	req.Trigger = Trigger{CommentID: published.CommentID + 1, AuthorID: 42, Body: body}
	if change {
		req.Trigger.Body = "Keep ordering stable while removing the duplicate."
		state, review, _, e := f.db.CurrentBugAnalysisReview(t.Context(), session)
		if e != nil {
			t.Fatal(e)
		}
		// Simulate a crash after domain consumption and before parent succession.
		e = f.db.ConsumeBugAnalysisReviewChange(t.Context(), session, review.Review, store.GraphOwnerReply{InstanceID: life.ParentInstance, OwnerID: 42, AuthorID: 42, CommentID: req.Trigger.CommentID, QuestionCommentID: published.CommentID, TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, DeliveryKey: wait.DeliveryKey, Body: req.Trigger.Body, At: f.opts.MCP.Now()}, state.Revision)
		if e != nil {
			t.Fatal(e)
		}
	}
	next, e := runtime.continueOwner(t.Context(), req, life)
	if e != nil {
		t.Fatal(e)
	}
	state, _, approval, e := f.db.CurrentBugAnalysis(t.Context(), session)
	if e != nil || state.AnalysisUsed != 1 || (!questions && !change && approval == nil) || ((questions || change) && approval != nil) || calls != 1 {
		t.Fatalf("approval spent pass or absent: %+v %+v calls=%d err=%v", state, approval, calls, e)
	}
	if questions {
		answers, e := f.db.BugAnalysisAnswers(t.Context(), session, "", 100)
		if e != nil || len(answers) != 1 || answers[0].Body != body {
			t.Fatalf("answers lost: %+v %v", answers, e)
		}
	}
	current, e := f.db.GetBugLifecycle(t.Context(), session)
	if e != nil {
		t.Fatal(e)
	}
	replay, e := runtime.continueOwner(t.Context(), req, current)
	if e != nil || replay.ID != next.ID {
		t.Fatalf("replay duplicated: %+v %v", replay, e)
	}
	req.Trigger.Body += " and change the remedy"
	if _, e = runtime.continueOwner(t.Context(), req, current); e == nil {
		t.Fatal("changed reply replay accepted")
	}
}
