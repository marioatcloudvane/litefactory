package store

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestChatIssuePreviewClaimsAndRetention(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "acme", "app")
	at := time.Date(2026, 10, 10, 19, 0, 0, 0, time.UTC)
	makePreview := func(id string, when time.Time) ChatIssuePreview {
		return ChatIssuePreview{ID: id, Transport: "test", ChannelRef: "channel", SourceMessageID: "draft-" + id, Code: fmt.Sprintf("%08s", id), ContentHash: "hash", Title: "Feature", Body: "Body", RepoID: repo.ID, CreatedAt: when, ExpiresAt: when.Add(24 * time.Hour)}
	}
	first := makePreview("one", at)
	if err := db.SaveChatIssuePreview(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	second := makePreview("two", at.Add(time.Minute))
	if err := db.SaveChatIssuePreview(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	old, err := db.ChatIssuePreviewForMessage(t.Context(), "test", first.SourceMessageID)
	if err != nil || old.State != "superseded" || old.Title != "" || old.Body != "" {
		t.Fatalf("superseded text retained: %+v %v", old, err)
	}
	for _, tc := range []struct{ channel, message, code, status string }{{"elsewhere", "confirm", second.Code, "unknown"}, {"channel", "confirm", first.Code, "superseded"}, {"channel", second.SourceMessageID, second.Code, "separate"}} {
		_, status, err := db.ClaimChatIssuePreview(t.Context(), "test", tc.channel, tc.message, tc.code, at.Add(2*time.Minute))
		if err != nil || status != tc.status {
			t.Fatalf("claim %s: %s %v", tc.status, status, err)
		}
	}
	var wg sync.WaitGroup
	results := make(chan string, 8)
	for i := range 8 {
		wg.Go(func() {
			_, status, err := db.ClaimChatIssuePreview(t.Context(), "test", "channel", fmt.Sprint(i), second.Code, at.Add(2*time.Minute))
			if err != nil {
				results <- err.Error()
			} else {
				results <- status
			}
		})
	}
	wg.Wait()
	close(results)
	claimed := 0
	for status := range results {
		if status == "previewed" {
			claimed++
		} else if status != "busy" {
			t.Errorf("unexpected concurrent status %s", status)
		}
	}
	if claimed != 1 {
		t.Fatalf("%d simultaneous claims", claimed)
	}
	p, err := db.CurrentChatIssuePreview(t.Context(), "test", "channel")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveChatIssuePreview(t.Context(), makePreview("three", at.Add(3*time.Minute))); err == nil {
		t.Fatal("replaced live create")
	}
	_, status, err := db.ClaimChatIssuePreview(t.Context(), "test", "channel", "recovery", second.Code, at.Add(8*time.Minute))
	if err != nil || status != "uncertain" {
		t.Fatalf("recovery: %s %v", status, err)
	}
	if err := db.FinishChatIssuePreview(t.Context(), p, "created", 12, "https://example/12", at.Add(9*time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale claim not fenced: %v", err)
	}
	recovered, err := db.CurrentChatIssuePreview(t.Context(), "test", "channel")
	if err != nil {
		t.Fatal(err)
	}
	if !recovered.LastAttemptAt.Equal(at.Add(2 * time.Minute)) {
		t.Fatal("recovery reset attempt timing")
	}
	if err := db.FinishChatIssuePreview(t.Context(), recovered, "created", 12, "https://example/12", second.ExpiresAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	created, status, err := db.ClaimChatIssuePreview(t.Context(), "test", "channel", "repeat", second.Code, second.ExpiresAt.Add(time.Minute))
	if err != nil || status != "created" || created.IssueLink != "https://example/12" || created.Title != "" || created.Body != "" {
		t.Fatalf("created outcome: %+v %s %v", created, status, err)
	}
	if err := db.PurgeChatIssuePreviews(t.Context(), created.FinishedAt.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	_, status, err = db.ClaimChatIssuePreview(t.Context(), "test", "channel", "after-purge", second.Code, created.FinishedAt.Add(24*time.Hour))
	if err != nil || status != "unknown" {
		t.Fatalf("purged code: %s %v", status, err)
	}
	expired := makePreview("four", at.Add(50*time.Hour))
	if err := db.SaveChatIssuePreview(t.Context(), expired); err != nil {
		t.Fatal(err)
	}
	p, status, err = db.ClaimChatIssuePreview(t.Context(), "test", "channel", "at-expiry", expired.Code, expired.ExpiresAt)
	if err != nil || status != "expired" {
		t.Fatalf("expiry accepted: %+v %s %v", p, status, err)
	}
	p, err = db.ChatIssuePreviewForMessage(t.Context(), "test", expired.SourceMessageID)
	if err != nil || p.Title != "" || p.Body != "" {
		t.Fatalf("expired content retained: %+v %v", p, err)
	}
	late := makePreview("late", at.Add(100*time.Hour))
	if err := db.SaveChatIssuePreview(t.Context(), late); err != nil {
		t.Fatal(err)
	}
	collision := makePreview("collision", late.CreatedAt.Add(time.Hour))
	collision.Code = late.Code
	if err := db.SaveChatIssuePreview(t.Context(), collision); err == nil {
		t.Fatal("code collision accepted")
	}
	p, err = db.CurrentChatIssuePreview(t.Context(), "test", "channel")
	if err != nil || p.ID != late.ID || p.Title == "" {
		t.Fatalf("failed save superseded old preview: %+v %v", p, err)
	}
	_, status, err = db.ClaimChatIssuePreview(t.Context(), "test", "channel", "late-confirm", late.Code, late.ExpiresAt.Add(23*time.Hour))
	if err != nil || status != "expired" {
		t.Fatalf("late expiry: %s %v", status, err)
	}
	p, err = db.ChatIssuePreviewForMessage(t.Context(), "test", late.SourceMessageID)
	if err != nil || !p.FinishedAt.Equal(late.ExpiresAt) {
		t.Fatalf("retention restarted after late expiry: %+v %v", p, err)
	}
	if err := db.PurgeChatIssuePreviews(t.Context(), late.ExpiresAt.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ChatIssuePreviewForMessage(t.Context(), "test", late.SourceMessageID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("late-expired code retained: %v", err)
	}

}
