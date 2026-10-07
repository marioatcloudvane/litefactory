package turn

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// These tests run the story creation cycle end to end over the same assembled
// graph Plan the other integration tests use: a promoted three-story set whose
// first-listed story depends on the second and whose third depends on both, so
// dependency-first drafting is not the displayed order. A fake agent is the only
// fake. One run advances one agent stage, so the cycle continues the way the
// daemon continues it: through the dispatch a run leaves behind.

// creationAgent is a fake story-stage agent for the creation cycle. It authors
// the request the factory bound for the selected story and records the order it
// was asked in. ask and crash name a story whose first attempt asks the owner a
// question, or ends without a result.
type creationAgent struct {
	order []string
	stage []string
	ask   string
	crash string
	done  map[string]bool
}

func (c *creationAgent) behave(t *testing.T, fx **fixture) planAgentBehavior {
	c.done = map[string]bool{}
	return func(s planAgentStep) (*agent.RunResult, error) {
		f := *fx
		c.order = append(c.order, s.story)
		c.stage = append(c.stage, s.stage)
		once := func(kind string) bool {
			if c.done[kind+s.story] {
				return false
			}
			c.done[kind+s.story] = true
			return true
		}
		if s.story == c.crash && s.stage == "story" && once("crash") {
			return &agent.RunResult{Outcome: agent.OutcomeFailed, Reason: "scripted provider failure", FinishedAt: time.Now()}, nil
		}
		if s.story == c.ask && s.stage == "story" && once("ask") {
			asked := ar.Result{Outcome: "incomplete", Reason: "Unclear.", Comment: "Question for the owner.", Questions: []ar.Question{{ID: "q-1", Audience: "business", Text: "Which variant of " + s.story + "?"}}, Payload: ar.Payload{Plan: &ar.Plan{}}}
			if _, accepted := s.call("communication_submit", submitBytes(t, asked)); !accepted.Validation.Valid {
				t.Fatalf("question refused: %+v", accepted)
			}
			envelope, err := json.Marshal(ar.ResearchNode{Version: 1, Response: "ask", Result: asked})
			mustProgress(t, err)
			return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: string(envelope), FinishedAt: time.Now()}, nil
		}
		request := integrationCreateRequest(s.story)
		if binding, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), s.story); err == nil && binding.IssueID > 0 && binding.Candidate != "" {
			request.Operation, request.ExpectedRevision = "update", binding.Revision
			mustProgress(t, json.Unmarshal([]byte(binding.DraftJSON), &request.Draft))
		} else {
			state := graphPlanState(t, f)
			for _, item := range state.Stories {
				if item.Key == s.story {
					request.Draft.Dependencies.StoryKeys = append([]string{}, item.Draft.Dependencies.StoryKeys...)
				}
			}
		}
		request.Draft.Name = "Story " + s.story
		if s.stage == "technical" {
			request.Draft.Architecture = &planbrief.PlanArchitecture{General: []planbrief.GeneralDecision{{Area: "Roles", Decision: "Reuse focused roles", Rationale: "Existing boundaries"}}}
			request.Draft.Guidance = []string{"agent-setup"}
			request.ChangedSections = []string{"architecture", "guidance"}
		}
		if s.stage == "acceptance" {
			request.Draft.Criteria = []planbrief.PlanCriterion{{ID: "AC-001", Text: "Story remains stable", ProofMethod: "Scoped check"}}
			request.Draft.Tasks = []planbrief.TaskContent{{ID: "T-001", Description: "Implement", OwnerRole: "go-implementation-engineer", CriterionIDs: []string{"AC-001"}, DependsOn: []string{}}}
			request.ChangedSections = []string{"criteria", "tasks"}
		}
		good := integrationDone(request, "Drafted "+s.story)
		if _, accepted := s.call("communication_submit", submitBytes(t, good)); !accepted.Validation.Valid || accepted.Receipt == nil {
			t.Fatalf("submission for %s not accepted: %s", s.story, diagnosticPaths(accepted.Validation))
		}
		return agentDone(t, good), nil
	}
}

// creationFixture is a graph Plan session whose three-story set is proposed and
// not promoted, with the owner's promotion request ready to run.
func creationFixture(t *testing.T, c *creationAgent) (*fixture, *graphPlanHTTP, Request, *fakeForge, Request) {
	t.Helper()
	return creationFixtureFor(t, c, promoteEntries())
}

