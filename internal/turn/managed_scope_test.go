package turn

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

type scopeTestForge struct {
	publicationForge
	comments     map[int][]github.Comment
	posts        int
	loseResponse bool
}

func (f *scopeTestForge) CreateSubIssues(context.Context, github.Repo, int, []github.SubIssueSpec) ([]github.SubIssueResult, error) {
	return nil, errors.New("unexpected legacy publication")
}
func (f *scopeTestForge) ListCommentsAfter(_ context.Context, _ github.Repo, n int, _ int64) ([]github.Comment, error) {
	return f.comments[n], nil
}
func (f *scopeTestForge) PostComment(_ context.Context, _ github.Repo, n int, b redact.Body) (github.Comment, error) {
	f.posts++
	c := github.Comment{ID: int64(f.posts), Body: b.String(), Author: github.User{ID: 9}}
	f.comments[n] = append(f.comments[n], c)
	if f.loseResponse {
		f.loseResponse = false
		return github.Comment{}, errors.New("response lost")
	}
	return c, nil
}
func TestFreezeNoticesRecoverOriginalRevisionAfterRestart(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	ref, err := naming.ParseRef("github:o/r#1")
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: 65536})
	if err != nil {
		t.Fatal(err)
	}
	forge := &scopeTestForge{publicationForge: publicationForge{attached: true}, comments: map[int][]github.Comment{}, loseResponse: true}
	body := "## User Story\nOwner sees full details.\n## Acceptance Criteria\nFull acceptance.\n## Tasks\nEvery task.\n## Architecture\nEvery technical detail.\n## Dependencies\nNone.\n"
	if err := db.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: ref.String(), Generation: 1, State: "planning"}); err != nil {
		t.Fatal(err)
	}
	for i, key := range []string{"US-1", "US-2"} {
		issue := github.Issue{ID: int64(200 + i), Number: 2 + i, Title: key, Body: body, State: github.StateOpen}
		forge.issues = append(forge.issues, issue)
		if err := db.PutManagedStory(ctx, store.ManagedStory{ParentTask: ref.String(), Generation: 1, Key: key, IssueNumber: issue.Number, IssueID: issue.ID, BodyHash: planbrief.StoryHash(issue.Title, issue.Body), State: "planning"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.UpsertIdentity(ctx, store.Identity{Forge: "github", Role: store.RoleBot, UserID: 9}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetManagedFeatureReady(ctx, ref.String(), 1, true); err != nil {
		t.Fatal(err)
	}
	o := Options{DB: db, SubIssueCreator: forge, SubIssueRenderer: renderer}
	req := Request{Mode: agent.ModeImplement}
	forge.issues[0].Body += "manual dependency edit"
	if err := prepareManagedScope(ctx, o, req, ref); err == nil || !strings.Contains(err.Error(), "refresh and publish") {
		t.Fatalf("manual edit bypassed Plan metadata validation: %v", err)
	}
	if forge.posts != 0 {
		t.Fatal("invalid scope announced before approval")
	}
	forge.issues[0].Body = body
	if err := prepareManagedScope(ctx, o, req, ref); err == nil {
		t.Fatal("lost notice response allowed launch")
	}
	f, err := db.GetManagedFeature(ctx, ref.String())
	if err != nil || f.State != "freezing" {
		t.Fatalf("scope advanced: %+v %v", f, err)
	}
	if forge.posts != 2 {
		t.Fatalf("independent notices not attempted: %d", forge.posts)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	o.DB = db
	forge.issues[0].Body = "changed on GitHub after approval"
	if err := prepareManagedScope(ctx, o, req, ref); err != nil {
		t.Fatal(err)
	}
	if forge.posts != 2 {
		t.Fatalf("uncertain successful notice duplicated: %d", forge.posts)
	}
	revisions, err := db.ListApprovedStoryRevisions(ctx, ref.String(), 1)
	if err != nil || len(revisions) != 2 || revisions[0].Body != body {
		t.Fatalf("approved scope drifted: %+v %v", revisions, err)
	}
	for _, comments := range forge.comments {
		if !strings.Contains(comments[0].Body, "replan") || !strings.Contains(comments[0].Body, "approved revision") {
			t.Fatal("notice omitted scope or planning direction")
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := prepareManagedScope(cancelled, o, req, ref); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled preparation accepted: %v", err)
	}
}
func TestManagedReplanRequiresExactParentControl(t *testing.T) {
	for _, text := range []string{"replan", " RePlan ", "/plan revise unfinished stories"} {
		if !ManagedReplan(ParseCommand(text)) {
			t.Fatalf("explicit control rejected: %q", text)
		}
	}
	for _, text := range []string{"please replan", "quoted replan", "/spike replan", "replan the story", "<note>replan</note>"} {
		if ManagedReplan(ParseCommand(text)) {
			t.Fatalf("incidental text unlocked: %q", text)
		}
	}
}

type linearScopeForge struct {
	scopeTestForge
	actor ticket.Identity
	bot   int64
}

func (f *linearScopeForge) BotIdentityFor(context.Context, github.Repo, int) (ticket.Identity, error) {
	return f.actor, nil
}
func (f *linearScopeForge) PostComment(_ context.Context, _ github.Repo, n int, body redact.Body) (github.Comment, error) {
	f.posts++
	c := github.Comment{ID: int64(f.posts), Body: body.String(), Author: github.User{ID: f.bot}}
	f.comments[n] = append(f.comments[n], c)
	return c, nil
}
func TestLinearScopeNoticesUseContextualPublisherWithoutGlobalIdentity(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "scope.db"))
	mustProgress(t, err)
	defer func() { _ = db.Close() }()
	mustProgress(t, db.Migrate(ctx))
	actor := ticket.Identity{Provider: ticket.Linear, Origin: "https://linear.example/graphql", RemoteID: "verified-bot"}
	slot, err := db.LocalTicketSlot(ctx, "actor", actor)
	mustProgress(t, err)
	forge := &linearScopeForge{scopeTestForge: scopeTestForge{publicationForge: publicationForge{attached: true}, comments: map[int][]github.Comment{}}, actor: actor, bot: slot}
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: 65536})
	mustProgress(t, err)
	ref := naming.Ref{Forge: "linear", Owner: "team", Repo: "project", Issue: 1}
	feature := store.ManagedFeature{ParentTask: ref.String(), Generation: 1, State: "planning"}
	mustProgress(t, db.PutManagedFeature(ctx, feature))
	story := store.ManagedStory{ParentTask: ref.String(), Generation: 1, Key: "story-001", IssueNumber: 2, IssueID: 200, BodyHash: strings.Repeat("a", 64), State: "planning"}
	mustProgress(t, db.PutManagedStory(ctx, story))
	mustProgress(t, db.SetManagedFeatureReady(ctx, ref.String(), 1, true))
	mustProgress(t, db.ApproveManagedStories(ctx, ref.String(), 1, []store.ApprovedStoryRevision{{ManagedStory: story, ParentID: 100, Title: "Story", Body: "Body", Hash: story.BodyHash, FetchedAt: implementClock}}))
	opts := Options{DB: db, SubIssueCreator: forge, SubIssueRenderer: renderer}
	for range 2 {
		mustProgress(t, deliverScopeNotice(ctx, opts, ref, feature, story, false))
	}
	if forge.posts != 1 || forge.comments[2][0].Author.ID != slot {
		t.Fatalf("wrong or repeated publisher: %+v", forge.comments)
	}
}
