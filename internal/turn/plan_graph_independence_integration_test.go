package turn

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

// T-027 (AC-047): once every promoted story has its first draft, each story goes
// through its own story, technical and acceptance reviews on the owner's approval
// of that story alone. A story that waits for its owner blocks no other story and
// no set-wide approval is needed, but independence does not bypass the binding
// rules: an approval is for one story, one stage, one candidate and the current
// content of everything it depends on.
//
// The drafted set is the three-story one the creation tests use: story-001 depends
// on story-002, and story-003 depends on both. Fake agents prove orchestration and
// the store's rules only; they say nothing about the quality of real reviews.

// indepFixture is a session whose three promoted stories all have a first draft
// and each waits for its own story review.
func indepFixture(t *testing.T) (*fixture, *graphPlanHTTP, Request, *fakeForge, *creationAgent) {
	t.Helper()
	c := &creationAgent{}
	f, remote, base, forge, promote := creationFixture(t, c)
	if _, err := Execute(t.Context(), f.opts, promote); err != nil {
		t.Fatalf("promotion: %v logs=%s", err, f.logs.String())
	}
	drainGraphSteps(t, f, base)
	requireCreated(t, f, remote)
	for _, item := range graphPlanState(t, f).Stories {
		if item.Wait == nil || item.Wait.Kind != "review" || item.Wait.Stage != "story" || item.Stage != "story" {
			t.Fatalf("%s is not waiting for its first review: %+v", item.Key, item)
		}
	}
	return f, remote, base, forge, c
}

// indepFingerprint is everything durable and visible about one story: its saved
// state, its store row, its sub-issue on the forge and every comment posted on it.
// Two equal fingerprints mean the story did not change at all.
func indepFingerprint(t *testing.T, f *fixture, remote *graphPlanHTTP, forge *fakeForge, key string) string {
	t.Helper()
	state := graphPlanState(t, f)
	var item *planbrief.PlanStoryState
	for i := range state.Stories {
		if state.Stories[i].Key == key {
			item = &state.Stories[i]
		}
	}
	if item == nil {
		t.Fatalf("no story %s", key)
	}
	stateJSON, err := json.Marshal(item)
	mustProgress(t, err)
	row, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), key)
	mustProgress(t, err)
	rowJSON, err := json.Marshal(row)
	mustProgress(t, err)
	remote.mu.Lock()
	childJSON, err := json.Marshal(remote.children[row.IssueNumber])
	remote.mu.Unlock()
	mustProgress(t, err)
	var comments []string
	for _, post := range forge.all() {
		if strings.Contains(post.path, fmt.Sprintf("/issues/%d/comments", row.IssueNumber)) {
			comments = append(comments, post.body)
		}
	}
	return fmt.Sprintf("state=%s\nrow=%s\nchild=%s\ncomments=%q", stateJSON, rowJSON, childJSON, comments)
}

// indepApprove has the owner approve one story's current wait with the exact
// command, and returns the reply's trigger so a test can replay it.
func indepApprove(t *testing.T, f *fixture, base Request, key string, id int64) Request {
	t.Helper()
	req := graphPlanApproveKey(t, f, base, key, id)
	executeGraphPlan(t, f, req)
	return req
}

func indepStory(t *testing.T, f *fixture, key string) planbrief.PlanStoryState {
	t.Helper()
	for _, item := range graphPlanState(t, f).Stories {
		if item.Key == key {
			return item
		}
	}
	t.Fatalf("no story %s", key)
	return planbrief.PlanStoryState{}
}

