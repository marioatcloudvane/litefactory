package turn

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	publisher    int64
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
	actor := f.publisher
	if actor == 0 {
		actor = 9
	}
	c := github.Comment{ID: int64(f.posts), Body: b.String(), Author: github.User{ID: actor}}
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
	if _, err := ticketPublisherID(ctx, Options{DB: db, ProjectApprovers: true}, forge, 0, ref); err == nil {
		t.Fatal("connected GitHub session without snapshot used global bot")
	}
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

func TestScopeNoticeRetainsConnectedPublisher(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "scope.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(db.Migrate(ctx))
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "o", Name: "code", RemoteURL: "git@github.com:o/code.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	check(err)
	code, err := db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: "o/code", Enabled: true, Provenance: "explicit"}, 0)
	check(err)
	tickets, err := db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.Linear, Origin: "https://api.linear.app", Destination: "team/project", Enabled: true, Provenance: "explicit", Linear: store.LinearConfiguration{WorkspaceID: "workspace", TeamID: "team", ProjectID: "project", AuthKind: "api_key", ModeLabelsJSON: `{"implement":"label"}`, LifecycleStatesJSON: `{"completed":"done","retired":"cancelled"}`}}, 0)
	check(err)
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: 65536})
	check(err)
	for i, actor := range []int64{77, 88} {
		remote := fmt.Sprintf("parent-%d", i)
		qualified := ticket.Ref{Identity: ticket.Identity{Provider: ticket.Linear, Origin: tickets.Origin, RemoteID: remote}, Locator: ticket.Locator{Provider: ticket.Linear, Origin: tickets.Origin, Container: tickets.Destination, Key: remote}}
		_, err := db.AddTicketBinding(ctx, store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: tickets.ID, Ref: qualified, Enabled: true, Provenance: "explicit"})
		check(err)
		ref := naming.Ref{Forge: "linear", Owner: "team", Repo: fmt.Sprintf("project-%d", i), Issue: 1}
		// Invalid publisher evidence cannot be retained by admission.
		if _, err := db.AdmitSession(ctx, repo, qualified, code.Revision, tickets.Revision, 0, "invalid", ref.String()); err == nil {
			t.Fatal("invalid publisher admitted")
		}
		session, err := db.AdmitSession(ctx, repo, qualified, code.Revision, tickets.Revision, actor, "ticket-publisher", ref.String())
		check(err)
		story := store.ManagedStory{ParentTask: ref.String(), Generation: 1, Key: "story", IssueNumber: 2, IssueID: 200, BodyHash: planbrief.StoryHash("Story", "approved"), State: "planning"}
		check(db.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: ref.String(), Generation: 1, State: "planning"}))
		check(db.PutManagedStory(ctx, story))
		check(db.SetManagedFeatureReady(ctx, ref.String(), 1, true))
		check(db.ApproveManagedStories(ctx, ref.String(), 1, []store.ApprovedStoryRevision{{ManagedStory: story, ParentID: 100, Title: "Story", Body: "approved", Hash: story.BodyHash, FetchedAt: time.Now()}}))
		forge := &scopeTestForge{publicationForge: publicationForge{attached: true}, comments: map[int][]github.Comment{}, publisher: actor, loseResponse: true}
		o := Options{DB: db, SubIssueCreator: forge, SubIssueRenderer: renderer, ProjectApprovers: true}
		// Even a global Linear identity must not rescue a missing snapshot.
		if i == 1 {
			_, err := db.UpsertIdentity(ctx, store.Identity{Forge: "linear", Role: store.RoleBot, UserID: 999})
			check(err)
		}
		if err := prepareManagedScope(ctx, o, Request{Mode: agent.ModeImplement}, ref); err == nil {
			t.Fatal("missing snapshot accepted")
		}
		feature, err := db.GetManagedFeature(ctx, ref.String())
		check(err)
		delivered, err := db.StoryScopeNoticeDelivered(ctx, ref.String(), 1, "story", "freeze")
		check(err)
		if feature.State != "freezing" || delivered || forge.posts != 0 {
			t.Fatal("missing publisher changed scope")
		}
		req := Request{Mode: agent.ModeImplement, SessionID: session.ID}
		if err := prepareManagedScope(ctx, o, req, ref); err == nil {
			t.Fatal("lost response activated scope")
		}
		check(prepareManagedScope(ctx, o, req, ref))
		check(prepareManagedScope(ctx, o, req, ref))
		feature, err = db.GetManagedFeature(ctx, ref.String())
		check(err)
		delivered, err = db.StoryScopeNoticeDelivered(ctx, ref.String(), 1, "story", "freeze")
		check(err)
		if feature.State != "implementing" || !delivered || forge.posts != 1 || forge.comments[2][0].Author.ID != actor {
			t.Fatalf("retained publisher or replay failed: %+v posts=%d", feature, forge.posts)
		}
	}
}
