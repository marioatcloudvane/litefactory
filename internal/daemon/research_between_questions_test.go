package daemon_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// The #367 sequence (issue #378), with comment ids in the same order:
// investigate posts a final report, the graph continues into shaping by itself,
// the owner comments while shaping runs, and shaping then publishes a question.
const (
	betweenReportComment   = int64(100) // investigate's final report, no owner wait
	betweenOwnerComment    = int64(110) // written while shaping was running
	betweenQuestionComment = int64(120) // shaping's question, published afterwards
	betweenPlanComment     = int64(130) // the owner's /plan, queued behind it
	betweenGenuineReply    = int64(140) // a real answer to the open question
)

// betweenRefusalText identifies the old "retained and undelivered" reply, which
// a queued owner comment no longer draws.
const betweenRefusalText = "Your instruction is retained and undelivered"

type betweenQuestions struct {
	f        *fixture
	session  store.Session
	instance store.GraphInstance
	segment  store.Segment
	at       time.Time
}

func betweenCheck(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// newBetweenQuestions bootstraps issue 123 and seeds a real Research graph in
// the store: investigate finished and its final report is published, and the
// instance has moved on to shape without any owner wait.
func newBetweenQuestions(t *testing.T) *betweenQuestions {
	t.Helper()
	f := newFixture(t)
	f.plantCursor()
	f.issue("Research the health endpoint")
	f.settle()
	f.bootstrapIssues(f.repo, testIssue)
	ctx := t.Context()
	b := &betweenQuestions{f: f, session: f.session(), at: f.clock.peek()}
	ref, err := naming.ParseRef(b.session.TaskRef)
	betweenCheck(t, err)
	sessionRef, err := naming.SessionRef(ref, store.FirstSegmentOrdinal)
	betweenCheck(t, err)
	b.segment, err = f.db.GetOrCreateSegment(ctx, b.session.ID, store.FirstSegmentOrdinal, sessionRef, store.AgentClaude)
	betweenCheck(t, err)
	definition, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research.dot", Digest: strings.Repeat("a", 64), CreatedAt: b.at})
	betweenCheck(t, err)
	b.instance, err = f.db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: b.session.ID, DefinitionID: definition.ID, CurrentNode: "investigate", StartedAt: b.at, NextDueAt: b.at})
	betweenCheck(t, err)

	investigateRun := b.run(t, 20, "investigate.log")
	investigate := b.start(t, "investigate", investigateRun)
	betweenCheck(t, f.db.CompleteGraphNodeAttempt(ctx, investigate.ID, store.GraphCompletion{
		Status: store.GraphSucceeded, Outcome: "success", FinishedAt: b.at,
		Transition: &store.GraphTransition{InstanceID: b.instance.ID, FromNode: "investigate", ToNode: "shape", OnOutcome: "success", At: b.at},
	}))
	b.publish(t, "investigate-report", investigate, investigateRun, "final", betweenReportComment)
	b.finish(t, investigateRun)
	return b
}

func (b *betweenQuestions) run(t *testing.T, trigger int64, log string) store.Run {
	t.Helper()
	run, err := b.f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: b.session.ID, SegmentID: b.segment.ID, Mode: "research", Agent: store.AgentClaude, TriggerCommentID: trigger, LogPath: func(int) string { return log }})
	betweenCheck(t, err)
	return run
}

func (b *betweenQuestions) start(t *testing.T, node string, run store.Run) store.GraphNodeAttempt {
	t.Helper()
	attempt, err := b.f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: b.instance.ID, Node: node, RunID: run.ID, Limit: 3, StartedAt: b.at})
	betweenCheck(t, err)
	return attempt
}

func (b *betweenQuestions) finish(t *testing.T, run store.Run) {
	t.Helper()
	zero := int64(0)
	_, err := b.f.db.FinishRun(t.Context(), run.ID, store.RunOutcome{Status: store.RunSucceeded, ExitCode: &zero})
	betweenCheck(t, err)
}

