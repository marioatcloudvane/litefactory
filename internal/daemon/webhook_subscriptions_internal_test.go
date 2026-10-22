package daemon

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/webhook"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func subscriptionCacheFixture(t *testing.T, f *discoveryHTTPFixture, name, origin string) (store.ConnectionVersion, store.TicketSubscription) {
	t.Helper()
	repo, err := f.d.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: name, RemoteURL: "git@github.com:acme/" + name + ".git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	v, err := f.d.db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: ticket.Origin(origin), Destination: repo.Slug(), RemoteID: "container-" + name, Enabled: true, Provenance: "explicit"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := f.d.db.PutTicketSubscription(t.Context(), store.TicketSubscription{ID: name, RepoID: repo.ID, ConnectionVersionID: v.ID, Provider: v.Provider, Origin: v.Origin, Container: v.Destination, ContainerID: v.RemoteID, CallbackURL: "https://callback.invalid/webhook?subscription=" + name, DesiredEnabled: true, SecretService: "test-service", SecretAccount: name, SecretVersion: "1"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return v, sub
}

func TestSubscriptionCacheRefreshFollowsRevisionAndDisable(t *testing.T) {
	f := newDiscoveryHTTPFixture(t)
	_, sub := subscriptionCacheFixture(t, f, "cached", "https://api.github.com")
	calls := 0
	secret := "old"
	w := newWebhookRuntime(WebhookOptions{Secrets: secrets.Resolver{Keychain: func(context.Context, string, string) (string, error) { calls++; return secret, nil }}}, f.d.log)
	w.refreshSubscriptions(t.Context(), f.d.db, f.now)
	source, ok := w.LookupSubscription(sub.ID)
	if !ok || string(source.Secret) != "old" || calls != 1 {
		t.Fatalf("initial cache %+v %v calls=%d", source, ok, calls)
	}
	source.Secret[0] = 'X'
	source, ok = w.LookupSubscription(sub.ID)
	if !ok || string(source.Secret) != "old" {
		t.Fatal("caller mutated cached signing material")
	}
	w.refreshSubscriptions(t.Context(), f.d.db, f.now.Add(time.Minute))
	if calls != 1 {
		t.Fatal("same revision reread keychain")
	}
	secret = "new"
	sub.SecretVersion = "2"
	var err error
	sub, err = f.d.db.PutTicketSubscription(t.Context(), sub, sub.Revision)
	if err != nil {
		t.Fatal(err)
	}
	w.refreshSubscriptions(t.Context(), f.d.db, f.now.Add(2*time.Minute))
	source, ok = w.LookupSubscription(sub.ID)
	if !ok || string(source.Secret) != "new" || source.Revision != sub.Revision || calls != 2 {
		t.Fatalf("secret rotation %+v %v calls=%d", source, ok, calls)
	}
	sub.DesiredEnabled = false
	if _, err := f.d.db.PutTicketSubscription(t.Context(), sub, sub.Revision); err != nil {
		t.Fatal(err)
	}
	w.refreshSubscriptions(t.Context(), f.d.db, f.now.Add(3*time.Minute))
	if _, ok := w.LookupSubscription(sub.ID); ok || calls != 2 {
		t.Fatalf("disabled cache loaded signing material calls=%d", calls)
	}
}

func TestSubscriptionCacheDiscardsDisableDuringSecretLoad(t *testing.T) {
	f := newDiscoveryHTTPFixture(t)
	_, sub := subscriptionCacheFixture(t, f, "race", "https://api.github.com")
	w := newWebhookRuntime(WebhookOptions{Secrets: secrets.Resolver{Keychain: func(ctx context.Context, _, _ string) (string, error) {
		sub.DesiredEnabled = false
		_, err := f.d.db.PutTicketSubscription(ctx, sub, sub.Revision)
		return "stale-secret", err
	}}}, f.d.log)
	w.refreshSubscriptions(t.Context(), f.d.db, f.now)
	if _, ok := w.LookupSubscription(sub.ID); ok {
		t.Fatal("disable during secret resolution admitted stale secret")
	}
}

