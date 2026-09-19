package daemon

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
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
	researchRecoveryCheck(t, d.claimResearchReply(t.Context(), task, trigger))
	// Repeating admission after a restart/preparation failure reuses the retained
	// claim. It neither claims another reply nor allocates an agent attempt.
	researchRecoveryCheck(t, d.claimResearchReply(t.Context(), task, trigger))
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
	if err := d.claimResearchReply(t.Context(), task, bad); err == nil {
		t.Fatal("foreign author reused claimed input")
	}
	bad = trigger
	bad.Body = "changed content"
	if err := d.claimResearchReply(t.Context(), task, bad); err == nil {
		t.Fatal("changed reply reused claimed input")
	}
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), f.instance.ID)
	researchRecoveryCheck(t, err)
	if len(attempts) != 1 {
		t.Fatal("admission allocated agent work")
	}
}
