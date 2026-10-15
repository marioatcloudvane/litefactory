package turn

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestTicketConnectionRoutingRetainsPublicationOriginAndBlocksSelectionFailure(t *testing.T) {
	t.Parallel()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "connections.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	var originalOrigin string
	var originalPosts, replacementRequests atomic.Int64
	original := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var response any
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues/123":
			response = map[string]any{"id": 700, "number": 123, "url": originalOrigin + r.URL.Path, "state": "open", "title": "Original ticket", "body": "saved work", "user": map[string]any{"id": 42, "login": "owner"}}
		case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/issues/123/comments":
			originalPosts.Add(1)
			response = map[string]any{"id": 5001, "url": originalOrigin + "/repos/owner/repo/issues/comments/5001", "issue_url": originalOrigin + "/repos/owner/repo/issues/123", "body": "retained publication", "user": map[string]any{"id": 77, "login": "factory-bot"}}
		default:
			t.Errorf("unexpected original request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	defer original.Close()
	originalOrigin = original.URL
	replacement := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		replacementRequests.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer replacement.Close()
	adapter := func(origin string) *github.Tickets {
		t.Helper()
		client, e := github.New(github.Options{APIBase: origin, Token: secrets.NewToken("test-external-token")})
		if e != nil {
			t.Fatal(e)
		}
		tickets, e := github.NewTickets(client)
		if e != nil {
			t.Fatal(e)
		}
		return tickets
	}
	originalTickets := adapter(original.URL)
	replacementTickets := adapter(replacement.URL)
	var blocked atomic.Bool
	selector := func(_ context.Context, repo github.Repo, number int, session int64) (WorkflowTickets, int64, error) {
		if blocked.Load() {
			return nil, 0, errors.New("original credential unavailable")
		}
		if repo.String() != "owner/repo" || number != 123 {
			t.Errorf("selection alias %s#%d", repo.String(), number)
		}
		return originalTickets, 77, nil
	}
	workflow, err := NewRoutedGitHubTicketWorkflow(db, replacementTickets, 77, selector)
	if err != nil {
		t.Fatal(err)
	}
	ref := naming.Ref{Forge: naming.ForgeGitHub, Owner: "owner", Repo: "repo", Issue: 123}
	if err = workflow.BindPublication(t.Context(), "wrong-actor", 0, ref, 88); err == nil {
		t.Fatal("replacement actor was accepted for retained publication")
	}
	if err = workflow.BindPublication(t.Context(), "retained-report", 0, ref, 77); err != nil {
		t.Fatal(err)
	}
	body, err := redact.New(redact.Options{}).Apply(t.Context(), "retained publication", 10000)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := workflow.PostComment(t.Context(), github.RepoFromRef(ref), ref.Issue, body)
	if err != nil {
		t.Fatal(err)
	}
	if originalPosts.Load() != 1 || replacementRequests.Load() != 0 || !workflow.VerifiedCommentDestinationFor(t.Context(), reply, ref) {
		t.Fatalf("publication rerouted or receipt lost: original posts %d replacement requests %d reply %+v", originalPosts.Load(), replacementRequests.Load(), reply)
	}
	retained, err := db.GetTicketEffect(t.Context(), "retained-report")
	if err != nil {
		t.Fatal(err)
	}
	if string(retained.Locator.Origin) != original.URL || retained.Actor.RemoteID != strconv.Itoa(77) {
		t.Fatalf("retained destination/actor = %+v", retained)
	}
	blocked.Store(true)
	if _, err = workflow.PostComment(t.Context(), github.RepoFromRef(ref), ref.Issue, body); err == nil {
		t.Fatal("selection failure allowed publication")
	}
	if originalPosts.Load() != 1 || replacementRequests.Load() != 0 {
		t.Fatal("selection failure fell back to another client")
	}
}

func TestTicketReceiptMetadataDistinguishesCollidingOrigins(t *testing.T) {
	t.Parallel()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "receipts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	origins := []ticket.Origin{"https://original.example/api", "https://replacement.example/api"}
	bridges := make([]*TicketPublicationBridge, 0, len(origins))
	for index, origin := range origins {
		ref := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: strconv.Itoa(700 + index)}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: origin, Container: "owner/repo", Key: "123"}}
		snapshot := ticket.Snapshot{Ref: ref}
		bridge, e := NewTicketPublicationBridge(db, &publicationTicketsFixture{snapshot: snapshot}, origin)
		if e != nil {
			t.Fatal(e)
		}
		observed, e := bridge.destination(t.Context(), github.Repo{Owner: "owner", Name: "repo"}, 123)
		if e != nil || !observed.Ref.Equal(ref) {
			t.Fatalf("colliding destination metadata redirected: %+v %v", observed, e)
		}
		client, e := github.New(github.Options{APIBase: string(origin), Token: secrets.NewToken("offline-metadata-token")})
		if e != nil {
			t.Fatal(e)
		}
		adapter, e := github.NewTickets(client)
		if e != nil {
			t.Fatal(e)
		}
		plan := TicketPlanBridge{DB: db, Tickets: adapter}
		if _, e = plan.bind(t.Context(), snapshot); e != nil {
			t.Fatalf("colliding Plan metadata: %v", e)
		}
		reply := ticket.Reply{Ticket: ref, Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "5001"}, Author: ticket.Actor{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "77"}}}
		if e = bridge.rememberReply(t.Context(), reply); e != nil {
			t.Fatal(e)
		}
		bridges = append(bridges, bridge)
	}
	historical, err := db.GetTicketEffect(t.Context(), publicationReplyKey("owner/repo", "5001"))
	if err != nil || historical.Locator.Origin != origins[0] {
		t.Fatalf("historical receipt metadata overwritten: %+v %v", historical, err)
	}
	for index, bridge := range bridges {
		reply, e := bridge.retainedReply(t.Context(), github.Repo{Owner: "owner", Name: "repo"}, 5001)
		if e != nil || reply.Identity.Origin != origins[index] || reply.Ticket.Identity.RemoteID != strconv.Itoa(700+index) {
			t.Fatalf("colliding receipt lookup redirected: %+v %v", reply, e)
		}
	}
}