// advanceIndependently walks one story through its story, technical and
// acceptance reviews and requires, after every single approval, that each held
// story is exactly as it was and that no set-wide state came into being.
func advanceIndependently(t *testing.T, f *fixture, remote *graphPlanHTTP, base Request, forge *fakeForge, advance string, held []string, id int64) {
	t.Helper()
	want := map[string]string{}
	for _, key := range held {
		want[key] = indepFingerprint(t, f, remote, forge, key)
	}
	for _, stage := range []string{"story", "technical", "acceptance"} {
		if w := indepStory(t, f, advance).Wait; w == nil || w.Stage != stage {
			t.Fatalf("%s should wait for its %s review: %+v", advance, stage, w)
		}
		id++
		indepApprove(t, f, base, advance, id)
		for _, key := range held {
			if got := indepFingerprint(t, f, remote, forge, key); got != want[key] {
				t.Fatalf("%s changed while %s was approved at %s:\nbefore %s\nafter  %s", key, advance, stage, want[key], got)
			}
		}
		state := graphPlanState(t, f)
		if state.AssembledWait != "" || state.AssembledApproval != "" || state.PublishedRevision != "" || state.SavedRevision != "" {
			t.Fatalf("a partial set produced a set-wide wait or approval after %s: %+v", stage, state)
		}
	}
	item := indepStory(t, f, advance)
	if item.Stage != "acceptance" || len(item.Approvals) != 3 || item.Wait != nil {
		t.Fatalf("%s did not finish its three reviews: %+v", advance, item)
	}
	feature, err := f.db.GetManagedFeature(t.Context(), f.ref().String())
	if err == nil && feature.Ready {
		t.Fatal("a partly approved set marked the feature ready")
	}
}

// AC-047: the dependency story advances through all three of its own reviews while
// both of its dependants wait for their first review. The waiting stories are
// byte-for-byte unchanged after every approval, technical and acceptance included,
// there is no set-wide barrier, and the agent ran exactly the two later stages of
// the advancing story and nothing for the held ones.
func TestPlanIndependence_DependencyAdvancesThroughAllReviewsWhileDependantsWait(t *testing.T) {
	f, remote, base, forge, c := indepFixture(t)
	drafting := len(c.order)
	advanceIndependently(t, f, remote, base, forge, "story-002", []string{"story-001", "story-003"}, 950)

	if !slices.Equal(c.order[drafting:], []string{"story-002", "story-002"}) || !slices.Equal(c.stage[drafting:], []string{"technical", "acceptance"}) {
		t.Fatalf("later agent runs %v %v", c.order[drafting:], c.stage[drafting:])
	}
	if remote.creates != 3 {
		t.Fatalf("an approval created an issue: %d", remote.creates)
	}
	// The held stories are still approvable with the very commands they were waiting
	// for: nothing the other story did outdated them, so they progress on their own.
	for i, key := range []string{"story-001", "story-003"} {
		indepApprove(t, f, base, key, int64(970+i))
		if item := indepStory(t, f, key); item.Stage != "technical" || len(item.Approvals) != 1 {
			t.Fatalf("%s could not proceed on its own approval: %+v", key, item)
		}
	}
}

// AC-047: the same holds the other way round. The story that depends on another
// advances through all three reviews while the story it depends on, and the one
// depending on both, wait: no dependency approval barrier and no set-wide barrier
// exists, and the waiting ones stay exactly as they were.
func TestPlanIndependence_DependantAdvancesThroughAllReviewsWhileItsDependencyWaits(t *testing.T) {
	f, remote, base, forge, c := indepFixture(t)
	drafting := len(c.order)
	advanceIndependently(t, f, remote, base, forge, "story-001", []string{"story-002", "story-003"}, 950)
	if !slices.Equal(c.order[drafting:], []string{"story-001", "story-001"}) || !slices.Equal(c.stage[drafting:], []string{"technical", "acceptance"}) {
		t.Fatalf("later agent runs %v %v", c.order[drafting:], c.stage[drafting:])
	}
}

// AC-047: the last story, which depends on the other two, advances alone as well.
func TestPlanIndependence_StoryDependingOnBothAdvancesWhileBothWait(t *testing.T) {
	f, remote, base, forge, _ := indepFixture(t)
	advanceIndependently(t, f, remote, base, forge, "story-003", []string{"story-001", "story-002"}, 950)
}

