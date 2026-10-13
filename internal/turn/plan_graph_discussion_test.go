package turn

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

func discussionRunner(t *testing.T, f *fixture, stage, question string) *int {
	t.Helper()
	calls := 0
	f.opts.Runners = runnerSet(lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		if req.Graph.Node != stage+"_agent" || !strings.Contains(req.Prompt, question) || !strings.Contains(req.Prompt, "Retained Plan state") || !strings.Contains(req.Prompt, "story-001") {
			t.Fatalf("wrong discussion context: node=%s prompt=%s", req.Graph.Node, req.Prompt)
		}
		result := integrationDone(nil, "This plan delivers bounded story shaping.")
		raw, err := json.Marshal(ar.ResearchNode{Version: 1, Response: "done", Result: result})
		mustProgress(t, err)
		submitLiveResult(t, req, string(raw))
		return agentDone(t, result), nil
	}))
	return &calls
}

func TestPlanDiscussion_FinalParentPreservesExactApproval(t *testing.T) {
	f, base, forge, before := completeSingleStory(t)
	question := "Explain what this story actually delivers"
	calls := discussionRunner(t, f, "set", question)
	req := nextTrigger(base, 9901, question)
	executeGraphPlan(t, f, req)
	if *calls != 1 {
		t.Fatalf("calls=%d", *calls)
	}
	if after := graphPlanState(t, f); !reflect.DeepEqual(before, after) {
		t.Fatalf("answer changed Plan state: before=%+v after=%+v", before, after)
	}
	if posts := parentPostsWith(f, forge, "This plan delivers bounded story shaping."); len(posts) != 1 {
		t.Fatalf("answers=%v", posts)
	}
	executeGraphPlan(t, f, req)
	if *calls != 1 || len(parentPostsWith(f, forge, "This plan delivers bounded story shaping.")) != 1 {
		t.Fatal("replay duplicated discussion")
	}
	executeGraphPlan(t, f, nextTrigger(base, 9902, "/approve-plan-set "+before.AssembledWait))
	feature, err := f.db.GetManagedFeature(t.Context(), f.ref().String())
	mustProgress(t, err)
	if !feature.Ready {
		t.Fatal("original approval no longer works")
	}
	// A question about the saved Plan must preserve readiness too.
	executeGraphPlan(t, f, nextTrigger(base, 9903, question))
	feature, err = f.db.GetManagedFeature(t.Context(), f.ref().String())
	mustProgress(t, err)
	if !feature.Ready {
		t.Fatal("saved Plan question cleared readiness")
	}
}

func TestPlanDiscussion_StoryAnswersPreserveStageAndWait(t *testing.T) {
	for _, approvals := range []int{0, 1, 2, 3} {
		t.Run(string(rune('0'+approvals)), func(t *testing.T) {
			f, _, base, _, forge := graphPlanFixtureWithForge(t, "agent")
			executeGraphPlan(t, f, base)
			for i := 0; i < approvals; i++ {
				executeGraphPlan(t, f, graphPlanApprove(t, f, base, base.Trigger.CommentID+int64(i+1)))
			}
			binding, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-001")
			mustProgress(t, err)
			waits := []string{"review", "question", "none"}
			if approvals == 3 {
				waits = []string{"none"}
			}
			for i, kind := range waits {
				mustProgress(t, f.db.UpdatePlanWorkflowState(t.Context(), f.ref().String(), func(s *planbrief.PlanState) (bool, error) {
					if kind == "none" {
						s.Stories[0].Wait = nil
						return true, nil
					}
					return true, s.OpenPlanWait("story-001", "discussion-"+kind, kind, binding.IssueID)
				}))
				before := graphPlanState(t, f)
				question := "Explain the current stage"
				calls := discussionRunner(t, f, before.Stories[0].Stage, question)
				req := asStoryTask(t, f, nextTrigger(base, 9951+int64(i), question), "story-001")
				executeGraphPlan(t, f, req)
				if *calls != 1 || !reflect.DeepEqual(before, graphPlanState(t, f)) {
					t.Fatalf("%s answer moved stage, approvals or wait", kind)
				}
				n := 0
				for _, post := range forge.all() {
					if strings.Contains(post.body, "This plan delivers bounded story shaping.") {
						n++
						if strings.Contains(post.path, "/123/comments") {
							t.Fatal("story answer posted to parent")
						}
					}
				}
				if n != i+1 {
					t.Fatalf("answers=%d", n)
				}
			}
		})
	}
}

func TestPlanDiscussion_ParentNeverSelectsOwedStory(t *testing.T) {
	for _, state := range []planbrief.PlanState{
		{SharedRevision: "initial"},
		{SharedRevision: "initial", Stories: []planbrief.PlanStoryState{{Key: "story-001", Stage: "story"}}},
	} {
		f, _, base, _, _ := graphPlanFixtureWithForge(t, "agent")
		_, rev, err := f.db.GetPlanWorkflowState(t.Context(), f.ref().String())
		mustProgress(t, err)
		_, err = f.db.SavePlanWorkflowState(t.Context(), f.ref().String(), rev, state)
		mustProgress(t, err)
		r := &planGraphRuntime{opts: f.opts, req: nextTrigger(base, 9900, "What next?"), parentRef: f.ref(), discussion: true}
		mustProgress(t, r.selectStory(t.Context()))
		if r.stage != "set" || r.selected != "" || r.creating {
			t.Fatalf("discussion selected unrelated work: %+v", r)
		}
	}
}

