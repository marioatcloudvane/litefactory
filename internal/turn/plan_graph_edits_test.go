package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

// editStoryIssue applies an owner's edit of a story's sub-issue body.
func editStoryIssue(t *testing.T, f *fixture, remote *graphPlanHTTP, key, from, to string) {
	t.Helper()
	binding, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), key)
	mustProgress(t, err)
	remote.mu.Lock()
	body, _ := remote.children[binding.IssueNumber]["body"].(string)
	if !strings.Contains(body, from) {
		remote.mu.Unlock()
		t.Fatalf("the sub-issue of %s lacks %q: %q", key, from, body)
	}
	remote.children[binding.IssueNumber]["body"] = strings.Replace(body, from, to, 1)
	remote.mu.Unlock()
	client, ok := f.opts.SubIssueCreator.(PlanStoryForge)
	if !ok {
		t.Fatal("fixture requires PlanStoryForge")
	}
	issue, err := client.GetIssue(t.Context(), github.Repo{Owner: "acme", Name: "acme-api"}, binding.IssueNumber)
	mustProgress(t, err)
	mustProgress(t, ReconcilePlanChildEdit(t.Context(), f.db, f.ref().String(), 123, 1230, issue, planbrief.ContentScope{Roles: []string{"go-implementation-engineer"}}))
}

// #1160 c13: the #1155 trigger. An owner edits only the technical section of a
// story whose story stage is not approved. The story stays at its story stage,
// the owner's next reply runs the story stage, and that review's command works.
func TestPlanGraphEdits_TechnicalEditNeverMovesPastAnUnapprovedStory(t *testing.T) {
	f, remote, base, forge, _ := upstreamFixture(t)
	// story-002 has technical content; an edit of its story section reopens it
	// at the story stage, and the factory re-runs that stage to a review.
	editStoryIssue(t, f, remote, "story-002", "I want shape intent", "I want a reshaped intent")
	if item := indepStory(t, f, "story-002"); item.Stage != "story" || item.Wait != nil || item.Draft.Architecture == nil {
		t.Fatalf("story edit did not reopen story-002 at story: %+v", item)
	}
	executeGraphPlan(t, f, planWorkRun(t, f, base, "story-002"))
	if item := indepStory(t, f, "story-002"); item.Stage != "story" || item.Wait == nil || item.Wait.Stage != "story" || item.Approvals["story"] != "" {
		t.Fatalf("story-002 is not waiting for its story review: %+v", item)
	}

	editStoryIssue(t, f, remote, "story-002", "Reuse focused roles", "Reuse narrowly focused roles")
	item := indepStory(t, f, "story-002")
	if item.Stage != "story" || !strings.HasPrefix(item.Candidate, "owner-edit-") {
		t.Fatalf("a technical edit moved the story past its unapproved story stage: %+v", item)
	}

	posts := len(forge.all())
	executeGraphPlan(t, f, ownerReplyRun(t, f, base, "story-002", 5201, "Please keep the story tight."))
	item = indepStory(t, f, "story-002")
	if item.Wait == nil || item.Wait.Stage != "story" || item.Stage != "story" {
		t.Fatalf("the owner's reply did not run the story stage: %+v", item)
	}
	command := "/approve-plan story-002 story " + item.Candidate
	shown := false
	for _, post := range forge.all()[posts:] {
		shown = shown || strings.Contains(post.body, command)
	}
	if !shown {
		t.Fatalf("the review does not show %q", command)
	}
	executeGraphPlan(t, f, graphPlanApproveKey(t, f, base, "story-002", 5202))
	if item = indepStory(t, f, "story-002"); item.Approvals["story"] == "" || item.Stage != "technical" {
		t.Fatalf("the shown command did not approve the story stage: %+v", item)
	}
}

