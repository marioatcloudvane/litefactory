package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	var refusal *PlanReplyRefusal
	if !errors.As(err, &refusal) || !strings.Contains(refusal.Reason, "story identity or provenance changed") {
		t.Fatalf("missing retained refusal: %v", err)
	}
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

func TestPlanRetryUpstreamIneligibleRefusal(t *testing.T) {
	f, remote, base, calls, _ := graphPlanFixtureWithForge(t, "agent")
	executeGraphPlan(t, f, base)
	// Keep a failed reply eligible for recovery while its prerequisite has lost
	// approval; recovery must not bypass that prerequisite or mutate the sibling.
	mustProgress(t, f.db.UpdatePlanWorkflowState(t.Context(), f.ref().String(), func(s *planbrief.PlanState) (bool, error) {
		s.Stories[0].Wait = nil
		s.Stories[0].Draft.Dependencies.StoryKeys = []string{"upstream"}
		s.Stories = append(s.Stories, planbrief.PlanStoryState{Key: "upstream", Stage: "story", Candidate: "upstream-candidate", Draft: planbrief.PlanDraft{Key: "upstream"}})
		return true, nil
	}))
	before := graphPlanState(t, f)
	r := &planGraphRuntime{opts: f.opts, parentRef: f.ref(), story: "story-001"}
	mustProgress(t, r.selectStory(t.Context()))
	if r.stage != "" {
		t.Fatal("upstream-ineligible work selected")
	}
	var refusal *PlanReplyRefusal
	if err := r.unappliedReply(t.Context()); !errors.As(err, &refusal) || !strings.Contains(refusal.Reason, "upstream") {
		t.Fatalf("missing eligibility reason: %v", err)
	}
	if !reflect.DeepEqual(before, graphPlanState(t, f)) || *calls != 1 || remote.patches != 0 {
		t.Fatal("refusal changed state or launched work")
	}
}

func TestPlanDiscussion_RetryPreservesApprovedEarlierStages(t *testing.T) {
	for _, stage := range []string{"technical", "acceptance"} {
		for _, failure := range []string{"readback", "before effect"} {
			t.Run(stage+"/"+failure, func(t *testing.T) {
				failing := false
				var drafted []string
				f, remote, base, calls, forge := graphPlanFixtureWithForge(t, "agent", func(a *graphPlanAnswer) {
					drafted = append(drafted, a.Stage)
					if failing && failure == "before effect" {
						a.Stop, a.Reason = agent.OutcomeFailed, "fixture failed before publication"
					}
				})
				executeGraphPlan(t, f, base)
				if stage == "acceptance" {
					executeGraphPlan(t, f, graphPlanApprove(t, f, base, base.Trigger.CommentID+1))
				}
				failing = true
				remote.losePatch = failure == "readback"
				req := asStoryTask(t, f, graphPlanApprove(t, f, base, base.Trigger.CommentID+2), "story-001")
				out, err := Execute(t.Context(), f.opts, req)
				if err == nil || out.Run.Status != store.RunFailed {
					t.Fatalf("expected failure: %+v %v", out, err)
				}
				before := graphPlanState(t, f)
				if before.Stories[0].Stage != stage || before.Stories[0].Wait != nil {
					t.Fatalf("wrong failed state: %+v", before)
				}
				if failure == "readback" {
					mustProgress(t, f.db.ObservePlanStoryLifecycle(t.Context(), f.ref().String(), "story-001", planbrief.PlanLifecycle{}, childContentHold))
					held := graphPlanState(t, f)
					if held.Stories[0].Stage != stage || !reflect.DeepEqual(held.Stories[0].Approvals, before.Stories[0].Approvals) {
						t.Fatal("hold erased progress")
					}
					// Discovery verifies the same bound content; effect recovery still owns
					// the interrupted publication and must settle it before drafting again.
					mustProgress(t, f.db.ObservePlanStoryLifecycle(t.Context(), f.ref().String(), "story-001", planbrief.PlanLifecycle{State: "open"}, ""))
				}
				failing = false
				priorCalls, priorPosts := *calls, len(forge.all())
				req = asStoryTask(t, f, nextTrigger(base, 9995, "Retry"), "story-001")
				executeGraphPlan(t, f, req)
				after := graphPlanState(t, f).Stories[0]
				if *calls != priorCalls+1 || drafted[len(drafted)-1] != stage || after.Stage != stage || after.Wait == nil || after.Wait.Stage != stage || after.Wait.Kind != "review" || after.Blocked() {
					t.Fatalf("retry did not draft only %s: calls=%v state=%+v", stage, drafted, after)
				}
				if !reflect.DeepEqual(after.Approvals, before.Stories[0].Approvals) || after.Approvals[stage] != "" {
					t.Fatal("retry changed approvals")
				}
				for _, post := range forge.all()[priorPosts:] {
					if strings.Contains(post.body, "/approve-plan story-001 story ") || (stage == "acceptance" && strings.Contains(post.body, "/approve-plan story-001 technical ")) {
						t.Fatal("retry requested earlier approval")
					}
				}
				posts := len(forge.all())
				executeGraphPlan(t, f, req)
				if *calls != priorCalls+1 || len(forge.all()) != posts {
					t.Fatal("replayed reply duplicated draft or review")
				}
			})
		}
	}
}