// Independence does not let an approval leave its own story, stage or candidate.
// While every story waits, a reply for a stage the story is not at, for a
// candidate it does not have, or carrying another story's wait is refused and
// changes nothing anywhere, and a legitimate approval of one story does not turn a
// consumed or outdated command of the same or another story into a valid one.
func TestPlanIndependence_ApprovalCannotSkipAStageOrBorrowAnotherStoryWait(t *testing.T) {
	f, remote, base, forge, _ := indepFixture(t)
	snapshot := func() string {
		var parts []string
		for _, key := range []string{"story-001", "story-002", "story-003"} {
			parts = append(parts, indepFingerprint(t, f, remote, forge, key))
		}
		state, _ := json.Marshal(graphPlanState(t, f))
		return strings.Join(parts, "\n") + "\n" + string(state)
	}
	refused := func(name string, mutate func(*Request)) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			before := snapshot()
			req := graphPlanApproveKey(t, f, base, "story-001", 960)
			mutate(&req)
			out, err := Execute(t.Context(), f.opts, req)
			if err == nil || !strings.Contains(err.Error(), "stale or wrong-story Plan reply") {
				t.Fatalf("the reply was not refused as stale or for another story: err=%v out=%+v", err, out.Err)
			}
			if after := snapshot(); after != before {
				t.Fatalf("a refused reply changed state (err=%v out=%+v):\nbefore %s\nafter  %s", err, out.Err, before, after)
			}
			if item := indepStory(t, f, "story-001"); item.Stage != "story" || len(item.Approvals) != 0 {
				t.Fatalf("story-001 advanced on a refused reply: %+v", item)
			}
		})
	}
	// Skipping ahead: the story waits at its story review, the reply names its technical stage.
	refused("technical reply at a story review", func(r *Request) {
		r.Trigger.PlanReply.Stage = "technical"
		r.Trigger.PlanReply.Body = "/approve-plan story-001 technical " + r.Trigger.PlanReply.Candidate
		r.Trigger.Body, r.Command.Text = r.Trigger.PlanReply.Body, r.Trigger.PlanReply.Body
	})
	refused("acceptance reply at a story review", func(r *Request) {
		r.Trigger.PlanReply.Stage = "acceptance"
		r.Trigger.PlanReply.Body = "/approve-plan story-001 acceptance " + r.Trigger.PlanReply.Candidate
		r.Trigger.Body, r.Command.Text = r.Trigger.PlanReply.Body, r.Trigger.PlanReply.Body
	})
	// Borrowing: story-001's command carrying story-002's wait, candidate and issue.
	refused("another story's wait", func(r *Request) {
		other := indepStory(t, f, "story-002").Wait
		r.Trigger.PlanReply.WaitID, r.Trigger.PlanReply.Candidate, r.Trigger.PlanReply.IssueID = other.ID, other.Candidate, other.IssueID
		r.Trigger.PlanReply.Body = "/approve-plan story-001 story " + other.Candidate
		r.Trigger.Body, r.Command.Text = r.Trigger.PlanReply.Body, r.Trigger.PlanReply.Body
	})
	refused("a candidate the story does not have", func(r *Request) {
		r.Trigger.PlanReply.Candidate = "plan-9-9"
		r.Trigger.PlanReply.Body = "/approve-plan story-001 story plan-9-9"
		r.Trigger.Body, r.Command.Text = r.Trigger.PlanReply.Body, r.Trigger.PlanReply.Body
	})

	// A consumed approval is not a new one: replaying story-002's story approval after it
	// advanced does not advance it again, and story-001 stays where it was.
	first := indepApprove(t, f, base, "story-002", 961)
	if item := indepStory(t, f, "story-002"); item.Stage != "technical" || len(item.Approvals) != 1 {
		t.Fatalf("story-002 after its story approval: %+v", item)
	}
	held := indepFingerprint(t, f, remote, forge, "story-001")
	mid, _ := json.Marshal(graphPlanState(t, f))
	if out, err := Execute(t.Context(), f.opts, first); err != nil || out.Err != nil {
		t.Fatalf("replaying a consumed approval is a no-op, not a failure: err=%v out=%v", err, out.Err)
	}
	after, _ := json.Marshal(graphPlanState(t, f))
	if string(mid) != string(after) || indepFingerprint(t, f, remote, forge, "story-001") != held {
		t.Fatalf("replaying a consumed approval changed state:\n%s\n%s", mid, after)
	}
	// The story-stage command is outdated once its story is at technical: it cannot skip a stage twice.
	stale := first
	stale.Trigger.CommentID = 962
	stale.Trigger.PlanReply = &planbrief.PlanOwnerReply{}
	*stale.Trigger.PlanReply = *first.Trigger.PlanReply
	stale.Trigger.PlanReply.ID = 962
	if _, err := Execute(t.Context(), f.opts, stale); err == nil || !strings.Contains(err.Error(), "stale or wrong-story Plan reply") {
		t.Fatalf("an outdated command was not refused as stale: %v", err)
	}
	if item := indepStory(t, f, "story-002"); item.Stage != "technical" || len(item.Approvals) != 1 {
		t.Fatalf("an outdated command advanced story-002: %+v", item)
	}
}

