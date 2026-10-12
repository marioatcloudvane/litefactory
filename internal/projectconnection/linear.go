package projectconnection

import (
	"context"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/linear"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func (f *Factory) loadLinear(ctx context.Context, v store.ConnectionVersion, expectedActorID int64, refresh bool) (*Clients, error) {
	if v.Role != store.TicketConnection || f.options.DB == nil {
		return nil, ErrInvalidConnection
	}
	ref := credential(v)
	if f.options.Launchd && ref.Source != secrets.SourceKeychain {
		return nil, ErrCredentialPolicy
	}
	ctx, cancel := context.WithTimeout(ctx, f.options.Timeout)
	defer cancel()
	key := cacheKey{v.ID, expectedActorID}
	for {
		f.mu.Lock()
		if !refresh {
			if cached := f.clients[key]; cached != nil {
				f.mu.Unlock()
				return cached, nil
			}
		}
		loading := f.loading[key]
		if loading == nil {
			f.loading[key] = make(chan struct{})
			f.mu.Unlock()
			break
		}
		f.mu.Unlock()
		select {
		case <-loading:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	defer func() { f.mu.Lock(); close(f.loading[key]); delete(f.loading, key); f.mu.Unlock() }()
	token, err := f.options.Resolver.Resolve(ctx, ref)
	if err != nil {
		return nil, safeFailure(err)
	}
	f.options.Redactor.AddToken(token)
	labels, err := v.Linear.ModeLabels()
	if err != nil {
		return nil, ErrInvalidConnection
	}
	states, err := v.Linear.LifecycleStates()
	if err != nil {
		return nil, ErrInvalidConnection
	}
	adapter, err := linear.New(linear.Config{RequestContext: f.bindRequest(v), Origin: string(v.Origin), AuthKind: v.Linear.AuthKind, WorkspaceID: v.Linear.WorkspaceID, TeamID: v.Linear.TeamID, ProjectID: v.Linear.ProjectID, ModeLabels: labels, States: states}, token, f.options.HTTPClient)
	if err != nil {
		return nil, ErrInvalidConnection
	}
	publisher, err := adapter.Publisher(ctx)
	if err != nil {
		return nil, safeFailure(err)
	}
	slot, err := f.options.DB.LocalTicketSlot(ctx, "actor", publisher.Identity)
	if err != nil {
		return nil, err
	}
	if expectedActorID > 0 && slot != expectedActorID {
		return nil, ErrActorChanged
	}
	clients := &Clients{Tickets: linearTickets{adapter}, Linear: adapter, Publisher: publisher, Actor: legacyLinearActor(publisher, slot), Redactor: f.options.Redactor}
	f.mu.Lock()
	f.clients[key] = clients
	f.mu.Unlock()
	return clients, nil
}

func (f *Factory) checkLinear(ctx context.Context, v store.ConnectionVersion) (o store.ConnectionObservation) {
	ctx, cancel := context.WithTimeout(ctx, f.options.Timeout)
	defer cancel()
	o = store.ConnectionObservation{VersionID: v.ID, CheckedAt: time.Now().UTC()}
	defer func() {
		labels, err := v.Linear.ModeLabels()
		if err == nil {
			if missing := linear.MissingModeLabels(labels); len(missing) > 0 {
				o.Remedy += " Missing mode mappings: " + strings.Join(missing, ", ") + ". Add label IDs to the connection's --mode-labels, or use a mapped mode."
			}
		}
	}()
	clients, err := f.loadLinear(ctx, v, 0, true)
	if err != nil {
		o.Status, o.Reason, o.Remedy = healthFailure(err)
		return o
	}
	inspection, err := clients.Linear.Inspect(ctx)
	if err != nil {
		o.Status = "inaccessible_or_missing_destination"
		o.Reason = "linear_configuration_unverified"
		o.Remedy = "Verify workspace, team, project and explicit label/state mappings."
		return o
	}
	o.Status = "usable_for_checked_capabilities"
	o.Reason = "read_access_verified"
	o.Capabilities = []string{"authenticated_actor", "ticket_read", "write_unknown"}
	if inspection.WebhookAdministration {
		o.Capabilities = append(o.Capabilities, "webhook_administration")
	} else {
		o.Capabilities = append(o.Capabilities, "webhook_administration_unavailable")
	}
	o.Remedy = "Writes enforce provider permissions. Webhook administration is separate; polling remains available."
	return o
}

func verifiedLinearAppointment(v store.ConnectionVersion, actor, publisher ticket.Actor) (store.ApproverAppointment, error) {
	if actor.Identity.Validate() != nil || actor.IsBot || actor.Handle == "" || actor.Identity.Provider != ticket.Linear || actor.Identity.Origin != v.Origin {
		return store.ApproverAppointment{}, ErrApproverNotHuman
	}
	if actor.Identity.Equal(publisher.Identity) {
		return store.ApproverAppointment{}, ErrApproverPublisher
	}
	return store.ApproverAppointment{RepoID: v.RepoID, Actor: actor.Identity, Handle: actor.Handle, VerifiedAt: time.Now().UTC(), Provenance: "explicit"}, nil
}