// This seam returns only a verified connection identity; global rows deliberately
// disagree so any compatibility fallback changes the observed publisher.
type contextualPublisherFixture struct {
	actor ticket.Identity
	err   error
}

func (f contextualPublisherFixture) BotIdentityFor(context.Context, github.Repo, int) (ticket.Identity, error) {
	return f.actor, f.err
}

func TestTicketPublisherUsesQualifiedConnectionAndNeverFallsBack(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "publisher.db"))
	mustProgress(t, err)
	defer func() { _ = db.Close() }()
	mustProgress(t, db.Migrate(t.Context()))
	_, err = db.UpsertIdentity(t.Context(), store.Identity{Forge: "github", Role: store.RoleBot, UserID: 9})
	mustProgress(t, err)
	ref := naming.Ref{Forge: "linear", Owner: "team", Repo: "project", Issue: 1}
	var slots []int64
	for _, origin := range []ticket.Origin{"https://first.example/graphql", "https://second.example/graphql"} {
		actor := ticket.Identity{Provider: ticket.Linear, Origin: origin, RemoteID: "publisher"}
		slot, e := ticketPublisherID(t.Context(), db, contextualPublisherFixture{actor: actor}, ref)
		mustProgress(t, e)
		retained, e := db.TicketIdentityForSlot(t.Context(), "actor", slot)
		mustProgress(t, e)
		if !retained.Equal(actor) {
			t.Fatalf("publisher changed: %+v", retained)
		}
		slots = append(slots, slot)
	}
	if slots[0] == slots[1] {
		t.Fatal("origins shared an actor slot")
	}
	for _, cause := range []error{store.ErrNotFound, store.ErrTicketIdentityConflict, context.Canceled, errors.New("transient transport failure")} {
		slot, e := ticketPublisherID(t.Context(), db, contextualPublisherFixture{err: cause}, ref)
		if slot != 0 || !errors.Is(e, cause) {
			t.Fatalf("routed error fell back: %d %v", slot, e)
		}
		permanent := errors.Is(cause, store.ErrNotFound) || errors.Is(cause, store.ErrTicketIdentityConflict)
		if errors.Is(e, ErrRequiredTicketPublisher) != permanent {
			t.Fatalf("wrong retry classification: %v", e)
		}
	}
	if _, e := ticketPublisherID(t.Context(), db, contextualPublisherFixture{}, ref); !errors.Is(e, ErrRequiredTicketPublisher) {
		t.Fatalf("invalid identity: %v", e)
	}
	if _, e := ticketPublisherID(t.Context(), db, struct{}{}, ref); !errors.Is(e, ErrRequiredTicketPublisher) {
		t.Fatalf("Linear used global identity: %v", e)
	}
	ref.Forge = "github"
	if id, e := ticketPublisherID(t.Context(), db, struct{}{}, ref); e != nil || id != 9 {
		t.Fatalf("legacy GitHub: %d %v", id, e)
	}
}
