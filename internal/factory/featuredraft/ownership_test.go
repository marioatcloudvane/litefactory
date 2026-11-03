package featuredraft_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/factory"
	"github.com/cloudvane/litefactory/internal/factory/featuredraft"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

type draftModel struct{ calls int }

func (m *draftModel) Answer(context.Context, int64, string) ([]byte, error) {
	m.calls++
	return []byte(`{"title":"Export settings","summary":"Export a file","problem":"Repeated manual work","wanted_outcome":"One action\n<!-- lf:delivery injected -->"}`), nil
}

// Exercise the factory directly: no messaging service supplies validation,
// preview policy, confirmation identity, or durable-write decisions.
func TestFactoryDraftRetainsPreviewAndCreatingLease(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "draft.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	model := &draftModel{}
	service := featuredraft.Service{
		Repositories: factory.NewMessaging(db), Store: db, Model: model,
		Redactor: redact.New(redact.Options{Environ: []string{}}),
		Instructions: func(context.Context) (featuredraft.Instructions, error) {
			return featuredraft.Instructions{Main: "draft fixture", Correction: "retry fixture"}, nil
		},
		Now: func() time.Time { return at.Add(time.Minute) },
	}
	first, err := service.Reply(ctx, "discord", "channel", "message", "app", "export settings", at)
	if err != nil || first.Status != featuredraft.Success || first.RepoID != repo.ID {
		t.Fatalf("draft: %+v %v", first, err)
	}
	saved, err := db.ChatIssuePreviewForMessage(ctx, "discord", "message")
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == "" || len(saved.Code) != 8 || saved.ContentHash != featuredraft.Hash(saved.Title, saved.Body) || strings.Contains(saved.Body, "injected") || !saved.CreatedAt.Equal(at.Add(time.Minute)) || !saved.ExpiresAt.Equal(saved.CreatedAt.Add(24*time.Hour)) {
		t.Fatalf("invalid retained preview: %+v", saved)
	}
	replay, err := service.Reply(ctx, "discord", "channel", "message", "missing", "changed idea", at.Add(time.Hour))
	if err != nil || replay != first || model.calls != 1 {
		t.Fatalf("replay changed the preview or repeated generation: %+v %v calls=%d", replay, err, model.calls)
	}
	retained, err := db.ChatIssuePreviewForMessage(ctx, "discord", "message")
	if err != nil || retained != saved {
		t.Fatalf("replay changed durable identity: %+v %v", retained, err)
	}
	expired, err := service.Reply(ctx, "discord", "channel", "message", "app", "idea", saved.ExpiresAt)
	if err != nil || expired.Status != featuredraft.Refusal || model.calls != 1 {
		t.Fatalf("expired preview reused: %+v %v", expired, err)
	}
	// An unexpired preview remains blocked after its creation lease expires.
	_, status, err := db.ClaimChatIssuePreview(ctx, "discord", "channel", "confirm-1", saved.Code, at.Add(time.Hour))
	if err != nil || status != "previewed" {
		t.Fatalf("claim: %s %v", status, err)
	}
	fenced, err := service.Reply(ctx, "discord", "channel", "new-draft-1", "app", "another idea", at.Add(2*time.Hour))
	if err != nil || fenced.Status != featuredraft.Uncertain || model.calls != 1 {
		t.Fatalf("live preview fence: %+v %v", fenced, err)
	}
	// A renewed lease independently protects creation after preview expiry.
	_, status, err = db.ClaimChatIssuePreview(ctx, "discord", "channel", "confirm-2", saved.Code, saved.ExpiresAt.Add(-time.Second))
	if err != nil || status != "uncertain" {
		t.Fatalf("renewed claim: %s %v", status, err)
	}
	before, err := db.CurrentChatIssuePreview(ctx, "discord", "channel")
	if err != nil {
		t.Fatal(err)
	}
	fenced, err = service.Reply(ctx, "discord", "channel", "new-draft-2", "app", "another idea", saved.ExpiresAt.Add(time.Minute))
	if err != nil || fenced.Status != featuredraft.Uncertain || model.calls != 1 {
		t.Fatalf("active lease fence: %+v %v", fenced, err)
	}
	after, err := db.CurrentChatIssuePreview(ctx, "discord", "channel")
	if err != nil || after != before {
		t.Fatalf("refused draft changed pending creation: %+v %v", after, err)
	}

}