func TestQualifiedWebhookHintsRejectStaleRevisionAndSource(t *testing.T) {
	for _, change := range []string{"disabled-subscription", "subscription-revision", "connection-version"} {
		t.Run(change, func(t *testing.T) {
			f := newDiscoveryHTTPFixture(t)
			v, sub := subscriptionCacheFixture(t, f, "stale", "https://api.github.com")
			hint := hintKey{repoID: v.RepoID, number: 7, versionID: v.ID, revision: sub.Revision, selector: sub.ID}
			f.d.webhook = newWebhookRuntime(WebhookOptions{}, f.d.log)
			f.d.webhook.hints.add(hint)
			var err error
			switch change {
			case "disabled-subscription":
				sub.DesiredEnabled = false
				_, err = f.d.db.PutTicketSubscription(t.Context(), sub, sub.Revision)
			case "subscription-revision":
				sub.SecretVersion = "new"
				_, err = f.d.db.PutTicketSubscription(t.Context(), sub, sub.Revision)
			case "connection-version":
				v.Origin = "https://different.example.test/api/v3"
				_, err = f.d.db.SetConnection(t.Context(), v, v.Revision)
			}
			if err != nil {
				t.Fatal(err)
			}
			before := f.requests
			seen := map[int64]*candidate{}
			result := &TickResult{}
			if err := f.d.webhookHints(t.Context(), nil, seen, result); err != nil {
				t.Fatal(err)
			}
			if len(seen) != 0 || result.Hinted != 0 || len(f.d.webhook.hints.pending()) != 0 || f.requests != before {
				t.Fatalf("stale source read or admitted work: %+v", result)
			}
		})
	}
}

func TestSubscriptionSecretFailureIsPacedAndRevisionRefreshes(t *testing.T) {
	f := newDiscoveryHTTPFixture(t)
	_, sub := subscriptionCacheFixture(t, f, "missing", "https://api.github.com")
	calls := 0
	w := newWebhookRuntime(WebhookOptions{Secrets: secrets.Resolver{Keychain: func(context.Context, string, string) (string, error) { calls++; return "", secrets.ErrNotFound }}}, f.d.log)
	w.refreshSubscriptions(t.Context(), f.d.db, f.now)
	w.refreshSubscriptions(t.Context(), f.d.db, f.now.Add(time.Minute))
	if calls != 1 {
		t.Fatalf("missing secret retried every heartbeat: %d", calls)
	}
	sub.SecretVersion = "2"
	var err error
	_, err = f.d.db.PutTicketSubscription(t.Context(), sub, sub.Revision)
	if err != nil {
		t.Fatal(err)
	}
	w.refreshSubscriptions(t.Context(), f.d.db, f.now.Add(2*time.Minute))
	if calls != 2 {
		t.Fatalf("new revision did not resolve immediately: %d", calls)
	}
	w.refreshSubscriptions(t.Context(), f.d.db, f.now.Add(8*time.Minute))
	if calls != 3 {
		t.Fatalf("missing secret did not retry after pacing: %d", calls)
	}
}

func TestSubscriptionCacheDiscardsConnectionChangeDuringSecretLoad(t *testing.T) {
	f := newDiscoveryHTTPFixture(t)
	v, sub := subscriptionCacheFixture(t, f, "changed", "https://api.github.com")
	w := newWebhookRuntime(WebhookOptions{Secrets: secrets.Resolver{Keychain: func(ctx context.Context, _, _ string) (string, error) {
		v.Origin = "https://different.example.test/api/v3"
		_, err := f.d.db.SetConnection(ctx, v, v.Revision)
		return "old-source-secret", err
	}}}, f.d.log)
	w.refreshSubscriptions(t.Context(), f.d.db, f.now)
	if _, ok := w.LookupSubscription(sub.ID); ok {
		t.Fatal("connection changed during resolution admitted old source")
	}
}

