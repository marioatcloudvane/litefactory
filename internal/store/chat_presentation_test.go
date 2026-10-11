package store

import (
	"errors"
	"testing"
	"time"
)

func TestChatPresentationRecoveryFencesAndCleanup(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	input := ChatReply{Transport: "discord", MessageID: "1", ChannelRef: "2", Command: "health", CreatedAt: at, PresentationState: "pending"}
	if _, err := db.AdmitChatReply(ctx, input); err != nil {
		t.Fatal(err)
	}
	r, err := db.ClaimChatReply(ctx, "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SaveChatReply(ctx, r, "original"); err != nil {
		t.Fatal(err)
	}
	if err = db.SaveChatOutcome(ctx, r, `{"source":"original"}`); err != nil {
		t.Fatal(err)
	}
	if err = db.SaveChatOutcome(ctx, r, `{"source":"changed"}`); !errors.Is(err, ErrNotFound) {
		t.Fatalf("source is mutable: %v", err)
	}
	if err = db.SaveChatReply(ctx, r, "changed after source"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("immutable source overwritten: %v", err)
	}
	if err = db.BeginChatPresentation(ctx, r, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("checker before generation: %v", err)
	}
	if err = db.BeginChatPresentation(ctx, r, false); err != nil {
		t.Fatal(err)
	}
	if err = db.BeginChatPresentation(ctx, r, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unbounded generation: %v", err)
	}
	newClaim, err := db.ClaimChatReply(ctx, "discord", at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !newClaim.PresentationGeneration || newClaim.PresentationOutcome == "" || newClaim.ReplyText != "original" {
		t.Fatalf("restart lost source: %+v", newClaim)
	}
	if err = db.SaveChatPresentation(ctx, r, "narrated", "stale", "original"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale worker saved: %v", err)
	}
	if err = db.SaveChatPresentation(ctx, newClaim, "raw_fallback", "fixed", "original"); err != nil {
		t.Fatal(err)
	}
	if err = db.SaveChatPresentation(ctx, newClaim, "narrated", "changed", "other"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("final payload mutable: %v", err)
	}
	if err = db.InlineChatPresentation(ctx, newClaim, "original\n\nRaw result."); err != nil {
		t.Fatal(err)
	}
	if err = db.FinishChatReply(ctx, newClaim, false, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	terminal, err := db.ReadChatReply(ctx, "discord", "1")
	if err != nil {
		t.Fatal(err)
	}
	if terminal.PresentationState != "" || terminal.PresentationOutcome != "" || terminal.ReplyAttachment != "" || terminal.PresentationGeneration || terminal.PresentationCheck {
		t.Fatalf("content retained: %+v", terminal)
	}
}

func TestPresentationMeterRealOwnersAndPinnedPricing(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	repo := testRepo(t, db, "acme", "app")
	factory, err := db.StartPresentationMeterAttempt(ctx, 0, "factory-reply", "generation", "claude")
	if err != nil {
		t.Fatal(err)
	}
	checking, err := db.StartPresentationMeterAttempt(ctx, repo.ID, "repo-reply", "checking", "claude")
	if err != nil {
		t.Fatal(err)
	}
	for _, attempt := range []MeterAttempt{factory, checking} {
		if attempt.PriceRevisionID < 1 || attempt.RunID != 0 || attempt.ChatRepoID != 0 {
			t.Fatalf("wrong ownership: %+v", attempt)
		}
		if err := db.FinishMeterAttempt(ctx, attempt.ID, "success", "unknown", "", "", nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, filter := range []MeterFilter{{}, {RepoID: repo.ID}} {
		snapshot, err := db.NewMeterSnapshot(ctx, filter, MeterSnapshotOptions{})
		if err != nil {
			t.Fatal(err)
		}
		rows, err := snapshot.Next(ctx, 100)
		if err != nil {
			t.Fatal(err)
		}
		if err = snapshot.Close(ctx); err != nil {
			t.Fatal(err)
		}
		want := 2
		if filter.RepoID != 0 {
			want = 1
		}
		if len(rows) != want {
			t.Fatalf("ownership filter: %+v", rows)
		}
		for _, row := range rows {
			if row.AttemptID == factory.ID && (row.Project != "factory" || row.ExecutionPath != "chat_presentation_generation") {
				t.Fatalf("factory borrowed repository: %+v", row)
			}
			if row.AttemptID == checking.ID && (row.Project != "github:acme/app" || row.ExecutionPath != "chat_presentation_checking") {
				t.Fatalf("repository owner lost: %+v", row)
			}
		}
	}
}