func creationFixtureFor(t *testing.T, c *creationAgent, entries []planbrief.PlanStorySetEntry) (*fixture, *graphPlanHTTP, Request, *fakeForge, Request) {
	t.Helper()
	var fx *fixture
	f, remote, base, forge, _ := graphPlanAgentFixture(t, c.behave(t, &fx))
	fx = f
	revision := saveProposedSet(t, f, entries)
	return f, remote, base, forge, promoteTrigger(base, 900, 7, promoteCommand(revision))
}

// runDueGraphStep does what the daemon does for one owed graph stage: it reserves
// the due dispatch and executes the turn under the dispatch's private trigger.
// ran is false when no stage is owed.
func runDueGraphStep(t *testing.T, f *fixture, base Request) (out Result, ran bool, err error) {
	t.Helper()
	due, listErr := f.db.ListDueGraphDispatches(t.Context(), time.Now().Add(time.Minute), 8)
	mustProgress(t, listErr)
	if len(due) == 0 {
		return Result{}, false, nil
	}
	reserved, ok, reserveErr := f.db.ReserveGraphDispatch(t.Context(), due[0].ID, time.Now().Add(time.Minute))
	mustProgress(t, reserveErr)
	if !ok {
		t.Fatal("due dispatch could not be reserved")
	}
	if reserved.SessionID != base.SessionID {
		session, sessionErr := f.db.GetSession(t.Context(), reserved.SessionID)
		mustProgress(t, sessionErr)
		base.SessionID = session.ID
		base.Issue = Issue{Number: session.IssueNumber, Title: "Story", Author: base.Issue.Author}
	}
	instruction := fmt.Sprintf("Continue the workflow at its %s stage.", reserved.Node)
	base.Trigger = Trigger{CommentID: reserved.TriggerID(), Body: instruction, Author: "owner"}
	base.Command = Command{Kind: KindFreeText, Text: instruction}
	out, err = Execute(t.Context(), f.opts, base)
	mustProgress(t, f.db.FinishGraphDispatch(t.Context(), reserved.ID, time.Now()))
	return out, true, err
}

// drainGraphSteps runs every owed stage in turn and reports how many ran.
func drainGraphSteps(t *testing.T, f *fixture, base Request) int {
	t.Helper()
	steps := 0
	for {
		out, ran, err := runDueGraphStep(t, f, base)
		if !ran {
			out, ran, err = runOwedStory(t, f, base)
		}
		if !ran {
			return steps
		}
		if err != nil || out.Err != nil {
			t.Fatalf("graph step %d failed: %v %v logs=%s", steps+1, err, out.Err, f.logs.String())
		}
		steps++
		if steps > 12 {
			t.Fatal("the creation cycle did not end")
		}
	}
}

func requireOwedStages(t *testing.T, f *fixture, want int) {
	t.Helper()
	due, err := f.db.ListDueGraphDispatches(t.Context(), time.Now().Add(time.Hour), 8)
	mustProgress(t, err)
	if len(due) != want {
		t.Fatalf("%d graph stages owed, want %d", len(due), want)
	}
}

// requireCreated checks that every promoted story has exactly one sub-issue, a
// draft and its own review wait, and that keys and ordinals stayed as promoted.
func requireCreated(t *testing.T, f *fixture, remote *graphPlanHTTP) {
	t.Helper()
	state := graphPlanState(t, f)
	drafts, err := f.db.ListPlanStoryDrafts(t.Context(), f.ref().String())
	mustProgress(t, err)
	if len(state.Stories) != 3 || len(drafts) != 3 || remote.creates != 3 {
		t.Fatalf("stories=%d rows=%d creates=%d", len(state.Stories), len(drafts), remote.creates)
	}
	numbers := map[int]bool{}
	for i, d := range drafts {
		if d.Key != fmt.Sprintf("story-%03d", i+1) || d.Ordinal != int64(i+1) || d.IssueID == 0 || d.Candidate == "" || numbers[d.IssueNumber] {
			t.Fatalf("draft row %d: %+v", i, d)
		}
		numbers[d.IssueNumber] = true
		item := state.Stories[i]
		waitingUpstream := item.Wait == nil && len(item.Approvals) > 0
		if item.Key != d.Key || item.Candidate != d.Candidate || (!waitingUpstream && (item.Wait == nil || item.Wait.Kind != "review" || item.Wait.IssueID != d.IssueID)) {
			t.Fatalf("story %s has no review wait on its own issue: %+v", d.Key, item)
		}
	}
}

