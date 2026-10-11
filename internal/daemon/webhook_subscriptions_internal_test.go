package daemon

import (
	"context"
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
