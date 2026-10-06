package turn

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// parentPostsWith returns the bodies posted on the parent issue that contain needle.
func parentPostsWith(f *fixture, forge *fakeForge, needle string) []string {
	var out []string
	parent := fmt.Sprintf("/issues/%d/comments", f.ref().Issue)
	for _, post := range forge.all() {
		if strings.HasSuffix(post.path, parent) && strings.Contains(post.body, needle) {
			out = append(out, post.body)
		}
	}
	return out
}

// C1: once every promoted story has a sub-issue and a draft and waits for the
// owner's review, the parent's next run posts exactly one comment that lists every
// story with its sub-issue and explains how to continue. Rerunning adds no second
// comment. The set is independent: a dependant is drafted only after its upstream
// story review, so a set with dependencies never has every story waiting unapproved.
func TestPlanNotices_StoriesCreatedGuideIsPostedOnceOnTheParent(t *testing.T) {
	c := &creationAgent{}
	f, _, base, forge, promote := creationFixtureFor(t, c, []planbrief.PlanStorySetEntry{
		{ID: "s1", Name: "Storage", Intent: "Deliver Storage"},
		{ID: "s2", Name: "Schema", Intent: "Deliver Schema"},
		{ID: "s3", Name: "Screens", Intent: "Deliver Screens"},
	})
	executeGraphPlan(t, f, promote)
	if guides := parentPostsWith(f, forge, "stories are ready for your review"); len(guides) != 0 {
		t.Fatalf("the guide was posted before every story existed: %v", guides)
	}
	drainGraphSteps(t, f, base)
	runParent(t, f, base)
	guides := parentPostsWith(f, forge, "stories are ready for your review")
	if len(guides) != 1 {
		t.Fatalf("%d guides on the parent, want 1", len(guides))
	}
	guide := guides[0]
	drafts, err := f.db.ListPlanStoryDrafts(t.Context(), f.ref().String())
	mustProgress(t, err)
	for _, d := range drafts {
		link := fmt.Sprintf("[#%d](https://github.com/%s/%s/issues/%d)", d.IssueNumber, f.ref().Owner, f.ref().Repo, d.IssueNumber)
		if !strings.Contains(guide, "| `"+d.Key+"` | Story "+d.Key+" |") || !strings.Contains(guide, link) {
			t.Fatalf("guide lacks the row for %s with %s:\n%s", d.Key, link, guide)
		}
	}
	for _, want := range []string{"| ID | Name | Description | Dependencies | Sub-issue |", "Deliver Storage", "/approve-plan", "in plain words", "technical design", "acceptance", "`/approve-plan-set`"} {
		if !strings.Contains(guide, want) {
			t.Fatalf("guide lacks %q:\n%s", want, guide)
		}
	}
	// Another tick and an owner comment on the parent find only waits: the one
	// guide is resumed, never posted again.
	runParent(t, f, base)
	calls := discussionRunner(t, f, "set", "What happens now?")
	executeGraphPlan(t, f, nextTrigger(base, 991, "What happens now?"))
	if *calls != 1 || len(parentPostsWith(f, forge, "This plan delivers bounded story shaping.")) != 1 {
		t.Fatal("parent question received no agent answer")
	}
	if again := parentPostsWith(f, forge, "stories are ready for your review"); len(again) != 1 {
		t.Fatalf("rerunning posted %d guides", len(again))
	}
}

// completeSingleStory drives the single-story fixture through its three stage
// approvals, which opens the final plan review on the parent.
func completeSingleStory(t *testing.T) (*fixture, Request, *fakeForge, planbrief.PlanState) {
	t.Helper()
	f, _, base, _, forge := graphPlanFixtureWithForge(t, "agent")
	executeGraphPlan(t, f, base)
	for i := range 3 {
		executeGraphPlan(t, f, graphPlanApprove(t, f, base, base.Trigger.CommentID+int64(i+1)))
	}
	runParent(t, f, base)
	state := graphPlanState(t, f)
	if state.AssembledWait == "" {
		t.Fatal("final plan review did not open")
	}
	return f, base, forge, state
}

// C3: the final plan review is the story set table with sub-issue links and the
// approved stages, carries the exact approval command and no internal terms.
func TestPlanNotices_AssembledReviewShowsTheStoryTable(t *testing.T) {
	f, _, forge, state := completeSingleStory(t)
	reviews := parentPostsWith(f, forge, "Final plan review")
	if len(reviews) != 1 {
		t.Fatalf("%d final reviews on the parent", len(reviews))
	}
	review := reviews[0]
	binding, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-001")
	mustProgress(t, err)
	for _, want := range []string{
		"| ID | Name | Description | Dependencies |",
		"| `story-001` | Bounded Plan | Unclear plan | none |",
		fmt.Sprintf("[#%d](https://github.com/%s/%s/issues/%d)", binding.IssueNumber, f.ref().Owner, f.ref().Repo, binding.IssueNumber),
		"story, technical, acceptance",
		"`/approve-plan-set " + state.AssembledWait + "`",
	} {
		if !strings.Contains(review, want) {
			t.Fatalf("final review lacks %q:\n%s", want, review)
		}
	}
	if rows := strings.Count(review, "| `story-"); rows != 1 {
		t.Fatalf("%d story rows, want one per active story", rows)
	}
	for _, internal := range []string{"Current shared decisions", "candidate", state.SharedRevision + "\n"} {
		if strings.Contains(review, internal) {
			t.Fatalf("final review contains internal term %q:\n%s", internal, review)
		}
	}
}