// AC-043: after promotion the cycle drafts each promoted story in one agent run,
// dependencies before dependants although the dependency is listed later, and
// ends only when every story has a draft. Each story gets its own review.
func TestPlanCreation_ThreeStoriesInDependencyOrderOneAgentRunEach(t *testing.T) {
	c := &creationAgent{}
	f, remote, base, forge, promote := creationFixture(t, c)
	out, err := Execute(t.Context(), f.opts, promote)
	if err != nil || out.Err != nil || !out.Succeeded() {
		t.Fatalf("promotion run: %v %v logs=%s", err, out.Err, f.logs.String())
	}
	// Promotion creates every story issue and drafts nothing: each story drafts in
	// the task of its own issue, a dependant only after its upstream story review.
	if remote.creates != 3 || len(c.order) != 0 {
		t.Fatalf("promotion: creates=%d drafted=%v", remote.creates, c.order)
	}
	if _, ran, err := runOwedStory(t, f, base); !ran || err != nil || !slices.Equal(c.order, []string{"story-002"}) {
		t.Fatalf("first story task: ran=%v err=%v drafted=%v", ran, err, c.order)
	}
	if _, ran, _ := runOwedStory(t, f, base); ran {
		t.Fatalf("a dependant was drafted before its upstream story review: %v", c.order)
	}
	draftAllStories(t, f, base)
	var first []string
	for i, stage := range c.stage {
		if stage == "story" {
			first = append(first, c.order[i])
		}
	}
	if want := []string{"story-002", "story-001", "story-003"}; !slices.Equal(first, want) {
		t.Fatalf("first drafts %v, want %v", first, want)
	}
	requireCreated(t, f, remote)

	state := graphPlanState(t, f)
	if got := state.Stories[0].Draft.Dependencies.StoryKeys; !slices.Equal(got, []string{"story-002"}) {
		t.Fatalf("story-001 dependencies %v", got)
	}
	if got := state.Stories[2].Draft.Dependencies.StoryKeys; !slices.Equal(got, []string{"story-001", "story-002"}) {
		t.Fatalf("story-003 dependencies %v", got)
	}
	// Every story's review request reached its own sub-issue with its own candidate.
	for _, story := range state.Stories {
		found := false
		for _, post := range forge.all() {
			if strings.Contains(post.body, "/approve-plan "+story.Key+" ") {
				found = true
			}
		}
		if !found {
			t.Fatalf("no review request for %s", story.Key)
		}
	}
}

// AC-043, AC-044: restarting or replaying any part of the cycle changes nothing.
func TestPlanCreation_ReplaysAndRestartsCreateNothingTwice(t *testing.T) {
	c := &creationAgent{}
	f, remote, base, _, promote := creationFixture(t, c)
	first, err := Execute(t.Context(), f.opts, promote)
	mustProgress(t, err)
	if replay, err := Execute(t.Context(), f.opts, promote); err != nil || replay.Run.ID != first.Run.ID || len(c.order) != 0 || remote.creates != 3 {
		t.Fatalf("the promotion replayed as new work: run %d/%d agent runs %d creates %d err=%v", replay.Run.ID, first.Run.ID, len(c.order), remote.creates, err)
	}
	draftAllStories(t, f, base)
	requireCreated(t, f, remote)
	before := graphPlanState(t, f)
	ran := len(c.order)
	if replay, err := Execute(t.Context(), f.opts, promote); err != nil || replay.Run.ID != first.Run.ID {
		t.Fatalf("replay after completion: %v", err)
	}
	if len(c.order) != ran || remote.creates != 3 || len(graphPlanState(t, f).ConsumedReplies) != len(before.ConsumedReplies) {
		t.Fatalf("replay repeated work: order=%v creates=%d", c.order, remote.creates)
	}
}

