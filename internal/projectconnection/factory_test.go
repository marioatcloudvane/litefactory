package projectconnection_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func version(origin string, id int64) store.ConnectionVersion {
	return store.ConnectionVersion{ID: id, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: ticket.Origin(origin), Destination: "owner/tickets", RemoteID: "91", CredentialSource: "env", CredentialEnv: "TOKEN", Enabled: true}
}

func TestFactoryVersionIsolationRefreshAndRedaction(t *testing.T) {
	var lookups atomic.Int32
	var actor atomic.Int64
	actor.Store(7)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprintf(w, `{"id":%d,"login":"worker"}`, actor.Load())
	}))
	defer server.Close()
	shared := redact.New(redact.Options{Environ: []string{}})
	factory := projectconnection.NewFactory(projectconnection.Options{Redactor: shared, Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) {
		n := lookups.Add(1)
		return fmt.Sprintf("opaque-token-for-version-%d", n), true
	}}})
	first, err := factory.Clients(t.Context(), version(server.URL, 1), 7)
	if err != nil {
		t.Fatal(err)
	}
	again, err := factory.Clients(t.Context(), version(server.URL, 1), 7)
	if err != nil || again != first || lookups.Load() != 1 {
		t.Fatalf("cached load: %v, lookups %d", err, lookups.Load())
	}
	second, err := factory.Clients(t.Context(), version(server.URL, 2), 7)
	if err != nil || second.Code == first.Code {
		t.Fatalf("version isolation: %v", err)
	}
	actor.Store(8)
	if _, err = factory.Refresh(t.Context(), version(server.URL, 1), 7); !errors.Is(err, projectconnection.ErrActorChanged) {
		t.Fatalf("changed actor: %v", err)
	}
	retained, err := factory.Clients(t.Context(), version(server.URL, 1), 7)
	if err != nil || retained != first {
		t.Fatalf("failed refresh changed cached actor: %v", err)
	}
	body, err := shared.Apply(t.Context(), "opaque-token-for-version-1 opaque-token-for-version-2 opaque-token-for-version-3", 1000)
	if err != nil || strings.Contains(body.String(), "opaque-token") {
		t.Fatalf("all credentials redacted: %q, %v", body.String(), err)
	}
}

func TestHealthReadOnlyAndSafeFailures(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		want   string
	}{{"readable", 200, "usable_for_checked_capabilities"}, {"missing", 404, "inaccessible_or_missing_destination"}, {"denied", 403, "insufficient_permission"}, {"rate", 429, "rate_limited"}} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("health performed %s", r.Method)
				}
				switch r.URL.Path {
				case "/user":
					_, _ = w.Write([]byte(`{"id":7,"login":"worker"}`))
				case "/repos/owner/tickets":
					w.WriteHeader(test.status)
					if test.status == 200 {
						_, _ = w.Write([]byte(`{"id":91,"full_name":"owner/tickets","has_issues":true}`))
					} else {
						_, _ = w.Write([]byte(`{"message":"opaque-secret-do-not-save"}`))
					}
				case "/repos/owner/tickets/issues":
					_, _ = w.Write([]byte(`[]`))
				default:
					t.Errorf("unexpected health request %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			factory := projectconnection.NewFactory(projectconnection.Options{ExpectedActorID: 7, Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "opaque-secret-do-not-save", true }}})
			observation := factory.Check(t.Context(), version(server.URL, 1))
			if observation.Status != test.want {
				t.Fatalf("status %q, want %q", observation.Status, test.want)
			}
			if strings.Contains(fmt.Sprint(observation), "opaque-secret") {
				t.Fatal("secret persisted in health")
			}
			if test.status == 200 && (!slices.Contains(observation.Capabilities, "ticket_read") || !slices.Contains(observation.Capabilities, "write_unknown")) {
				t.Fatalf("capability evidence %v", observation.Capabilities)
			}
		})
	}
}

func TestHealthRejectsLateVersion(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "health.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: "/health", Forge: "github", Owner: "owner", Name: "tickets"})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			close(started)
			<-release
			_, _ = w.Write([]byte(`{"id":7,"login":"worker"}`))
		case "/repos/owner/tickets":
			_, _ = w.Write([]byte(`{"id":91,"full_name":"owner/tickets","has_issues":true}`))
		case "/repos/owner/tickets/issues":
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	v := version(server.URL, 0)
	v.RepoID = repo.ID
	v.Provenance = "explicit"
	v, err = db.SetConnection(t.Context(), v, 0)
	if err != nil {
		t.Fatal(err)
	}
	factory := projectconnection.NewFactory(projectconnection.Options{Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "opaque-health-token", true }}})
	done := make(chan error, 1)
	go func() { _, err := factory.CheckAndSave(t.Context(), db, v); done <- err }()
	<-started
	_, changeErr := db.DisableConnection(t.Context(), repo.ID, store.TicketConnection, v.Revision)
	close(release)
	if changeErr != nil {
		t.Fatal(changeErr)
	}
	if err := <-done; !errors.Is(err, store.ErrConnectionConflict) {
		t.Fatalf("late response accepted: %v", err)
	}
	current, err := db.CurrentConnection(t.Context(), repo.ID, store.TicketConnection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetConnectionObservation(t.Context(), current.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("late response marked new version healthy: %v", err)
	}
}

