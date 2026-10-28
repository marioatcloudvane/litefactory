package daemon_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/messaging/feature"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

type ownerActionRecoveryForge struct {
	creates  int
	listed   []github.Issue
	observed github.Issue
	closed   bool
}

func (f *ownerActionRecoveryForge) CreateIssue(context.Context, github.Repo, redact.Body, redact.Body) (github.Issue, error) {
	f.creates++
	return github.Issue{}, errors.New("recovery must not create")
}
func (f *ownerActionRecoveryForge) ListIssuesPage(_ context.Context, _ github.Repo, state string, _, _ int) (github.IssuePage, error) {
	if state == github.StateOpen && !f.closed || state == "closed" && f.closed {
		return github.IssuePage{Items: f.listed}, nil
	}
	return github.IssuePage{}, nil
}
func (f *ownerActionRecoveryForge) GetIssue(context.Context, github.Repo, int) (github.Issue, error) {
	return f.observed, nil
}

func TestOwnerActionFeatureRecoveryNeverCreates(t *testing.T) {
	for _, mode := range []string{"expired-lease", "live-lease", "uncertain", "closed-issue", "absent", "ambiguous", "changed-body", "different-bot", "wrong-hash"} {
		t.Run(mode, func(t *testing.T) {
			ctx := t.Context()
			db, err := store.Open(ctx, filepath.Join(t.TempDir(), "recovery.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if closeErr := db.Close(); closeErr != nil {
					t.Error(closeErr)
				}
			})
			if err = db.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "app", BaseBranch: "main"})
			if err != nil {
				t.Fatal(err)
			}
			created := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
			p := store.ChatIssuePreview{ID: "recovery-preview", Transport: "discord", ChannelRef: "channel", SourceMessageID: "draft", Code: "abcdefgh", RepoID: repo.ID, Title: "Export report", Body: feature.Heading + "\n\n## Summary\nExport", CreatedAt: created, ExpiresAt: created.Add(24 * time.Hour)}
			p.ContentHash = feature.Hash(p.Title, p.Body)
			if mode == "wrong-hash" {
				p.ContentHash = "tampered"
			}
			if err = db.SaveChatIssuePreview(ctx, p); err != nil {
				t.Fatal(err)
			}
			claim, status, err := db.ClaimChatIssuePreview(ctx, p.Transport, p.ChannelRef, "confirmation", p.Code, created.Add(time.Second))
			if err != nil || status != "previewed" {
				t.Fatalf("claim: %s %v", status, err)
			}
			at := created.Add(10 * time.Second)
			expectedState := "creating"
			switch mode {
			case "expired-lease", "closed-issue":
				at = created.Add(6 * time.Minute)
				expectedState = "created"
			case "uncertain":
				if err = db.FinishChatIssuePreview(ctx, claim, "uncertain", 0, "", created.Add(2*time.Second)); err != nil {
					t.Fatal(err)
				}
				expectedState = "uncertain"
			}
			issue := github.Issue{Number: 7, Author: github.User{ID: 42}, Title: p.Title, Body: feature.IssueBody(p), HTMLURL: "https://github.com/acme/app/issues/7"}
			forge := &ownerActionRecoveryForge{listed: []github.Issue{issue}, observed: issue, closed: mode == "closed-issue"}
			switch mode {
			case "absent":
				forge.listed = nil
			case "ambiguous":
				other := issue
				other.Number = 8
				forge.listed = append(forge.listed, other)
			case "changed-body":
				forge.observed.Body += "\nchanged"
			case "different-bot":
				forge.observed.Author.ID = 99
			}
			creator := &daemon.ChatIssueCreator{DB: db, Forge: forge, BotUserID: 42}
			text, found, recoverErr := creator.RecoverOwnerActionFeature(ctx, p.Transport, p.ChannelRef, p.Code, at)
			success := mode == "expired-lease" || mode == "live-lease" || mode == "uncertain" || mode == "closed-issue"
			if success && (recoverErr != nil || !found || !strings.Contains(text, issue.HTMLURL)) {
				t.Fatalf("observed outcome missing: %q %v %v", text, found, recoverErr)
			}
			if !success && found {
				t.Fatalf("unverified outcome became success: %q %v", text, recoverErr)
			}
			if !success && mode != "absent" && recoverErr == nil {
				t.Fatal("ambiguous or changed facts were not refused")
			}
			saved, err := db.ChatIssuePreviewByCode(ctx, p.Transport, p.ChannelRef, p.Code)
			if err != nil {
				t.Fatal(err)
			}
			if saved.State != expectedState {
				t.Fatalf("unexpected claim mutation: %+v want %s", saved, expectedState)
			}
			if expectedState == "created" && (saved.IssueNumber != issue.Number || saved.IssueLink != issue.HTMLURL) {
				t.Fatalf("factual receipt missing: %+v", saved)
			}
			if forge.creates != 0 {
				t.Fatal("read-only recovery issued a create")
			}
			// Reconciliation may be repeated, but an absent marker never permits replay.
			_, again, err := creator.RecoverOwnerActionFeature(ctx, p.Transport, p.ChannelRef, p.Code, at)
			if success && (!again || err != nil) {
				t.Fatalf("repeated observation lost result: %v %v", again, err)
			}
			if forge.creates != 0 {
				t.Fatal("repeated recovery created an issue")
			}
		})
	}
}