// AC-044: a question while drafting pauses the cycle and, after the owner's
// reply, resumes with the same story; nothing earlier is repeated.
func TestPlanCreation_QuestionPausesTheCycleAndResumesTheSameStory(t *testing.T) {
	c := &creationAgent{ask: "story-001"}
	f, remote, base, forge, promote := creationFixture(t, c)
	_, err := Execute(t.Context(), f.opts, promote)
	mustProgress(t, err)
	if _, ran, err := runOwedStory(t, f, base); !ran || err != nil {
		t.Fatalf("story-002 draft: ran=%v %v", ran, err)
	}
	executeGraphPlan(t, f, graphPlanApproveKey(t, f, base, "story-002", 950))
	out, ran, err := runOwedStory(t, f, base)
	if !ran || err != nil || out.Err != nil {
		t.Fatalf("the asking stage: ran=%v %v %v", ran, err, out.Err)
	}
	if got := c.order[len(c.order)-1]; got != "story-001" || remote.creates != 3 {
		t.Fatalf("order %v creates %d", c.order, remote.creates)
	}
	// Paused: the question is on the story's own issue and story-001 has no draft.
	story, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-001")
	mustProgress(t, err)
	asked := false
	for _, post := range forge.all() {
		if strings.Contains(post.body, "Which variant of story-001?") && strings.Contains(post.path, fmt.Sprintf("/issues/%d/comments", story.IssueNumber)) {
			asked = true
		}
	}
	if !asked {
		t.Fatal("the question did not reach the story issue")
	}
	if state := graphPlanState(t, f); state.StoryDrafted("story-001") || !state.StoryDrafted("story-002") {
		t.Fatalf("draft presence after the question: %+v", state.Stories)
	}
	asks := len(c.order)

	// The owner's reply on the story's issue resumes the same story.
	answer := asStoryTask(t, f, nextTrigger(base, 901, "Variant A, please."), "story-001")
	out, err = Execute(t.Context(), f.opts, answer)
	if err != nil || out.Err != nil {
		t.Fatalf("resumed run: %v %v logs=%s", err, out.Err, f.logs.String())
	}
	if got := c.order[len(c.order)-1]; got != "story-001" || len(c.order) != asks+1 {
		t.Fatalf("after the reply the agent worked on %v, want the same story again", c.order)
	}
	if resumed := graphPlanState(t, f); !resumed.StoryDrafted("story-001") {
		t.Fatal("the resumed story has no draft")
	}
}

// AC-044: a failed draft preserves the sub-issues already created, tells the
// owner the cause and how to retry, and the retry resumes at the failed story
// without a second result, key or sub-issue.
func TestPlanCreation_FailureKeepsEarlierStoriesAndResumesAtTheFailedOne(t *testing.T) {
	c := &creationAgent{crash: "story-001"}
	f, remote, base, forge, promote := creationFixture(t, c)
	_, err := Execute(t.Context(), f.opts, promote)
	mustProgress(t, err)
	if _, ran, err := runOwedStory(t, f, base); !ran || err != nil {
		t.Fatalf("story-002 draft: ran=%v %v", ran, err)
	}
	executeGraphPlan(t, f, graphPlanApproveKey(t, f, base, "story-002", 950))
	out, ran, err := runOwedStory(t, f, base)
	if !ran || err == nil || out.Run.Status != store.RunFailed {
		t.Fatalf("the failing stage: ran=%v err=%v run=%+v", ran, err, out.Run)
	}
	requireFailureComment(t, forge, "process outcome")
	state := graphPlanState(t, f)
	if remote.creates != 3 || !state.StoryDrafted("story-002") || state.StoryDrafted("story-001") {
		t.Fatalf("the earlier story was not preserved: creates=%d", remote.creates)
	}
	drafts, err := f.db.ListPlanStoryDrafts(t.Context(), f.ref().String())
	mustProgress(t, err)
	if len(drafts) != 3 {
		t.Fatalf("keys reserved at promotion changed: %d", len(drafts))
	}

	retry := asStoryTask(t, f, nextTrigger(base, 902, "Please retry."), "story-001")
	if _, err = Execute(t.Context(), f.opts, retry); err != nil {
		t.Fatalf("retry: %v logs=%s", err, f.logs.String())
	}
	if got := c.order[len(c.order)-1]; got != "story-001" {
		t.Fatalf("the retry worked on %v, want the failed story again", c.order)
	}
	if retried := graphPlanState(t, f); !retried.StoryDrafted("story-001") || remote.creates != 3 {
		t.Fatalf("the retry did not draft the failed story once: creates=%d", remote.creates)
	}
	if len(failureBodies(forge)) != 1 {
		t.Fatalf("failure comments: %d", len(failureBodies(forge)))
	}
}

