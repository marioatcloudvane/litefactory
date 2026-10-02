package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// T-024 (AC-024, AC-025, AC-042, AC-043, AC-044, AC-046): the whole set flow, from
// an empty scope to one first-draft sub-issue per promoted story, over the real
// SQLite store, the real shared result service and the real story effect against
// a fake forge. A fake agent is the only fake.
//
// These tests prove orchestration and recovery only: revisioned publication,
// promotion authority, key and ordinal reservation, drafting order and the
// resumption behavior. They do not prove that a real agent decomposes a real scope
// well, and they are not the owner-run real tryout (AC-045/AC-046): that stays an
// unsatisfied parent obligation, and the hold-back on graph Plan is not touched.
//
// The displayed set lists three deliverables whose FIRST entry depends on a LATER
// one: a (Alpha) depends on c (Charlie), b (Bravo) is free. Keys and public
// ordinals follow the displayed order (a=001, b=002, c=003), but the creation
// order follows the rule dependencies first, displayed order among the eligible:
// b and c are eligible at the start and b is displayed first, then c, and a last.

var setFlowStoryPattern = regexp.MustCompile(`Factory-selected story: ([a-z0-9-]+); stage: ([a-z]+);`)

// setFlowSets is what the fake set agent proposes on its first and second
// invocation. The second differs in content, so it has a different revision, and
// keeps the first entry depending on the later one.
func setFlowSets() [][]ar.PlanStorySetEntry {
	return [][]ar.PlanStorySetEntry{
		{setEntry("a", "Alpha", "c"), setEntry("b", "Bravo"), setEntry("c", "Charlie")},
		{setEntry("a", "Alpha", "c"), setEntry("b", "Bravo revised"), setEntry("c", "Charlie")},
	}
}

func setFlowEntries(in []ar.PlanStorySetEntry) []planbrief.PlanStorySetEntry {
	out := make([]planbrief.PlanStorySetEntry, 0, len(in))
	for _, e := range in {
		out = append(out, planbrief.PlanStorySetEntry{ID: e.ID, Name: e.Name, Intent: e.Intent, DependsOn: e.DependsOn})
	}
	return out
}

// setFlowSnapshot is what the store and the forge showed at one moment.
type setFlowSnapshot struct {
	creates  int
	ordinals map[string]int64
	issueIDs map[string]int64
}

// setFlowRun counts the agent invocations by kind and records what was true when
// the first story draft was requested, which is before any sub-issue exists.
type setFlowRun struct {
	setCalls, storyCalls int
	atFirstStory         *setFlowSnapshot
}

func setFlowSnap(t *testing.T, f *fixture, remote *graphPlanHTTP) *setFlowSnapshot {
	t.Helper()
	drafts, err := f.db.ListPlanStoryDrafts(t.Context(), f.ref().String())
	mustProgress(t, err)
	snap := &setFlowSnapshot{ordinals: map[string]int64{}, issueIDs: map[string]int64{}}
	for _, d := range drafts {
		snap.ordinals[d.Key], snap.issueIDs[d.Key] = d.Ordinal, d.IssueID
	}
	remote.mu.Lock()
	snap.creates = remote.creates
	remote.mu.Unlock()
	return snap
}

