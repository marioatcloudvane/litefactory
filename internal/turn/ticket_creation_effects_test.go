package turn

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/linear"
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
	remote.snapshot.Title = "Other title"
	if _, handled, err := bridge.RecoverIssueEffect(t.Context(), key, repo, 0, "Title", "Body"); !handled || err == nil {
		t.Fatal("changed title was acknowledged")
	}
	remote.snapshot.Title = "Title"
	remote.snapshot.Body = "Changed body"
	if _, handled, err := bridge.RecoverIssueEffect(t.Context(), key, repo, 0, "Title", "Body"); !handled || err == nil {
		t.Fatal("materially different body was acknowledged")
	}
	// Linear's readback of the same content (blank line layout normalized) is
	// equivalent though not byte-identical, and confirms the creation.
	remote.snapshot.Body = "\nBody\n"
	if !linear.ContentEquivalent("Body", remote.snapshot.Body) {
		t.Fatal("fixture body is not Linear-equivalent")
	}
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
	if len(remote.lookups) != 5 {
		t.Fatalf("lookups=%d", len(remote.lookups))
	}
	// The fake deliberately provides no creation or inventory implementation:
	// recovery would fail if it attempted to redispatch or search by markers.
}

// correlatedCreationTickets fakes Linear's client-ID creation. Each create
// returns the configured snapshot under the requested UUID; Resolve serves
// only an issue that a create has made (or that a test placed remotely).
type correlatedCreationTickets struct {
	exactCreationRecoveryTickets
	creates int
	created func(uuid string) ticket.Snapshot
}

func (f *correlatedCreationTickets) CreateWithID(context.Context, string, string, redact.Body, redact.Body) (ticket.Snapshot, error) {
	return ticket.Snapshot{}, errors.New("unexpected root creation")
}
func (f *correlatedCreationTickets) CreateChildWithID(_ context.Context, _ string, uuid string, _ *ticket.Ref, _, _ redact.Body) (ticket.Snapshot, error) {
	f.creates++
	if f.creates > 1 {
		return ticket.Snapshot{}, errors.New("linear rejects a second create with an existing id")
	}
	return f.created(uuid), nil
}
func (f *correlatedCreationTickets) PublishReplyWithID(context.Context, ticket.Ref, string, redact.Body) (ticket.Reply, error) {
	return ticket.Reply{}, errors.New("unexpected reply")
}
func (f *correlatedCreationTickets) ReadReply(context.Context, ticket.Ref, string) (ticket.Reply, error) {
	return ticket.Reply{}, errors.New("unexpected reply")
}

const (
	creationAuthoredBody = "<!-- lf:story story-001 -->\n\nStory body.\n\n\nMore detail."
	creationReadbackBody = "<!-- lf:story story-001 -->\nStory body.\n\nMore detail."
)

func creationBody(t *testing.T, text string) redact.Body {
	t.Helper()
	body, err := redact.New(redact.Options{}).Apply(t.Context(), text, 1<<16)
	if err != nil || body.String() != text {
		t.Fatalf("redacted %q=%q err=%v", text, body.String(), err)
	}
	return body
}

func newCorrelatedCreationBridge(t *testing.T) (*store.DB, *TicketWorkflowBridge, *correlatedCreationTickets, ticket.Identity) {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "effects.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if creationAuthoredBody == creationReadbackBody || !linear.ContentEquivalent(creationAuthoredBody, creationReadbackBody) {
		t.Fatal("fixture readback must be Linear-equivalent but not byte-identical")
	}
	bot := ticket.Identity{Provider: "linear", Origin: "https://api.linear.app", RemoteID: "publisher"}
	remote := &correlatedCreationTickets{}
	remote.created = func(uuid string) ticket.Snapshot {
		locator := ticket.Locator{Provider: "linear", Origin: bot.Origin, Container: "team/project", Key: uuid}
		return ticket.Snapshot{Ref: ticket.Ref{Identity: ticket.Identity{Provider: "linear", Origin: bot.Origin, RemoteID: uuid}, Locator: locator}, Title: "Story", Body: creationReadbackBody, ProviderBody: "provider:" + creationReadbackBody, Representation: "linear-markdown/v1", Author: ticket.Actor{Identity: bot}, State: "open"}
	}
	return db, &TicketWorkflowBridge{TicketPlanBridge: &TicketPlanBridge{DB: db, Tickets: remote, Bot: bot}}, remote, bot
}