// A create whose reply was lost is settled from the forge on the next admission,
// never created again, and the cycle then continues with the remaining stories.
func TestPlanCreation_LostCreateReplyIsSettledNotRepeated(t *testing.T) {
	c := &creationAgent{}
	f, remote, base, _, promote := creationFixture(t, c)
	remote.loseCreate = true
	if _, err := Execute(t.Context(), f.opts, promote); err == nil {
		t.Fatal("a lost create reply was reported as success")
	}
	if remote.creates != 1 {
		t.Fatalf("creates %d", remote.creates)
	}
	if _, err := Execute(t.Context(), f.opts, nextTrigger(base, 903, "/plan retry")); err != nil {
		t.Fatalf("retry: %v logs=%s", err, f.logs.String())
	}
	draftAllStories(t, f, base)
	state := graphPlanState(t, f)
	drafts, err := f.db.ListPlanStoryDrafts(t.Context(), f.ref().String())
	mustProgress(t, err)
	if remote.creates != 3 || len(drafts) != 3 || len(state.Stories) != 3 {
		t.Fatalf("creates=%d rows=%d stories=%d", remote.creates, len(drafts), len(state.Stories))
	}
	for i, d := range drafts {
		if d.Key != fmt.Sprintf("story-%03d", i+1) || d.Ordinal != int64(i+1) || d.IssueID == 0 || !state.StoryDrafted(d.Key) {
			t.Fatalf("draft row %d: %+v", i, d)
		}
	}
	// The story whose reply was lost was settled from the forge, not drafted again.
	firstDrafts := 0
	for i, key := range c.order {
		if key == "story-002" && c.stage[i] == "story" {
			firstDrafts++
		}
	}
	if count := firstDrafts; count != 1 {
		t.Fatalf("story-002 drafted %d times: %v", count, c.order)
	}
}

// AC-047: once every story has a draft, one story waiting for its owner review
// blocks no other. The owner advances story-002 through all three stages while
// story-001 and story-003 stay exactly as they were.
func TestPlanCreation_StoryReviewsDoNotBlockEachOther(t *testing.T) {
	c := &creationAgent{}
	f, remote, base, _, promote := creationFixture(t, c)
	_, err := Execute(t.Context(), f.opts, promote)
	mustProgress(t, err)
	draftAllStories(t, f, base)
	requireCreated(t, f, remote)
	held := map[string]planbrief.PlanStoryState{}
	for _, item := range graphPlanState(t, f).Stories {
		if item.Key == "story-003" {
			held[item.Key] = item
		}
	}

	// story-002 has its story review approved; the owner advances it through the
	// other two stages while story-003, still at its story review, stays as it was.
	id := int64(950)
	for range 2 {
		id++
		executeGraphPlan(t, f, graphPlanApproveKey(t, f, base, "story-002", id))
	}
	state := graphPlanState(t, f)
	for _, item := range state.Stories {
		switch item.Key {
		case "story-002":
			if item.Stage != "acceptance" || len(item.Approvals) != 3 {
				t.Fatalf("story-002 did not advance independently: %+v", item)
			}
		case "story-003":
			before := held[item.Key]
			if item.Stage != before.Stage || item.Candidate != before.Candidate || item.Wait == nil || *item.Wait != *before.Wait || len(item.Approvals) != 0 {
				t.Fatalf("waiting story %s changed: %+v", item.Key, item)
			}
		}
	}
	if remote.creates != 3 {
		t.Fatalf("creates %d", remote.creates)
	}
}

