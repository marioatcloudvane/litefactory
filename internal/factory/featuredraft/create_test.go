package featuredraft_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/factory/featuredraft"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

type featureForge struct {
	mu      sync.Mutex
	creates int
	issue   github.Issue
	err     error
	order   []string
}

func (f *featureForge) CreateIssue(_ context.Context, _ github.Repo, title, body redact.Body) (github.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates++
	f.order = append(f.order, "create")
	if f.err != nil {
		return github.Issue{}, f.err
	}
	f.issue = github.Issue{Number: 7, Author: github.User{ID: 42}, Title: title.String(), Body: body.String(), HTMLURL: "https://github.com/acme/app/issues/7"}
	return f.issue, nil
}
func (f *featureForge) ListIssuesPage(_ context.Context, _ github.Repo, state string, _, _ int) (github.IssuePage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.order = append(f.order, "list:"+state)
	if state == "open" && f.issue.Number > 0 {
		return github.IssuePage{Items: []github.Issue{f.issue}}, nil
	}
	return github.IssuePage{}, nil
}
func (f *featureForge) GetIssue(context.Context, github.Repo, int) (github.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.order = append(f.order, "get")
	return f.issue, nil
}

func TestChatIssueCreatorConcurrentAndIntegrity(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "preview.db"))
	if err != nil {
		t.Fatal(err)
	}
	open := true
	t.Cleanup(func() {
		if !open {
			return
		}
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app", BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 10, 20, 0, 0, 0, time.UTC)
	p := store.ChatIssuePreview{ID: "preview", SourceMessageID: "draft", Transport: "test", ChannelRef: "channel", Code: "abcdefgh", RepoID: repo.ID, Title: "Export report", Body: featuredraft.Heading + "\n\n## Summary\nExport", CreatedAt: at, ExpiresAt: at.Add(24 * time.Hour)}
	p.ContentHash = featuredraft.Hash(p.Title, p.Body)
	if err := db.SaveChatIssuePreview(ctx, p); err != nil {
		t.Fatal(err)
	}
	forge := &featureForge{}
	creator := &featuredraft.Creator{DB: db, Forge: forge, BotUserID: 42, Redactor: redact.New(redact.Options{Environ: []string{}}), Now: func() time.Time { return at.Add(30 * time.Second) }}
	reply, err := creator.Confirm(ctx, "test", "channel", p.SourceMessageID, p.Code, at)
	if err != nil || !strings.Contains(reply.Text, "separate message") || forge.creates != 0 {
		t.Fatalf("same-message confirmation: %v %v", reply, err)
	}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			_, e := creator.Confirm(ctx, "test", "channel", strings.Repeat("c", i+1), p.Code, at)
			if e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
	if forge.creates != 1 || strings.Join(forge.order[:4], ",") != "list:open,list:closed,create,get" {
		t.Fatalf("duplicate or missing reconciliation: %d %v", forge.creates, forge.order)
	}
	reply, err = creator.Confirm(ctx, "test", "channel", "later", p.Code, at)
	if err != nil || !strings.Contains(reply.Text, forge.issue.HTMLURL) {
		t.Fatalf("repeat: %v %v", reply, err)
	}
	p.ID = "bad"
	p.SourceMessageID = "draft-bad"
	p.Code = "bad-hash"
	p.ContentHash = "tampered"
	if err := db.SaveChatIssuePreview(ctx, p); err != nil {
		t.Fatal(err)
	}
	reply, err = creator.Confirm(ctx, "test", "channel", "bad", p.Code, at)
	if err != nil || !strings.Contains(reply.Text, "Nothing was created") || forge.creates != 1 {
		t.Fatalf("integrity guard: %v %v", reply, err)
	}
	// A known failure is not retried by reprocessing the same interaction.
	p.ID = "known"
	p.SourceMessageID = "draft-known"
	p.Code = "knownerr"
	p.ContentHash = featuredraft.Hash(p.Title, p.Body)
	if err := db.SaveChatIssuePreview(ctx, p); err != nil {
		t.Fatal(err)
	}
	forge.err = github.ErrUnprocessable
	_, err = creator.Confirm(ctx, "test", "channel", "same", p.Code, at)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := db.CurrentChatIssuePreview(ctx, "test", "channel")
	if err != nil || !attempt.LastAttemptAt.Equal(at.Add(30*time.Second)) {
		t.Fatalf("create timing guard started before reconciliation: %+v %v", attempt, err)
	}
	before := forge.creates
	_, err = creator.Confirm(ctx, "test", "channel", "same", p.Code, at.Add(3*time.Minute))
	if err != nil || forge.creates != before {
		t.Fatal("same interaction retried creation")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	open = false
	_, err = creator.Confirm(ctx, "test", "channel", "fresh", p.Code, at)
	if err == nil || forge.creates != before {
		t.Fatal("storage failure allowed creation")
	}
}

func TestChatIssueUncertainMarkerRecovery(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "creation.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	p := store.ChatIssuePreview{ID: "uncertain", SourceMessageID: "draft", Transport: "test", ChannelRef: "channel", Code: "confirm1", RepoID: repo.ID, Title: "Export", Body: featuredraft.Heading + "\nExport", CreatedAt: at, ExpiresAt: at.Add(24 * time.Hour)}
	p.ContentHash = featuredraft.Hash(p.Title, p.Body)
	if err := db.SaveChatIssuePreview(ctx, p); err != nil {
		t.Fatal(err)
	}
	forge := &featureForge{err: errors.New("lost acknowledgment")}
	creator := &featuredraft.Creator{DB: db, Forge: forge, BotUserID: 42, Redactor: redact.New(redact.Options{Environ: []string{}})}
	reply, err := creator.Confirm(ctx, "test", "channel", "confirm-1", p.Code, at)
	if err != nil || !strings.Contains(reply.Text, "uncertain") || forge.creates != 1 {
		t.Fatalf("initial create: %v %v %d", reply, err, forge.creates)
	}
	reply, err = creator.Confirm(ctx, "test", "channel", "confirm-2", p.Code, at.Add(time.Minute))
	if err != nil || !strings.Contains(reply.Text, "2 minutes") || forge.creates != 1 {
		t.Fatalf("retry guard: %v %v %d", reply, err, forge.creates)
	}
	forge.issue = github.Issue{Number: 7, Author: github.User{ID: 42}, Title: p.Title, Body: featuredraft.IssueBody(p), HTMLURL: "https://github.com/acme/app/issues/7"}
	reply, err = creator.Confirm(ctx, "test", "channel", "confirm-3", p.Code, at.Add(3*time.Minute))
	if err != nil || !strings.Contains(reply.Text, forge.issue.HTMLURL) || forge.creates != 1 {
		t.Fatalf("marker recovery: %v %v %d", reply, err, forge.creates)
	}
}
