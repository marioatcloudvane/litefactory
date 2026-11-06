package projectconnection_test

import (
	"context"
	"encoding/json"
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
				case "/repos/owner/tickets/labels":
					_, _ = w.Write([]byte(`[{"name":"Bug"}]`))
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

func TestLinearMappingHealthPreservesAccessDiagnostics(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "linear-health.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, complete := range []bool{false, true} {
		for _, denied := range []bool{false, true} {
			t.Run(fmt.Sprintf("complete=%t/denied=%t", complete, denied), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var req struct {
						Query     string
						Variables map[string]string
					}
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Error(err)
						return
					}
					if strings.Contains(req.Query, "mutation") {
						t.Error("health performed provider write")
					}
					if denied {
						http.Error(w, "denied", http.StatusForbidden)
						return
					}
					id := req.Variables["id"]
					var data any
					switch {
					case strings.Contains(req.Query, "viewer"):
						data = map[string]any{"viewer": map[string]any{"id": "bot", "active": true, "name": "bot", "organization": map[string]any{"id": "workspace"}}}
					case strings.Contains(req.Query, "team("):
						data = map[string]any{"team": map[string]any{"id": "team", "organization": map[string]any{"id": "workspace"}}, "project": map[string]any{"id": "project", "teams": map[string]any{"nodes": []any{map[string]string{"id": "team"}}, "pageInfo": map[string]bool{"hasNextPage": false}}}}
					case strings.Contains(req.Query, "issueLabel("):
						data = map[string]any{"issueLabel": map[string]any{"id": id, "organization": map[string]string{"id": "workspace"}, "team": map[string]string{"id": "team"}}}
					case strings.Contains(req.Query, "workflowState("):
						kind := "completed"
						if id == "retired" {
							kind = "canceled"
						}
						data = map[string]any{"workflowState": map[string]any{"id": id, "type": kind, "team": map[string]string{"id": "team"}}}
					case strings.Contains(req.Query, "webhooks("):
						data = map[string]any{"webhooks": map[string]any{"nodes": []any{}, "pageInfo": map[string]bool{"hasNextPage": false}}}
					default:
						t.Errorf("unexpected query: %s", req.Query)
					}
					if err := json.NewEncoder(w).Encode(map[string]any{"data": data}); err != nil {
						t.Error(err)
					}
				}))
				defer server.Close()
				labels := `{"Mode: Plan":"p","implement":"i","validate":"v"}`
				if complete {
					labels = `{"research":"r","Plan":"p","implement":"i","validate":"v","Mode: Spike":"s"}`
				}
				v := version(server.URL, 1)
				v.Provider = ticket.Linear
				v.Destination = "team/project"
				v.Linear = store.LinearConfiguration{WorkspaceID: "workspace", TeamID: "team", ProjectID: "project", AuthKind: "api_key", ModeLabelsJSON: labels, LifecycleStatesJSON: `{"completed":"completed","retired":"retired"}`}
				f := projectconnection.NewFactory(projectconnection.Options{DB: db, Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "test-token", true }}})
				observation := f.Check(ctx, v)
				if strings.Contains(observation.Remedy, "Missing mode mappings: research, spike") == complete {
					t.Fatalf("mapping health: %+v", observation)
				}
				if !complete && !strings.Contains(observation.Remedy, "--mode-labels") {
					t.Fatal("remedy missing")
				}
				if denied {
					if observation.Status == "usable_for_checked_capabilities" {
						t.Fatalf("access failure hidden: %+v", observation)
					}
				} else if observation.Status != "usable_for_checked_capabilities" || !slices.Contains(observation.Capabilities, "ticket_read") {
					t.Fatalf("mapped modes disabled: %+v", observation)
				}
			})
		}
	}
}

