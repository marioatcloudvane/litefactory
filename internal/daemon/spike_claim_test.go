package daemon

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/turn"
)

// A reply is claimed only against the parked question of its own workflow: a
// Spike reply never claims a Research question, and a Research reply is
// unchanged by the shared claim path.
func TestGraphReplyClaimIsScopedToItsWorkflow(t *testing.T) {
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
	d := &Daemon{db: f.db, filter: filter}
	ref, err := naming.ParseRef(f.session.TaskRef)
	researchRecoveryCheck(t, err)
	task := &task{session: f.session, ref: ref, tickAt: f.at.Add(time.Minute)}
	trigger := turn.Trigger{CommentID: 102, AuthorID: 7, Body: "Continue saved findings"}
	researchRecoveryCheck(t, d.claimGraphReply(t.Context(), task, trigger, "spike"))
	wait, err := f.db.GetGraphOwnerWait(t.Context(), f.instance.ID)
	researchRecoveryCheck(t, err)
	if wait.State != "waiting" || wait.ReplyID != 0 {
		t.Fatalf("a Spike reply claimed a Research question: %+v", wait)
	}
	researchRecoveryCheck(t, d.claimGraphReply(t.Context(), task, trigger, "research"))
	wait, err = f.db.GetGraphOwnerWait(t.Context(), f.instance.ID)
	researchRecoveryCheck(t, err)
	if wait.State != "claimed" || wait.ReplyID != 102 {
		t.Fatalf("the Research claim regressed: %+v", wait)
	}
}