func TestPlanDiscussion_ChangesReopenCurrentStoryReview(t *testing.T) {
	for _, approvals := range []int{0, 1, 2, 3} {
		t.Run(string(rune('0'+approvals)), func(t *testing.T) {
			changing := false
			f, _, base, _, _ := graphPlanFixtureWithForge(t, "agent", func(a *graphPlanAnswer) {
				if !changing {
					return
				}
				switch a.Stage {
				case "story":
					a.Request.Draft.Problem = "Clarified scope"
					a.Request.ChangedSections = []string{"intent"}
				case "technical":
					a.Request.Draft.Architecture.General[0].Decision = "Refined role boundary"
				case "acceptance":
					a.Request.Draft.Criteria[0].Text = "Revised acceptance requirement"
				}
			})
			executeGraphPlan(t, f, base)
			for i := 0; i < approvals; i++ {
				executeGraphPlan(t, f, graphPlanApprove(t, f, base, base.Trigger.CommentID+int64(i+1)))
			}
			mustProgress(t, f.db.UpdatePlanWorkflowState(t.Context(), f.ref().String(), func(s *planbrief.PlanState) (bool, error) {
				sibling := s.Stories[0]
				sibling.Key, sibling.Draft.Key = "story-002", "story-002"
				s.Stories = append(s.Stories, sibling)
				return true, nil
			}))
			before := graphPlanState(t, f)
			changing = true
			executeGraphPlan(t, f, asStoryTask(t, f, nextTrigger(base, 9970, "Please revise this stage"), "story-001"))
			after := graphPlanState(t, f)
			if !reflect.DeepEqual(before.Stories[1], after.Stories[1]) {
				t.Fatal("discussion revised an unrelated story")
			}
			item := after.Stories[0]
			if item.Candidate == before.Stories[0].Candidate || item.Wait == nil || item.Wait.Kind != "review" || item.Wait.Stage != before.Stories[0].Stage {
				t.Fatalf("change did not open current review: %+v", item)
			}
			ref, err := after.ReviewRevision(item.Key, item.Stage)
			mustProgress(t, err)
			if item.Approvals[item.Stage] == ref {
				t.Fatal("changed content retained approval")
			}
		})
	}
}

func TestPlanDiscussion_LostPublicationRecoversWithoutRepeatingAgent(t *testing.T) {
	f, base, forge, before := completeSingleStory(t)
	question := "Explain the current plan"
	calls := discussionRunner(t, f, "set", question)
	req := nextTrigger(base, 9990, question)
	forge.failWith(503)
	out, err := Execute(t.Context(), f.opts, req)
	if err == nil && out.Err == nil {
		t.Fatal("failed publication completed")
	}
	pending, err := f.db.ListPendingReportDeliveries(t.Context())
	mustProgress(t, err)
	var held store.ReportDelivery
	for _, d := range pending {
		if strings.Contains(d.Body, "This plan delivers bounded story shaping.") {
			held = d
		}
	}
	if held.Key == "" || held.Completed {
		t.Fatal("answer publication not retained")
	}
	if !reflect.DeepEqual(before, graphPlanState(t, f)) {
		t.Fatal("failed answer changed approvals")
	}
	// Reconcile an uncertain response against the original authenticated remote comment.
	forge.mu.Lock()
	forge.posts = append(forge.posts, forgePost{path: "/repos/acme/acme-api/issues/123/comments", body: held.Body, id: 6001})
	forge.mu.Unlock()
	forge.recover()
	var replayed Result
	var replayErr error
	if out.Repair != nil {
		replayed, replayErr = out.Repair(t.Context())
	} else {
		replayed, replayErr = Execute(t.Context(), f.opts, req)
	}
	if replayErr != nil || replayed.Err != nil {
		t.Fatalf("replay failed: %v / %v", replayErr, replayed.Err)
	}
	delivery, err := f.db.GetReportDelivery(t.Context(), held.Key)
	mustProgress(t, err)
	if !delivery.Completed || *calls != 1 || len(parentPostsWith(f, forge, "This plan delivers bounded story shaping.")) != 1 {
		t.Fatalf("recovery duplicated or lost answer: delivery=%+v calls=%d", delivery, *calls)
	}
}