func TestLegacySecretWithoutRecordedHookAdmitsVerifiedSource(t *testing.T) {
	for _, mode := range []string{"present", "missing", "denied", "ambiguous"} {
		t.Run(mode, func(t *testing.T) {
			f := newDiscoveryHTTPFixture(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("daemon mutated remote hook: %s", r.Method)
				}
				switch r.URL.Path {
				case "/user":
					_, _ = w.Write([]byte(`{"id":7,"login":"bot"}`))
				case "/repos/acme/legacy":
					_, _ = w.Write([]byte(`{"id":101,"full_name":"acme/legacy","has_issues":true}`))
				case "/repos/acme/legacy/hooks":
					_, _ = w.Write([]byte(`[{"id":55,"active":true,"events":["issues","issue_comment","sub_issues"],"config":{"url":"https://callback.invalid` + webhook.Path + `","content_type":"json","insecure_ssl":"0"}}]`))
				default:
					t.Errorf("unexpected remote call %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			repo, err := f.d.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "legacy", RemoteURL: "git@github.com:acme/legacy.git"})
			if err != nil {
				t.Fatal(err)
			}
			v, err := f.d.db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: ticket.Origin(server.URL), Destination: repo.Slug(), Enabled: true, Provenance: "legacy", CredentialSource: "env", CredentialEnv: "TOKEN"}, 0)
			if err != nil {
				t.Fatal(err)
			}
			pinned := v
			pinned.RemoteID = "101"
			if mode == "ambiguous" {
				pinned.RemoteID = "102"
			}
			if _, err := f.d.db.RetainConnectionVersion(t.Context(), pinned); err != nil {
				t.Fatal(err)
			}
			f.d.connections = projectconnection.NewFactory(projectconnection.Options{ExpectedActorID: 7, Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "fake-token", true }}})
			calls := 0
			f.d.webhook = newWebhookRuntime(WebhookOptions{PublicURL: "https://callback.invalid" + webhook.Path, Secrets: secrets.Resolver{Keychain: func(_ context.Context, service, account string) (string, error) {
				calls++
				if service != WebhookKeychainService || account != repo.Slug() {
					t.Fatalf("wrong secret reference: %s/%s", service, account)
				}
				if mode == "missing" {
					return "", secrets.ErrNotFound
				}
				if mode == "denied" {
					return "", errors.New("access denied private detail")
				}
				return "legacy-secret", nil
			}}}, f.d.log)
			f.d.refreshWebhookSecrets(t.Context(), f.now)
			before := calls
			f.d.refreshWebhookSecrets(t.Context(), f.now.Add(time.Minute))
			if calls != before {
				t.Fatal("secret retried before cadence")
			}
			handler, err := webhook.NewHandler(webhook.Options{Lookup: f.d.webhook, Sink: f.d.webhook, Logger: f.d.log})
			if err != nil {
				t.Fatal(err)
			}
			send := func(secret, delivery string) int {
				body := `{"action":"opened","repository":{"id":101,"full_name":"acme/legacy"},"issue":{"number":7}}`
				mac := hmac.New(sha256.New, []byte(secret))
				_, _ = mac.Write([]byte(body))
				req := httptest.NewRequest(http.MethodPost, webhook.Path, strings.NewReader(body))
				req.Header.Set("X-GitHub-Event", "issues")
				req.Header.Set("X-GitHub-Delivery", delivery)
				req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
				out := httptest.NewRecorder()
				handler.ServeHTTP(out, req)
				return out.Code
			}
			if got := send("wrong", "bad"); got != http.StatusUnauthorized {
				t.Fatalf("bad signature status=%d", got)
			}
			status := send("legacy-secret", "good")
			if mode != "present" {
				if status != http.StatusUnauthorized {
					t.Fatalf("unavailable source admitted: %d", status)
				}
				if mode != "ambiguous" {
					logs := f.logs.String()
					if strings.Count(logs, `"level":"WARN"`) != 1 || !strings.Contains(logs, repo.Slug()) || !strings.Contains(logs, "litefactory repo webhook") || strings.Contains(logs, "private detail") {
						t.Fatalf("missing paced safe warning: %s", logs)
					}
					f.d.refreshWebhookSecrets(t.Context(), f.now.Add(6*time.Minute))
					if calls != before+1 {
						t.Fatal("no retry after cadence")
					}
				}
				return
			}
			if status < 200 || status >= 300 {
				t.Fatalf("signed delivery rejected: %d logs=%s", status, f.logs.String())
			}
			hints := f.d.webhook.hints.pending()
			if len(hints) != 1 || hints[0].versionID != v.ID || hints[0].repoID != repo.ID {
				t.Fatalf("wrong routing: %+v", hints)
			}
			// The one hook with this installation's callback is adopted, so
			// recovery knows it; the daemon only read it.
			if hook, err := f.d.db.GetRepoWebhook(t.Context(), repo.ID); err != nil || hook.HookID != 55 {
				t.Fatalf("existing hook not adopted: %+v %v", hook, err)
			}
			subs, err := f.d.db.ListTicketSubscriptions(t.Context(), repo.ID)
			if err != nil || len(subs) != 1 || subs[0].RemoteHookID != "55" {
				t.Fatalf("subscriptions: %+v %v", subs, err)
			}
			sub := subs[0]
			sub.DesiredEnabled = false
			if _, err := f.d.db.PutTicketSubscription(t.Context(), sub, sub.Revision); err != nil {
				t.Fatal(err)
			}
			f.d.refreshWebhookSecrets(t.Context(), f.now.Add(2*time.Minute))
			if got := send("legacy-secret", "disabled"); got != http.StatusUnauthorized {
				t.Fatalf("disable admitted: %d", got)
			}
		})
	}
}