// saveStaleEarlierApproval saves the #1155 state for story-002: a review wait at
// a later stage while its story approval no longer matches, and returns the
// command that wait showed.
func saveStaleEarlierApproval(t *testing.T, f *fixture) string {
	t.Helper()
	var command string
	mustProgress(t, f.db.UpdatePlanWorkflowState(t.Context(), f.ref().String(), func(s *planbrief.PlanState) (bool, error) {
		for i := range s.Stories {
			if item := &s.Stories[i]; item.Key == "story-002" {
				if item.Wait == nil || item.Wait.Stage == "story" {
					t.Fatalf("story-002 has no later-stage wait: %+v", item)
				}
				command = s.PlanCurrentCommand("story-002")
				item.Approvals["story"] = "stale-story-approval"
			}
		}
		return true, nil
	}))
	if command == "" {
		t.Fatal("no command was shown")
	}
	return command
}

// requireStaleWaitRecovers runs the work the factory offers after the stale wait
// was cleared and approves the story stage with the command its review shows.
func requireStaleWaitRecovers(t *testing.T, f *fixture, base Request, forge *fakeForge) {
	t.Helper()
	item := indepStory(t, f, "story-002")
	if item.Wait != nil {
		t.Fatalf("the stale wait survived the reply: %+v", item.Wait)
	}
	state := graphPlanState(t, f)
	if stage, _, ok := state.StoryNextStep("story-002"); stage != "story" || !ok {
		t.Fatalf("the factory does not offer the story stage again: %q %v", stage, ok)
	}
	posts := len(forge.all())
	executeGraphPlan(t, f, planWorkRun(t, f, base, "story-002"))
	item = indepStory(t, f, "story-002")
	if item.Wait == nil || item.Wait.Stage != "story" {
		t.Fatalf("the re-run did not open a story review: %+v", item)
	}
	command := "/approve-plan story-002 story " + item.Candidate
	shown := false
	for _, post := range forge.all()[posts:] {
		shown = shown || strings.Contains(post.body, command)
	}
	if !shown {
		t.Fatalf("the re-run review does not show %q", command)
	}
	executeGraphPlan(t, f, graphPlanApproveKey(t, f, base, "story-002", 5302))
	if item = indepStory(t, f, "story-002"); item.Approvals["story"] == "" || item.Stage != "technical" {
		t.Fatalf("the shown command did not approve the story stage: %+v", item)
	}
}

// #1160 c8: a story saved with a later-stage wait behind a stale earlier
// approval is never left without a working command, whether the owner answers
// on the story issue or names the story on the parent.
func TestPlanGraphEdits_StaleEarlierApprovalReofferedWithAWorkingCommand(t *testing.T) {
	t.Run("story issue", func(t *testing.T) {
		f, _, base, forge, _ := upstreamFixture(t)
		refused := saveStaleEarlierApproval(t, f)
		// The daemon explains a reply against the state it reads; that read clears
		// the wait, so the explanation names no refused command.
		state := graphPlanState(t, f)
		reason := state.PlanReplyReason("story-002", refused)
		if !strings.Contains(reason, "its next review will show the command to use") || strings.Contains(reason, refused) {
			t.Fatalf("reply explanation: %q", reason)
		}
		req := ownerReplyRun(t, f, base, "story-002", 5301, refused)
		if _, err := Execute(t.Context(), f.opts, req); err == nil {
			t.Fatal("a reply to a cleared wait was applied")
		}
		requireStaleWaitRecovers(t, f, base, forge)
	})
	t.Run("parent issue", func(t *testing.T) {
		f, _, base, forge, _ := upstreamFixture(t)
		refused := saveStaleEarlierApproval(t, f)
		req := base
		req.Trigger = Trigger{CommentID: 5401, Body: refused, Author: "owner", AuthorID: 7}
		req.Command = Command{Kind: KindFreeText, Text: refused}
		out, err := Execute(t.Context(), f.opts, req)
		mustProgress(t, err)
		if !strings.Contains(out.Reason, "Story story-002 not approved:") || !strings.Contains(out.Reason, "its next review will show the command to use") {
			t.Fatalf("parent refusal: %q", out.Reason)
		}
		requireStaleWaitRecovers(t, f, base, forge)
	})
}
