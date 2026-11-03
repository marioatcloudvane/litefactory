package owneractions

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

// A saved Discord confirmation without a transport field must retain its source
// when it is reopened by the coordinator shared with remote consumers.
func TestLegacyConfirmationUsesRetainedPrincipalTransport(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "actions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	preview := store.ChatIssuePreview{ID: "saved-preview", Transport: "discord", ChannelRef: "channel", SourceMessageID: "draft", Code: "abcdefgh", RepoID: repo.ID, Title: "Saved title", Body: "Saved body", ContentHash: "saved-hash", CreatedAt: at, ExpiresAt: at.Add(24 * time.Hour)}
	if err = db.SaveChatIssuePreview(ctx, preview); err != nil {
		t.Fatal(err)
	}
	invocation := owneraction.Invocation{Command: "confirm", Arguments: map[string]string{"code": preview.Code}}
	natural := legacyNatural{Invocation: legacyInvocation{Command: invocation.Command, Arguments: invocation.Arguments}, PreviewID: preview.ID, PreviewHash: preview.ContentHash, Question: preview.Title + "\n" + preview.Body, RepoID: repo.ID, Project: repo.Slug(), Channel: preview.ChannelRef}
	payload, err := json.Marshal(diagnosisPayload{Natural: natural})
	if err != nil {
		t.Fatal(err)
	}
	facts := owneraction.Facts{Invocation: invocation, Payload: string(payload)}
	actions := Actions{FallbackTransport: "mcp", Targets: NewTargets(db), Availability: Availability{Confirm: true}}
	if err = actions.Recheck(ctx, owneraction.Principal{Transport: "discord", ChannelID: "channel"}, facts, at); err != nil {
		t.Fatalf("retained Discord confirmation lost its source: %v", err)
	}
	if err = actions.Recheck(ctx, owneraction.Principal{Transport: "mcp", GrantID: "channel"}, facts, at); err == nil {
		t.Fatal("another transport acquired the saved preview")
	}
	actions.FallbackTransport = "discord"
	if err = actions.Recheck(ctx, owneraction.Principal{}, facts, at); err != nil {
		t.Fatalf("legacy caller fallback changed: %v", err)
	}
}