// C2: a successful final approval produces one parent confirmation that names
// the stories and the next step; a replay posts nothing new.
func TestPlanNotices_FinalApprovalIsConfirmedOnceOnTheParent(t *testing.T) {
	f, base, forge, state := completeSingleStory(t)
	final := base
	final.Trigger = Trigger{CommentID: base.Trigger.CommentID + 10, Body: "/approve-plan-set " + state.AssembledWait, AuthorID: 7}
	final.Command = Command{Kind: KindFreeText, Text: final.Trigger.Body}
	executeGraphPlan(t, f, final)
	feature, err := f.db.GetManagedFeature(t.Context(), f.ref().String())
	mustProgress(t, err)
	if !feature.Ready {
		t.Fatal("the plan did not become ready")
	}
	confirmations := parentPostsWith(f, forge, "Plan approved and ready")
	if len(confirmations) != 1 {
		t.Fatalf("%d confirmations on the parent, want 1", len(confirmations))
	}
	for _, want := range []string{"| `story-001` | Bounded Plan |", "`/implement`"} {
		if !strings.Contains(confirmations[0], want) {
			t.Fatalf("confirmation lacks %q:\n%s", want, confirmations[0])
		}
	}
	// A replay of the approval and a repeat of the confirmation post nothing new.
	_, _ = Execute(t.Context(), f.opts, final)
	saved := graphPlanState(t, f)
	r := &planGraphRuntime{opts: f.opts, req: final, result: Result{Ref: f.ref()}, parentRef: f.ref(), parentNumber: f.ref().Issue, scope: planbrief.TaskOwnerScope()}
	mustProgress(t, r.planReady(t.Context(), saved))
	if again := parentPostsWith(f, forge, "Plan approved and ready"); len(again) != 1 {
		t.Fatalf("a replay posted %d confirmations", len(again))
	}
}

// C5: a stale final approval is a refusal on the parent that quotes the current
// command, not a generic stop, and it approves nothing.
func TestPlanNotices_StaleFinalApprovalIsRefusedWithTheCurrentCommand(t *testing.T) {
	f, base, _, state := completeSingleStory(t)
	stale := base
	stale.Trigger = Trigger{CommentID: base.Trigger.CommentID + 20, Body: "/approve-plan-set " + strings.Repeat("c", 64), AuthorID: 7}
	stale.Command = Command{Kind: KindFreeText, Text: stale.Trigger.Body}
	out, err := Execute(t.Context(), f.opts, stale)
	if err != nil {
		t.Fatalf("a stale final approval is a refusal, not an error: %v", err)
	}
	if out.Disposition != DispositionPrepareFailed || !out.ConsumesComment() || !out.Reportable() || !strings.Contains(out.Reason, "`/approve-plan-set "+state.AssembledWait+"`") {
		t.Fatalf("disposition %v reason %q", out.Disposition, out.Reason)
	}
	after := graphPlanState(t, f)
	if after.AssembledApproval != "" || after.AssembledWait != state.AssembledWait {
		t.Fatalf("a refused final approval changed the plan: %+v", after)
	}
}

