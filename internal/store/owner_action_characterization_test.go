package store

import (
	"errors"
	"testing"
	"time"
)

func TestOwnerActionCharacterizationExactProposalAndAtomicRollback(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	d := NaturalDialogue{Transport: "discord", OwnerID: "1", ChannelRef: "2", Revision: 1, State: "awaiting_confirmation", SourceID: "100", Payload: `{"invocation":{"Command":"diagnose","Arguments":{"issue":"3","note":"reviewed"}},"revision":"exact-revision"}`, Digest: "reviewed-digest", Code: "reviewed-code", ExpiresAt: at.Add(time.Minute), UpdatedAt: at}
	if err := db.SaveNaturalDialogue(ctx, d, 0); err != nil {
		t.Fatal(err)
	}
	if err := db.BindNaturalPrompt(ctx, d, "500"); err != nil {
		t.Fatal(err)
	}
	r := ChatReply{Transport: "discord", MessageID: "characterization-execution", ChannelRef: "2", Command: "diagnose", Arguments: map[string]string{"issue": "3", "note": "reviewed"}}
	for _, mutate := range []func(*NaturalDialogue){
		func(p *NaturalDialogue) { p.Payload = `{"revision":"replacement"}` },
		func(p *NaturalDialogue) { p.Digest = "replacement" },
		func(p *NaturalDialogue) { p.Code = "replacement" },
		func(p *NaturalDialogue) { p.Revision++ },
		func(p *NaturalDialogue) { p.OwnerID = "other" },
	} {
		changed := d
		mutate(&changed)
		if err := db.AuthorizeNaturalDialogue(ctx, changed, "101", r, at); !errors.Is(err, ErrNotFound) {
			t.Fatalf("changed proposal admitted: %v", err)
		}
	}
	// A conflicting inbox identity must roll back both admission and journal insertion.
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO chat_inbox(transport,message_id,channel_ref,command,arguments,state,next_attempt_at,created_at) VALUES(?,?,?,?,?,?,?,?)`, r.Transport, r.MessageID, r.ChannelRef, r.Command, `{}`, "pending", FormatTime(at), FormatTime(at)); err != nil {
		t.Fatal(err)
	}
	if err := db.AuthorizeNaturalDialogue(ctx, d, "101", r, at); err == nil {
		t.Fatal("conflicting inbox identity admitted")
	}
	current, err := db.ReadNaturalDialogue(ctx, d.Transport, d.OwnerID, d.ChannelRef)
	if err != nil || current.State != "awaiting_confirmation" || current.Payload != d.Payload {
		t.Fatalf("failed transaction consumed proposal: %+v %v", current, err)
	}
	if _, err = db.ReadNaturalExecution(ctx, r.MessageID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("failed transaction left execution: %v", err)
	}
	r.MessageID = "characterization-success"
	if err = db.AuthorizeNaturalDialogue(ctx, d, "102", r, at); err != nil {
		t.Fatal(err)
	}
	execution, err := db.ReadNaturalExecution(ctx, r.MessageID)
	if err != nil || execution.Payload != d.Payload || execution.Revision != d.Revision || execution.OwnerID != d.OwnerID {
		t.Fatalf("exact facts lost: %+v %v", execution, err)
	}
	if err = db.AuthorizeNaturalDialogue(ctx, d, "103", r, at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("proposal admitted twice: %v", err)
	}
}
