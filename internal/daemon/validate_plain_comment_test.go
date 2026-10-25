package daemon_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// plainCommentParent is a managed parent past planning whose Implement turn is
// recorded rather than run.
type plainCommentParent struct {
	f       *fixture
	session store.Session
	calls   *[]turn.Request
}

func newPlainCommentParent(t *testing.T, autoStart bool) *plainCommentParent {
	t.Helper()
	f := newFixture(t)
	if autoStart {
		f.daemon = f.newDaemon(daemon.Options{ValidateAutoStart: true})
	}
	var calls []turn.Request
	f.turns.fn = func(ctx context.Context, req turn.Request) (turn.Result, error) {
		calls = append(calls, req)
		return f.ranSuccessfully(ctx, req)
	}
	f.plantCursor()
	f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Parent", User: stranger(), Labels: toFakeLabels("Mode: Implement")})
	f.settle()
	session := f.sessionOf(testIssue)
	ctx := t.Context()
	mustDo(t, f.db.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: session.TaskRef, Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}))
	mustDo(t, f.db.CompareManagedFeature(ctx, store.ManagedFeature{ParentTask: session.TaskRef, Generation: 1, State: "implementing"}, "planning"))
	_, err := f.db.AdmitWorkflowPath(ctx, session.ID, "graph")
	mustDo(t, err)
	return &plainCommentParent{f: f, session: session, calls: &calls}
}

func mustDo(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// stories records the parent's generation-1 ledger with the given final states
// (merged or parked).
func (p *plainCommentParent) stories(t *testing.T, states ...string) {
	t.Helper()
	ctx := t.Context()
	at := p.f.clock.Now()
	var seeds []store.ImplementStorySeed
	for i := range states {
		key := "story-00" + string(rune('1'+i))
		seeds = append(seeds, store.ImplementStorySeed{Key: key, Position: i, Branch: "lf/" + key})
	}
	mustDo(t, p.f.db.EnsureImplementStories(ctx, p.session.TaskRef, 1, seeds, at))
	for i, state := range states {
		key := seeds[i].Key
		move := func(from, to, reason, commit string) {
			t.Helper()
			row, err := p.f.db.GetImplementStory(ctx, p.session.TaskRef, 1, key)
			mustDo(t, err)
			_, err = p.f.db.TransitionImplementStory(ctx, store.ImplementTransition{Parent: p.session.TaskRef, Generation: 1, Key: key, From: from, To: to, ExpectedRevision: row.Revision, Reason: reason, MergeCommit: commit, At: at})
			mustDo(t, err)
		}
		move(store.ImplementWaiting, store.ImplementRunning, "", "")
		if state == store.ImplementParked {
			move(store.ImplementRunning, store.ImplementParked, "needs the owner", "")
			continue
		}
		move(store.ImplementRunning, store.ImplementDone, "", "")
		move(store.ImplementDone, store.ImplementMerged, "", strings.Repeat("1", 40))
	}
}

// instance records a workflow instance of mode at node, finished with status
// unless status is running.
func (p *plainCommentParent) instance(t *testing.T, mode, node, status string) store.GraphInstance {
	t.Helper()
	ctx := t.Context()
	def, err := p.f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "builtin", Mode: mode, ProfileVersion: 1, Path: mode + "/" + mode + ".dot", Digest: strings.Repeat("a", 64)})
	mustDo(t, err)
	at := p.f.clock.Now()
	instance, err := p.f.db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: p.session.ID, DefinitionID: def.ID, CurrentNode: node, StartedAt: at})
	mustDo(t, err)
	if status != store.GraphRunning {
		mustDo(t, p.f.db.SetGraphInstanceStatus(ctx, instance.ID, status, time.Time{}, at))
	}
	return instance
}

// openValidateWait publishes and confirms a Validate owner question, as
// validateOwnerWaitFixture does.
func (p *plainCommentParent) openValidateWait(t *testing.T, instance store.GraphInstance) {
	t.Helper()
	ctx := t.Context()
	at := p.f.clock.Now()
	attempt, err := p.f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "owner", Limit: 3, StartedAt: at})
	mustDo(t, err)
	mustDo(t, p.f.db.FinishGraphNodeAttempt(ctx, attempt.ID, store.GraphSucceeded, "done", at))
	mustDo(t, p.f.db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "validate-question", InstanceID: instance.ID, SourceAttemptID: attempt.ID, Node: "owner", Kind: "question", Candidate: []byte("{}")}))
	_, err = p.f.db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: "validate-question", SessionID: p.session.ID, TriggerID: 20, Task: p.session.TaskRef, Destination: "validate", TriggerNamespace: "issue-comment", Purpose: "validate-question", BotUserID: 99, Body: "Validate report", Marker: "validate-question"})
	mustDo(t, err)
	_, err = p.f.db.MarkReportDeliverySent(ctx, "validate-question")
	mustDo(t, err)
	_, err = p.f.db.ConfirmReportDelivery(ctx, "validate-question", 30)
	mustDo(t, err)
	_, err = p.f.db.CompleteReportDelivery(ctx, "validate-question")
	mustDo(t, err)
	wait, err := p.f.db.GetGraphOwnerWait(ctx, instance.ID)
	if err != nil || wait.State != "waiting" {
		t.Fatalf("fixture wait: %+v %v", wait, err)
	}
}