// setFlowFixture is a graph Plan session with an empty scope. One runner plays
// both agents: the set agent proposes sets[n] on its n-th call (repeating the last
// one) and quotes the exact promotion command in its own comment, and the story
// agent is the creation cycle's fake.
func setFlowFixture(t *testing.T, c *creationAgent, sets [][]ar.PlanStorySetEntry) (*fixture, *graphPlanHTTP, Request, *fakeForge, *setFlowRun) {
	t.Helper()
	run := &setFlowRun{}
	var f *fixture
	var remote *graphPlanHTTP
	var base Request
	var forge *fakeForge
	story := c.behave(t, &f)
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		if req.Graph == nil || req.Delivery == nil || req.Mode != "plan" {
			t.Fatal("Plan native graph invocation absent")
		}
		if strings.Contains(req.Prompt, "Factory-selected stage: set;") {
			run.setCalls++
			entries := sets[min(run.setCalls, len(sets))-1]
			revision := planbrief.PlanStorySetRevision(setFlowEntries(entries))
			// The agent's own comment quotes the exact command. It is text, not authority.
			result := ar.Result{Outcome: "completed", Comment: fmt.Sprintf("Proposal %d. Approved on your behalf: %s", run.setCalls, promoteCommand(revision)), Payload: ar.Payload{Plan: &ar.Plan{StorySet: &ar.PlanStorySet{Entries: entries}}}}
			envelope, err := json.Marshal(ar.ResearchNode{Version: 1, Response: "done", Result: result})
			mustProgress(t, err)
			submitLiveResult(t, req, string(envelope))
			return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: string(envelope), FinishedAt: time.Now()}, nil
		}
		matched := setFlowStoryPattern.FindStringSubmatch(req.Prompt)
		if len(matched) != 3 {
			t.Fatal("factory story binding missing")
		}
		run.storyCalls++
		if run.storyCalls == 1 {
			run.atFirstStory = setFlowSnap(t, f, remote)
		}
		return story(planAgentStep{f: f, t: t, req: req, client: submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}, story: matched[1], stage: matched[2]})
	})
	f, remote, base, forge = setupGraphPlanFixture(t, runner, false)
	return f, remote, base, forge, run
}

func setFlowParentPosts(f *fixture, forge *fakeForge, needle string) []string {
	var out []string
	for _, post := range forge.all() {
		if strings.Contains(post.body, needle) && strings.Contains(post.path, fmt.Sprintf("/issues/%d/comments", f.ref().Issue)) {
			out = append(out, post.body)
		}
	}
	return out
}

// setFlowReviewSet drives the flow up to, but not including, the owner's current
// promotion: the set is proposed and published with its revision and exact
// command, the owner asks for a change so the agent proposes a set with another
// revision, and a stale approval of the older revision, an agent-quoted command
// and a non-owner command are each refused with nothing reserved or created. It
// returns the older and the current revision.
func setFlowReviewSet(t *testing.T, f *fixture, remote *graphPlanHTTP, base Request, forge *fakeForge, run *setFlowRun) (older, current string) {
	t.Helper()
	sets := setFlowSets()
	older = planbrief.PlanStorySetRevision(setFlowEntries(sets[0]))
	current = planbrief.PlanStorySetRevision(setFlowEntries(sets[1]))
	if older == current {
		t.Fatal("the fixture's two sets share a revision")
	}

	// (1) The set stage publishes the revisioned set on the parent.
	if out, err := Execute(t.Context(), f.opts, base); err != nil || out.Err != nil || !out.Succeeded() {
		t.Fatalf("set stage: err=%v result=%+v logs=%s", err, out, f.logs.String())
	}
	state := graphPlanState(t, f)
	if state.StorySet == nil || state.StorySet.Revision != older || state.StorySet.Promoted || len(state.Stories) != 0 || len(state.StorySet.Keys) != 0 {
		t.Fatalf("proposed set state %+v stories %d", state.StorySet, len(state.Stories))
	}
	if first := state.StorySet.Entries[0]; first.ID != "a" || !slices.Equal(first.DependsOn, []string{"c"}) {
		t.Fatalf("the displayed first entry does not depend on the later one: %+v", first)
	}
	posts := setFlowParentPosts(f, forge, "Proposed story set, revision")
	if len(posts) != 1 || !strings.Contains(posts[0], "`"+older+"`") || !strings.Contains(posts[0], promoteCommand(older)) || !strings.Contains(posts[0], "| `a` | Alpha |") || !strings.Contains(posts[0], "| `c` |\n") {
		t.Fatalf("the set is not shown on the parent with its revision and exact command: %q", posts)
	}
	if run.setCalls != 1 || run.storyCalls != 0 || remote.creates != 0 || len(reservedOrdinals(t, f)) != 0 {
		t.Fatalf("proposing reserved or created something: set=%d story=%d creates=%d", run.setCalls, run.storyCalls, remote.creates)
	}

	// The agent's comment quoted the current command, and that promoted nothing.
	// Nor does a comment from someone who is not the owner.
	requireRefusal(t, promoteRuntime(f, base, 799, 99, promoteCommand(older)).admitReply(t.Context()), "only the authenticated owner")

	// (2) The owner asks for a different split: the agent proposes another revision.
	if out, err := Execute(t.Context(), f.opts, nextTrigger(base, 800, "Please rename the second story.")); err != nil || out.Err != nil {
		t.Fatalf("revised set stage: err=%v result=%+v logs=%s", err, out, f.logs.String())
	}
	state = graphPlanState(t, f)
	if state.StorySet == nil || state.StorySet.Revision != current || state.StorySet.Promoted || run.setCalls != 2 {
		t.Fatalf("revised set state %+v calls %d", state.StorySet, run.setCalls)
	}
	posts = setFlowParentPosts(f, forge, "Proposed story set, revision")
	if len(posts) != 2 || !strings.Contains(posts[1], promoteCommand(current)) || strings.Contains(posts[1], promoteCommand(older)) {
		t.Fatalf("the revised set is not published with its own revision: %d posts", len(posts))
	}

	// The older revision's approval is refused with an explanation and changes nothing.
	before, _ := json.Marshal(graphPlanState(t, f))
	out, err := Execute(t.Context(), f.opts, promoteTrigger(base, 801, 7, promoteCommand(older)))
	if err != nil {
		t.Fatalf("a stale approval is an explained refusal, not an error: %v", err)
	}
	if out.Disposition != DispositionPrepareFailed || !out.ConsumesComment() || !out.Reportable() || !strings.Contains(out.Reason, "not the current story set") || !strings.Contains(out.Reason, current) || !strings.Contains(out.Reason, promoteCommand(current)) {
		t.Fatalf("stale approval: disposition %v reason %q", out.Disposition, out.Reason)
	}
	after, _ := json.Marshal(graphPlanState(t, f))
	if string(before) != string(after) || len(reservedOrdinals(t, f)) != 0 || remote.creates != 0 || run.storyCalls != 0 || run.setCalls != 2 {
		t.Fatalf("the stale approval changed state, reserved ordinals, ran an agent or created an issue:\n%s\n%s", before, after)
	}
	if set := graphPlanState(t, f).StorySet; set.Promoted || len(set.Keys) != 0 {
		t.Fatalf("the stale approval promoted: %+v", set)
	}
	return older, current
}

