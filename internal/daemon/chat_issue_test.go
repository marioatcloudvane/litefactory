package daemon_test

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/messaging/feature"
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
	p := store.ChatIssuePreview{ID: "preview", SourceMessageID: "draft", Transport: "test", ChannelRef: "channel", Code: "abcdefgh", RepoID: repo.ID, Title: "Export report", Body: feature.Heading + "\n\n## Summary\nExport", CreatedAt: at, ExpiresAt: at.Add(24 * time.Hour)}
	p.ContentHash = feature.Hash(p.Title, p.Body)
	if err := db.SaveChatIssuePreview(ctx, p); err != nil {
		t.Fatal(err)
	}
	forge := &featureForge{}
	creator := &daemon.ChatIssueCreator{DB: db, Forge: forge, BotUserID: 42, Redactor: redact.New(redact.Options{Environ: []string{}}), Now: func() time.Time { return at.Add(30 * time.Second) }}
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
	reply, err := creator.Confirm(ctx, "test", "channel", "later", p.Code, at)
	if err != nil || !strings.Contains(reply, forge.issue.HTMLURL) {
		t.Fatalf("repeat: %s %v", reply, err)
	}
	p.ID = "bad"
	p.SourceMessageID = "draft-bad"
	p.Code = "bad-hash"
	p.ContentHash = "tampered"
	if err := db.SaveChatIssuePreview(ctx, p); err != nil {
		t.Fatal(err)
	}
	reply, err = creator.Confirm(ctx, "test", "channel", "bad", p.Code, at)
	if err != nil || !strings.Contains(reply, "Nothing was created") || forge.creates != 1 {
		t.Fatalf("integrity guard: %s %v", reply, err)
	}
	// A known failure is not retried by reprocessing the same interaction.
	p.ID = "known"
	p.SourceMessageID = "draft-known"
	p.Code = "knownerr"
	p.ContentHash = feature.Hash(p.Title, p.Body)
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

func TestChatCreatedFeatureDiscoveryStaysIdle(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	p := store.ChatIssuePreview{ID: "preview", Body: feature.Heading + "\n\n## Summary\nExport reports\n\n## Problem\nManual copying\n\n## Wanted outcome\nDownload\n\n## Origin", CreatedAt: time.Date(2026, 10, 10, 20, 0, 0, 0, time.UTC)}
	f.forge.addIssue(fakeIssue{Number: testIssue, Title: "Export report", Body: feature.IssueBody(p), User: bot()})
	f.settle()
	if len(f.turns.calls()) != 0 || len(f.runs()) != 0 {
		t.Fatal("chat-created issue started work")
	}
	posted, reacted, _ := f.forge.snapshot()
	if len(posted) != 0 || len(reacted) != 0 {
		t.Fatal("chat-created issue entered admission")
	}
}
