package daemon

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
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
	researchRecoveryCheck(t, claimErr(d.claimGraphReply(t.Context(), task, trigger, "research")))
	// Repeating admission after a restart/preparation failure reuses the retained
	// claim. It neither claims another reply nor allocates an agent attempt.
	researchRecoveryCheck(t, claimErr(d.claimGraphReply(t.Context(), task, trigger, "research")))
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
	if err := claimErr(d.claimGraphReply(t.Context(), task, bad, "research")); err == nil {
		t.Fatal("foreign author reused claimed input")
	}
	bad = trigger
	bad.Body = "changed content"
	if err := claimErr(d.claimGraphReply(t.Context(), task, bad, "research")); err == nil {
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

// An owner comment written before the open question was published answers it:
// owner input is queued, never refused for its timing.
func TestResearchClaimAcceptsCommentThatPredatesQuestion(t *testing.T) {
	f := newResearchRecoveryFixture(t)
	f.authored(t, true)
	publishResearchQuestion(t, f)
	d, task := researchClaimDaemon(t, f)
	researchRecoveryCheck(t, claimErr(d.claimGraphReply(t.Context(), task, turn.Trigger{CommentID: 100, AuthorID: 7, Body: "written while shaping ran"}, "research")))
	wait, err := f.db.GetGraphOwnerWait(t.Context(), f.instance.ID)
	researchRecoveryCheck(t, err)
	if wait.State != "claimed" || wait.ReplyID != 100 || wait.ReplyBody != "written while shaping ran" {
		t.Fatalf("predating owner comment not claimed: %+v", wait)
	}
}

// Every unconsumed owner comment since the last question reaches the agent as
// one batch, oldest first, each with its id; the batch is claimed under its
// last comment, and a replay rebuilds the same claim.
func TestResearchClaimDeliversQueuedOwnerCommentsAsOneBatch(t *testing.T) {
	f := newResearchRecoveryFixture(t)
	f.authored(t, true)
	publishResearchQuestion(t, f)
	d, task := researchClaimDaemon(t, f)
	at := f.at
	first := turn.Trigger{CommentID: 100, AuthorID: 7, Body: "first thought", CreatedAt: at}
	task.queue = []github.Comment{
		{ID: 100, Author: github.User{ID: 7}, Body: "first thought", CreatedAt: at},
		{ID: 102, Author: github.User{ID: 99}, Body: "factory question", CreatedAt: at.Add(time.Second)},
		{ID: 103, Author: github.User{ID: 7}, Body: "second thought", CreatedAt: at.Add(2 * time.Second)},
		{ID: 104, Author: github.User{ID: 7}, Body: "/implement now", CreatedAt: at.Add(3 * time.Second)},
	}
	got, err := d.claimGraphReply(t.Context(), task, first, "research")
	researchRecoveryCheck(t, err)
	if got.CommentID != 103 || !strings.Contains(got.Body, "Owner comment 100") || !strings.Contains(got.Body, "Owner comment 103") ||
		strings.Index(got.Body, "first thought") > strings.Index(got.Body, "second thought") || strings.Contains(got.Body, "factory question") || strings.Contains(got.Body, "/implement") {
		t.Fatalf("batch = %d %q", got.CommentID, got.Body)
	}
	wait, err := f.db.GetGraphOwnerWait(t.Context(), f.instance.ID)
	researchRecoveryCheck(t, err)
	if wait.State != "claimed" || wait.ReplyID != 103 || wait.ReplyBody != got.Body {
		t.Fatalf("batch not claimed: %+v", wait)
	}
	again, err := d.claimGraphReply(t.Context(), task, first, "research")
	researchRecoveryCheck(t, err)
	if again != got {
		t.Fatalf("replay rebuilt %+v, want %+v", again, got)
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
	if _, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: f.instance.ID, Node: "research", StartedAt: f.at}); err != nil {
		t.Fatal(err)
	}
	researchRecoveryCheck(t, f.db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "original-operation", InstanceID: f.instance.ID, SourceAttemptID: f.attempt.ID, Node: "comment", Kind: "question", Candidate: []byte(`{"outcome":"incomplete"}`)}))
	_, err := f.db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: "original-operation", SessionID: f.session.ID, RunID: f.run.ID, TriggerID: 20, Task: f.session.TaskRef, Destination: "acme/research#342", TriggerNamespace: "issue-comment", Purpose: "research-question", BotUserID: 99, Body: "Exact authored text", Marker: "original-operation"})
	researchRecoveryCheck(t, err)
	publishResearchQuestion(t, f)
	d, task := researchClaimDaemon(t, f)
	err = claimErr(d.claimGraphReply(ctx, task, turn.Trigger{CommentID: 100, AuthorID: 7, Body: "written while shaping ran"}, "research"))
	if !errors.Is(err, store.ErrGraphAttemptRunning) {
		t.Fatalf("error=%v, want ErrGraphAttemptRunning while an attempt runs", err)
	}
	wait, err := f.db.GetGraphOwnerWait(ctx, f.instance.ID)
	researchRecoveryCheck(t, err)
	if wait.State != "waiting" || wait.ReplyID != 0 {
		t.Fatalf("held comment changed the open question: %+v", wait)
	}
}
