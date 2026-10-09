package daemon

import (
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestResearchClaimReplayResumesOnlySavedOwnerInput(t *testing.T) {
	f := newResearchRecoveryFixture(t)
	f.authored(t, true)
	_, err := f.db.MarkReportDeliverySent(t.Context(), "original-operation")
	researchRecoveryCheck(t, err)
	_, err = f.db.ConfirmReportDelivery(t.Context(), "original-operation", 101)
	researchRecoveryCheck(t, err)
	_, err = f.db.CompleteReportDelivery(t.Context(), "original-operation")
	researchRecoveryCheck(t, err)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	filter, err := turn.NewFilter(f.db, turn.Identities{OwnerUserID: 7, BotUserID: 99}, log)
	researchRecoveryCheck(t, err)
	d := &Daemon{db: f.db, filter: filter, now: func() time.Time { panic("reply admission must use task timestamp") }}
	ref, err := naming.ParseRef(f.session.TaskRef)
	researchRecoveryCheck(t, err)
	task := &task{session: f.session, ref: ref, tickAt: f.at.Add(time.Minute)}
	trigger := turn.Trigger{CommentID: 102, AuthorID: 7, Body: "Continue saved findings"}
	researchRecoveryCheck(t, d.claimGraphReply(t.Context(), task, trigger, "research"))
	// Repeating admission after a restart/preparation failure reuses the retained
	// claim. It neither claims another reply nor allocates an agent attempt.
	researchRecoveryCheck(t, d.claimGraphReply(t.Context(), task, trigger, "research"))
	wait, err := f.db.GetGraphOwnerWait(t.Context(), f.instance.ID)
	researchRecoveryCheck(t, err)
	if !wait.ClaimedAt.Equal(task.tickAt) {
		t.Fatalf("claim timestamp = %v, want task instant %v", wait.ClaimedAt, task.tickAt)
	}
	if wait.State != "claimed" || wait.ReplyID != 102 || wait.ContinuationAttemptID != 0 {
		t.Fatalf("claim not retained: %+v", wait)
	}
	bad := trigger
	bad.AuthorID = 8
	if err := d.claimGraphReply(t.Context(), task, bad, "research"); err == nil {
		t.Fatal("foreign author reused claimed input")
	}
	bad = trigger
	bad.Body = "changed content"
	if err := d.claimGraphReply(t.Context(), task, bad, "research"); err == nil {
		t.Fatal("changed reply reused claimed input")
	}
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), f.instance.ID)
	researchRecoveryCheck(t, err)
	if len(attempts) != 1 {
		t.Fatal("admission allocated agent work")
	}
}

func researchClaimDaemon(t *testing.T, f *researchRecoveryFixture) (*Daemon, *task) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	filter, err := turn.NewFilter(f.db, turn.Identities{OwnerUserID: 7, BotUserID: 99}, log)
	researchRecoveryCheck(t, err)
	d := &Daemon{db: f.db, filter: filter, now: func() time.Time { panic("reply admission must use task timestamp") }}
	ref, err := naming.ParseRef(f.session.TaskRef)
	researchRecoveryCheck(t, err)
	return d, &task{session: f.session, ref: ref, tickAt: f.at.Add(time.Minute)}
}

func publishResearchQuestion(t *testing.T, f *researchRecoveryFixture) {
	t.Helper()
	_, err := f.db.MarkReportDeliverySent(t.Context(), "original-operation")
	researchRecoveryCheck(t, err)
	_, err = f.db.ConfirmReportDelivery(t.Context(), "original-operation", 101)
	researchRecoveryCheck(t, err)
	_, err = f.db.CompleteReportDelivery(t.Context(), "original-operation")
	researchRecoveryCheck(t, err)
}

// #378: an owner comment written before the open question was published
// surfaces the dedicated sentinel that run() turns into a visible refusal,
// never the generic invariant it used to leave to d.leave. The question stays
// open and a genuine later reply is still claimed.
func TestResearchClaimReportsCommentThatPredatesQuestion(t *testing.T) {
	f := newResearchRecoveryFixture(t)
	f.authored(t, true)
	publishResearchQuestion(t, f)
	d, task := researchClaimDaemon(t, f)
	for _, id := range []int64{100, 101} {
		err := d.claimGraphReply(t.Context(), task, turn.Trigger{CommentID: id, AuthorID: 7, Body: "written while shaping ran"}, "research")
		if !errors.Is(err, store.ErrGraphReplyPredatesQuestion) || errors.Is(err, store.ErrGraphInvariant) {
			t.Fatalf("comment %d: error=%v, want only ErrGraphReplyPredatesQuestion", id, err)
		}
	}
	wait, err := f.db.GetGraphOwnerWait(t.Context(), f.instance.ID)
	researchRecoveryCheck(t, err)
	if wait.State != "waiting" || wait.ReplyID != 0 {
		t.Fatalf("refused comment changed the open question: %+v", wait)
	}
	researchRecoveryCheck(t, d.claimGraphReply(t.Context(), task, turn.Trigger{CommentID: 102, AuthorID: 7, Body: "Continue saved findings"}, "research"))
	wait, err = f.db.GetGraphOwnerWait(t.Context(), f.instance.ID)
	researchRecoveryCheck(t, err)
	if wait.State != "claimed" || wait.ReplyID != 102 {
		t.Fatalf("genuine reply after the question not claimed: %+v", wait)
	}
}

// #378 C4: while any attempt of the instance is still running the comment is
// held exactly as before (ErrGraphAttemptRunning), not refused. The running
// attempt is started before the question's wait is retained so the store
// permits both at once.
func TestResearchClaimHoldsPredatingCommentWhileAttemptRuns(t *testing.T) {
	f := newResearchRecoveryFixture(t)
	ctx := t.Context()
	researchRecoveryCheck(t, f.db.CompleteGraphNodeAttempt(ctx, f.attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: f.at}))
	if _, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: f.instance.ID, Node: "research", Limit: 3, StartedAt: f.at}); err != nil {
		t.Fatal(err)
	}
	researchRecoveryCheck(t, f.db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "original-operation", InstanceID: f.instance.ID, SourceAttemptID: f.attempt.ID, Node: "comment", Kind: "question", Candidate: []byte(`{"outcome":"incomplete"}`)}))
	_, err := f.db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: "original-operation", SessionID: f.session.ID, RunID: f.run.ID, TriggerID: 20, Task: f.session.TaskRef, Destination: "acme/research#342", TriggerNamespace: "issue-comment", Purpose: "research-question", BotUserID: 99, Body: "Exact authored text", Marker: "original-operation"})
	researchRecoveryCheck(t, err)
	publishResearchQuestion(t, f)
	d, task := researchClaimDaemon(t, f)
	err = d.claimGraphReply(ctx, task, turn.Trigger{CommentID: 100, AuthorID: 7, Body: "written while shaping ran"}, "research")
	if !errors.Is(err, store.ErrGraphAttemptRunning) || errors.Is(err, store.ErrGraphReplyPredatesQuestion) {
		t.Fatalf("error=%v, want ErrGraphAttemptRunning while an attempt runs", err)
	}
	wait, err := f.db.GetGraphOwnerWait(ctx, f.instance.ID)
	researchRecoveryCheck(t, err)
	if wait.State != "waiting" || wait.ReplyID != 0 {
		t.Fatalf("held comment changed the open question: %+v", wait)
	}
}