func TestPlanDiscussion_FailedStoryReplyRecoversBeforeFreshDraft(t *testing.T) {
	for _, body := range []string{"Retry", "Clarify the scope for the owner"} {
		t.Run(body, func(t *testing.T) {
			changing := false
			f, remote, base, calls, forge := graphPlanFixtureWithForge(t, "agent", func(a *graphPlanAnswer) {
				if changing {
					a.Request.Draft.Problem = body
					a.Request.ChangedSections = []string{"intent"}
				}
			})
			remote.failAttach = true
			out, err := Execute(t.Context(), f.opts, base)
			if err == nil || out.Run.Status != store.RunFailed {
				t.Fatalf("expected failed first publication: %+v %v", out, err)
			}
			requireFailureComment(t, forge, "attach")
			mustProgress(t, f.db.UpdatePlanWorkflowState(t.Context(), f.ref().String(), func(s *planbrief.PlanState) (bool, error) {
				s.Stories = append(s.Stories, planbrief.PlanStoryState{Key: "story-002", Stage: "story", Hold: "independent sibling hold"})
				return true, nil
			}))
			before := graphPlanState(t, f)
			if before.Stories[0].Wait != nil || !before.Stories[0].Blocked() {
				t.Fatalf("failure did not retain non-waiting debt: %+v", before)
			}
			oldRequest := before.Stories[0].EffectRequest
			changing = true
			req := asStoryTask(t, f, nextTrigger(base, 9980, body), "story-001")
			binding, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-001")
			mustProgress(t, err)
			req.Trigger.PlanReply = &planbrief.PlanOwnerReply{ID: req.Trigger.CommentID, OwnerID: 7, ActorID: 7, IssueID: binding.IssueID, Key: "story-001", Body: body}
			executeGraphPlan(t, f, req)
			after := graphPlanState(t, f)
			item := after.Stories[0]
			if !reflect.DeepEqual(before.Stories[1], after.Stories[1]) {
				t.Fatal("retry changed sibling state")
			}
			if *calls != 2 || item.Blocked() || item.Wait == nil || item.Wait.Kind != "review" || item.Draft.Problem != body || len(item.Approvals) != 0 || item.Candidate == before.Stories[0].Candidate || item.EffectRequest == oldRequest {
				t.Fatalf("retry did not produce a fresh unapproved review: calls=%d state=%+v", *calls, after)
			}
			complete, err := f.db.PlanStoryCompleted(t.Context(), f.ref().String(), oldRequest)
			mustProgress(t, err)
			if !complete || remote.creates != 1 {
				t.Fatalf("original request not reconciled safely: complete=%v creates=%d", complete, remote.creates)
			}
			posts := len(forge.all())
			executeGraphPlan(t, f, req)
			if *calls != 2 || len(forge.all()) != posts {
				t.Fatal("handled reply duplicated the draft or review")
			}
		})
	}
}

func TestPlanDiscussion_FailedStoryUnknownOutcomePreservesOriginalRequest(t *testing.T) {
	f, remote, base, calls, forge := graphPlanFixtureWithForge(t, "agent")
	executeGraphPlan(t, f, base)
	remote.losePatch = true
	out, err := Execute(t.Context(), f.opts, graphPlanApprove(t, f, base, base.Trigger.CommentID+1))
	if err == nil || out.Run.Status != store.RunFailed {
		t.Fatalf("expected failed update: %+v %v", out, err)
	}
	requireFailureComment(t, forge, "lost update")
	before := graphPlanState(t, f)
	request := before.Stories[0].EffectRequest
	original, err := f.db.GetPlanStoryCandidate(t.Context(), f.ref().String(), request)
	mustProgress(t, err)
	remote.mu.Lock()
	remote.children[400]["body"] = "Changed remotely; the original outcome cannot be established."
	patches := remote.patches
	remote.mu.Unlock()
	req := asStoryTask(t, f, nextTrigger(base, 9981, "Retry"), "story-001")
	_, err = Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	after := graphPlanState(t, f)
	retained, err := f.db.GetPlanStoryCandidate(t.Context(), f.ref().String(), request)
	mustProgress(t, err)
	if *calls != 2 || remote.patches != patches || !after.Stories[0].Blocked() || !reflect.DeepEqual(original, retained) || after.Stories[0].Wait != nil || after.Stories[0].Approvals["technical"] != "" {
		t.Fatalf("unknown outcome replaced or approved: calls=%d state=%+v", *calls, after)
	}
}

func TestPlanDiscussion_FailedStoryReplySourceMismatchCannotRecover(t *testing.T) {
	f, remote, base, calls, _ := graphPlanFixtureWithForge(t, "agent")
	remote.failAttach = true
	_, err := Execute(t.Context(), f.opts, base)
	if err == nil {
		t.Fatal("expected publication failure")
	}
	before := graphPlanState(t, f)
	req := asStoryTask(t, f, nextTrigger(base, 9982, "Retry"), "story-001")
	req.Trigger.PlanReply = &planbrief.PlanOwnerReply{ID: req.Trigger.CommentID, OwnerID: 7, ActorID: 7, IssueID: 999, Key: "story-001", Body: "Retry"}
	_, err = Execute(t.Context(), f.opts, req)
	if err == nil || !strings.Contains(err.Error(), "source mismatch") || *calls != 1 || !reflect.DeepEqual(before, graphPlanState(t, f)) {
		t.Fatalf("source mismatch admitted recovery: calls=%d err=%v", *calls, err)
	}
}
