package turn

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// setAnswer is what a fake set-stage agent submits: a proposed set, an ask, or a
// process that ends without a result.
type setAnswer struct {
	Entries []ar.PlanStorySetEntry
	Ask     bool
	Stop    agent.Outcome
	// Before runs inside the agent's call, to change saved state while it works.
	Before func()
}

func setEntry(id, name string, deps ...string) ar.PlanStorySetEntry {
	return ar.PlanStorySetEntry{ID: id, Name: name, Intent: "Deliver " + name, DependsOn: deps}
}

// graphSetFixture is a graph Plan session with an empty scope, so it starts at
// the set stage. script chooses the answer for the n-th agent call, counted from 1.
func graphSetFixture(t *testing.T, script func(call int) setAnswer) (*fixture, Request, *int, *fakeForge) {
	t.Helper()
	calls := 0
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		if req.Graph == nil || req.Mode != "plan" || !strings.Contains(req.Prompt, "Factory-selected stage: set;") {
			t.Fatal("the set stage prompt carries no set binding")
		}
		answer := script(calls)
		if answer.Before != nil {
			answer.Before()
		}
		if answer.Stop != 0 {
			return &agent.RunResult{Outcome: answer.Stop, Reason: "scripted stop", FinishedAt: time.Now()}, nil
		}
		result, response := ar.Result{Outcome: "completed", Comment: "Here is the proposed split.", Payload: ar.Payload{Plan: &ar.Plan{StorySet: &ar.PlanStorySet{Entries: answer.Entries}}}}, "done"
		if answer.Ask {
			result, response = ar.Result{Outcome: "incomplete", Reason: "The scope is unclear.", Comment: "Please clarify.", Questions: []ar.Question{{ID: "q-1", Audience: "business", Text: "Which parts are in scope?"}}, Payload: ar.Payload{Plan: &ar.Plan{}}}, "ask"
		}
		envelope, err := json.Marshal(ar.ResearchNode{Version: 1, Response: response, Result: result})
		if err != nil {
			t.Fatal(err)
		}
		submitLiveResult(t, req, string(envelope))
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: string(envelope), FinishedAt: time.Now()}, nil
	})
	f, _, base, forge := setupGraphPlanFixture(t, runner, false)
	return f, base, &calls, forge
}

func setPosts(f *fixture, forge *fakeForge) []string {
	var out []string
	for _, post := range forge.all() {
		if strings.Contains(post.body, "Proposed story set, revision") {
			out = append(out, post.body)
		}
	}
	return out
}

func nextTrigger(base Request, id int64, body string) Request {
	base.Trigger = Trigger{CommentID: id, Body: body, Author: "owner", AuthorID: 7}
	base.Command = Command{Kind: KindFreeText, Text: body}
	return base
}

var threeStories = []ar.PlanStorySetEntry{setEntry("s1", "Storage", "s2"), setEntry("s2", "Schema"), setEntry("s3", "Screens", "s1", "s2")}

