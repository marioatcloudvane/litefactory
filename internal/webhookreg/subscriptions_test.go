package webhookreg_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/webhookreg"
)

type uncertainSubscriptionForge struct {
	adapter    *github.Tickets
	loseCreate bool
	creates    int
}

func (f *uncertainSubscriptionForge) ListSubscriptions(ctx context.Context, c string) ([]ticket.Subscription, error) {
	return f.adapter.ListSubscriptions(ctx, c)
}
func (f *uncertainSubscriptionForge) GetSubscription(ctx context.Context, c string, id ticket.Identity) (ticket.Subscription, error) {
	return f.adapter.GetSubscription(ctx, c, id)
}
func (f *uncertainSubscriptionForge) CreateSubscription(ctx context.Context, c string, s ticket.SubscriptionSpec, secret secrets.Token) (ticket.Subscription, error) {
	f.creates++
	h, err := f.adapter.CreateSubscription(ctx, c, s, secret)
	if err == nil && f.loseCreate {
		return ticket.Subscription{}, errors.New("response lost")
	}
	return h, err
}
func (f *uncertainSubscriptionForge) UpdateSubscription(ctx context.Context, c string, id ticket.Identity, s ticket.SubscriptionSpec, secret secrets.Token) (ticket.Subscription, error) {
	return f.adapter.UpdateSubscription(ctx, c, id, s, secret)
}

func qualifiedEnv(t *testing.T, provenance ...string) (*env, webhookreg.Subscriptions, *uncertainSubscriptionForge, store.ConnectionVersion) {
	t.Helper()
	e := newEnv(t)
	origin, err := ticket.CanonicalOrigin(e.gh.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	source := "explicit"
	if len(provenance) > 0 {
		source = provenance[0]
	}
	v, err := e.db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: e.repo.ID, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: origin, Destination: "acme/api", RemoteID: "101", CredentialSource: "env", CredentialEnv: "TEST_TOKEN", Enabled: true, Provenance: source}, 0)
	if err != nil {
		t.Fatal(err)
	}
	legacy := e.registrar(t, publicURL)
	client, ok := legacy.Forge.(*github.Client)
	if !ok {
		t.Fatal("unexpected fixture client")
	}
	adapter, err := github.NewTickets(client)
	if err != nil {
		t.Fatal(err)
	}
	forge := &uncertainSubscriptionForge{adapter: adapter}
	return e, webhookreg.Subscriptions{Store: e.db, Forge: forge, Keychain: e.kc, PublicURL: publicURL}, forge, v
}

func TestQualifiedSubscriptionRecoversLostCreationWithoutDuplicate(t *testing.T) {
	t.Parallel()
	e, r, forge, v := qualifiedEnv(t)
	forge.loseCreate = true
	s, err := r.Create(t.Context(), v, "101")
	if err == nil || s.ID == "" || s.ObservedState != "unresolved" || forge.creates != 1 {
		t.Fatalf("create=%+v error=%v count=%d", s, err, forge.creates)
	}
	recovered, err := r.Repair(t.Context(), s.ID, s.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.RemoteHookID == "" || recovered.ObservedState != "enabled" || forge.creates != 1 {
		t.Fatalf("recovered=%+v count=%d", recovered, forge.creates)
	}
	disabled, err := r.Update(t.Context(), s.ID, s.Revision, false, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	repaired, err := r.Repair(t.Context(), s.ID, disabled.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if repaired.DesiredEnabled || repaired.ObservedState != "disabled" {
		t.Fatalf("repair reenabled %+v", repaired)
	}
	if _, err := r.Update(t.Context(), s.ID, s.Revision, true, "", nil); !errors.Is(err, store.ErrSubscriptionConflict) {
		t.Fatalf("stale update=%v", err)
	}
	e.gh.getStatus = 403
	pending, err := r.Update(t.Context(), s.ID, disabled.Revision, false, "", nil)
	if err == nil || pending.DesiredEnabled {
		t.Fatalf("failed disable lost intent %+v err=%v", pending, err)
	}
}

func TestQualifiedSubscriptionRepairsOwnedDriftAndDeletedHook(t *testing.T) {
	t.Parallel()
	e, r, forge, v := qualifiedEnv(t)
	s, err := r.Create(t.Context(), v, "101")
	if err != nil {
		t.Fatal(err)
	}
	e.gh.hooks[100].Active = false
	repaired, err := r.Repair(t.Context(), s.ID, s.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if repaired.Revision <= s.Revision || !e.gh.hooks[100].Active {
		t.Fatalf("drift not repaired %+v", repaired)
	}
	delete(e.gh.hooks, 100)
	replaced, err := r.Repair(t.Context(), s.ID, repaired.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.RemoteHookID == "100" || forge.creates != 2 {
		t.Fatalf("replacement=%+v count=%d", replaced, forge.creates)
	}
}

func TestQualifiedSubscriptionDisableDuringUncertainCreationRetainsOwnership(t *testing.T) {
	t.Parallel()
	_, r, forge, v := qualifiedEnv(t)
	forge.loseCreate = true
	s, err := r.Create(t.Context(), v, "101")
	if err == nil {
		t.Fatal("create response should be uncertain")
	}
	disabled, err := r.Update(t.Context(), s.ID, s.Revision, false, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if disabled.DesiredEnabled || disabled.RemoteHookID == "" || disabled.ObservedState != "disabled" || forge.creates != 1 {
		t.Fatalf("disabled=%+v creates=%d", disabled, forge.creates)
	}
}

func TestQualifiedSubscriptionImportsProvenLegacyOwnership(t *testing.T) {
	t.Parallel()
	e, r, forge, v := qualifiedEnv(t, "legacy")
	result := e.registrar(t, publicURL).Ensure(t.Context(), e.repo)
	if !result.OK() {
		t.Fatal(result.Describe())
	}
	imported, err := e.db.ImportLegacyTicketSubscription(t.Context(), v, "101")
	if err != nil {
		t.Fatal(err)
	}
	repaired, err := r.Repair(t.Context(), imported.ID, imported.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if forge.creates != 0 || repaired.RemoteHookID != "100" || repaired.SecretAccount != "acme/api" {
		t.Fatalf("legacy ownership lost %+v creates=%d", repaired, forge.creates)
	}
}
