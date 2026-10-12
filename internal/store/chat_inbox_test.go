package store

import (
	"errors"
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

func TestDeliveredRepliesKeepRawDetailsForDetails(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	deliver := func(id, channel, command, raw string, failed bool, when time.Time) {
		t.Helper()
		r := ChatReply{Transport: "discord", MessageID: id, ChannelRef: channel, Command: command, CreatedAt: when, PresentationState: "pending"}
		if command == "ask" {
			r.Arguments = map[string]string{"question": "Q " + id}
		}
		if _, err := db.AdmitChatReply(ctx, r); err != nil {
			t.Fatal(err)
		}
		claim, err := db.ClaimChatReply(ctx, "discord", when)
		if err != nil || claim.MessageID != id {
			t.Fatalf("claim: %+v %v", claim, err)
		}
		if err = db.SaveChatReply(ctx, claim, raw); err != nil {
			t.Fatal(err)
		}
		if err = db.SaveChatOutcome(ctx, claim, `{"source":"`+id+`"}`); err != nil {
			t.Fatal(err)
		}
		if err = db.SaveChatPresentation(ctx, claim, "narrated", "narration "+id, raw); err != nil {
			t.Fatal(err)
		}
		if err = db.FinishChatReply(ctx, claim, failed, when); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ReadLatestReplyDetails(ctx, "discord", "3"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty channel: %v", err)
	}
	deliver("1", "3", "ask", "raw ask", false, at)
	deliver("2", "3", "health", "raw health", false, at.Add(time.Minute))
	for _, id := range []string{"1", "2"} {
		r, err := db.ReadChatReply(ctx, "discord", id)
		if err != nil || r.ReplyAttachment != "raw "+map[string]string{"1": "ask", "2": "health"}[id] {
			t.Fatalf("row %s lost raw details: %+v %v", id, r, err)
		}
	}
	deliver("3", "3", "details", "raw health", false, at.Add(2*time.Minute))
	deliver("4", "3", "status", "raw failed", true, at.Add(3*time.Minute))
	deliver("5", "4", "status", "raw other channel", false, at.Add(4*time.Minute))
	if failed, err := db.ReadChatReply(ctx, "discord", "4"); err != nil || failed.ReplyAttachment != "" {
		t.Fatalf("failed row kept raw details: %+v %v", failed, err)
	}
	if raw, err := db.ReadLatestReplyDetails(ctx, "discord", "3"); err != nil || raw != "raw health" {
		t.Fatalf("latest details %q %v", raw, err)
	}
	if raw, err := db.ReadLatestReplyDetails(ctx, "discord", "4"); err != nil || raw != "raw other channel" {
		t.Fatalf("other channel details %q %v", raw, err)
	}
	if rows, err := db.ReadAskReplies(ctx, "discord", "3", 10); err != nil || len(rows) != 1 || rows[0].ReplyAttachment != "raw ask" {
		t.Fatalf("ask history: %+v %v", rows, err)
	}
	if err := db.PurgeChatReplies(ctx, at.Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ReadLatestReplyDetails(ctx, "discord", "3"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("purge kept details: %v", err)
	}
}