// A promoted first draft keeps the dependency graph the owner approved: it can
// neither drop nor add a prerequisite story, and the refusal names the field.
func TestPlanCreation_FirstDraftMustKeepThePromotedDependencies(t *testing.T) {
	c := &creationAgent{}
	f, _, _, _, promote := creationFixture(t, c)
	mustProgress(t, promoteRuntime(f, promote, 900, 7, promote.Trigger.Body).admitReply(t.Context()))
	state := graphPlanState(t, f)
	binding, err := planGraphBinding(state, "story-001", "story", 1, 2, "")
	mustProgress(t, err)
	if !binding.Promoted || !slices.Equal(binding.PromotedDependencies, []string{"story-002"}) {
		t.Fatalf("binding %+v", binding)
	}
	for _, wrong := range [][]string{{}, {"story-002", "story-003"}, {"story-003"}} {
		request := integrationCreateRequest("story-001")
		request.Draft.Dependencies.StoryKeys = wrong
		d := ar.ValidateResult(integrationDone(request, "x"), ar.Context{SchemaVersion: ar.SchemaVersion, Mode: "plan", WorkflowVersion: store.WorkflowSeparated, PlanScope: planbrief.ContentScope{Roles: []string{"go-implementation-engineer"}, IssueRefs: []string{}}, PlanGraph: &binding})
		paths := diagnosticPaths(ar.UpdateDiagnostics{Diagnostics: d})
		if d.Valid || !strings.Contains(paths, "storyrequest.draft.dependencies.story_keys") {
			t.Fatalf("dependencies %v were accepted or unnamed: %s", wrong, paths)
		}
	}
	right := integrationCreateRequest("story-001")
	right.Draft.Dependencies.StoryKeys = []string{"story-002"}
	if d := ar.ValidateResult(integrationDone(right, "x"), ar.Context{SchemaVersion: ar.SchemaVersion, Mode: "plan", WorkflowVersion: store.WorkflowSeparated, PlanScope: planbrief.ContentScope{Roles: []string{"go-implementation-engineer"}, IssueRefs: []string{}}, PlanGraph: &binding}); !d.Valid {
		t.Fatalf("the promoted dependencies were refused: %s", diagnosticPaths(ar.UpdateDiagnostics{Diagnostics: d}))
	}
}

// AC-043: the largest set gets every story issue at promotion, in one run, and the
// only story owed a first draft is the one that depends on nothing.
func TestPlanCreation_TwelveStoriesGetTheirIssuesAtPromotion(t *testing.T) {
	var entries []planbrief.PlanStorySetEntry
	for i := 1; i <= 12; i++ {
		e := planbrief.PlanStorySetEntry{ID: fmt.Sprintf("s%d", i), Name: fmt.Sprintf("Story %d", i), Intent: fmt.Sprintf("Deliver %d", i)}
		if i < 12 {
			e.DependsOn = []string{fmt.Sprintf("s%d", i+1)}
		}
		entries = append(entries, e)
	}
	c := &creationAgent{}
	f, remote, base, _, promote := creationFixtureFor(t, c, entries)
	_, err := Execute(t.Context(), f.opts, promote)
	mustProgress(t, err)
	drafts, err := f.db.ListPlanStoryDrafts(t.Context(), f.ref().String())
	mustProgress(t, err)
	if remote.creates != 12 || len(drafts) != 12 || len(c.order) != 0 {
		t.Fatalf("creates %d rows %d drafted %v", remote.creates, len(drafts), c.order)
	}
	for _, d := range drafts {
		if d.IssueID == 0 {
			t.Fatalf("%s has no issue", d.Key)
		}
	}
	if _, ran, err := runOwedStory(t, f, base); !ran || err != nil || !slices.Equal(c.order, []string{"story-012"}) {
		t.Fatalf("first draft: ran=%v err=%v order=%v", ran, err, c.order)
	}
	if _, ran, _ := runOwedStory(t, f, base); ran {
		t.Fatalf("a dependant was drafted before its upstream story review: %v", c.order)
	}
}

// AC-029: a session that already has stories skips the set stage and adds stories
// through the existing new-story path; no set is proposed for it.
func TestPlanCreation_SessionWithStoriesSkipsTheSetStage(t *testing.T) {
	c := &creationAgent{}
	var fx *fixture
	f, remote, base, _, _ := graphPlanAgentFixture(t, c.behave(t, &fx))
	fx = f
	mustProgress(t, func() error { _, err := Execute(t.Context(), f.opts, base); return err }())
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "plan")
	mustProgress(t, err)
	transitions, err := f.db.ListGraphTransitions(t.Context(), instance.ID)
	mustProgress(t, err)
	if len(transitions) < 2 || transitions[0].FromNode != "select_set" || transitions[0].ToNode != "select_story" {
		t.Fatalf("the set stage was not skipped: %+v", transitions)
	}
	executeGraphPlan(t, f, nextTrigger(base, 960, "/plan-new-story"))
	state := graphPlanState(t, f)
	if state.StorySet != nil || len(state.Stories) != 2 || remote.creates != 2 || !slices.Equal(c.order, []string{"story-001", "story-002"}) {
		t.Fatalf("set=%v stories=%d creates=%d order=%v", state.StorySet, len(state.Stories), remote.creates, c.order)
	}
	requireOwedStages(t, f, 0)
}