// Independence does not let a dependant keep an approval that was given against
// dependency content which has since changed. story-001 (which depends on
// story-002) is approved at its story review and moves to its technical review;
// then the owner edits story-002's sub-issue in substance. Story-002 goes back to
// its story review and story-001, and story-003 which also depends on it, lose the
// approvals and waits built on the old content, so their old commands are refused;
// there is no path by which the earlier independent progress carries over.
func TestPlanIndependence_DependencyChangeOutdatesTheDependantsProgressAndWaits(t *testing.T) {
	f, remote, base, _, _ := indepFixture(t)
	indepApprove(t, f, base, "story-001", 950)
	dependant := indepStory(t, f, "story-001")
	if dependant.Stage != "technical" || dependant.Wait == nil || dependant.Wait.Stage != "technical" || len(dependant.Approvals) != 1 {
		t.Fatalf("story-001 after its first approval: %+v", dependant)
	}
	stale := graphPlanApproveKey(t, f, base, "story-001", 951)
	third := indepStory(t, f, "story-003")
	if third.Wait == nil || third.Wait.Stage != "story" {
		t.Fatalf("story-003 should wait at its story review: %+v", third)
	}

	// The owner changes the substance of story-002's sub-issue.
	binding, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-002")
	mustProgress(t, err)
	remote.mu.Lock()
	body, _ := remote.children[binding.IssueNumber]["body"].(string)
	if !strings.Contains(body, "I want shape intent") {
		remote.mu.Unlock()
		t.Fatalf("the fixture's sub-issue lacks the expected intent: %q", body)
	}
	remote.children[binding.IssueNumber]["body"] = strings.Replace(body, "I want shape intent", "I want a different intent", 1)
	remote.mu.Unlock()
	client, ok := f.opts.SubIssueCreator.(PlanStoryForge)
	if !ok {
		t.Fatal("fixture requires PlanStoryForge")
	}
	issue, err := client.GetIssue(t.Context(), github.Repo{Owner: "acme", Name: "acme-api"}, binding.IssueNumber)
	mustProgress(t, err)
	mustProgress(t, ReconcilePlanChildEdit(t.Context(), f.db, f.ref().String(), 123, 1230, issue, planbrief.ContentScope{Roles: []string{"go-implementation-engineer"}}))

	changed := indepStory(t, f, "story-002")
	if changed.Stage != "story" || changed.Wait != nil || len(changed.Approvals) != 0 {
		t.Fatalf("the edited story was not reopened at its story review: %+v", changed)
	}
	for _, key := range []string{"story-001", "story-003"} {
		item := indepStory(t, f, key)
		if item.Stage != "story" || item.Wait != nil || len(item.Approvals) != 0 {
			t.Fatalf("%s kept progress or a wait built on the old dependency content: %+v", key, item)
		}
	}
	// The command story-001 was waiting for is refused and moves nothing.
	before, _ := json.Marshal(graphPlanState(t, f))
	out, err := Execute(t.Context(), f.opts, stale)
	if err == nil || !strings.Contains(err.Error(), "stale or wrong-story Plan reply") {
		t.Fatalf("the outdated approval was not refused as stale: err=%v out=%+v", err, out.Err)
	}
	after, _ := json.Marshal(graphPlanState(t, f))
	if string(before) != string(after) {
		t.Fatalf("an outdated approval changed state:\n%s\n%s", before, after)
	}
	if item := indepStory(t, f, "story-001"); item.Stage != "story" || len(item.Approvals) != 0 {
		t.Fatalf("an approval given against replaced dependency content advanced story-001: %+v", item)
	}
}
