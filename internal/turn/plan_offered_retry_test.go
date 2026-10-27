package turn

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// offeredPlanRetry is the reply the failure notice tells the owner to post.
const offeredPlanRetry = "/plan Continue the unfinished step from the saved Plan state."

// storyReply builds an owner reply on a story's own issue the way the daemon
// does: no wait fields when no review is waiting, the comment body as free text.
func storyReply(t *testing.T, f *fixture, base Request, id int64, key, body string) Request {
	t.Helper()
	binding, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), key)
	mustProgress(t, err)
	reply := &planbrief.PlanOwnerReply{ID: id, OwnerID: 7, ActorID: 7, IssueID: binding.IssueID, Key: key, Body: body}
	for _, item := range graphPlanState(t, f).Stories {
		if item.Key == key && item.Wait != nil {
			reply.Stage, reply.Candidate, reply.WaitID = item.Wait.Stage, item.Wait.Candidate, item.Wait.ID
		}
	}
	base.Trigger = Trigger{CommentID: id, Body: body, Author: "owner", AuthorID: 7, PlanReply: reply}
	base.Command = Command{Kind: KindFreeText, Text: body}
	return asStoryTask(t, f, base, key)
}

// A stage that failed is retried by exactly the reply its failure notice offers,
// written on the story's issue: the stage drafts again, earlier approvals stay,
// nothing new is approved, and a replay drafts nothing more (#1229). An approval
// that does not match the current review is still refused.
func TestPlanFailure_OfferedRetryOnStoryIssueRerunsTheStage(t *testing.T) {
	for _, failure := range []string{"before effect", "readback"} {
		t.Run(failure, func(t *testing.T) {
			failing := false
			var drafted []string
			f, remote, base, calls, forge := graphPlanFixtureWithForge(t, "agent", func(a *graphPlanAnswer) {
				drafted = append(drafted, a.Stage)
				if failing && failure == "before effect" {
					a.Stop, a.Reason = agent.OutcomeFailed, "fixture failed before publication"
				}
			})
			executeGraphPlan(t, f, base)
			executeGraphPlan(t, f, graphPlanApprove(t, f, base, base.Trigger.CommentID+1))
			failing = true
			remote.losePatch = failure == "readback"
			out, err := Execute(t.Context(), f.opts, asStoryTask(t, f, graphPlanApprove(t, f, base, base.Trigger.CommentID+2), "story-001"))
			if err == nil || out.Run.Status != store.RunFailed {
				t.Fatalf("expected the acceptance stage to fail: %+v %v", out, err)
			}
			bodies := failureBodies(forge)
			if len(bodies) != 1 || !strings.Contains(bodies[0], "`"+offeredPlanRetry+"`") {
				t.Fatalf("the failure notice does not offer the retry reply: %q", bodies)
			}
			before := graphPlanState(t, f)
			if before.Stories[0].Stage != "acceptance" || before.Stories[0].Wait != nil {
				t.Fatalf("wrong failed state: %+v", before.Stories[0])
			}
			if failure == "readback" {
				mustProgress(t, f.db.ObservePlanStoryLifecycle(t.Context(), f.ref().String(), "story-001", planbrief.PlanLifecycle{}, childContentHold))
				mustProgress(t, f.db.ObservePlanStoryLifecycle(t.Context(), f.ref().String(), "story-001", planbrief.PlanLifecycle{State: "open"}, ""))
				before = graphPlanState(t, f)
			}
			failing, remote.losePatch = false, false

			// An approval that names no current review is still refused and changes nothing.
			wrong := storyReply(t, f, base, 9990, "story-001", "/approve-plan story-001 acceptance "+before.Stories[0].Candidate)
			wrong.Trigger.PlanReply.WaitID, wrong.Trigger.PlanReply.Stage, wrong.Trigger.PlanReply.Candidate = "plan-review:1:2:acceptance_review", "acceptance", before.Stories[0].Candidate
			priorCalls := *calls
			if _, err := Execute(t.Context(), f.opts, wrong); err == nil || !strings.Contains(err.Error(), "stale or wrong-story Plan reply") {
				t.Fatalf("an approval for a wait that does not exist was not refused: %v", err)
			}
			if *calls != priorCalls || !reflect.DeepEqual(before, graphPlanState(t, f)) {
				t.Fatal("a refused approval changed state or ran a stage")
			}

			req := storyReply(t, f, base, 9991, "story-001", offeredPlanRetry)
			executeGraphPlan(t, f, req)
			after := graphPlanState(t, f).Stories[0]
			if *calls != priorCalls+1 || drafted[len(drafted)-1] != "acceptance" || after.Stage != "acceptance" || after.Wait == nil || after.Wait.Stage != "acceptance" || after.Wait.Kind != "review" || after.Blocked() {
				t.Fatalf("the offered retry did not draft acceptance again: calls=%d drafted=%v state=%+v", *calls-priorCalls, drafted, after)
			}
			if !reflect.DeepEqual(after.Approvals, before.Stories[0].Approvals) || after.Approvals["acceptance"] != "" {
				t.Fatalf("the retry changed approvals: %v -> %v", before.Stories[0].Approvals, after.Approvals)
			}
			if !slices.Contains(graphPlanState(t, f).ConsumedReplies, req.Trigger.CommentID) {
				t.Fatal("the retry reply was not consumed")
			}
			posts := len(forge.all())
			if _, err := Execute(t.Context(), f.opts, req); err != nil {
				t.Fatalf("a replayed retry failed: %v", err)
			}
			if *calls != priorCalls+1 || len(forge.all()) != posts {
				t.Fatal("a replayed retry drafted or posted again")
			}
		})
	}
}