func (p *plainCommentParent) implementCalls() int {
	n := 0
	for _, call := range *p.calls {
		if call.Mode == agent.ModeImplement {
			n++
		}
	}
	return n
}

// postedSince returns the bodies posted after the first skip bodies.
func (p *plainCommentParent) postedSince(skip int) []string {
	posted, _, _ := p.f.forge.snapshot()
	return posted[skip:]
}

// assertRefused checks that comment id ran nothing in Implement mode, posted
// no completion note, was answered with one reply containing every want, and
// was consumed.
func (p *plainCommentParent) assertRefused(t *testing.T, id int64, before int, want ...string) {
	t.Helper()
	if n := p.implementCalls(); n != 0 {
		t.Fatalf("a turn ran in Implement mode: %+v\n%s", *p.calls, p.f.logs)
	}
	posted := p.postedSince(before)
	if len(posted) != 1 {
		t.Fatalf("want one reply, got %d: %q\n%s", len(posted), posted, p.f.logs)
	}
	for _, text := range want {
		if !strings.Contains(posted[0], text) {
			t.Fatalf("reply lacks %q: %s", text, posted[0])
		}
	}
	if strings.Contains(posted[0], "parked") {
		t.Fatalf("reply carries parked-story instructions: %s", posted[0])
	}
	if strings.Contains(posted[0], "start Validate by commenting `/validate …`") {
		t.Fatalf("reply re-posts the completion note: %s", posted[0])
	}
	if w := p.f.watermark(); w < id {
		t.Fatalf("comment %d not consumed: watermark %d", id, w)
	}
}

const validateRunningReply = "Your instruction is retained and undelivered"

// #1123 c1: while the latest Validate instance runs, in any step, a plain
// owner comment on the parent runs no Implement turn and gets one fixed reply.
func TestPlainCommentWhileValidateRunsIsRefused(t *testing.T) {
	for _, state := range []string{"running", "waiting", "claimed"} {
		t.Run(state, func(t *testing.T) {
			p := newPlainCommentParent(t, false)
			p.stories(t, store.ImplementMerged, store.ImplementMerged)
			p.instance(t, "implement", "ready", store.GraphSucceeded)
			node := "review"
			if state != "running" {
				node = "owner"
			}
			validate := p.instance(t, "validate", node, store.GraphRunning)
			if state != "running" {
				p.openValidateWait(t, validate)
			}
			if state == "claimed" {
				p.f.forge.addComment(testIssue, 43, owner(), turn.ValidateCheckCommand+" r1 story-001:AC-001 pass")
				p.f.settle()
				wait, err := p.f.db.GetGraphOwnerWait(t.Context(), validate.ID)
				if err != nil || wait.State != "claimed" {
					t.Fatalf("fixture claim: %+v %v\n%s", wait, err, p.f.logs)
				}
			}
			before := len(p.postedSince(0))
			p.f.forge.addComment(testIssue, 44, owner(), "what are the blocking findings? tell me exactly")
			p.f.settle()
			p.assertRefused(t, 44, before, validateRunningReply, "Undelivered owner instructions")
		})
	}
}

// #1123 c7: a plain comment after a finished Implement, with every story merged
// and no Validate running, is refused with the true next step.
func TestPlainCommentAfterFinishedImplementIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name      string
		auto      bool
		validated bool
		want      string
	}{
		{name: "validate by command", want: "`/validate`"},
		{name: "validate starts automatically", auto: true, want: "starts automatically"},
		{name: "validate already finished", validated: true, want: "Validate has already finished"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newPlainCommentParent(t, tc.auto)
			p.stories(t, store.ImplementMerged, store.ImplementMerged)
			p.instance(t, "implement", "ready", store.GraphSucceeded)
			if tc.validated {
				p.instance(t, "validate", "done", store.GraphSucceeded)
			}
			before := len(p.postedSince(0))
			p.f.forge.addComment(testIssue, 44, owner(), "is it done?")
			p.f.settle()
			p.assertRefused(t, 44, before, "Your instruction is retained and undelivered", "Undelivered owner instructions")
		})
	}
}

// #1123 c3: with a parked story and no Validate instance, a plain comment still
// reaches Implement.
func TestPlainCommentWithParkedStoryStillRunsImplement(t *testing.T) {
	p := newPlainCommentParent(t, false)
	p.stories(t, store.ImplementMerged, store.ImplementParked)
	p.f.forge.addComment(testIssue, 44, owner(), "story-002: use the second option")
	p.f.settle()
	if n := p.implementCalls(); n != 1 {
		t.Fatalf("want one Implement turn, got %d: %+v\n%s", n, *p.calls, p.f.logs)
	}
}
