package turn

import (
	"slices"
	"sync/atomic"
	"testing"
)

var storyTriggerSeq atomic.Int64

func nextStoryTrigger() int64 { return 700000 + storyTriggerSeq.Add(1) }

// asStoryTask addresses req to the task of the story's own issue, the way the
// daemon runs a story: the story task is the session of its sub-issue. A story
// that has no issue yet stays with the parent task.
func asStoryTask(t *testing.T, f *fixture, req Request, key string) Request {
	t.Helper()
	draft, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), key)
	if err != nil || draft.IssueNumber == 0 {
		return req
	}
	session, err := f.db.GetOrCreateSession(t.Context(), f.storeRepo, draft.IssueNumber)
	mustProgress(t, err)
	req.SessionID = session.ID
	req.Issue = Issue{Number: draft.IssueNumber, Title: "Story " + key, Author: req.Issue.Author}
	return req
}

// runOwedStory runs the first story task that has a stage owed, as one daemon
// tick does, and reports whether one ran.
func runOwedStory(t *testing.T, f *fixture, base Request) (Result, bool, error) {
	t.Helper()
	state := graphPlanState(t, f)
	var keys []string
	if state.StorySet != nil {
		for _, entry := range state.StorySet.Entries {
			keys = append(keys, state.StorySet.Keys[entry.ID])
		}
	}
	for _, item := range state.Stories {
		if !slices.Contains(keys, item.Key) {
			keys = append(keys, item.Key)
		}
	}
	for _, key := range keys {
		if _, _, ok := state.StoryNextStep(key); !ok {
			continue
		}
		req := asStoryTask(t, f, base, key)
		if req.SessionID == base.SessionID {
			continue
		}
		instruction := "Continue the story " + key + "."
		req.Trigger = Trigger{CommentID: nextStoryTrigger(), Body: instruction, Author: "owner", AuthorID: 7}
		req.Command = Command{Kind: KindFreeText, Text: instruction}
		out, err := Execute(t.Context(), f.opts, req)
		return out, true, err
	}
	return Result{}, false, nil
}

// draftAllStories runs the story tasks the way the daemon does until every story
// of the set has a draft. A dependant is drafted only after its upstream stories
// have their story review approved, so the helper approves the story review of
// whichever drafted story is waiting for it. It returns the last trigger id used.
func draftAllStories(t *testing.T, f *fixture, base Request) {
	t.Helper()
	id := int64(3000)
	for range 40 {
		drainGraphSteps(t, f, base)
		state := graphPlanState(t, f)
		undrafted := false
		for _, entry := range state.StorySet.Entries {
			undrafted = undrafted || !state.StoryDrafted(state.StorySet.Keys[entry.ID])
		}
		if !undrafted {
			// Let every story that has a stage owed reach its review wait.
			drainGraphSteps(t, f, base)
			return
		}
		approved := false
		for _, item := range state.Stories {
			if item.Wait != nil && item.Wait.Stage == "story" && item.Candidate != "" {
				id++
				executeGraphPlan(t, f, graphPlanApproveKey(t, f, base, item.Key, id))
				approved = true
				break
			}
		}
		if !approved {
			t.Fatalf("no story review to approve and a story still has no draft: %+v", state.Stories)
		}
	}
	t.Fatal("the stories were not all drafted")
}

// runParent runs the Plan parent's own work, as the daemon does when every story
// has its three approvals: it posts the assembled review.
func runParent(t *testing.T, f *fixture, base Request) Result {
	t.Helper()
	instruction := "Continue the plan."
	base.Trigger = Trigger{CommentID: nextStoryTrigger(), Body: instruction, Author: "owner", AuthorID: 7}
	base.Command = Command{Kind: KindFreeText, Text: instruction}
	return executeGraphPlan(t, f, base)
}