func TestIssueCreationCorrelatesLinearNormalizedBody(t *testing.T) {
	db, bridge, remote, _ := newCorrelatedCreationBridge(t)
	repo := github.Repo{Owner: "team", Name: "project"}
	issue, err := bridge.CreateIssueEffect(t.Context(), "plan-shell:MAR-20:story-001", repo, 0, creationBody(t, "Story"), creationBody(t, creationAuthoredBody))
	if err != nil || issue.ID == 0 || issue.Number == 0 {
		t.Fatalf("creation=%+v err=%v", issue, err)
	}
	prepared, err := db.GetTicketEffectPreparation(t.Context(), "plan-shell:MAR-20:story-001")
	if err != nil || !prepared.Observed || prepared.ObservedContent != "Story\nprovider:"+creationReadbackBody {
		t.Fatalf("provider representation was not retained: %+v, %v", prepared, err)
	}
	if remote.creates != 1 {
		t.Fatalf("creates=%d", remote.creates)
	}
}

func TestIssueCreationCorrelationRejectsMismatches(t *testing.T) {
	for name, change := range map[string]func(*ticket.Snapshot){
		"uuid":           func(s *ticket.Snapshot) { s.Ref.Identity.RemoteID = "11111111-1111-4111-8111-111111111111" },
		"title":          func(s *ticket.Snapshot) { s.Title = "Other story" },
		"representation": func(s *ticket.Snapshot) { s.Representation = "linear-markdown/v0" },
		"publisher":      func(s *ticket.Snapshot) { s.Author.Identity.RemoteID = "other-publisher" },
		"body":           func(s *ticket.Snapshot) { s.Body = strings.Replace(creationReadbackBody, "detail", "details", 1) },
	} {
		t.Run(name, func(t *testing.T) {
			_, bridge, remote, _ := newCorrelatedCreationBridge(t)
			created := remote.created
			remote.created = func(uuid string) ticket.Snapshot {
				s := created(uuid)
				change(&s)
				return s
			}
			_, err := bridge.CreateIssueEffect(t.Context(), "plan-shell:MAR-20:story-001", github.Repo{Owner: "team", Name: "project"}, 0, creationBody(t, "Story"), creationBody(t, creationAuthoredBody))
			var failure *ticket.Failure
			if !errors.As(err, &failure) || failure.Kind != ticket.Uncertain || failure.Operation != "creation correlation" {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

// The MAR-20 retry: an earlier attempt created MAR-22 under the retained UUID
// but failed correlation. The retry reads that UUID back and recovers the issue;
// it never sends a second create with the same UUID.
func TestIssueCreationRetryRecoversRetainedUUIDWithoutSecondCreate(t *testing.T) {
	db, bridge, remote, _ := newCorrelatedCreationBridge(t)
	repo := github.Repo{Owner: "team", Name: "project"}
	key := "plan-shell:MAR-20:story-001"
	created := remote.created
	remote.created = func(uuid string) ticket.Snapshot {
		s := created(uuid)
		s.Author.Identity.RemoteID = "unconfirmed-publisher"
		return s
	}
	if _, err := bridge.CreateIssueEffect(t.Context(), key, repo, 0, creationBody(t, "Story"), creationBody(t, creationAuthoredBody)); err == nil {
		t.Fatal("first attempt unexpectedly confirmed")
	}
	prepared, err := db.GetTicketEffectPreparation(t.Context(), key)
	if err != nil || prepared.Observed {
		t.Fatalf("preparation=%+v err=%v", prepared, err)
	}
	// The issue exists remotely under the retained UUID with Linear's readback.
	remote.snapshot = created(prepared.CreationUUID)
	issue, err := bridge.CreateIssueEffect(t.Context(), key, repo, 0, creationBody(t, "Story"), creationBody(t, creationAuthoredBody))
	if err != nil || issue.ID == 0 || issue.Number == 0 {
		t.Fatalf("retry=%+v err=%v", issue, err)
	}
	if remote.creates != 1 {
		t.Fatalf("retry sent another create: creates=%d", remote.creates)
	}
	if len(remote.lookups) != 1 || remote.lookups[0].Key != prepared.CreationUUID {
		t.Fatalf("retry lookups=%+v want only retained UUID %s", remote.lookups, prepared.CreationUUID)
	}
	confirmed, err := db.GetTicketEffectPreparation(t.Context(), key)
	if err != nil || !confirmed.Observed || confirmed.CreationUUID != prepared.CreationUUID {
		t.Fatalf("recovered preparation=%+v err=%v", confirmed, err)
	}
	// A later retry is still recovery-only and binds the same issue.
	again, err := bridge.CreateIssueEffect(t.Context(), key, repo, 0, creationBody(t, "Story"), creationBody(t, creationAuthoredBody))
	if err != nil || again.ID != issue.ID || again.Number != issue.Number || remote.creates != 1 {
		t.Fatalf("second retry=%+v err=%v creates=%d", again, err, remote.creates)
	}
	// If the retained UUID cannot be read back, the retry fails without creating.
	remote.missing = true
	if _, err = bridge.CreateIssueEffect(t.Context(), key, repo, 0, creationBody(t, "Story"), creationBody(t, creationAuthoredBody)); err == nil || remote.creates != 1 {
		t.Fatalf("missing retained issue: err=%v creates=%d", err, remote.creates)
	}
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
