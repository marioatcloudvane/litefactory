package store

import (
	"testing"
	"time"
)

func TestOwnerActionOutcomeNoticeSurvivesChatCleanup(t *testing.T) {
	db, p := ownerActionStoreFixture(t)
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE owner_action_proposals SET approval_channel='200' WHERE id=?`, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.MutateOwnerAction(t.Context(), p.Principal, p.ID, "confirm", "", p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClaimOwnerActionExecution(t.Context(), p.ID, p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishOwnerAction(t.Context(), p.ID, "uncertain", `{"state":"uncertain"}`, "inspect", p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	due, err := db.OwnerActionNoticesDue(t.Context())
	if err != nil || len(due) != 1 {
		t.Fatalf("notice not owed: %d %v", len(due), err)
	}
	notice := ChatNotice{TaskRef: p.ID, ChatReply: ChatReply{Transport: "discord", MessageID: "owner-outcome", ChannelRef: "200", Command: "announce", CreatedAt: p.ConfirmNotBefore, ReplyText: "Uncertain. History reference: " + p.ID}}
	if err = db.QueueOwnerActionOutcome(t.Context(), p.ID, "uncertain", []ChatNotice{notice}); err != nil {
		t.Fatal(err)
	}
	if err = db.QueueOwnerActionOutcome(t.Context(), p.ID, "uncertain", []ChatNotice{notice}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `DELETE FROM chat_notices`); err != nil {
		t.Fatal(err)
	}
	if err = db.PurgeNaturalChat(t.Context(), p.CreatedAt.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	due, err = db.OwnerActionNoticesDue(t.Context())
	if err != nil || len(due) != 0 {
		t.Fatalf("chat cleanup renewed notification: %d %v", len(due), err)
	}
	if err = db.FinishOwnerAction(t.Context(), p.ID, "succeeded", `{"state":"succeeded"}`, "receipt recovered", p.CreatedAt.Add(49*time.Hour)); err != nil {
		t.Fatal(err)
	}
	due, err = db.OwnerActionNoticesDue(t.Context())
	if err != nil || len(due) != 1 || due[0].State != "succeeded" {
		t.Fatalf("reconciled result not owed: %+v %v", due, err)
	}
}