// The set effect records the set with its content revision in saved state and the
// set is published once on the parent, showing that revision, the ordered stories
// and the promotion command. It promotes nothing and creates no story issue.
func TestPlanSet_RecordsRevisionAndPublishesOnTheParent(t *testing.T) {
	f, base, calls, forge := graphSetFixture(t, func(int) setAnswer { return setAnswer{Entries: threeStories} })
	out, err := Execute(t.Context(), f.opts, base)
	if err != nil || out.Err != nil {
		t.Fatalf("set stage failed: err=%v result=%+v logs=%s", err, out, f.logs.String())
	}
	state := graphPlanState(t, f)
	want := planbrief.PlanStorySetRevision([]planbrief.PlanStorySetEntry{{ID: "s1", Name: "Storage", Intent: "Deliver Storage", DependsOn: []string{"s2"}}, {ID: "s2", Name: "Schema", Intent: "Deliver Schema"}, {ID: "s3", Name: "Screens", Intent: "Deliver Screens", DependsOn: []string{"s1", "s2"}}})
	if state.StorySet == nil || state.StorySet.Revision != want || state.StorySet.Promoted || len(state.StorySet.Entries) != 3 || len(state.StorySet.Keys) != 0 || len(state.Stories) != 0 {
		t.Fatalf("saved set %+v stories %d", state.StorySet, len(state.Stories))
	}
	posts := setPosts(f, forge)
	if len(posts) != 1 {
		t.Fatalf("published %d set comments, want one", len(posts))
	}
	for _, needle := range []string{
		"Here is the proposed split.",
		"Proposed story set, revision `" + want + "`",
		"| ID | Name | Description | Dependencies |",
		"| `s1` | Storage | Deliver Storage | `s2` |",
		"| `s2` | Schema | Deliver Schema | none |",
		"| `s3` | Screens | Deliver Screens | `s1`, `s2` |",
		"reply `/approve-plan set " + want + "`",
	} {
		if !strings.Contains(posts[0], needle) {
			t.Fatalf("set comment lacks %q:\n%s", needle, posts[0])
		}
	}
	if !strings.Contains(forge.all()[0].path, "/issues/"+strconv.Itoa(f.ref().Issue)+"/comments") {
		t.Fatalf("the set was not posted on the parent: %s", forge.all()[0].path)
	}
	drafts, err := f.db.ListPlanStoryDrafts(t.Context(), f.ref().String())
	mustProgress(t, err)
	if len(drafts) != 0 {
		t.Fatalf("proposing a set created %d story drafts", len(drafts))
	}
	// Replaying the run neither runs the agent nor posts again.
	if _, err = Execute(t.Context(), f.opts, base); err != nil || *calls != 1 || len(setPosts(f, forge)) != 1 {
		t.Fatalf("replay: err=%v calls=%d posts=%d", err, *calls, len(setPosts(f, forge)))
	}
}

// The revision follows content: a changed set replaces the unpromoted one under a
// new revision and is published again; the same content proposed again keeps its
// revision and adds no revision of its own to saved state.
func TestPlanSet_ChangedSetGetsANewRevisionAndSameContentKeepsIt(t *testing.T) {
	answers := [][]ar.PlanStorySetEntry{threeStories, {setEntry("s1", "Storage"), setEntry("s2", "Schema")}, {setEntry("s1", "Storage"), setEntry("s2", "Schema")}}
	f, base, _, forge := graphSetFixture(t, func(call int) setAnswer { return setAnswer{Entries: answers[call-1]} })
	revisions := make([]string, 3)
	for i := range answers {
		req := base
		if i > 0 {
			req = nextTrigger(base, base.Trigger.CommentID+int64(i), "please change the split")
		}
		if out, err := Execute(t.Context(), f.opts, req); err != nil || out.Err != nil {
			t.Fatalf("proposal %d: err=%v result=%+v logs=%s", i+1, err, out, f.logs.String())
		}
		set := graphPlanState(t, f).StorySet
		if set == nil {
			t.Fatalf("proposal %d recorded no set", i+1)
		}
		revisions[i] = set.Revision
		if len(set.Entries) != len(answers[i]) {
			t.Fatalf("proposal %d recorded %d entries", i+1, len(set.Entries))
		}
	}
	if revisions[0] == revisions[1] {
		t.Fatal("a changed set kept its revision")
	}
	if revisions[1] != revisions[2] {
		t.Fatalf("identical content changed the revision: %s then %s", revisions[1], revisions[2])
	}
	if posts := setPosts(f, forge); len(posts) != 3 || !strings.Contains(posts[1], revisions[1]) || strings.Contains(posts[1], revisions[0]) {
		t.Fatalf("each proposal publishes its own revision: %d posts", len(posts))
	}
}

// A question ends the stage at the owner: nothing is recorded and no set is shown.
func TestPlanSet_QuestionRecordsNoSet(t *testing.T) {
	f, base, _, forge := graphSetFixture(t, func(int) setAnswer { return setAnswer{Ask: true} })
	if out, err := Execute(t.Context(), f.opts, base); err != nil || out.Err != nil {
		t.Fatalf("question: err=%v result=%+v", err, out)
	}
	if graphPlanState(t, f).StorySet != nil || len(setPosts(f, forge)) != 0 || len(forge.all()) != 1 {
		t.Fatalf("a question recorded or published a set: %d posts", len(forge.all()))
	}
}