// C4: a stale story-stage approval on a sub-issue whose story waits at
// acceptance is answered on that sub-issue, in plain words, with the command
// that works now.
func TestPlanNotices_StaleChildReplyIsAnsweredOnTheChildWithTheCurrentCommand(t *testing.T) {
	f, _, _, _, forge := graphPlanFixtureWithForge(t, "agent")
	s := planbrief.PlanState{SharedRevision: "initial", Stories: []planbrief.PlanStoryState{{Key: "story-009", Stage: "acceptance", Candidate: "plan-55-600", Approvals: map[string]string{}, Draft: planbrief.PlanDraft{Key: "story-009"}}}}
	revision, err := s.ReviewRevision("story-009", "acceptance")
	mustProgress(t, err)
	s.Stories[0].Wait = &planbrief.PlanWait{ID: "plan-review:1:2:acceptance_review", Stage: "acceptance", Candidate: "plan-55-600", Revision: revision, Kind: "review", IssueID: 3980}
	reason := s.PlanReplyReason("story-009", "/approve-plan story-009 story plan-35-412")
	if !strings.Contains(reason, "`/approve-plan story-009 acceptance plan-55-600`") {
		t.Fatalf("reason lacks the current command: %q", reason)
	}
	if _, err = f.opts.ResearchReporter.ReportPlanReply(t.Context(), f.ref(), f.session.ID, "story-009", 398, 5855, reason); err != nil {
		t.Fatal(err)
	}
	var onChild, onParent int
	for _, post := range forge.all() {
		if !strings.Contains(post.body, "/approve-plan story-009 acceptance plan-55-600") {
			continue
		}
		switch {
		case strings.HasSuffix(post.path, "/issues/398/comments"):
			onChild++
			if strings.Contains(post.body, "acknowledged only after") || strings.Contains(post.body, "current candidate") || !strings.Contains(post.body, "Your reply on #398 did not change story story-009") {
				t.Fatalf("explanation is not in plain words: %s", post.body)
			}
		case strings.HasSuffix(post.path, fmt.Sprintf("/issues/%d/comments", f.ref().Issue)):
			onParent++
		}
	}
	if onChild != 1 || onParent != 0 {
		t.Fatalf("explanation posted %d times on the child and %d on the parent", onChild, onParent)
	}
}

// C6: every edge that ends the shipped Plan graph at the stop terminal without a
// failure comment has an owner explanation, and a loop that runs out of passes
// says so and how to resume.
func TestPlanNotices_EveryStopTerminalHasAnExplanation(t *testing.T) {
	specs, err := PlanSpecs()
	mustProgress(t, err)
	definition, err := teamsource.CapturePlan(t.Context(), specs)
	mustProgress(t, err)
	loops := 0
	for _, e := range definition.Profile.Edges {
		if e.To != "stopped" || e.From == PlanFailureNode {
			continue
		}
		text, ok := planStopText(definition.Profile, e.From, e.On)
		if !ok || text == "" {
			t.Fatalf("edge %s/%s -> stopped has no owner explanation", e.From, e.On)
		}
		if strings.HasSuffix(e.From, "_loop") {
			loops++
			stage := strings.TrimSuffix(e.From, "_loop")
			if !strings.Contains(text, "the "+stage+" stage ran out of passes") || !strings.Contains(text, "reply on this issue") {
				t.Fatalf("out-of-passes text for %s: %s", e.From, text)
			}
		}
	}
	if loops != 4 {
		t.Fatalf("%d out-of-passes edges checked, want the four stage loops", loops)
	}
	if _, ok := planStopText(definition.Profile, PlanFailureNode, "published"); ok {
		t.Fatal("the failure comment path would be explained twice")
	}
}

// C6: an instance that stopped because a stage loop ran out of passes gets one
// parent comment, however often the stop is reported.
func TestPlanNotices_OutOfPassesStopIsReportedOnce(t *testing.T) {
	f, _, base, _, forge := graphPlanFixtureWithForge(t, "agent")
	specs, err := PlanSpecs()
	mustProgress(t, err)
	definition, err := teamsource.CapturePlan(t.Context(), specs)
	mustProgress(t, err)
	pin, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "plan", ProfileVersion: definition.Profile.ProfileVersion, Path: "plan/plan.dot", Digest: definition.Profile.Digest, TeamSnapshotDigest: definition.Digest})
	mustProgress(t, err)
	now := time.Now()
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: pin.ID, CurrentNode: "stopped", StartedAt: now})
	mustProgress(t, err)
	_, err = f.db.RecordGraphTransition(t.Context(), store.GraphTransition{InstanceID: instance.ID, FromNode: "technical_loop", ToNode: "stopped", OnOutcome: "false", At: now})
	mustProgress(t, err)
	r := &planGraphRuntime{opts: f.opts, req: base, result: Result{Ref: f.ref()}, parentRef: f.ref(), parentNumber: f.ref().Issue, definition: definition}
	for range 2 {
		mustProgress(t, r.reportStop(t.Context(), instance.ID))
	}
	if stops := parentPostsWith(f, forge, "the technical stage ran out of passes"); len(stops) != 1 {
		t.Fatalf("%d out-of-passes comments, want 1", len(stops))
	}
}

// C7: the node binding names skills by name and carries no path.
func TestPlanNotices_SkillBindingNamesSkillsWithoutPaths(t *testing.T) {
	t.Parallel()
	got := skillNamesBinding([]string{"skills/plan-acceptance-shaping", "plan-story-shaping"})
	if !strings.Contains(got, "plan-acceptance-shaping, plan-story-shaping") || strings.Contains(got, "skills/") || strings.Contains(got, "SKILL.md") {
		t.Fatalf("binding %q", got)
	}
	if skillNamesBinding(nil) != "" {
		t.Fatal("a node without skills gained a binding")
	}
}
