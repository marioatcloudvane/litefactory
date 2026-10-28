package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestOwnerActionUncertainFeatureRetainsExactPreview(t *testing.T) {
	db, p := ownerActionStoreFixture(t)
	repo, err := db.UpsertRepo(t.Context(), Repo{Path: filepath.Join(t.TempDir(), "repo"), Forge: "github", Owner: "acme", Name: "web"})
	if err != nil {
		t.Fatal(err)
	}
	preview := ChatIssuePreview{ID: "retained-preview", SourceMessageID: "draft", Transport: "mcp", ChannelRef: "grant", Code: "abcdefgh", RepoID: repo.ID, Title: "Exact title", Body: "Exact body", ContentHash: "fixed", CreatedAt: p.CreatedAt, ExpiresAt: p.CreatedAt.Add(24 * time.Hour)}
	if err = db.SaveChatIssuePreview(t.Context(), preview); err != nil {
		t.Fatal(err)
	}
	p.ID = "feature-retention"
	p.ExecutionID = "owner-action:" + p.ID
	p.RequestKey = p.ID
	p.Facts = `{"target_id":"retained-preview"}`
	if err = db.CreateOwnerAction(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if _, err = db.MutateOwnerAction(t.Context(), p.Principal, p.ID, "confirm", "", p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ClaimOwnerActionExecution(t.Context(), p.ID, p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	if err = db.FinishOwnerAction(t.Context(), p.ID, "uncertain", `{"state":"uncertain"}`, "inspect", p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	if err = db.PurgeChatIssuePreviews(t.Context(), p.CreatedAt.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	saved, err := db.ChatIssuePreviewByCode(t.Context(), preview.Transport, preview.ChannelRef, preview.Code)
	if err != nil || saved.Title != preview.Title || saved.Body != preview.Body {
		t.Fatalf("uncertain exact input lost: %+v %v", saved, err)
	}
	if err = db.FinishOwnerAction(t.Context(), p.ID, "succeeded", `{"state":"succeeded"}`, "reconciled", p.CreatedAt.Add(49*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = db.PurgeChatIssuePreviews(t.Context(), p.CreatedAt.Add(50*time.Hour)); err != nil {
		t.Fatal(err)
	}
	saved, err = db.ChatIssuePreviewByCode(t.Context(), preview.Transport, preview.ChannelRef, preview.Code)
	if !errors.Is(err, ErrNotFound) && (err != nil || saved.State != "expired" || saved.Title != "" || saved.Body != "") {
		t.Fatalf("resolved payload retained: %+v %v", saved, err)
	}
}