// publish retains a graph comment and records its delivery as confirmed and
// completed under the given forge comment id, as the graph publication does.
func (b *betweenQuestions) publish(t *testing.T, key string, source store.GraphNodeAttempt, run store.Run, kind string, commentID int64) {
	t.Helper()
	ctx := t.Context()
	db := b.f.db
	betweenCheck(t, db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: key, InstanceID: b.instance.ID, SourceAttemptID: source.ID, Node: kind, Kind: kind, Candidate: []byte(`{"outcome":"incomplete"}`)}))
	_, err := db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: key, SessionID: b.session.ID, RunID: run.ID, TriggerID: run.TriggerCommentID, Task: b.session.TaskRef, Destination: testOwnerName + "/" + testRepoName + "#123", TriggerNamespace: "issue-comment", Purpose: "research-" + kind, BotUserID: botID, Body: "Research " + kind, Marker: key})
	betweenCheck(t, err)
	_, err = db.MarkReportDeliverySent(ctx, key)
	betweenCheck(t, err)
	_, err = db.ConfirmReportDelivery(ctx, key, commentID)
	betweenCheck(t, err)
	_, err = db.CompleteReportDelivery(ctx, key)
	betweenCheck(t, err)
}

// shapeAsks runs the shaping node to completion and publishes its question,
// which opens the owner wait the stale comment used to be forced onto.
func (b *betweenQuestions) shapeAsks(t *testing.T) {
	t.Helper()
	shapeRun := b.run(t, 21, "shape.log")
	shape := b.start(t, "shape", shapeRun)
	betweenCheck(t, b.f.db.CompleteGraphNodeAttempt(t.Context(), shape.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: b.at}))
	b.publish(t, "shape-question", shape, shapeRun, "question", betweenQuestionComment)
	b.finish(t, shapeRun)
}

func betweenRefusals(f *fixture) int {
	posted, _, _ := f.forge.snapshot()
	n := 0
	for _, body := range posted {
		if strings.Contains(body, betweenRefusalText) {
			n++
		}
	}
	return n
}

func turnFor(f *fixture, commentID int64) (agent.Mode, bool) {
	for _, req := range f.turns.calls() {
		if req.Trigger.CommentID == commentID {
			return req.Mode, true
		}
	}
	return "", false
}

// The comment written while shaping ran stays queued and, once the question is
// published, answers it: it reaches a Research turn, claims the open wait, and
// draws no refusal reply.
func TestResearchBetweenQuestions_QueuedCommentAnswersQuestion(t *testing.T) {
	b := newBetweenQuestions(t)
	f := b.f
	f.forge.addComment(testIssue, betweenOwnerComment, owner(), "Please also cover the readiness probe")
	b.shapeAsks(t)
	for range 6 {
		if _, ran := turnFor(f, betweenOwnerComment); ran {
			break
		}
		f.clock.advance(time.Minute)
		f.settle()
	}
	mode, ran := turnFor(f, betweenOwnerComment)
	if !ran || mode != agent.ModeResearch {
		t.Fatalf("queued comment did not reach a Research turn: ran=%v mode=%q\nlogs:\n%s", ran, mode, f.logs)
	}
	wait, err := f.db.GetGraphOwnerWait(t.Context(), b.instance.ID)
	betweenCheck(t, err)
	if wait.ReplyID != betweenOwnerComment || (wait.State != "claimed" && wait.State != "allocated") {
		t.Fatalf("queued comment not bound to the open question: %+v", wait)
	}
	if got := betweenRefusals(f); got != 0 {
		t.Fatalf("refusal replies = %d, want none", got)
	}
}

// Several owner comments written while the workflow worked reach the agent as
// one batch, oldest first, claimed under the last of them.
func TestResearchBetweenQuestions_QueuedCommentsArriveAsOneBatch(t *testing.T) {
	b := newBetweenQuestions(t)
	f := b.f
	const later = betweenOwnerComment + 5
	f.forge.addComment(testIssue, betweenOwnerComment, owner(), "Please also cover the readiness probe")
	f.forge.addComment(testIssue, later, owner(), "And the liveness probe")
	b.shapeAsks(t)
	for range 6 {
		if _, ran := turnFor(f, later); ran {
			break
		}
		f.clock.advance(time.Minute)
		f.settle()
	}
	var body string
	for _, req := range f.turns.calls() {
		if req.Trigger.CommentID == betweenOwnerComment {
			t.Fatal("the first comment ran on its own instead of in the batch")
		}
		if req.Trigger.CommentID == later {
			body = req.Trigger.Body
		}
	}
	first, second := strings.Index(body, "readiness probe"), strings.Index(body, "liveness probe")
	if first < 0 || second < first {
		t.Fatalf("batch body = %q\nlogs:\n%s", body, f.logs)
	}
	wait, err := f.db.GetGraphOwnerWait(t.Context(), b.instance.ID)
	betweenCheck(t, err)
	if wait.ReplyID != later || wait.ReplyBody != body {
		t.Fatalf("batch not claimed: %+v", wait)
	}
	if wm := f.watermark(); wm < later {
		t.Fatalf("batch not consumed: watermark=%d", wm)
	}
}

