package localprojects

import (
	"context"
	"errors"
	"strings"

	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/webhookreg"
)

// ListSubscriptions presents owned subscription status and recovery evidence.
func (a *Administration) ListSubscriptions(ctx context.Context, request contracts.ProjectSubscriptionRequest, present contracts.ProjectSubscriptionPresentation) error {
	return a.runSubscriptions(ctx, "list", request, present)
}

// CreateSubscription records and creates an explicitly requested owned subscription.
func (a *Administration) CreateSubscription(ctx context.Context, request contracts.ProjectSubscriptionRequest, present contracts.ProjectSubscriptionPresentation) error {
	return a.runSubscriptions(ctx, "create", request, present)
}

// RepairSubscription repairs the exact retained subscription revision.
func (a *Administration) RepairSubscription(ctx context.Context, request contracts.ProjectSubscriptionRequest, present contracts.ProjectSubscriptionPresentation) error {
	return a.runSubscriptions(ctx, "repair", request, present)
}

// UpdateSubscription saves desired state before reconciling provider delivery.
func (a *Administration) UpdateSubscription(ctx context.Context, request contracts.ProjectSubscriptionRequest, present contracts.ProjectSubscriptionPresentation) error {
	return a.runSubscriptions(ctx, "update", request, present)
}

// DisableSubscription saves disable intent before reconciling provider delivery.
func (a *Administration) DisableSubscription(ctx context.Context, request contracts.ProjectSubscriptionRequest, present contracts.ProjectSubscriptionPresentation) error {
	return a.runSubscriptions(ctx, "disable", request, present)
}

func (a *Administration) runSubscriptions(ctx context.Context, action string, request contracts.ProjectSubscriptionRequest, present contracts.ProjectSubscriptionPresentation) (err error) {
	id, revision, callback, eventNames, enabled := request.ProjectID, request.Revision, request.Callback, request.Events, request.Enabled
	_, err = a.db.GetRepo(ctx, id)
	if err != nil {
		return err
	}
	subscriptions, err := a.db.ListTicketSubscriptions(ctx, id)
	if err != nil {
		return err
	}
	if action == "list" || action == "status" {
		for _, s := range subscriptions {
			if request.HasID && request.ID != s.ID {
				continue
			}
			if err := a.presentSubscription(ctx, s, present); err != nil {
				return err
			}
		}
		if len(subscriptions) == 0 {
			err = present.Empty(id)
		}
		return err
	}
	if action == "create" && revision != 0 {
		return usage("create requires --expected-revision=0")
	}
	if action == "create" && (eventNames != "" || !enabled) {
		return usage("create uses the default event selection and enabled delivery; use update to change them")
	}
	if action != "create" && revision < 1 {
		return usage("--expected-revision is required")
	}
	var selected store.TicketSubscription
	if action != "create" {
		if !request.HasID {
			return usage("subscription-id is required")
		}
		selected, err = a.db.GetTicketSubscription(ctx, request.ID)
		if err != nil {
			return err
		}
		if selected.RepoID != id {
			return errors.New("subscription belongs to another project")
		}
	}
	version, err := a.db.CurrentConnection(ctx, id, store.TicketConnection)
	if action != "create" {
		version, err = a.db.GetConnectionVersion(ctx, selected.ConnectionVersionID)
	}
	if err != nil {
		return err
	}
	if action == "create" && !version.Enabled {
		return store.ErrConnectionDisabled
	}
	if action == "disable" || action == "update" {
		desired := enabled
		if action == "disable" {
			desired = false
		}
		var events []string
		if eventNames != "" {
			events = strings.Split(eventNames, ",")
		}
		if action == "disable" {
			callback = ""
			events = nil
		}
		saved, saveErr := (webhookreg.Subscriptions{Store: a.db}).Update(ctx, selected.ID, revision, desired, callback, events)
		if saved.Revision != revision+1 {
			return saveErr
		}
		selected = saved
		revision = saved.Revision
	}
	registrar, err := a.projectSubscriptions(ctx, version)
	if err != nil {
		if selected.ID != "" {
			return errors.Join(err, a.presentSubscription(ctx, selected, present))
		}
		return err
	}
	if selected.ID != "" {
		actual, verifyErr := verifySubscriptionContainer(ctx, registrar, version)
		if verifyErr != nil || actual != selected.ContainerID {
			return errors.Join(errors.New("owned subscription repository could not be verified; desired state remains saved"), a.presentSubscription(ctx, selected, present))
		}
	}
	switch action {
	case "create":
		if callback != "" {
			registrar.PublicURL = callback
		}
		for _, s := range subscriptions {
			if s.ConnectionVersionID == version.ID {
				return errors.New("this connection already has an owned subscription; inspect or repair it")
			}
		}
		containerID, e := verifySubscriptionContainer(ctx, registrar, version)
		if e != nil {
			return e
		}
		selected, err = registrar.Create(ctx, version, containerID)
	case "repair":
		selected, err = registrar.Repair(ctx, selected.ID, revision)
	case "disable", "update":
		selected, err = registrar.Repair(ctx, selected.ID, revision)
	default:
		return usage("unknown subscription operation")
	}
	if selected.ID != "" {
		err = errors.Join(err, a.presentSubscription(ctx, selected, present))
	}
	return err
}