func setFlowIdentities(t *testing.T, f *fixture) map[string]string {
	t.Helper()
	drafts, err := f.db.ListPlanStoryDrafts(t.Context(), f.ref().String())
	mustProgress(t, err)
	out := map[string]string{}
	for _, d := range drafts {
		out[d.Key] = fmt.Sprintf("ordinal=%d issue=%d/%d candidate=%s", d.Ordinal, d.IssueID, d.IssueNumber, d.Candidate)
	}
	return out
}

// requireIdentitiesKept fails when a story that already had an issue changed its
// key, ordinal, issue identity or candidate, or when a story lost its reservation.
func requireIdentitiesKept(t *testing.T, before, after map[string]string) {
	t.Helper()
	for key, was := range before {
		now, ok := after[key]
		if !ok {
			t.Fatalf("%s lost its reservation", key)
		}
		// A reservation without an issue may gain one; a story with an issue never changes.
		if strings.Contains(was, "issue=0/0") {
			if !strings.HasPrefix(now, strings.SplitN(was, " ", 2)[0]) {
				t.Fatalf("%s ordinal changed: %q then %q", key, was, now)
			}
			continue
		}
		if now != was {
			t.Fatalf("%s identity changed: %q then %q", key, was, now)
		}
	}
}

func setFlowTitle(remote *graphPlanHTTP, number int) string {
	remote.mu.Lock()
	defer remote.mu.Unlock()
	title, _ := remote.children[number]["title"].(string)
	return title
}

