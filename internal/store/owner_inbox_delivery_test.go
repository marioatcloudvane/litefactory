package store

import (
	"errors"
	"testing"
	"time"
)

func TestOwnerInboxWithheldReplyPersistsWithoutChangingOriginalAndFencesLease(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Now().UTC()
	r := ChatReply{Transport: "discord", MessageID: "reply", ChannelRef: "room", Command: "conversation", ReplyText: "protected original", CreatedAt: at}
	if _, err := db.AdmitChatReply(ctx, r); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `UPDATE chat_inbox SET state='sent',attempts=1,next_attempt_at=? WHERE transport=? AND message_id=?`, FormatTime(at.Add(time.Minute)), r.Transport, r.MessageID); err != nil {
		t.Fatal(err)
	}
	r.Attempts = 1
	stale := r
	stale.Attempts = 0
	if _, err := db.SaveInboxWithheldReply(ctx, stale, at); !errors.Is(err, ErrNotFound) {
		t.Fatal("stale lease", err)
	}
	if _, err := db.SaveInboxWithheldReply(ctx, r, at.Add(time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired lease", err)
	}
	notice, err := db.SaveInboxWithheldReply(ctx, r, at)
	if err != nil {
		t.Fatal(err)
	}
	if notice == "" || notice == r.ReplyText {
		t.Fatal("unsafe substitution")
	}
	saved, err := db.ReadChatReply(ctx, r.Transport, r.MessageID)
	if err != nil || saved.ReplyText != r.ReplyText {
		t.Fatal("original changed", err)
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE chat_inbox SET attempts=2 WHERE transport=? AND message_id=?`, r.Transport, r.MessageID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReadInboxWithheldReply(ctx, r); !errors.Is(err, ErrNotFound) {
		t.Fatal("stale read", err)
	}
	r.Attempts = 2
	replay, err := db.ReadInboxWithheldReply(ctx, r)
	if err != nil || replay != notice {
		t.Fatal("retry changed substitution", err)
	}
}