func (a *Administration) projectSubscriptions(ctx context.Context, v store.ConnectionVersion) (webhookreg.Subscriptions, error) {
	factory := projectconnection.NewFactory(projectconnection.Options{DB: a.db, Logger: a.options.Logger, Resolver: secrets.Resolver{Keychain: a.options.Keychain}, UserAgent: a.options.UserAgent})
	clients, err := factory.Clients(ctx, v, 0)
	if err != nil {
		return webhookreg.Subscriptions{}, err
	}
	return webhookreg.Subscriptions{Store: a.db, Forge: clients.Tickets, Keychain: webhookreg.SystemKeychain{Resolver: secrets.Resolver{Keychain: a.options.Keychain}, Writer: secrets.Writer{Keychain: a.options.KeychainWrite}}, PublicURL: a.cfg.Webhook.PublicURL}, nil
}

func verifySubscriptionContainer(ctx context.Context, r webhookreg.Subscriptions, v store.ConnectionVersion) (string, error) {
	return VerifySubscriptionContainer(ctx, r, contracts.ProjectContainerClaim{Destination: v.Destination, RemoteID: v.RemoteID})
}

// VerifySubscriptionContainer verifies an immutable container without trusting callback names.
func VerifySubscriptionContainer(ctx context.Context, r webhookreg.Subscriptions, claim contracts.ProjectContainerClaim) (string, error) {
	adapter, ok := r.Forge.(projectconnection.TicketClient)
	if !ok {
		return "", errors.New("ticket subscription adapter is unavailable")
	}
	// Resolve a container identity without trusting callback payload names.
	identity, err := adapter.ContainerIdentity(ctx, claim.Destination)
	if err != nil {
		return "", err
	}
	if claim.RemoteID != "" && claim.RemoteID != identity.RemoteID {
		return "", errors.New("ticket connection repository identity changed")
	}
	return identity.RemoteID, nil
}

func (a *Administration) presentSubscription(ctx context.Context, subscription store.TicketSubscription, present contracts.ProjectSubscriptionPresentation) error {
	view := contracts.ProjectSubscriptionView{ID: subscription.ID, RepoID: subscription.RepoID, Revision: subscription.Revision, ConnectionVersionID: subscription.ConnectionVersionID, Provider: subscription.Provider, Origin: subscription.Origin, Container: subscription.Container, DesiredEnabled: subscription.DesiredEnabled, ObservedState: subscription.ObservedState, RemoteHookID: subscription.RemoteHookID, Failure: subscription.Failure, CallbackURL: subscription.CallbackURL, SecretService: subscription.SecretService, SecretAccount: subscription.SecretAccount, CheckedAt: subscription.CheckedAt, DeliveredAt: subscription.DeliveredAt}
	if err := present.Subscription(view); err != nil {
		return err
	}
	position, err := a.db.GetTicketRecoveryPosition(ctx, subscription.ConnectionVersionID)
	if errors.Is(err, store.ErrNotFound) {
		return present.Recovery(contracts.ProjectRecoveryView{})
	}
	if err != nil {
		return err
	}
	return present.Recovery(contracts.ProjectRecoveryView{Known: true, CompletedThrough: position.CompletedThrough, EnrollmentAt: position.EnrollmentAt, ReconcileCompletedAt: position.ReconcileCompletedAt})
}