// requireSetFlowCreated checks that the three promoted stories each have exactly
// one sub-issue titled with its reserved public ordinal, in the displayed order of
// keys and in the given creation order of issue numbers.
func requireSetFlowCreated(t *testing.T, f *fixture, remote *graphPlanHTTP, creationOrder []string) {
	t.Helper()
	drafts, err := f.db.ListPlanStoryDrafts(t.Context(), f.ref().String())
	mustProgress(t, err)
	if len(drafts) != 3 || remote.creates != 3 {
		t.Fatalf("rows=%d creates=%d", len(drafts), remote.creates)
	}
	byKey := map[string]int{}
	seen := map[int]bool{}
	for i, d := range drafts {
		if d.Key != fmt.Sprintf("story-%03d", i+1) || d.Ordinal != int64(i+1) || d.IssueID == 0 || seen[d.IssueNumber] {
			t.Fatalf("row %d: %+v", i, d)
		}
		seen[d.IssueNumber] = true
		byKey[d.Key] = d.IssueNumber
		if want := fmt.Sprintf("123-%03d: Story %s", i+1, d.Key); setFlowTitle(remote, d.IssueNumber) != want {
			t.Fatalf("%s is titled %q, want %q", d.Key, setFlowTitle(remote, d.IssueNumber), want)
		}
		remote.mu.Lock()
		attached := remote.attached[d.IssueNumber]
		remote.mu.Unlock()
		if !attached {
			t.Fatalf("%s is not attached to the parent", d.Key)
		}
	}
	// The forge numbered the issues in creation order, so the reserved ordinals and the
	// creation order are two different orders.
	for i := 1; i < len(creationOrder); i++ {
		if byKey[creationOrder[i-1]] >= byKey[creationOrder[i]] {
			t.Fatalf("%s was created after %s: issues %v", creationOrder[i-1], creationOrder[i], byKey)
		}
	}
}

// AC-024, AC-025, AC-042, AC-043: the full happy path. The set is shown with its
// revision and command, a stale approval is refused, the owner's current
// promotion reserves keys and ordinals in the displayed order before any issue
// exists, and exactly three agent runs create the first drafts in the order the
// dependency rule gives: b, c, then a.
func TestPlanSetFlow_PublishedRevisionStaleRefusalPromotionAndDraftingOrder(t *testing.T) {
	c := &creationAgent{}
	f, remote, base, forge, run := setFlowFixture(t, c, setFlowSets())
	_, current := setFlowReviewSet(t, f, remote, base, forge, run)

	// (3) The owner's current promotion is accepted.
	out, err := Execute(t.Context(), f.opts, promoteTrigger(base, 802, 7, promoteCommand(current)))
	if err != nil || out.Err != nil || !out.Succeeded() {
		t.Fatalf("promotion run: err=%v result=%+v logs=%s", err, out.Err, f.logs.String())
	}
	state := graphPlanState(t, f)
	set := state.StorySet
	if set == nil || !set.Promoted || set.PromotedRevision != current || set.Keys["a"] != "story-001" || set.Keys["b"] != "story-002" || set.Keys["c"] != "story-003" {
		t.Fatalf("promoted set %+v", set)
	}
	// Keys and public ordinals were reserved in the displayed order, with no issue yet,
	// before the first story was even drafted.
	if snap := run.atFirstStory; snap == nil || snap.creates != 0 || len(snap.ordinals) != 3 || snap.ordinals["story-001"] != 1 || snap.ordinals["story-002"] != 2 || snap.ordinals["story-003"] != 3 {
		t.Fatalf("at the first draft request: %+v", snap)
	} else {
		for key, id := range snap.issueIDs {
			if id != 0 {
				t.Fatalf("%s already had an issue when its first draft was requested", key)
			}
		}
	}
	if got := state.Stories[0].Draft.Dependencies.StoryKeys; !slices.Equal(got, []string{"story-003"}) {
		t.Fatalf("the first displayed story's dependency on the later one was not translated: %v", got)
	}
	// The dependency rule chose b first: a is not eligible before c, and b is displayed before c.
	if !slices.Equal(c.order, []string{"story-002"}) {
		t.Fatalf("the promotion run drafted %v", c.order)
	}
	requireOwedStages(t, f, 1)

	// (4) Two more runs, one per remaining story: c, then a.
	if steps := drainGraphSteps(t, f, base); steps != 2 {
		t.Fatalf("%d further runs, want 2", steps)
	}
	want := []string{"story-002", "story-003", "story-001"}
	if !slices.Equal(c.order, want) || run.storyCalls != 3 || run.setCalls != 2 {
		t.Fatalf("drafting order %v (story calls %d, set calls %d), want %v in three runs", c.order, run.storyCalls, run.setCalls, want)
	}
	requireSetFlowCreated(t, f, remote, want)
	requireOwedStages(t, f, 0)

	// One retained result per drafting attempt, each of its own, none revised.
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "plan")
	mustProgress(t, err)
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
	mustProgress(t, err)
	contexts, storyAttempts := map[string]bool{}, 0
	for _, attempt := range attempts {
		if attempt.Node != "story_agent" {
			continue
		}
		storyAttempts++
		outputs, err := f.db.ListGraphOutputs(t.Context(), attempt.ID)
		mustProgress(t, err)
		for _, output := range outputs {
			if ref, ok := decodePlanResultRef(output.Payload); ok {
				if ref.Revision != 1 || contexts[ref.Binding.WorkContext] {
					t.Fatalf("attempt %d shares or revises another attempt's result: %+v", attempt.ID, ref)
				}
				contexts[ref.Binding.WorkContext] = true
			}
		}
	}
	if storyAttempts != 3 || len(contexts) != 3 {
		t.Fatalf("story attempts %d with %d retained results, want 3 and 3", storyAttempts, len(contexts))
	}

	// Each story's review request reached its own sub-issue, not the parent.
	final := graphPlanState(t, f)
	for _, item := range final.Stories {
		if item.Wait == nil || item.Wait.Kind != "review" {
			t.Fatalf("%s has no review wait: %+v", item.Key, item)
		}
		if len(setFlowParentPosts(f, forge, "/approve-plan "+item.Key+" story ")) != 0 {
			t.Fatalf("the review request for %s went to the parent", item.Key)
		}
	}

	// A replay of the promotion, once complete, changes nothing.
	before := setFlowIdentities(t, f)
	if replay, err := Execute(t.Context(), f.opts, promoteTrigger(base, 802, 7, promoteCommand(current))); err != nil || replay.Run.ID != out.Run.ID {
		t.Fatalf("replay: %v", err)
	}
	requireIdentitiesKept(t, before, setFlowIdentities(t, f))
	if remote.creates != 3 || !slices.Equal(c.order, want) {
		t.Fatalf("replay repeated work: creates=%d order=%v", remote.creates, c.order)
	}
}

