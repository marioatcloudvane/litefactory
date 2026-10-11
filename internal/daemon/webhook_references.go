package daemon

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/webhook"
)

func referenceSourceKey(provider, origin, container string) string {
	return provider + "\x00" + origin + "\x00" + strings.ToLower(container)
}

// WakeReference keeps the authenticating subscription separate from the target
// source. Cached enabled configuration selects credentials and project ownership;
// payload container names alone never appoint a project or relabel a number.
func (w *webhookRuntime) WakeReference(authenticatedSource webhook.Source, container string, number int) {
	if number <= 0 || authenticatedSource.Selector == "" {
		return
	}
	key := referenceSourceKey(authenticatedSource.Provider, authenticatedSource.Origin, container)
	w.mu.RLock()
	sources := w.referenceSources[key]
	if len(sources) != 1 {
		w.mu.RUnlock()
		return
	}
	target := sources[0]
	w.mu.RUnlock()
	if target.Provider != authenticatedSource.Provider || target.Origin != authenticatedSource.Origin || !strings.EqualFold(target.Container, container) {
		return
	}
	w.hints.add(hintKey{repoID: target.RepoID, number: number, versionID: target.ConnectionVersionID, revision: target.Revision, selector: target.Selector, authSelector: authenticatedSource.Selector, authVersionID: authenticatedSource.ConnectionVersionID, authRevision: authenticatedSource.Revision})
	if w.schedule != nil {
		w.schedule.WebhookAccepted(target.RepoID, w.now())
		w.schedule.Announce(target.RepoID, number)
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// LookupLegacySource supplies the same qualified provenance as an opaque route
// only when the retained legacy callback and cached secret are unambiguous.
func (w *webhookRuntime) LookupLegacySource(fullName string) (webhook.Source, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	source, ok := w.legacySources[strings.ToLower(fullName)]
	source.Secret = slices.Clone(source.Secret)
	return source, ok && len(source.Secret) > 0
}

func (d *Daemon) webhookHintCurrent(ctx context.Context, hint hintKey) (bool, error) {
	target, err := d.db.CurrentConnection(ctx, hint.repoID, store.TicketConnection)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !target.Enabled || target.ID != hint.versionID {
		return false, nil
	}
	if hint.selector != "" {
		sub, err := d.db.GetTicketSubscription(ctx, hint.selector)
		if errors.Is(err, store.ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !sub.DesiredEnabled || sub.Revision != hint.revision || sub.ConnectionVersionID != target.ID {
			return false, nil
		}
	} else {
		if hint.authSelector == "" {
			return false, nil
		}
		subscriptions, err := d.db.ListTicketSubscriptions(ctx, hint.repoID)
		if err != nil {
			return false, err
		}
		for _, sub := range subscriptions {
			if sub.ConnectionVersionID == target.ID && !sub.DesiredEnabled {
				return false, nil
			}
		}
	}
	if hint.authSelector != "" {
		auth, err := d.db.GetTicketSubscription(ctx, hint.authSelector)
		if errors.Is(err, store.ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !auth.DesiredEnabled || auth.Revision != hint.authRevision || auth.ConnectionVersionID != hint.authVersionID || auth.Provider != target.Provider || auth.Origin != target.Origin {
			return false, nil
		}
		current, err := d.db.CurrentConnection(ctx, auth.RepoID, store.TicketConnection)
		if errors.Is(err, store.ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !current.Enabled || current.ID != hint.authVersionID {
			return false, nil
		}
	}
	return true, nil
}
