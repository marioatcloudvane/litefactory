package store

import (
	"errors"
	"testing"
	"time"
)

func TestChatDiagnosisBusyAndRestartAllowance(t *testing.T) {
	db, _ := chatResearchFixture(t)
	ctx, at := t.Context(), time.Now().UTC()
	_, err := db.AdmitChatReply(ctx, ChatReply{Transport: "discord", MessageID: "1", ChannelRef: "2", Command: "diagnose", CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ClaimChatReply(ctx, "discord", at); err != nil {
		t.Fatal(err)
	}
	if err = db.DeferChatDiagnosis(ctx, "discord", "1", "github:acme/api#1", at); err != nil {
		t.Fatal(err)
	}
	r, err := db.ClaimChatDiagnosis(ctx, "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.RequeueChatDiagnosisBusy(ctx, r, at); err != nil {
		t.Fatal(err)
	}
	r, err = db.ClaimChatDiagnosis(ctx, "discord", at)
	if err != nil || r.AnswerAttempts != 1 {
		t.Fatalf("first attempt: %+v %v", r, err)
	}
	if _, err = db.ClaimChatDiagnosis(ctx, "discord", at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("active lease: %v", err)
	}
	for attempt := 2; attempt <= 3; attempt++ {
		if err = db.RecoverChatDiagnoses(ctx, "discord", at); err != nil {
			t.Fatal(err)
		}
		r, err = db.ClaimChatDiagnosis(ctx, "discord", at)
		if err != nil {
			t.Fatal(err)
		}
		if r.AnswerAttempts != 2 || (r.State == "exhausted") != (attempt == 3) {
			t.Fatalf("restart %d: %+v", attempt, r)
		}
	}
	if err = db.CompleteChatDiagnosis(ctx, "discord", "1", 2, "diagnosis failed", at); err != nil {
		t.Fatal(err)
	}
	r, err = db.ClaimChatReply(ctx, "discord", at)
	if err != nil || r.ReplyText != "diagnosis failed" {
		t.Fatalf("failure delivery: %+v %v", r, err)
	}
	if err = db.FinishChatReply(ctx, r, false, at); err != nil {
		t.Fatal(err)
	}
	r, err = db.ReadChatReply(ctx, "discord", "1")
	if err != nil || r.TargetRef != "" {
		t.Fatalf("target retained: %+v %v", r, err)
	}
}
