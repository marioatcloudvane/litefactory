package turn

import (
	"strings"
	"testing"
)

// The shipped Plan graph stops the owner at the generic lf_gate action. These
// tests run it end to end and assert the gate node is the one that executed.

func executedNodes(t *testing.T, f *fixture) []string {
	t.Helper()
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "plan")
	mustProgress(t, err)
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
	mustProgress(t, err)
	var nodes []string
	for _, a := range attempts {
		nodes = append(nodes, a.Node)
	}
	return nodes
}

func TestPlanGate_ReviewPostsApproveCommandOpensWaitAndExactReplyApproves(t *testing.T) {
	f, _, base, _, forge := graphPlanFixtureWithForge(t, "agent")
	executeGraphPlan(t, f, base)
	if !strings.Contains(strings.Join(executedNodes(t, f), " "), "story_review") {
		t.Fatalf("gate node did not execute: %v", executedNodes(t, f))
	}
	item := graphPlanState(t, f).Stories[0]
	if item.Wait == nil || item.Wait.Kind != "review" {
		t.Fatalf("review wait not opened: %+v", item.Wait)
	}
	want := "/approve-plan story-001 story " + item.Candidate
	found := false
	for _, post := range forge.all() {
		found = found || strings.Contains(post.body, want)
	}
	if !found {
		t.Fatalf("review does not carry %q", want)
	}
	executeGraphPlan(t, f, graphPlanApprove(t, f, base, base.Trigger.CommentID+1))
	after := graphPlanState(t, f).Stories[0]
	if after.Approvals["story"] == "" {
		t.Fatalf("exact reply did not record approval: %+v", after)
	}
}

func TestPlanGate_RetireDraftPostsRetireCommandAndApproveReplyDoesNotRetire(t *testing.T) {
	technical := 0
	f, remote, base, _, forge := graphPlanFixtureWithForge(t, "agent", func(answer *graphPlanAnswer) {
		if answer.Stage == "technical" {
			technical++
			if technical == 2 {
				answer.Request.Operation = "retire"
				answer.Request.ChangedSections = nil
			}
		}
	})
	executeGraphPlan(t, f, base)
	executeGraphPlan(t, f, graphPlanApproveKey(t, f, base, "story-001", base.Trigger.CommentID+1))
	feedback := graphPlanApproveKey(t, f, base, "story-001", base.Trigger.CommentID+2)
	feedback.Trigger.Body = "This intent is no longer relevant"
	feedback.Trigger.PlanReply.Body = feedback.Trigger.Body
	feedback.Command.Text = feedback.Trigger.Body
	executeGraphPlan(t, f, feedback)
	item := graphPlanState(t, f).Stories[0]
	if item.Wait == nil || item.Wait.Kind != "retire" {
		t.Fatalf("retire wait not opened: %+v", item.Wait)
	}
	want := "/retire-plan story-001 " + item.Candidate
	found := false
	for _, post := range forge.all() {
		found = found || strings.Contains(post.body, want)
	}
	if !found {
		t.Fatalf("retire draft does not carry %q", want)
	}
	req := graphPlanApproveKey(t, f, base, "story-001", base.Trigger.CommentID+3)
	req.Trigger.Body = "/approve-plan story-001 " + item.Wait.Stage + " " + item.Candidate
	req.Trigger.PlanReply.Body = req.Trigger.Body
	req.Command.Text = req.Trigger.Body
	before := item.Approvals["technical"]
	out, err := Execute(t.Context(), f.opts, req)
	t.Logf("approve-on-retire result: err=%v out.Err=%v", err, out.Err)
	after := graphPlanState(t, f).Stories[0]
	t.Logf("after: retired=%v wait=%+v", after.Retired, after.Wait)
	remote.mu.Lock()
	state := remote.children[400]["state"]
	remote.mu.Unlock()
	if after.Retired || after.Approvals["technical"] != before || state == "closed" {
		t.Fatalf("an approve reply retired or approved a retire draft: %+v state=%v", after, state)
	}
}

func TestPlanGate_PublicationFailurePostsNoSecondFailureNotice(t *testing.T) {
	f, _, base, _, forge := graphPlanFixtureWithForge(t, "agent")
	// The advisory judge comment before the gate publishes; the gate's fails.
	forge.afterPost = func() { forge.failWith(500) }
	out, err := Execute(t.Context(), f.opts, base)
	if err == nil && out.Err == nil {
		t.Fatal("expected the gate publication to fail")
	}
	forge.recover()
	if bodies := failureBodies(forge); len(bodies) != 0 {
		t.Fatalf("a gate publication failure posted a failure notice: %q", bodies)
	}
	requireRunFailureReported(t, f, out.Run.ID, false)
	if !strings.Contains(strings.Join(executedNodes(t, f), " "), "story_review") {
		t.Fatalf("gate node did not execute: %v", executedNodes(t, f))
	}
}