func setRuntime(f *fixture, base Request, stage string) *planGraphRuntime {
	return &planGraphRuntime{opts: f.opts, req: base, result: Result{Ref: f.ref()}, parentRef: f.ref(), parentNumber: f.ref().Issue, stage: stage}
}

func setRetained(entries ...ar.PlanStorySetEntry) planRetained {
	return planRetained{Response: "done", Set: &ar.PlanStorySet{Entries: entries}}
}

func setReceiptOf(t *testing.T, got ActivityResult) PlanStoryReceipt {
	t.Helper()
	var receipt PlanStoryReceipt
	mustProgress(t, json.Unmarshal(got.Output, &receipt))
	return receipt
}

// A set cannot replace a promoted one, cannot be proposed for a scope with stories
// and cannot be proposed outside the set stage. Each is a failing outcome with the
// reason, which the graph routes to the failure comment. The promoted set itself
// is untouched, and applying the promoted set's own content again changes nothing.
func TestPlanSet_PromotedSetStoriesAndWrongStageAreRefused(t *testing.T) {
	promoted := planbrief.PlanState{SharedRevision: "initial", StorySet: &planbrief.PlanStorySetState{Revision: planbrief.PlanStorySetRevision([]planbrief.PlanStorySetEntry{{ID: "s1", Name: "Storage", Intent: "Deliver Storage"}}), Entries: []planbrief.PlanStorySetEntry{{ID: "s1", Name: "Storage", Intent: "Deliver Storage"}}, Promoted: true, Keys: map[string]string{"s1": "story-001"}}}
	promoted.StorySet.PromotedRevision = promoted.StorySet.Revision
	withStories := planbrief.PlanState{SharedRevision: "initial", Stories: []planbrief.PlanStoryState{{Key: "story-001", Stage: "story", Approvals: map[string]string{}}}}
	f, base, _, _ := graphSetFixture(t, func(int) setAnswer { return setAnswer{} })
	for _, tt := range []struct {
		name    string
		state   planbrief.PlanState
		stage   string
		entries []ar.PlanStorySetEntry
		outcome string
		detail  string
	}{
		{"a different set after promotion", promoted, "set", []ar.PlanStorySetEntry{setEntry("s1", "Other")}, "conflict", "already promoted"},
		{"the promoted set again", promoted, "set", []ar.PlanStorySetEntry{{ID: "s1", Name: "Storage", Intent: "Deliver Storage"}}, "applied", "nothing changed"},
		{"a scope with stories", withStories, "set", []ar.PlanStorySetEntry{setEntry("s1", "Storage")}, "conflict", "already has stories"},
		{"a story stage", planbrief.PlanState{SharedRevision: "initial"}, "story", []ar.PlanStorySetEntry{setEntry("s1", "Storage")}, "failed", "only at the set stage"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, rev, err := f.db.GetPlanWorkflowState(t.Context(), f.ref().String())
			mustProgress(t, err)
			_, err = f.db.SavePlanWorkflowState(t.Context(), f.ref().String(), rev, tt.state)
			mustProgress(t, err)
			before := graphPlanState(t, f)
			got, err := setRuntime(f, base, tt.stage).setEffect(t.Context(), ActivityRequest{InstanceID: 1, NodeAttemptID: 2}, setRetained(tt.entries...))
			if err != nil || got.Outcome != tt.outcome {
				t.Fatalf("outcome %q err=%v, want %q", got.Outcome, err, tt.outcome)
			}
			if receipt := setReceiptOf(t, got); receipt.Outcome != tt.outcome || !strings.Contains(receipt.Detail, tt.detail) {
				t.Fatalf("receipt %+v lacks %q", receipt, tt.detail)
			}
			after := graphPlanState(t, f)
			b, _ := json.Marshal(before)
			a, _ := json.Marshal(after)
			if string(a) != string(b) {
				t.Fatalf("a refused or repeated set changed saved state:\n%s\n%s", b, a)
			}
		})
	}
}