// AC-043, AC-044: a create whose reply is lost and a daemon restart between two
// runs keep every issue identity and ordinal; nothing is created twice.
func TestPlanSetFlow_LostCreateReplyAndRestartBetweenRunsKeepIdentities(t *testing.T) {
	c := &creationAgent{}
	f, remote, base, forge, run := setFlowFixture(t, c, setFlowSets())
	_, current := setFlowReviewSet(t, f, remote, base, forge, run)
	promote := promoteTrigger(base, 802, 7, promoteCommand(current))

	// The forge creates the first sub-issue and the reply is lost.
	remote.loseCreate = true
	if _, err := Execute(t.Context(), f.opts, promote); err == nil {
		t.Fatal("a lost create reply was reported as success")
	}
	if remote.creates != 1 {
		t.Fatalf("creates %d", remote.creates)
	}
	reserved := setFlowIdentities(t, f)
	if len(reserved) != 3 || !strings.HasPrefix(reserved["story-001"], "ordinal=1") || !strings.HasPrefix(reserved["story-002"], "ordinal=2") || !strings.HasPrefix(reserved["story-003"], "ordinal=3") {
		t.Fatalf("reserved rows %v", reserved)
	}
	// A retry settles the create from the forge, never creating that story again.
	if _, err := Execute(t.Context(), f.opts, nextTrigger(base, 803, "Please retry.")); err != nil {
		t.Fatalf("retry: %v logs=%s", err, f.logs.String())
	}
	// The retry settles story-002 from the forge (no second create) and the cycle goes
	// on to the next story, which is the only further create.
	if remote.creates != 2 || !slices.Equal(c.order, []string{"story-002", "story-003"}) {
		t.Fatalf("the retry created again or skipped ahead: creates=%d order=%v", remote.creates, c.order)
	}
	settled := setFlowIdentities(t, f)
	requireIdentitiesKept(t, reserved, settled)
	if strings.Contains(settled["story-002"], "issue=0/0") {
		t.Fatalf("story-002 was not settled onto the created issue: %q", settled["story-002"])
	}

	// A daemon restart between runs: the owed stage is reserved, the daemon dies, and
	// the stage is requeued. A separately opened store sees the same durable identities.
	due, err := f.db.ListDueGraphDispatches(t.Context(), time.Now().Add(time.Hour), 8)
	mustProgress(t, err)
	if len(due) != 1 {
		t.Fatalf("%d stages owed after the settled create, want 1", len(due))
	}
	if _, ok, err := f.db.ReserveGraphDispatch(t.Context(), due[0].ID, time.Now().Add(time.Hour)); err != nil || !ok {
		t.Fatalf("reserve: %v %v", ok, err)
	}
	if requeued, err := f.db.RequeueGraphDispatch(t.Context(), due[0].ID, time.Now(), "daemon restarted"); err != nil || !requeued {
		t.Fatalf("requeue: %v %v", requeued, err)
	}
	reopened, err := store.Open(t.Context(), f.paths.DBPath())
	mustProgress(t, err)
	rows, err := reopened.ListPlanStoryDrafts(t.Context(), f.ref().String())
	mustProgress(t, err)
	mustProgress(t, reopened.Close())
	if len(rows) != 3 || rows[1].Key != "story-002" || rows[1].IssueID == 0 || rows[1].Ordinal != 2 || rows[2].IssueID == 0 || rows[0].IssueID != 0 {
		t.Fatalf("a restarted daemon sees %+v", rows)
	}
	requireOwedStages(t, f, 1)
	if steps := drainGraphSteps(t, f, base); steps != 1 {
		t.Fatalf("%d further runs after the restart, want 1", steps)
	}
	requireIdentitiesKept(t, settled, setFlowIdentities(t, f))
	requireSetFlowCreated(t, f, remote, []string{"story-002", "story-003", "story-001"})
	if want := []string{"story-002", "story-003", "story-001"}; !slices.Equal(c.order, want) {
		t.Fatalf("a story was drafted twice or out of order: %v, want %v", c.order, want)
	}
	// Replaying the promotion after everything changes nothing.
	if _, err := Execute(t.Context(), f.opts, promote); err != nil {
		t.Fatalf("replay: %v", err)
	}
	requireIdentitiesKept(t, settled, setFlowIdentities(t, f))
	if remote.creates != 3 || len(c.order) != 3 {
		t.Fatalf("replay repeated work: creates=%d order=%v", remote.creates, c.order)
	}
}

