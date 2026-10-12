package webhookreg

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// SubscriptionForge is the neutral hook surface needed by subscription ownership.
// Discovery and ticket command authority are deliberately separate consumers.
type SubscriptionForge interface {
	ListSubscriptions(context.Context, string) ([]ticket.Subscription, error)
	GetSubscription(context.Context, string, ticket.Identity) (ticket.Subscription, error)
	CreateSubscription(context.Context, string, ticket.SubscriptionSpec, secrets.Token) (ticket.Subscription, error)
	UpdateSubscription(context.Context, string, ticket.Identity, ticket.SubscriptionSpec, secrets.Token) (ticket.Subscription, error)
}

// Subscriptions retains local intent and claims before any remote effect. A
// failed response is reconciled by exact installation-owned callback, never by
// adopting the first hook sharing a general public listener URL.
type Subscriptions struct {
	Store     *store.DB
	Forge     SubscriptionForge
	Keychain  Keychain
	PublicURL string
	Now       func() time.Time
	Random    io.Reader
}

func (r Subscriptions) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

// Create saves one new version-qualified intent with an opaque callback selector.
// The caller has already verified the immutable repository through this connection.
func (r Subscriptions) Create(ctx context.Context, v store.ConnectionVersion, containerID string) (store.TicketSubscription, error) {
	u, err := url.Parse(r.PublicURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.Fragment != "" {
		return store.TicketSubscription{}, errors.New("invalid webhook callback URL")
	}
	random := r.Random
	if random == nil {
		random = rand.Reader
	}
	raw := make([]byte, 16)
	if _, err := io.ReadFull(random, raw); err != nil {
		return store.TicketSubscription{}, errors.New("could not generate subscription selector")
	}
	id := hex.EncodeToString(raw)
	query := u.Query()
	query.Set("subscription", id)
	u.RawQuery = query.Encode()
	s := store.TicketSubscription{ID: id, RepoID: v.RepoID, ConnectionVersionID: v.ID, Provider: v.Provider, Origin: v.Origin, Container: v.Destination, ContainerID: containerID, CallbackURL: u.String(), Events: subscriptionEvents(v.Provider), SecretService: KeychainService, SecretAccount: "subscription/" + id, SecretVersion: "1", DesiredEnabled: true}
	s, err = r.Store.PutTicketSubscription(ctx, s, 0)
	if err != nil {
		return s, err
	}
	return r.Repair(ctx, s.ID, s.Revision)
}

// Update saves revision-checked desired state before attempting remote delivery.
// Disable therefore blocks local webhook admission even when GitHub is unavailable.
func (r Subscriptions) Update(ctx context.Context, id string, revision int64, enabled bool, callback string, events []string) (store.TicketSubscription, error) {
	s, err := r.Store.GetTicketSubscription(ctx, id)
	if err != nil {
		return s, err
	}
	if s.Revision != revision {
		return s, store.ErrSubscriptionConflict
	}
	s.DesiredEnabled = enabled
	if callback != "" {
		u, err := url.Parse(callback)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.Fragment != "" {
			return s, errors.New("invalid webhook callback URL")
		}
		q := u.Query()
		q.Set("subscription", s.ID)
		u.RawQuery = q.Encode()
		s.CallbackURL = u.String()
	}
	if events != nil {
		s.Events = slices.Clone(events)
	}
	if !validProviderSubscriptionEvents(s.Provider, s.Events) {
		return s, errors.New("unsupported webhook event selection")
	}
	s, err = r.Store.PutTicketSubscription(ctx, s, revision)
	if err != nil {
		return s, err
	}
	return r.Repair(ctx, s.ID, s.Revision)
}

func subscriptionEvents(provider ticket.Provider) []string {
	if provider == ticket.Linear {
		return []string{"Issue", "Comment"}
	}
	return slices.Clone(Events)
}
func validProviderSubscriptionEvents(provider ticket.Provider, events []string) bool {
	if provider == ticket.Linear {
		return len(events) == 2 && slices.Contains(events, "Issue") && slices.Contains(events, "Comment")
	}
	return validSubscriptionEvents(events)
}

func validSubscriptionEvents(events []string) bool {
	if len(events) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, event := range events {
		if !slices.Contains(Events, event) || seen[event] {
			return false
		}
		seen[event] = true
	}
	return true
}

func subscriptionSpec(s store.TicketSubscription) ticket.SubscriptionSpec {
	return ticket.SubscriptionSpec{CallbackURL: s.CallbackURL, Events: s.Events, Enabled: s.DesiredEnabled}
}
func subscriptionMatches(h ticket.Subscription, s store.TicketSubscription) bool {
	got, want := slices.Clone(h.Events), slices.Clone(s.Events)
	slices.Sort(got)
	slices.Sort(want)
	return h.ID.Provider == s.Provider && h.ID.Origin == s.Origin && h.Container == s.Container && h.CallbackURL == s.CallbackURL && h.Enabled == s.DesiredEnabled && h.ContentType == "json" && slices.Equal(got, want)
}

func (r Subscriptions) finish(ctx context.Context, op store.SubscriptionOperation, state, id, observed, reason string, cause error) (store.TicketSubscription, error) {
	err := r.Store.FinishSubscriptionOperation(ctx, op, state, id, observed, reason, r.now())
	if err != nil {
		return op.Specification, errors.Join(cause, err)
	}
	s, err := r.Store.GetTicketSubscription(ctx, op.SubscriptionID)
	return s, errors.Join(cause, err)
}

// Repair preserves explicit disable and revision. Existing claims are inspected
// without redispatch; an incomplete or ambiguous create stays uncertain.
func (r Subscriptions) Repair(ctx context.Context, id string, revision int64) (store.TicketSubscription, error) {
	s, err := r.Store.GetTicketSubscription(ctx, id)
	if err != nil {
		return s, err
	}
	if s.Revision != revision {
		return s, store.ErrSubscriptionConflict
	}
	if r.Forge == nil || r.Keychain == nil {
		return s, errors.New("subscription connection or secret access is unavailable")
	}
	prior, err := r.Store.GetSubscriptionOperation(ctx, id, revision)
	if err == nil {
		if prior.State == "confirmed" {
			hook, checkErr := r.Forge.GetSubscription(ctx, s.Container, ticket.Identity{Provider: s.Provider, Origin: s.Origin, RemoteID: s.RemoteHookID})
			if checkErr == nil && subscriptionMatches(hook, s) {
				secret, keyErr := r.Keychain.Read(ctx, s.SecretService, s.SecretAccount)
				if keyErr == nil && secret != "" {
					return s, nil
				}
				if keyErr != nil && !errors.Is(keyErr, secrets.ErrNotFound) {
					return s, errors.New("webhook signing secret is unavailable; check Keychain access")
				}
				s.SecretVersion = strconv.FormatInt(s.Revision+1, 10)
			}
			if checkErr != nil && !errors.Is(checkErr, github.ErrNotFound) {
				return s, errors.New("owned webhook could not be checked; polling continues")
			}
			if checkErr == nil && !r.ownsHook(ctx, hook, s) {
				return s, errors.New("owned webhook callback changed; inspect ownership before repair")
			}
			if errors.Is(checkErr, github.ErrNotFound) {
				if err := r.Store.ClearSubscriptionRemoteHook(ctx, s.ID, s.Revision, s.RemoteHookID); err != nil {
					return s, err
				}
				s.RemoteHookID = ""
			}
			s, err = r.Store.PutTicketSubscription(ctx, s, s.Revision)
			if err != nil {
				return s, err
			}
			return r.Repair(ctx, s.ID, s.Revision)
		}
		if prior.State == "failed" {
			s, err = r.Store.PutTicketSubscription(ctx, s, s.Revision)
			if err != nil {
				return s, err
			}
			return r.Repair(ctx, s.ID, s.Revision)
		}
		return r.reconcile(ctx, prior)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return s, err
	}
	operations, err := r.Store.ListSubscriptionOperations(ctx, id)
	if err != nil {
		return s, err
	}
	for _, old := range operations {
		if old.Revision == revision {
			continue
		}
		if old.State == "failed" {
			continue
		}
		if old.State != "confirmed" {
			if _, e := r.reconcile(ctx, old); e != nil {
				return s, errors.New("earlier subscription operation is uncertain; inspect owned remote callback before retrying")
			}
			var readErr error
			old, readErr = r.Store.GetSubscriptionOperation(ctx, id, old.Revision)
			if readErr != nil {
				return s, readErr
			}
		}
		if s.RemoteHookID == "" && old.Kind == "create" && old.RemoteHookID != "" {
			observed, e := r.Forge.GetSubscription(ctx, s.Container, ticket.Identity{Provider: s.Provider, Origin: s.Origin, RemoteID: old.RemoteHookID})
			if errors.Is(e, github.ErrNotFound) {
				continue
			}
			if e != nil || !ownedCallback(observed.CallbackURL, s.ID) {
				return s, errors.New("earlier owned webhook could not be verified; polling continues")
			}
			if err := r.Store.AdoptSubscriptionRemoteHook(ctx, s.ID, s.Revision, "", old.RemoteHookID); err != nil {
				return s, err
			}
			s.RemoteHookID = old.RemoteHookID
		}
	}

	kind := "update"
	if s.RemoteHookID == "" {
		kind = "create"
	}
	op, err := r.Store.ClaimSubscriptionOperation(ctx, id, revision, kind)
	if errors.Is(err, store.ErrSubscriptionBusy) {
		op, e := r.Store.GetSubscriptionOperation(ctx, id, revision)
		if e != nil {
			return s, e
		}
		return r.reconcile(ctx, op)
	}
	if err != nil {
		return s, err
	}
	secret, _, reason := (Registrar{Keychain: r.Keychain, Random: r.Random}).secretFor(ctx, s.SecretAccount)
	if reason != "" {
		return r.finish(ctx, op, "failed", s.RemoteHookID, "failed", "signing_secret_unavailable", errors.New("webhook signing secret is unavailable; check Keychain access"))
	}
	var observed ticket.Subscription
	if kind == "create" {
		observed, err = r.Forge.CreateSubscription(ctx, s.Container, subscriptionSpec(s), secrets.NewToken(secret))
	} else {
		identity := ticket.Identity{Provider: s.Provider, Origin: s.Origin, RemoteID: s.RemoteHookID}
		// Confirm callback ownership before editing even a retained remote ID.
		current, e := r.Forge.GetSubscription(ctx, s.Container, identity)
		if e != nil || !r.ownsHook(ctx, current, s) {
			return r.finish(ctx, op, "uncertain", s.RemoteHookID, "unresolved", "owned_remote_hook_unverified", errors.New("owned webhook could not be verified; polling continues"))
		}
		observed, err = r.Forge.UpdateSubscription(ctx, s.Container, identity, subscriptionSpec(s), secrets.NewToken(secret))
	}
	if err != nil {
		return r.finish(ctx, op, "uncertain", s.RemoteHookID, "unresolved", subscriptionFailure(err), errors.New("webhook operation is unresolved; inspect status and repair after access returns"))
	}
	confirmed, e := r.Forge.GetSubscription(ctx, s.Container, observed.ID)
	if e != nil || !subscriptionMatches(confirmed, s) {
		return r.finish(ctx, op, "uncertain", observed.ID.RemoteID, "unresolved", "remote_confirmation_missing", errors.New("webhook response could not be confirmed; polling continues"))
	}
	state := "enabled"
	if !s.DesiredEnabled {
		state = "disabled"
	}
	return r.finish(ctx, op, "confirmed", observed.ID.RemoteID, state, "", nil)
}

func subscriptionFailure(err error) string {
	switch {
	case errors.Is(err, github.ErrUnauthorized):
		return "authentication_failed"
	case errors.Is(err, github.ErrForbidden), errors.Is(err, github.ErrNotFound):
		return "hook_access_denied"
	case errors.Is(err, github.ErrRateLimited):
		return "provider_quota"
	default:
		return "remote_operation_uncertain"
	}
}

func (r Subscriptions) reconcile(ctx context.Context, op store.SubscriptionOperation) (store.TicketSubscription, error) {
	s := op.Specification
	var matches []ticket.Subscription
	if op.RemoteHookID != "" {
		observed, err := r.Forge.GetSubscription(ctx, s.Container, ticket.Identity{Provider: s.Provider, Origin: s.Origin, RemoteID: op.RemoteHookID})
		if err == nil && subscriptionMatches(observed, s) {
			matches = append(matches, observed)
		}
	} else {
		hooks, err := r.Forge.ListSubscriptions(ctx, s.Container)
		if err != nil {
			return r.finish(ctx, op, "uncertain", "", "unresolved", subscriptionFailure(err), errors.New("owned hook listing is unavailable; no duplicate hook was created"))
		}
		for _, hook := range hooks {
			if subscriptionMatches(hook, s) {
				matches = append(matches, hook)
			}
		}
	}
	if len(matches) != 1 {
		return r.finish(ctx, op, "uncertain", op.RemoteHookID, "unresolved", "owned_hook_not_unique", fmt.Errorf("subscription remains uncertain: found %d exact owned hook matches; no remote write was repeated", len(matches)))
	}
	state := "enabled"
	if !s.DesiredEnabled {
		state = "disabled"
	}
	return r.finish(ctx, op, "confirmed", matches[0].ID.RemoteID, state, "", nil)
}

func ownedCallback(callback, selector string) bool {
	u, err := url.Parse(callback)
	return err == nil && u.Query().Get("subscription") == selector
}

func (r Subscriptions) ownsHook(ctx context.Context, hook ticket.Subscription, s store.TicketSubscription) bool {
	if ownedCallback(hook.CallbackURL, s.ID) {
		return true
	}
	if !strings.HasPrefix(s.ID, "legacy-") {
		return false
	}
	v, err := r.Store.GetConnectionVersion(ctx, s.ConnectionVersionID)
	if err != nil || v.Provenance != "legacy" || s.SecretAccount != v.Destination {
		return false
	}
	legacy, err := r.Store.GetRepoWebhook(ctx, s.RepoID)
	return err == nil && strconv.FormatInt(legacy.HookID, 10) == hook.ID.RemoteID && legacy.URL == hook.CallbackURL
}