// The set effect is idempotent over saved state: applying the same retained set
// twice, as a replay or recovery would, records one revision and one state write.
func TestPlanSet_EffectIsIdempotentOverSavedState(t *testing.T) {
	f, base, _, _ := graphSetFixture(t, func(int) setAnswer { return setAnswer{} })
	_, err := f.db.SavePlanWorkflowState(t.Context(), f.ref().String(), 0, planbrief.PlanState{SharedRevision: "initial"})
	mustProgress(t, err)
	r := setRuntime(f, base, "set")
	first, err := r.setEffect(t.Context(), ActivityRequest{InstanceID: 1, NodeAttemptID: 2}, setRetained(threeStories...))
	mustProgress(t, err)
	_, rev1, err := f.db.GetPlanWorkflowState(t.Context(), f.ref().String())
	mustProgress(t, err)
	second, err := r.setEffect(t.Context(), ActivityRequest{InstanceID: 1, NodeAttemptID: 2}, setRetained(threeStories...))
	mustProgress(t, err)
	_, rev2, err := f.db.GetPlanWorkflowState(t.Context(), f.ref().String())
	mustProgress(t, err)
	if first.Outcome != "applied" || second.Outcome != "applied" || setReceiptOf(t, first).Revision != setReceiptOf(t, second).Revision || rev1 != rev2 {
		t.Fatalf("repeat changed state: %q %q revisions %d %d", first.Outcome, second.Outcome, rev1, rev2)
	}
}

// Every failing path of the set stage reaches the failure comment: an agent that
// stops without a result, and a set the effect refuses because a set was promoted
// while the agent worked.
func TestPlanSet_FailuresReachTheFailureComment(t *testing.T) {
	t.Run("agent stops", func(t *testing.T) {
		f, base, _, forge := graphSetFixture(t, func(int) setAnswer { return setAnswer{Stop: agent.OutcomeTimedOut} })
		out, err := Execute(t.Context(), f.opts, base)
		if err == nil || out.Run.Status != store.RunFailed {
			t.Fatalf("a stopped set agent completed nominally: %+v err=%v", out.Run, err)
		}
		requireFailureComment(t, forge, "process outcome")
		requireRunFailureReported(t, f, out.Run.ID, true)
		if len(setPosts(f, forge)) != 0 {
			t.Fatal("a set was published for a failed step")
		}
	})
	t.Run("effect conflict", func(t *testing.T) {
		var f *fixture
		promote := func() {
			_, rev, err := f.db.GetPlanWorkflowState(t.Context(), f.ref().String())
			mustProgress(t, err)
			entries := []planbrief.PlanStorySetEntry{{ID: "x1", Name: "Other", Intent: "Deliver Other"}}
			state := planbrief.PlanState{SharedRevision: "initial", StorySet: &planbrief.PlanStorySetState{Revision: planbrief.PlanStorySetRevision(entries), PromotedRevision: planbrief.PlanStorySetRevision(entries), Promoted: true, Entries: entries, Keys: map[string]string{"x1": "story-001"}}}
			_, err = f.db.SavePlanWorkflowState(t.Context(), f.ref().String(), rev, state)
			mustProgress(t, err)
		}
		var forge *fakeForge
		var base Request
		f, base, _, forge = graphSetFixture(t, func(int) setAnswer { return setAnswer{Entries: threeStories, Before: promote} })
		out, err := Execute(t.Context(), f.opts, base)
		if err != nil && out.Run.Status != store.RunFailed {
			t.Fatalf("unexpected: %v", err)
		}
		requireFailureComment(t, forge, "already promoted")
		if len(setPosts(f, forge)) != 0 {
			t.Fatal("a refused set was published for review")
		}
		if set := graphPlanState(t, f).StorySet; set == nil || set.Revision != planbrief.PlanStorySetRevision(set.Entries) || len(set.Entries) != 1 {
			t.Fatalf("the promoted set changed: %+v", set)
		}
	})
}
