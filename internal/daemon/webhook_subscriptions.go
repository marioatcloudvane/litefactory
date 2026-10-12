package daemon

import (
	"context"
	"log/slog"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/webhook"
)

type subscriptionSecretAttempt struct {
	revision int64
	at       time.Time
}

// LookupSubscription uses only the revision-scoped cache; ingress never calls
// the provider, keychain or store.
func (w *webhookRuntime) LookupSubscription(selector string) (webhook.Source, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	source, ok := w.subscriptions[selector]
	source.Secret = slices.Clone(source.Secret)
	return source, ok && len(source.Secret) > 0
}

func (w *webhookRuntime) WakeSubscription(source webhook.Source, number int) {
	w.hints.add(hintKey{repoID: source.RepoID, number: number, versionID: source.ConnectionVersionID, revision: source.Revision, selector: source.Selector})
	if w.schedule != nil {
		w.schedule.WebhookAccepted(source.RepoID, w.now())
		w.schedule.Announce(source.RepoID, number)
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *webhookRuntime) refreshSubscriptions(ctx context.Context, db *store.DB, now time.Time) {
	subscriptions, err := db.ListTicketSubscriptions(ctx, 0)
	if err != nil {
		w.log.WarnContext(ctx, "webhook subscriptions unavailable; admission cache cleared", slog.Any("error", err))
		w.mu.Lock()
		w.subscriptions = make(map[string]webhook.Source)
		w.referenceSources = make(map[string][]webhook.Source)
		w.legacySources = make(map[string]webhook.Source)
		w.repos = make(map[string]*webhookRepo)
		w.mu.Unlock()
		return
	}
	live := make(map[string]webhook.Source)
	legacy := make(map[string]*webhookRepo)
	legacySources := make(map[string]webhook.Source)
	ambiguous := make(map[string]bool)
	// Read all desired revisions before external secret reads. A disabled intent
	// removes the cached secret before any potentially slow resolution.
	for _, sub := range subscriptions {
		current, err := db.CurrentConnection(ctx, sub.RepoID, store.TicketConnection)
		if err != nil || !current.Enabled || !sub.DesiredEnabled || current.ID != sub.ConnectionVersionID {
			continue
		}
		if state, err := db.GetProjectImportState(ctx, sub.RepoID); err == nil && state.Status == "conflict" {
			continue
		}
		source := webhook.Source{WorkspaceID: current.Linear.WorkspaceID, TeamID: current.Linear.TeamID, ProjectID: current.Linear.ProjectID, RepoID: sub.RepoID, ConnectionVersionID: sub.ConnectionVersionID, Revision: sub.Revision, Selector: sub.ID, Provider: string(sub.Provider), Origin: string(sub.Origin), Container: sub.Container, ContainerID: sub.ContainerID}
		w.mu.RLock()
		old := w.subscriptions[sub.ID]
		w.mu.RUnlock()
		if old.Revision == sub.Revision {
			source.Secret = slices.Clone(old.Secret)
		}
		live[sub.ID] = source
	}
	w.mu.Lock()
	w.db = db
	for selector := range w.subscriptionSecretTries {
		if _, ok := live[selector]; !ok && !strings.HasPrefix(selector, "legacy/") {
			delete(w.subscriptionSecretTries, selector)
		}
	}
	w.subscriptions = live
	w.repos = legacy
	w.legacySources = legacySources
	w.referenceSources = make(map[string][]webhook.Source)
	w.mu.Unlock()
	for _, sub := range subscriptions {
		source, ok := live[sub.ID]
		if !ok {
			continue
		}
		if len(source.Secret) == 0 {
			w.mu.Lock()
			if w.subscriptionSecretTries == nil {
				w.subscriptionSecretTries = make(map[string]subscriptionSecretAttempt)
			}
			previous := w.subscriptionSecretTries[sub.ID]
			if previous.revision == sub.Revision && now.Sub(previous.at) < webhookSecretRetry {
				w.mu.Unlock()
				continue
			}
			w.subscriptionSecretTries[sub.ID] = subscriptionSecretAttempt{revision: sub.Revision, at: now}
			w.mu.Unlock()
			token, err := w.resolver.Resolve(ctx, secrets.KeychainRef(sub.SecretService, sub.SecretAccount))
			if err != nil || token.Reveal() == "" {
				w.log.WarnContext(ctx, "webhook signing secret unavailable; polling continues", slog.String("repo", sub.Container), slog.String("subscription", sub.ID), slog.String("remedy", "litefactory repo webhook "+sub.Container))
				continue
			}
			// A disable/edit during keychain I/O must discard the stale load.
			latest, err := db.GetTicketSubscription(ctx, sub.ID)
			if err != nil || latest.Revision != sub.Revision || !latest.DesiredEnabled {
				continue
			}
			current, err := db.CurrentConnection(ctx, sub.RepoID, store.TicketConnection)
			if err != nil || !current.Enabled || current.ID != sub.ConnectionVersionID {
				continue
			}
			source.Secret = []byte(token.Reveal())
		}
		w.mu.Lock()
		if cached, ok := w.subscriptions[sub.ID]; ok && cached.Revision == source.Revision {
			w.subscriptions[sub.ID] = source
		}
		w.mu.Unlock()
		callback, err := url.Parse(sub.CallbackURL)
		if err != nil || callback.Query().Get("subscription") != "" {
			continue
		}
		if sub.Provider != ticket.GitHub {
			continue
		}
		key := webhookKey(sub.Container, "")
		key = key[:len(key)-1]
		if _, exists := legacy[key]; exists {
			ambiguous[key] = true
		}
		legacy[key] = &webhookRepo{id: source.RepoID, secret: slices.Clone(source.Secret)}
		legacySources[key] = source
	}
	for key := range ambiguous {
		delete(legacy, key)
		delete(legacySources, key)
	}
	references := make(map[string][]webhook.Source)
	repos, listErr := db.ListRepos(ctx)
	if listErr == nil {
		for _, repo := range repos {
			current, e := db.CurrentConnection(ctx, repo.ID, store.TicketConnection)
			if e != nil || !current.Enabled {
				continue
			}
			if state, e := db.GetProjectImportState(ctx, repo.ID); e == nil && state.Status == "conflict" {
				continue
			}
			source := webhook.Source{RepoID: repo.ID, ConnectionVersionID: current.ID, Provider: string(current.Provider), Origin: string(current.Origin), Container: current.Destination, ContainerID: current.RemoteID}
			matching := 0
			blocked := false
			for _, sub := range subscriptions {
				if sub.ConnectionVersionID != current.ID {
					continue
				}
				matching++
				if !sub.DesiredEnabled {
					blocked = true
					continue
				}
				source.Selector = sub.ID
				source.Revision = sub.Revision
				source.ContainerID = sub.ContainerID
			}
			if blocked || matching > 1 {
				continue
			}
			key := referenceSourceKey(source.Provider, source.Origin, source.Container)
			references[key] = append(references[key], source)
		}
	}
	w.mu.Lock()
	w.repos = legacy
	w.legacySources = legacySources
	w.referenceSources = references
	w.mu.Unlock()
}

func (d *Daemon) verifyWebhookSource(ctx context.Context, hint hintKey, q *repoQueue) error {
	version, err := d.db.GetConnectionVersion(ctx, hint.versionID)
	if err != nil {
		return err
	}
	if version.Destination != q.forgeRepo.String() {
		return store.ErrTicketIdentityConflict
	}
	clients, err := d.connections.Clients(ctx, version, 0)
	if err != nil {
		return err
	}
	identity, err := doctorCall(ctx, d, func(ctx context.Context) (ticket.Identity, error) {
		return clients.Tickets.ContainerIdentity(ctx, version.Destination)
	})
	if err != nil {
		return err
	}
	if identity.Provider != version.Provider || identity.Origin != version.Origin || (version.RemoteID != "" && identity.RemoteID != version.RemoteID) {
		return store.ErrTicketIdentityConflict
	}
	if hint.selector != "" {
		sub, e := d.db.GetTicketSubscription(ctx, hint.selector)
		if e != nil {
			return e
		}
		if identity.RemoteID != sub.ContainerID || sub.ConnectionVersionID != version.ID {
			return store.ErrTicketIdentityConflict
		}
	}
	enrollment := version.CreatedAt
	if version.Provenance == "legacy" {
		enrollment = q.repo.CreatedAt
	}
	position, err := d.db.InitTicketRecoveryPosition(ctx, version, enrollment)
	if err != nil {
		return err
	}
	if _, err = d.db.PinTicketRecoveryContainer(ctx, version.ID, position.Revision, identity.RemoteID); err != nil {
		return err
	}
	authID, authRevision := hint.selector, hint.revision
	if hint.authSelector != "" {
		authID, authRevision = hint.authSelector, hint.authRevision
		auth, e := d.db.GetTicketSubscription(ctx, authID)
		if e != nil {
			return e
		}
		authVersion, e := d.db.GetConnectionVersion(ctx, hint.authVersionID)
		if e != nil {
			return e
		}
		if auth.Provider != version.Provider || auth.Origin != version.Origin || auth.ConnectionVersionID != authVersion.ID {
			return store.ErrTicketIdentityConflict
		}
		authClients, e := d.connections.Clients(ctx, authVersion, 0)
		if e != nil {
			return e
		}
		actual, e := doctorCall(projectContext(ctx, auth.RepoID), d, func(ctx context.Context) (ticket.Identity, error) {
			return authClients.Tickets.ContainerIdentity(ctx, auth.Container)
		})
		if e != nil {
			return e
		}
		if actual.RemoteID != auth.ContainerID || actual.Provider != auth.Provider || actual.Origin != auth.Origin {
			return store.ErrTicketIdentityConflict
		}
	}
	return d.db.RecordSubscriptionDelivery(ctx, authID, authRevision, d.webhook.now())
}

func (d *Daemon) handoffWebhookIssue(ctx context.Context, hint hintKey, q *repoQueue, issue github.Issue) error {
	version, err := d.db.GetConnectionVersion(ctx, hint.versionID)
	if err != nil {
		return err
	}
	position, err := d.db.GetTicketRecoveryPosition(ctx, version.ID)
	if err != nil {
		return err
	}
	observation := store.PendingTicketObservation{ConnectionVersionID: version.ID, RepoID: q.repo.ID, Provider: version.Provider, Origin: version.Origin, Container: version.Destination, ContainerID: position.ContainerID, TicketID: strconv.FormatInt(issue.ID, 10), TicketKey: strconv.Itoa(issue.Number), ObservedAt: d.webhook.now()}
	if hint.authSelector != "" {
		return d.db.HandoffRelatedTicketObservation(ctx, hint.authSelector, hint.authRevision, hint.selector, hint.revision, observation)
	}
	return d.db.HandoffSubscriptionObservation(ctx, hint.selector, hint.revision, observation)
}

// hasProjectSecret reports qualified listener readiness without admitting an
// ambiguous owner/name callback through the legacy lookup.
func (w *webhookRuntime) hasProjectSecret(repoID int64, key string) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	for _, source := range w.subscriptions {
		if source.RepoID == repoID && len(source.Secret) > 0 {
			return true
		}
	}
	entry := w.repos[key]
	return entry != nil && len(entry.secret) > 0
}