// While an attempt of the instance is still running, an owner comment stays
// queued: no reply, not consumed, no agent. Once nothing runs it answers the
// open question.
func TestResearchBetweenQuestions_HeldWhileAttemptRuns(t *testing.T) {
	b := newBetweenQuestions(t)
	f := b.f
	ctx := t.Context()
	shapeRun := b.run(t, 21, "shape.log")
	shape := b.start(t, "shape", shapeRun)
	attempts, err := f.db.ListGraphNodeAttempts(ctx, b.instance.ID)
	betweenCheck(t, err)
	investigate := attempts[0]
	investigateRun, err := f.db.GetRun(ctx, investigate.RunID)
	betweenCheck(t, err)
	b.publish(t, "held-question", investigate, investigateRun, "question", betweenQuestionComment)

	f.forge.addComment(testIssue, betweenOwnerComment, owner(), "Please also cover the readiness probe")
	for range 4 {
		f.clock.advance(time.Minute)
		f.settle()
	}
	if got := betweenRefusals(f); got != 0 {
		t.Fatalf("reply posted while an attempt was still running: %d", got)
	}
	if wm := f.watermark(); wm >= betweenOwnerComment {
		t.Fatalf("comment consumed while an attempt was still running: watermark=%d", wm)
	}
	if _, ran := turnFor(f, betweenOwnerComment); ran {
		t.Fatal("held comment was handed to an agent")
	}

	betweenCheck(t, f.db.CompleteGraphNodeAttempt(ctx, shape.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: b.at}))
	b.finish(t, shapeRun)
	for range 6 {
		if _, ran := turnFor(f, betweenOwnerComment); ran {
			break
		}
		f.clock.advance(time.Minute)
		f.settle()
	}
	if _, ran := turnFor(f, betweenOwnerComment); !ran {
		t.Fatalf("queued comment never answered the question once nothing ran\nlogs:\n%s", f.logs)
	}
	if got := betweenRefusals(f); got != 0 {
		t.Fatalf("refusal replies = %d, want none", got)
	}
}

// A comment that arrives while the wait already holds a claimed reply stays
// queued for the next question: it is neither refused nor consumed, and it
// starts no separate agent.
func TestResearchOwnerWaitHoldsLaterCommentQueued(t *testing.T) {
	b := newBetweenQuestions(t)
	f := b.f
	b.shapeAsks(t)
	f.forge.addComment(testIssue, betweenGenuineReply, owner(), "Yes, cover the readiness probe too")
	for range 6 {
		if _, ran := turnFor(f, betweenGenuineReply); ran {
			break
		}
		f.clock.advance(time.Minute)
		f.settle()
	}
	if _, ran := turnFor(f, betweenGenuineReply); !ran {
		t.Fatalf("genuine reply never reached a turn:\n%s", f.logs)
	}
	const second = int64(150)
	f.forge.addComment(testIssue, second, owner(), "Also, one more thought")
	for range 5 {
		f.clock.advance(time.Minute)
		f.settle()
	}
	if wm := f.watermark(); wm >= second {
		t.Fatalf("queued comment consumed while the wait held another reply: watermark=%d", wm)
	}
	if got := betweenRefusals(f); got != 0 {
		t.Fatalf("refusal replies = %d, want none\nlogs:\n%s", got, f.logs)
	}
	if _, ran := turnFor(f, second); ran {
		t.Fatal("the queued comment started a separate agent")
	}
}