func TestPlanDiscussion_RetryExplainsChangedReviewBasis(t *testing.T) {
	for _, change := range []string{"story", "technical", "dependency", "shared"} {
		t.Run(change, func(t *testing.T) {
			f, _, base, _, forge := graphPlanFixtureWithForge(t, "agent", func(a *graphPlanAnswer) {
				if a.Stage == "acceptance" {
					a.Stop, a.Reason = agent.OutcomeFailed, "before effect"
				}
			})
			executeGraphPlan(t, f, base)
			executeGraphPlan(t, f, graphPlanApprove(t, f, base, base.Trigger.CommentID+1))
			req := asStoryTask(t, f, graphPlanApprove(t, f, base, base.Trigger.CommentID+2), "story-001")
			if _, err := Execute(t.Context(), f.opts, req); err == nil {
				t.Fatal("expected acceptance failure")
			}
			wantStage, wantBasis := "story", "story content changed"
			mustProgress(t, f.db.UpdatePlanWorkflowState(t.Context(), f.ref().String(), func(s *planbrief.PlanState) (bool, error) {
				d := s.Stories[0].Draft
				switch change {
				case "story":
					d.Problem = "Changed intent"
				case "technical":
					// Copy the architecture before modifying the proposed draft.
					raw, err := json.Marshal(d)
					if err != nil {
						return false, err
					}
					d.Architecture = nil
					if err = json.Unmarshal(raw, &d); err != nil {
						return false, err
					}
					d.Architecture.General[0].Decision = "Changed technical design"
					wantStage, wantBasis = "technical", "technical content changed"
				case "dependency":
					d.Dependencies.IssueRefs = []string{"https://example.test/prerequisite"}
					wantBasis = "story dependencies changed"
				case "shared":
					wantBasis = "shared decisions changed"
					return true, s.SetSharedRevision("changed-shared")
				}
				return true, s.PutCandidate(d, wantStage, "owner-changed")
			}))
			changed := graphPlanState(t, f)
			if changed.Stories[0].Stage != wantStage || changed.Stories[0].Approvals[wantStage] != "" {
				t.Fatal("changed basis did not invalidate earliest stage")
			}
			if change == "technical" && changed.Stories[0].Approvals["story"] == "" {
				t.Fatal("technical edit invalidated story approval")
			}
			// The next draft can be unchanged; the explanation must come from retained
			// state and the previous immutable request, not from agent commentary.
			priorPosts := len(forge.all())
			req = asStoryTask(t, f, nextTrigger(base, 9996, "Retry"), "story-001")
			executeGraphPlan(t, f, req)
			found := false
			for _, post := range forge.all()[priorPosts:] {
				if strings.Contains(post.body, "Review resumes at the "+wantStage+" stage because "+wantBasis) {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %s rewind explanation: %+v", change, forge.all()[priorPosts:])
			}
		})
	}
}

func TestPlanReviewCommandText(t *testing.T) {
	draft := func(key, problem string, deps ...string) planbrief.PlanDraft {
		return planbrief.PlanDraft{Key: key, Problem: problem, Dependencies: planbrief.DependencyContent{StoryKeys: deps, IssueRefs: []string{}}}
	}
	// state builds a saved Plan state where story-001 sits at stage and
	// story-003 is approved at technical only when approved is set.
	state := func(stage string, d planbrief.PlanDraft, shared string, approved bool) planbrief.PlanState {
		s := planbrief.PlanState{SharedRevision: shared, Stories: []planbrief.PlanStoryState{
			{Key: "story-001", Stage: stage, Candidate: "c1", Approvals: map[string]string{}, Draft: d},
			{Key: "story-003", Stage: "acceptance", Candidate: "c3", Approvals: map[string]string{}, Draft: draft("story-003", "Upstream")},
		}}
		if approved {
			ref, err := s.ReviewRevision("story-003", "technical")
			mustProgress(t, err)
			s.Stories[1].Approvals["technical"] = ref
		}
		return s
	}
	before := planbrief.PlanState{SharedRevision: "shared-1", Stories: []planbrief.PlanStoryState{{Key: "story-001", Stage: "technical", Draft: draft("story-001", "Original")}}}
	r := &planGraphRuntime{}
	r.setPick(before, "story-001", "technical")
	issues := map[string]int{"story-001": 1118, "story-003": 1119}
	command := "/approve-plan story-001 story c1"
	added := []string{"needs your approval again", "its description changed", "dependencies changed", "shared decisions changed", "Review resumes at", "Approving the story stage", "The next review of this story will show"}
	cases := []struct {
		name          string
		saved         planbrief.PlanState
		command       string
		want, without []string
	}{
		{"unchanged stage", state("technical", draft("story-001", "Original"), "shared-1", false), "/approve-plan story-001 technical c1", []string{"\n\nTo promote this exact candidate, reply `/approve-plan story-001 technical c1`."}, added},
		{"new dependency approved", state("story", draft("story-001", "Original", "story-003"), "shared-1", true), command, []string{"This update changed the story itself, so it needs your approval again first.", "its dependencies changed: it now depends on story-003 (#1119) (before: none)", "Approving the story stage starts a new technical review of this story.", "To promote this exact candidate, reply `" + command + "`."}, []string{"its description changed", "shared decisions changed", "Review resumes at", "starts once"}},
		{"new dependency not approved", state("story", draft("story-001", "Original", "story-003"), "shared-1", false), command, []string{"its dependencies changed", "After you approve the story stage, the technical review of this story starts once the technical stage of story-003 (#1119) is approved.", "To promote this exact candidate"}, []string{"Approving the story stage starts a new technical review of this story."}},
		{"description only", state("story", draft("story-001", "Changed"), "shared-1", false), command, []string{"changed the story itself", "its description changed", "Approving the story stage starts a new technical review of this story."}, []string{"dependencies changed", "shared decisions changed", "Review resumes at"}},
		{"shared only", state("story", draft("story-001", "Original"), "shared-2", false), command, []string{"changed the story itself", "shared decisions changed"}, []string{"its description changed", "dependencies changed", "Review resumes at"}},
		{"none changed", state("story", draft("story-001", "Original"), "shared-1", false), command, []string{"changed the story itself", "Review resumes at the story stage. Approvals from that stage must be renewed."}, []string{"its description changed", "dependencies changed", "shared decisions changed"}},
		{"empty command", state("technical", draft("story-001", "Original"), "shared-1", false), "", []string{"The next review of this story will show the approval command."}, []string{"/approve-plan", "To promote this exact candidate"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := planReviewCommandText(r.picked, r.stage, c.saved, "story-001", issues, c.command)
			for _, want := range c.want {
				if !strings.Contains(got, want) {
					t.Fatalf("missing %q in %q", want, got)
				}
			}
			for _, unwanted := range c.without {
				if strings.Contains(got, unwanted) {
					t.Fatalf("unexpected %q in %q", unwanted, got)
				}
			}
		})
	}
}

// A technical review whose own result adds a story dependency moves the story
// back to the story stage; its comment must show the command that works now.
func TestPlanDiscussion_ReviewAfterOwnDependencyChangeShowsStoryCommand(t *testing.T) {
	f, _, base, _, forge := graphPlanFixtureWithForge(t, "agent", func(a *graphPlanAnswer) {
		if a.Stage == "technical" && a.Request != nil && a.Request.Draft.Key == "story-001" {
			a.Request.Draft.Dependencies.StoryKeys = []string{"story-002"}
			a.Request.ChangedSections = append(a.Request.ChangedSections, "dependencies")
		}
	})
	executeGraphPlan(t, f, base)
	next := base
	next.Trigger = Trigger{CommentID: base.Trigger.CommentID + 1, AuthorID: 7, Body: "/plan-new-story"}
	next.Command = Command{Kind: KindFreeText, Text: next.Trigger.Body}
	executeGraphPlan(t, f, next)
	upstream, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-002")
	mustProgress(t, err)
	if upstream.IssueNumber == 0 {
		t.Fatal("story-002 has no issue")
	}
	priorPosts := len(forge.all())
	executeGraphPlan(t, f, graphPlanApproveKey(t, f, base, "story-001", base.Trigger.CommentID+2))
	s := graphPlanState(t, f)
	item := s.Stories[0]
	if item.Key != "story-001" || item.Stage != "story" || item.Wait == nil || item.Wait.Kind != "review" {
		t.Fatalf("dependency change did not move story-001 back to a story review: %+v", item)
	}
	command := "/approve-plan story-001 story " + item.Candidate
	var review string
	for _, post := range forge.all()[priorPosts:] {
		if strings.Contains(post.body, "To promote this exact candidate") {
			review = post.body
		}
	}
	label := fmt.Sprintf("story-002 (#%d)", upstream.IssueNumber)
	for _, want := range []string{"`" + command + "`", "changed the story itself", "its dependencies changed", "it now depends on " + label, "starts once the technical stage of " + label + " is approved"} {
		if !strings.Contains(review, want) {
			t.Fatalf("review lacks %q: %q", want, review)
		}
	}
	if strings.Contains(review, "/approve-plan story-001 technical") || strings.Contains(review, "its description changed") {
		t.Fatalf("review shows a stale command or a wrong reason: %q", review)
	}
	if reason := s.PlanReplyReason("story-001", command); reason != "" {
		t.Fatalf("shown command refused: %s", reason)
	}
}

// A normal technical review keeps exactly today's command line and none of
// the stage-change sentences.
func TestPlanDiscussion_UnchangedReviewKeepsCommandLine(t *testing.T) {
	f, _, base, _, forge := graphPlanFixtureWithForge(t, "agent")
	executeGraphPlan(t, f, base)
	priorPosts := len(forge.all())
	executeGraphPlan(t, f, graphPlanApprove(t, f, base, base.Trigger.CommentID+1))
	item := graphPlanState(t, f).Stories[0]
	if item.Stage != "technical" || item.Wait == nil {
		t.Fatalf("no technical review: %+v", item)
	}
	line := "To promote this exact candidate, reply `/approve-plan story-001 technical " + item.Candidate + "`."
	var review string
	for _, post := range forge.all()[priorPosts:] {
		if strings.Contains(post.body, line) {
			review = post.body
		}
	}
	if review == "" {
		t.Fatalf("technical review lacks %q", line)
	}
	for _, unwanted := range []string{"needs your approval again", "dependencies changed", "Approving the story stage", "The next review of this story will show"} {
		if strings.Contains(review, unwanted) {
			t.Fatalf("normal review gained %q: %q", unwanted, review)
		}
	}
}
