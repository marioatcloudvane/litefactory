package turn

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

type exactCreationRecoveryTickets struct {
	PlanTickets
	snapshot ticket.Snapshot
	lookups  []ticket.Locator
	missing  bool
}

func (f *exactCreationRecoveryTickets) Provider() ticket.Provider { return "linear" }
func (f *exactCreationRecoveryTickets) Origin() ticket.Origin     { return "https://api.linear.app" }
func (f *exactCreationRecoveryTickets) Resolve(_ context.Context, locator ticket.Locator) (ticket.Snapshot, error) {
	f.lookups = append(f.lookups, locator)
	if f.missing {
		return ticket.Snapshot{}, errors.New("remote issue is unavailable")
	}
	return f.snapshot, nil
}

func TestIssueCreationRecoveryUsesOnlyRetainedUUID(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "effects.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	key := "creation-intent"
	bot := ticket.Identity{Provider: "linear", Origin: "https://api.linear.app", RemoteID: "publisher"}
	bound := ticket.Locator{Provider: "linear", Origin: bot.Origin, Container: "team/project", Key: "creation:" + key}
	if _, err = db.BindTicketEffect(t.Context(), key, bound, bot); err != nil {
		t.Fatal(err)
	}
	prepared, err := db.PrepareTicketEffect(t.Context(), key, "linear-markdown/v1", "Title\nBody", true)
	if err != nil {
		t.Fatal(err)
	}
	locator := bound
	locator.Key = prepared.CreationUUID
	remote := &exactCreationRecoveryTickets{snapshot: ticket.Snapshot{Ref: ticket.Ref{Identity: ticket.Identity{Provider: "linear", Origin: bot.Origin, RemoteID: prepared.CreationUUID}, Locator: locator}, Title: "Title", Body: "Body", ProviderBody: "Body", Representation: "linear-markdown/v1", Author: ticket.Actor{Identity: bot}, State: "open"}}
	bridge := &TicketWorkflowBridge{TicketPlanBridge: &TicketPlanBridge{DB: db, Tickets: remote, Bot: bot}}
	repo := github.Repo{Owner: "team", Name: "project"}
	remote.missing = true
	if _, handled, err := bridge.RecoverIssueEffect(t.Context(), key, repo, 0, "Title", "Body"); !handled || err == nil {
		t.Fatal("missing effect was acknowledged")
	}
	remote.missing = false
	remote.snapshot.Author.Identity.RemoteID = "other-publisher"
	if _, handled, err := bridge.RecoverIssueEffect(t.Context(), key, repo, 0, "Title", "Body"); !handled || err == nil {
		t.Fatal("substituted publisher was acknowledged")
	}
	remote.snapshot.Author.Identity = bot
	issue, handled, err := bridge.RecoverIssueEffect(t.Context(), key, repo, 0, "Title", "Body")
	if err != nil || !handled || issue.ID == 0 || issue.Number == 0 {
		t.Fatalf("recovery=%+v handled=%v err=%v", issue, handled, err)
	}
	confirmed, err := db.GetTicketEffectPreparation(t.Context(), key)
	if err != nil || !confirmed.Observed || confirmed.ObservedContent != "Title\nBody" {
		t.Fatalf("provider representation was not retained: %+v, %v", confirmed, err)
	}
	for _, lookup := range remote.lookups {
		if lookup != locator {
			t.Fatalf("non-exact creation lookup: %+v", lookup)
		}
	}
	if len(remote.lookups) != 3 {
		t.Fatalf("lookups=%d", len(remote.lookups))
	}
	// The fake deliberately provides no creation or inventory implementation:
	// recovery would fail if it attempted to redispatch or search by markers.
}

type retainedPublisherReportPoster struct {
	boundActor int64
	posts      int
}

func (p *retainedPublisherReportPoster) BindPublication(_ context.Context, _ string, _ int64, _ naming.Ref, actor int64) error {
	p.boundActor = actor
	return nil
}
func (p *retainedPublisherReportPoster) PostComment(_ context.Context, _ github.Repo, _ int, body redact.Body) (github.Comment, error) {
	p.posts++
	return github.Comment{ID: 501, Body: body.String(), Author: github.User{ID: p.boundActor}}, nil
}
func (p *retainedPublisherReportPoster) ListCommentsAfter(context.Context, github.Repo, int, int64) ([]github.Comment, error) {
	return nil, nil
}

func TestReporterRetainsLinearPublisherAfterSharedGitHubRestart(t *testing.T) {
	f := newFixture(t, newStubRunner(nil))
	connection := func(role store.ConnectionRole) store.ConnectionVersion {
		t.Helper()
		v, err := f.db.CurrentConnection(t.Context(), f.storeRepo.ID, role)
		if errors.Is(err, store.ErrNotFound) {
			v, err = f.db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: f.storeRepo.ID, Role: role, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: f.storeRepo.Slug(), CredentialSource: "env", CredentialEnv: "TOKEN", Enabled: true, Provenance: "explicit", ClonePath: f.storeRepo.Path, RemoteURL: f.storeRepo.RemoteURL, BaseBranch: f.storeRepo.BaseBranch, FeaturePrefix: f.storeRepo.FeaturePrefix, WorktreeRoot: f.storeRepo.WorktreeRoot}, 0)
		}
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	code, current := connection(store.CodeConnection), connection(store.TicketConnection)
	version, err := f.db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: f.storeRepo.ID, Role: store.TicketConnection, Provider: "linear", Origin: "https://api.linear.app", Destination: "team/project", CredentialSource: "env", CredentialEnv: "LINEAR_TOKEN", Enabled: true, Provenance: "explicit", Linear: store.LinearConfiguration{WorkspaceID: "workspace", TeamID: "team", ProjectID: "project", AuthKind: "api_key", ModeLabelsJSON: `{"plan":"label-plan"}`, LifecycleStatesJSON: `{"completed":"done","retired":"cancelled"}`}}, current.Revision)
	if err != nil {
		t.Fatal(err)
	}
	identity := ticket.Identity{Provider: "linear", Origin: version.Origin, RemoteID: "original-publisher"}
	publisher, err := f.db.LocalTicketSlot(t.Context(), "actor", identity)
	if err != nil {
		t.Fatal(err)
	}
	ref := ticket.Ref{Identity: ticket.Identity{Provider: "linear", Origin: version.Origin, RemoteID: "original-ticket"}, Locator: ticket.Locator{Provider: "linear", Origin: version.Origin, Container: version.Destination, Key: "original-ticket"}}
	binding, err := f.db.AddTicketBinding(t.Context(), store.TicketBinding{RepoID: f.storeRepo.ID, ConnectionVersionID: version.ID, Ref: ref, Enabled: true, Provenance: "explicit"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.db.AttachExecutionSnapshot(t.Context(), store.ExecutionSnapshot{SessionID: f.session.ID, RepoID: f.storeRepo.ID, BindingID: binding.ID, CodeVersionID: code.ID, TicketVersionID: version.ID, ActorID: publisher, ActorLogin: "original-publisher"}); err != nil {
		t.Fatal(err)
	}
	poster := &retainedPublisherReportPoster{}
	r := newReporter(t, f, newFakeForge(t), nil)
	r.forge, r.botUserID = poster, 99
	request := ReplyRequest{Ref: f.ref(), SessionID: f.session.ID, CommentID: 22, Command: ParseCommand("/unknown command")}
	if _, err = r.ReportReply(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if poster.boundActor != publisher || poster.posts != 1 || r.botUserID != 99 {
		t.Fatalf("publisher=%d want=%d posts=%d startup=%d", poster.boundActor, publisher, poster.posts, r.botUserID)
	}
}