// AC-044: a question during initial drafting pauses the cycle and, after the
// owner's reply, resumes the same story; a drafting failure then publishes its
// cause and retry advice, keeps the sub-issues already created and resumes at the
// failed story, with no second result, key or sub-issue for any story.
func TestPlanSetFlow_QuestionThenFailureDuringInitialDraftingResumeTheSameStories(t *testing.T) {
	c := &creationAgent{ask: "story-003", crash: "story-001"}
	f, remote, base, forge, run := setFlowFixture(t, c, setFlowSets())
	_, current := setFlowReviewSet(t, f, remote, base, forge, run)

	if out, err := Execute(t.Context(), f.opts, promoteTrigger(base, 802, 7, promoteCommand(current))); err != nil || out.Err != nil {
		t.Fatalf("promotion run: %v %v logs=%s", err, out.Err, f.logs.String())
	}
	// story-003 (the second in order) asks the owner instead of drafting.
	if out, ran, err := runDueGraphStep(t, f, base); !ran || err != nil || out.Err != nil {
		t.Fatalf("the asking stage: ran=%v %v %v", ran, err, out.Err)
	}
	if !slices.Equal(c.order, []string{"story-002", "story-003"}) || remote.creates != 1 {
		t.Fatalf("order %v creates %d", c.order, remote.creates)
	}
	if len(setFlowParentPosts(f, forge, "Which variant of story-003?")) != 1 {
		t.Fatal("the question did not reach the parent issue exactly once")
	}
	requireOwedStages(t, f, 0)
	if steps := drainGraphSteps(t, f, base); steps != 0 {
		t.Fatal("a paused cycle continued without the owner")
	}
	pausedState := graphPlanState(t, f)
	if pausedState.StoryDrafted("story-003") || !pausedState.StoryDrafted("story-002") {
		t.Fatalf("draft presence while paused: %+v", pausedState.Stories)
	}
	paused := setFlowIdentities(t, f)

	// The owner's reply resumes the same story.
	if out, err := Execute(t.Context(), f.opts, nextTrigger(base, 804, "Variant A, please.")); err != nil || out.Err != nil {
		t.Fatalf("resumed run: %v %v logs=%s", err, out.Err, f.logs.String())
	}
	if want := []string{"story-002", "story-003", "story-003"}; !slices.Equal(c.order, want) {
		t.Fatalf("after the reply the agent worked on %v, want the asked story again", c.order)
	}
	requireIdentitiesKept(t, paused, setFlowIdentities(t, f))

	// The last story fails: the cause and the retry advice reach the owner.
	out, ran, err := runDueGraphStep(t, f, base)
	if !ran || err == nil || out.Run.Status != store.RunFailed {
		t.Fatalf("the failing stage: ran=%v err=%v run=%+v", ran, err, out.Run)
	}
	requireFailureComment(t, forge, "process outcome")
	requireOwedStages(t, f, 0)
	failedState := graphPlanState(t, f)
	if remote.creates != 2 || failedState.StoryDrafted("story-001") {
		t.Fatalf("the earlier sub-issues were not preserved or the failed story has a draft: creates=%d", remote.creates)
	}
	failed := setFlowIdentities(t, f)
	requireIdentitiesKept(t, paused, failed)
	if len(failed) != 3 {
		t.Fatalf("keys changed after the failure: %v", failed)
	}

	// The retry resumes at the failed story and finishes the set.
	if _, err := Execute(t.Context(), f.opts, nextTrigger(base, 805, "Please retry.")); err != nil {
		t.Fatalf("retry: %v logs=%s", err, f.logs.String())
	}
	if want := []string{"story-002", "story-003", "story-003", "story-001", "story-001"}; !slices.Equal(c.order, want) {
		t.Fatalf("the retry worked on %v, want the failed story again", c.order)
	}
	if steps := drainGraphSteps(t, f, base); steps != 0 {
		t.Fatalf("%d further runs, want none: the failed story was the last", steps)
	}
	requireIdentitiesKept(t, failed, setFlowIdentities(t, f))
	requireSetFlowCreated(t, f, remote, []string{"story-002", "story-003", "story-001"})
	if len(failureBodies(forge)) != 1 || len(setFlowParentPosts(f, forge, "Which variant of story-003?")) != 1 {
		t.Fatalf("failure comments %d, questions %d", len(failureBodies(forge)), len(setFlowParentPosts(f, forge, "Which variant of story-003?")))
	}

	// No result is retained twice: every accepted result has its own context at revision 1.
	// A resumed or retried run is a new instance, so every instance is read.
	latest, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "plan")
	mustProgress(t, err)
	contexts := map[string]bool{}
	for id := int64(1); id <= latest.ID; id++ {
		attempts, err := f.db.ListGraphNodeAttempts(t.Context(), id)
		mustProgress(t, err)
		for _, attempt := range attempts {
			outputs, err := f.db.ListGraphOutputs(t.Context(), attempt.ID)
			mustProgress(t, err)
			for _, output := range outputs {
				if ref, ok := decodePlanResultRef(output.Payload); ok {
					if ref.Revision != 1 || contexts[ref.Binding.WorkContext] {
						t.Fatalf("instance %d attempt %d (%s) shares or revises another attempt's result: %+v", id, attempt.ID, attempt.Node, ref)
					}
					contexts[ref.Binding.WorkContext] = true
				}
			}
		}
	}
	// Two set proposals, the question, and one accepted draft per story.
	if len(contexts) != 6 {
		t.Fatalf("%d retained results, want 6 (2 sets, 1 question, 3 drafts)", len(contexts))
	}
}
