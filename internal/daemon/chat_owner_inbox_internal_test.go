package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestOwnerInboxReadFailure(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{"quota", &ticket.RateLimitError{RetryAfter: time.Minute}, "deferred"},
		{"wrapped quota", &ticket.Failure{Kind: ticket.Unavailable, Cause: &ticket.RateLimitError{RetryAfter: time.Minute}}, "deferred"},
		{"refused", &ticket.Failure{Kind: ticket.Refused, Cause: errors.New("private provider response")}, "refused access"},
		{"forbidden", github.ErrForbidden, "refused access"},
		{"cancelled", context.Canceled, "cancelled"},
		{"unknown", errors.New("private provider response"), "unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			detail := inboxReadFailure(test.err)
			if !strings.Contains(detail.Unavailable, test.want) || strings.Contains(detail.Unavailable, "private") || detail.Title != "" || detail.Body != "" || detail.URL != "" {
				t.Fatalf("unsafe or wrong failure: %#v", detail)
			}
		})
	}
}

// The adapter must bind both immutable identity and current source configuration.
func TestOwnerInboxQualifiedRead(t *testing.T) {
	ctx := t.Context()
	var origin string
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet {
			t.Errorf("unexpected mutation: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/user":
			_, _ = w.Write([]byte(`{"id":7,"login":"bot"}`))
		case "/repos/acme/tickets":
			_, _ = w.Write([]byte(`{"id":91,"full_name":"acme/tickets","has_issues":true}`))
		case "/repos/acme/tickets/issues/1":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 101, "number": 1, "title": "Current title", "body": "Current details fixture-token", "state": "open", "url": origin + r.URL.Path, "html_url": "https://github.com/acme/tickets/issues/1"})
		default:
			t.Errorf("unexpected read: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	origin = server.URL
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "inbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "code", RemoteURL: "git@github.com:acme/code.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	version, err := db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: ticket.Origin(origin), Destination: "acme/tickets", RemoteID: "91", CredentialSource: "env", CredentialEnv: "TEST_INBOX_TOKEN", Enabled: true, Provenance: "explicit"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	factory := projectconnection.NewFactory(projectconnection.Options{DB: db, Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "fixture-token", true }}})
	reader := ownerInboxReader{daemon: &Daemon{db: db, connections: factory}}
	decision := store.InboxDecision{RepoID: repo.ID, ConnectionID: version.ID, Ref: ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: ticket.Origin(origin), RemoteID: "101"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: ticket.Origin(origin), Container: "acme/tickets", Key: "1"}}}
	detail, err := reader.ReadOwnerInboxTicket(ctx, decision)
	if err != nil || detail.Title != "Current title" || !strings.HasPrefix(detail.Body, "Current details") || strings.Contains(detail.Body, "fixture-token") {
		t.Fatalf("qualified read: %#v, %v", detail, err)
	}
	before := requests
	wrong := decision
	wrong.Ref.Identity.Provider = ticket.Linear
	detail, err = reader.ReadOwnerInboxTicket(ctx, wrong)
	if err != nil || detail.Title != "" || detail.Unavailable == "" || requests != before {
		t.Fatalf("cross-provider identity leaked: %#v %v", detail, err)
	}
	if _, err = db.DisableConnection(ctx, repo.ID, store.TicketConnection, version.Revision); err != nil {
		t.Fatal(err)
	}
	detail, err = reader.ReadOwnerInboxTicket(ctx, decision)
	if err != nil || detail.Title != "" || detail.Unavailable == "" || requests != before {
		t.Fatalf("revoked source leaked: %#v %v", detail, err)
	}
}