func TestResolverFailureDiscardsRawDiagnostics(t *testing.T) {
	factory := projectconnection.NewFactory(projectconnection.Options{Resolver: secrets.Resolver{Keychain: func(context.Context, string, string) (string, error) {
		return "", errors.New("secret-credential-value")
	}}})
	v := version("https://api.github.com", 1)
	v.CredentialSource = "keychain"
	v.CredentialService = "service"
	v.CredentialAccount = "account"
	_, err := factory.Clients(t.Context(), v, 7)
	if err == nil || strings.Contains(err.Error(), "secret-credential") {
		t.Fatalf("unsafe resolver error: %v", err)
	}
}

func TestStartupTokenIsRestrictedToMatchingLegacyReference(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer startup-token" {
			t.Errorf("unexpected credential header")
		}
		_, _ = w.Write([]byte(`{"id":7,"login":"worker"}`))
	}))
	defer server.Close()
	var lookups atomic.Int32
	factory := projectconnection.NewFactory(projectconnection.Options{LegacyReference: secrets.EnvRef("TOKEN"), LegacyToken: secrets.NewToken("startup-token"), Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { lookups.Add(1); return "", false }}})
	legacy := version(server.URL, 1)
	legacy.Provenance = "legacy"
	if _, err := factory.Clients(t.Context(), legacy, 7); err != nil {
		t.Fatal(err)
	}
	explicit := legacy
	explicit.ID = 2
	explicit.Provenance = "explicit"
	if _, err := factory.Clients(t.Context(), explicit, 7); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("explicit reference bypassed resolver: %v", err)
	}
	if lookups.Load() != 1 {
		t.Fatalf("lookups=%d", lookups.Load())
	}
}

func TestSlowVersionDoesNotBlockOtherConnectionsOrCancellation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer FIRST" {
			close(started)
			<-release
		}
		_, _ = w.Write([]byte(`{"id":7,"login":"worker"}`))
	}))
	defer server.Close()
	factory := projectconnection.NewFactory(projectconnection.Options{Resolver: secrets.Resolver{LookupEnv: func(name string) (string, bool) { return name, true }}})
	first := version(server.URL, 1)
	first.CredentialEnv = "FIRST"
	done := make(chan error, 1)
	go func() { _, err := factory.Clients(t.Context(), first, 7); done <- err }()
	<-started
	second := version(server.URL, 2)
	second.CredentialEnv = "SECOND"
	secondCtx, cancelSecond := context.WithTimeout(t.Context(), time.Second)
	_, secondErr := factory.Clients(secondCtx, second, 7)
	cancelSecond()
	waiterCtx, cancelWaiter := context.WithTimeout(t.Context(), 20*time.Millisecond)
	_, waiterErr := factory.Clients(waiterCtx, first, 7)
	cancelWaiter()
	close(release)
	firstErr := <-done
	if secondErr != nil || firstErr != nil {
		t.Fatalf("independent version stalled: %v %v", secondErr, firstErr)
	}
	if !errors.Is(waiterErr, context.DeadlineExceeded) {
		t.Fatalf("in-flight waiter did not cancel: %v", waiterErr)
	}
}

func TestConnectionErrorsRedactAllTokensAndRetainRecoveryMetadata(t *testing.T) {
	const credential = "opaque-remote-diagnostic-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			_, _ = w.Write([]byte(`{"id":7,"login":"worker"}`))
			return
		}
		w.Header().Set("Retry-After", "17")
		w.Header().Set("X-GitHub-Request-Id", credential)
		w.Header().Set("X-RateLimit-Resource", credential)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprintf(w, `{"message":%q,"documentation_url":%q}`, credential, "https://example.test/"+credential)
	}))
	defer server.Close()
	factory := projectconnection.NewFactory(projectconnection.Options{ExpectedActorID: 7, Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return credential, true }}})
	clients, err := factory.Clients(t.Context(), version(server.URL, 1), 7)
	if err != nil {
		t.Fatal(err)
	}
	_, err = clients.Code.InspectRepository(t.Context(), github.Repo{Owner: "owner", Name: "repo"})
	var limited *github.RateLimitError
	if !errors.As(err, &limited) || !errors.Is(err, github.ErrRateLimited) || limited.RetryAfter != 17*time.Second {
		t.Fatalf("lost recovery classification: %v", err)
	}
	for _, diagnostic := range []string{err.Error(), limited.Message, limited.RequestID, limited.Resource, limited.DocumentationURL} {
		if strings.Contains(diagnostic, credential) {
			t.Fatal("connection credential survived in remote failure diagnostic")
		}
	}
}