func linearLabelServer(t *testing.T, writes, labelLookups *[]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string
			Variables map[string]any
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if strings.Contains(req.Query, "mutation") || strings.Contains(req.Query, "issueLabels") {
			*writes = append(*writes, req.Query)
		}
		var data any
		switch {
		case strings.Contains(req.Query, "viewer"):
			data = map[string]any{"viewer": map[string]any{"id": "bot", "active": true, "name": "bot", "organization": map[string]any{"id": "workspace"}}}
		case strings.Contains(req.Query, "team("):
			data = map[string]any{"team": map[string]any{"id": "team", "organization": map[string]any{"id": "workspace"}}, "project": map[string]any{"id": "project", "teams": map[string]any{"nodes": []any{map[string]string{"id": "team"}}, "pageInfo": map[string]bool{"hasNextPage": false}}}}
		case strings.Contains(req.Query, "issueLabel("):
			*labelLookups = append(*labelLookups, fmt.Sprint(req.Variables["id"]))
			data = map[string]any{"issueLabel": map[string]any{"id": req.Variables["id"], "organization": map[string]string{"id": "workspace"}, "team": map[string]string{"id": "team"}}}
		case strings.Contains(req.Query, "workflowState("):
			kind := "completed"
			if req.Variables["id"] == "retired" {
				kind = "canceled"
			}
			data = map[string]any{"workflowState": map[string]any{"id": req.Variables["id"], "type": kind, "team": map[string]string{"id": "team"}}}
		case strings.Contains(req.Query, "webhooks("):
			data = map[string]any{"webhooks": map[string]any{"nodes": []any{}, "pageInfo": map[string]bool{"hasNextPage": false}}}
		case strings.Contains(req.Query, "issue("):
			data = map[string]any{"issue": map[string]any{"id": "issue-id", "identifier": "LIN-1", "url": "https://linear.app/t/issue/LIN-1", "title": "T", "description": "B", "creator": map[string]any{"id": "owner", "active": true}, "team": map[string]string{"id": "team"}, "project": map[string]string{"id": "project"}, "state": map[string]string{"id": "open", "type": "unstarted"}, "labels": map[string]any{"nodes": []any{}, "pageInfo": map[string]bool{"hasNextPage": false}}}}
		default:
			t.Errorf("unexpected query: %s", req.Query)
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"data": data}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func linearLabelVersion(origin string, cfg store.LinearConfiguration) store.ConnectionVersion {
	v := version(origin, 1)
	v.Provider = ticket.Linear
	v.Destination = "team/project"
	cfg.WorkspaceID, cfg.TeamID, cfg.ProjectID, cfg.AuthKind = "workspace", "team", "project", "api_key"
	cfg.LifecycleStatesJSON = `{"completed":"completed","retired":"retired"}`
	v.Linear = cfg
	return v
}

func TestLinearCheckRemedyByLabelSetup(t *testing.T) {
	var writes, lookups []string
	server := linearLabelServer(t, &writes, &lookups)
	f := projectconnection.NewFactory(projectconnection.Options{DB: openLinearLabelDB(t), Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "test-token", true }}})
	full := `{"research":"r","Plan":"p","implement":"i","validate":"v","Mode: Spike":"s"}`
	for name, tc := range map[string]struct {
		cfg     store.LinearConfiguration
		want    []string
		notWant []string
		lookups int
	}{
		"empty":   {store.LinearConfiguration{}, []string{"Labels are not set up.", "litefactory repo labels setup <project>", "no coding agent label is mapped", "no bug label is mapped"}, []string{"Missing mode mappings"}, 0},
		"partial": {store.LinearConfiguration{ModeLabelsJSON: `{"plan":"p","implement":"i","validate":"v"}`}, []string{"Missing mode mappings: research, spike", "litefactory repo labels setup <project>"}, []string{"Labels are not set up"}, 3},
		"full":    {store.LinearConfiguration{ModeLabelsJSON: full}, []string{"no coding agent label is mapped", "no bug label is mapped"}, []string{"Missing mode mappings", "Labels are not set up"}, 5},
		"cockpit flat names": {store.LinearConfiguration{ModeLabelsJSON: `{"Mode: Plan":"p","Mode: Implement":"i","Mode: Validate":"v","Mode: Research":"r","Mode: Spike":"s"}`, CodingAgentLabelsJSON: `{"codex":"a"}`, BugLabelID: "b"},
			nil, []string{"Missing mode mappings", "Labels are not set up", "Warning"}, 7},
	} {
		t.Run(name, func(t *testing.T) {
			lookups = nil
			o := f.Check(t.Context(), linearLabelVersion(server.URL, tc.cfg))
			if o.Status != "usable_for_checked_capabilities" {
				t.Fatalf("status changed: %+v", o)
			}
			for _, s := range tc.want {
				if !strings.Contains(o.Remedy, s) {
					t.Errorf("remedy lacks %q: %s", s, o.Remedy)
				}
			}
			for _, s := range tc.notWant {
				if strings.Contains(o.Remedy, s) {
					t.Errorf("remedy has %q: %s", s, o.Remedy)
				}
			}
			if len(lookups) != tc.lookups {
				t.Errorf("per-ID label lookups = %v, want %d", lookups, tc.lookups)
			}
		})
	}
	if len(writes) != 0 {
		t.Fatalf("check wrote or searched labels: %v", writes)
	}
}

func openLinearLabelDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "linear-labels.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestHealthReportsMissingAndRefusedLabels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			_, _ = w.Write([]byte(`{"id":7,"login":"worker"}`))
		case "/repos/owner/tickets":
			_, _ = w.Write([]byte(`{"id":91,"full_name":"owner/tickets","has_issues":true}`))
		case "/repos/owner/tickets/issues":
			_, _ = w.Write([]byte(`[]`))
		case "/repos/owner/tickets/labels":
			_, _ = w.Write([]byte(`[{"name":"Bug"},{"name":"Mode: Plan"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	factory := projectconnection.NewFactory(projectconnection.Options{ExpectedActorID: 7, LabelAgents: []string{"claude"}, Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "tok", true }}})
	v := version(server.URL, 1)
	v.RepoID = 5
	o := factory.Check(t.Context(), v)
	if o.Status != "usable_for_checked_capabilities" || o.Reason != "read_access_verified" {
		t.Fatalf("status changed: %+v", o)
	}
	for _, want := range []string{"missing labels", "Mode: Research", "litefactory repo labels setup 5", `label "Bug" cannot be used`} {
		if !strings.Contains(o.Remedy, want) {
			t.Fatalf("remedy lacks %q: %s", want, o.Remedy)
		}
	}
}
