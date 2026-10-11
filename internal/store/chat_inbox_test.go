package store

import (
	"testing"
	"time"
)

func TestFinishedAskRowsKeepTurnForLocalHistory(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	deliver := func(r ChatReply, failed bool) {
		t.Helper()
		if _, err := db.AdmitChatReply(ctx, r); err != nil {
			t.Fatal(err)
		}
		claim, err := db.ClaimChatReply(ctx, r.Transport, r.CreatedAt)
		if err != nil || claim.MessageID != r.MessageID {
			t.Fatalf("claim: %+v %v", claim, err)
		}
		if err = db.SaveChatReply(ctx, claim, "raw "+r.MessageID); err != nil {
			t.Fatal(err)
		}
		if err = db.FinishChatReply(ctx, claim, failed, r.CreatedAt); err != nil {
			t.Fatal(err)
		}
	}
	ask := func(id, channel string, when time.Time) ChatReply {
		return ChatReply{Transport: "discord", MessageID: id, ChannelRef: channel, Command: "ask", Arguments: map[string]string{"question": "Q " + id, "project": "app"}, CreatedAt: when}
	}
	deliver(ask("1", "3", at), false)
	deliver(ask("2", "3", at.Add(time.Minute)), false)
	deliver(ask("3", "3", at.Add(2*time.Minute)), true)
	deliver(ask("4", "4", at.Add(3*time.Minute)), false)
	deliver(ChatReply{Transport: "discord", MessageID: "5", ChannelRef: "3", Command: "health", CreatedAt: at.Add(4 * time.Minute)}, false)

	rows, err := db.ReadAskReplies(ctx, "discord", "3", 10)
	if err != nil || len(rows) != 2 || rows[0].MessageID != "2" || rows[1].MessageID != "1" {
		t.Fatalf("ask rows: %+v %v", rows, err)
	}
	if rows[0].Question != "Q 2" || rows[0].ProjectOption != "app" || rows[0].ReplyText != "raw 2" || len(rows[0].Arguments) != 0 || rows[0].PresentationOutcome != "" {
		t.Fatalf("delivered turn not kept: %+v", rows[0])
	}
	for _, id := range []string{"3", "5"} {
		r, err := db.ReadChatReply(ctx, "discord", id)
		if err != nil || r.Question != "" || r.ProjectOption != "" || r.ReplyText != "" || r.ReplyAttachment != "" {
			t.Fatalf("row %s content retained: %+v %v", id, r, err)
		}
	}
	if err = db.PurgeChatReplies(ctx, at.Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if rows, err = db.ReadAskReplies(ctx, "discord", "3", 10); err != nil || len(rows) != 0 {
		t.Fatalf("purge kept ask rows: %+v %v", rows, err)
	}
}
